package tv.roxy.tv.data

import android.content.Context
import android.content.Intent
import androidx.core.content.FileProvider
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.serialization.Serializable
import okhttp3.Request
import timber.log.Timber
import tv.roxy.tv.BuildConfig
import tv.roxy.tv.api.ApiException
import tv.roxy.tv.api.RoxyApi
import java.io.File
import java.io.IOException

/** A newer build published on the server (see server/internal/api/apps.go). */
@Serializable
data class AppUpdate(
    val versionCode: Int,
    val versionName: String,
    val notes: String = "",
    val publishedAt: String = "",
    val size: Long = 0,
    val url: String = "",
)

/** Where an update stands: offered, downloading, ready to install, or failed. */
sealed interface UpdateState {
    data object None : UpdateState
    data class Available(val update: AppUpdate) : UpdateState
    data class Downloading(val update: AppUpdate, val fraction: Float) : UpdateState
    data class Ready(val update: AppUpdate, val file: File) : UpdateState
    data class Failed(val update: AppUpdate, val message: String) : UpdateState
}

/**
 * Checks the server for a newer build of this app (same applicationId, higher
 * versionCode) once per launch, downloads it on request and hands it to Android's
 * installer. Self-hosted apps have no store, so the server is the update channel:
 * `apps/android/tools/publish-apk.ps1` puts a build there after `gradlew assembleDebug`.
 */
class Updates(private val context: Context, private val api: RoxyApi, private val prefs: Prefs, private val scope: CoroutineScope) {

    private val _state = MutableStateFlow<UpdateState>(UpdateState.None)
    val state: StateFlow<UpdateState> get() = _state

    private var checkedAt = 0L

    /** What the last check from Settings found, for its row: "Checking…", "Up to date · 0.3.1", or why it failed. */
    private val _checkNote = MutableStateFlow<String?>(null)
    val checkNote: StateFlow<String?> get() = _checkNote

    /**
     * A check the person asked for in Settings: always asks the server, says so when the
     * app is current, and offers a build they dismissed earlier again.
     */
    fun checkNow() {
        _checkNote.value = "Checking…"
        checkedAt = System.currentTimeMillis()
        scope.launch {
            val result = runCatching { api.latestAppBuild(BuildConfig.APPLICATION_ID) }
            val err = result.exceptionOrNull()
            val u = result.getOrNull()
            when {
                err != null && !(err is ApiException && err.isNotFound) -> _checkNote.value = "Couldn't reach the server"
                u == null || u.versionCode <= BuildConfig.VERSION_CODE -> _checkNote.value = "Up to date  ·  ${BuildConfig.VERSION_NAME}"
                else -> {
                    _checkNote.value = "Roxy TV ${u.versionName} is available"
                    prefs.setDismissedUpdate(0)
                    _state.value = UpdateState.Available(u)
                }
            }
        }
    }

    /** Ask the server; no-op within [MIN_INTERVAL_MS] of the last check. Never throws. */
    fun check(force: Boolean = false) {
        val now = System.currentTimeMillis()
        if (!force && now - checkedAt < MIN_INTERVAL_MS) return
        checkedAt = now
        scope.launch {
            val u = runCatching { api.latestAppBuild(BuildConfig.APPLICATION_ID) }
                .onFailure { if (it !is ApiException || !it.isNotFound) Timber.w("update check: %s", it.message) }
                .getOrNull() ?: return@launch
            if (u.versionCode <= BuildConfig.VERSION_CODE) return@launch
            if (prefs.dismissedUpdate.value == u.versionCode) { Timber.i("update %s (%d) available but dismissed", u.versionName, u.versionCode); return@launch }
            if (_state.value is UpdateState.None || _state.value is UpdateState.Failed) {
                Timber.i("update available: %s (%d) > %s (%d)", u.versionName, u.versionCode, BuildConfig.VERSION_NAME, BuildConfig.VERSION_CODE)
                _state.value = UpdateState.Available(u)
            }
        }
    }

    /** Hide the bar for this build until a newer one appears. */
    fun dismiss() {
        val u = (_state.value as? UpdateState.Available)?.update ?: (_state.value as? UpdateState.Failed)?.update ?: return
        scope.launch { prefs.setDismissedUpdate(u.versionCode) }
        _state.value = UpdateState.None
    }

    /** Download the APK to the cache and open the installer (the system asks to allow installs from Roxy the first time). */
    fun install() {
        val u = when (val s = _state.value) {
            is UpdateState.Available -> s.update
            is UpdateState.Failed -> s.update
            is UpdateState.Ready -> { launchInstaller(s.file); return }
            else -> return
        }
        _state.value = UpdateState.Downloading(u, 0f)
        scope.launch {
            runCatching { download(u) }
                .onSuccess { f -> _state.value = UpdateState.Ready(u, f); launchInstaller(f) }
                .onFailure { t -> Timber.w(t, "update download failed"); _state.value = UpdateState.Failed(u, t.message ?: "download failed") }
        }
    }

    private suspend fun download(u: AppUpdate): File = withContext(Dispatchers.IO) {
        val dir = File(context.cacheDir, "updates").apply { mkdirs() }
        dir.listFiles()?.forEach { it.delete() }
        val target = File(dir, "roxy-tv-${u.versionName}.apk")
        val req = Request.Builder().url(api.baseUrl + u.url).get().build()
        api.http.newCall(req).execute().use { resp ->
            if (!resp.isSuccessful) throw IOException("HTTP ${resp.code}")
            val total = resp.body.contentLength().takeIf { it > 0 } ?: u.size
            target.outputStream().use { out ->
                val buf = ByteArray(256 * 1024)
                var done = 0L
                var last = 0L
                resp.body.byteStream().use { input ->
                    while (true) {
                        val n = input.read(buf)
                        if (n < 0) break
                        out.write(buf, 0, n)
                        done += n
                        val now = System.currentTimeMillis()
                        if (total > 0 && now - last > 200) { last = now; _state.value = UpdateState.Downloading(u, (done.toFloat() / total).coerceIn(0f, 1f)) }
                    }
                }
            }
        }
        target
    }

    private fun launchInstaller(file: File) {
        val uri = FileProvider.getUriForFile(context, "${BuildConfig.APPLICATION_ID}.files", file)
        val intent = Intent(Intent.ACTION_VIEW)
            .setDataAndType(uri, "application/vnd.android.package-archive")
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_GRANT_READ_URI_PERMISSION)
        runCatching { context.startActivity(intent) }.onFailure { Timber.w(it, "could not open the installer") }
    }

    private companion object { const val MIN_INTERVAL_MS = 6 * 60 * 60 * 1000L }
}
