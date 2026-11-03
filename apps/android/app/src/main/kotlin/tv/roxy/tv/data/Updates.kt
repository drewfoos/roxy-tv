package tv.roxy.tv.data

import android.content.Context
import android.content.Intent
import android.content.pm.PackageInfo
import android.content.pm.PackageManager
import android.content.pm.Signature
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
import java.security.MessageDigest

/** A newer build published on the server (see server/internal/api/apps.go). */
@Serializable
data class AppUpdate(
    val versionCode: Int,
    val versionName: String,
    val notes: String = "",
    val publishedAt: String = "",
    val size: Long = 0,
    val url: String = "",
    /** Hex SHA-256 of the APK (newer servers). When present the download must match it. */
    val sha256: String? = null,
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
 * `apps/android/tools/publish-apk.sh` puts a build there after `gradlew assemblePerf`.
 *
 * The server is not trusted with what gets installed: the download URL must be one of the
 * server's own /v1/apps/ paths, a published SHA-256 must match, and before the installer
 * opens, the APK must be this app (same package), signed with the same certificate as the
 * installed one, and newer. Anything else is deleted and refused.
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
            is UpdateState.Ready -> {
                // Checked again: the file sat in the cache since the last look.
                scope.launch {
                    val why = withContext(Dispatchers.IO) { verifyApk(s.file) }
                    if (why == null) launchInstaller(s.file) else { s.file.delete(); _state.value = UpdateState.Failed(s.update, why) }
                }
                return
            }
            else -> return
        }
        _state.value = UpdateState.Downloading(u, 0f)
        scope.launch {
            runCatching { download(u) }
                .onSuccess { f -> _state.value = UpdateState.Ready(u, f); launchInstaller(f) }
                .onFailure { t -> Timber.w(t, "update download failed"); _state.value = UpdateState.Failed(u, t.message ?: "download failed") }
        }
    }

    /** Refused updates say why in the bar; the message is the exception's. */
    private class UpdateRejected(message: String) : IOException(message)

    private suspend fun download(u: AppUpdate): File = withContext(Dispatchers.IO) {
        val path = safeAppPath(u.url) ?: throw UpdateRejected("The server offered an update from an unexpected address; not downloading it.")
        val expectSha = u.sha256?.trim()?.lowercase()?.takeIf { it.isNotEmpty() }
        if (expectSha != null && !expectSha.matches(Regex("[0-9a-f]{64}"))) throw UpdateRejected("The update's checksum is malformed; not downloading it.")
        val dir = File(context.cacheDir, "updates").apply { mkdirs() }
        dir.listFiles()?.forEach { it.delete() }
        // A fixed name: the server's versionName is not a safe file name.
        val target = File(dir, "roxy-tv-update.apk")
        val digest = MessageDigest.getInstance("SHA-256")
        val req = Request.Builder().url(api.baseUrl + path).get().build()
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
                        digest.update(buf, 0, n)
                        done += n
                        val now = System.currentTimeMillis()
                        if (total > 0 && now - last > 200) { last = now; _state.value = UpdateState.Downloading(u, (done.toFloat() / total).coerceIn(0f, 1f)) }
                    }
                }
            }
        }
        if (expectSha != null) {
            val got = digest.digest().joinToString("") { "%02x".format(it) }
            if (got != expectSha) {
                target.delete()
                Timber.w("update checksum mismatch: expected %s got %s", expectSha, got)
                throw UpdateRejected("The download doesn't match the server's checksum; not installing.")
            }
        }
        verifyApk(target)?.let { why -> target.delete(); throw UpdateRejected(why) }
        target
    }

    /**
     * Null when [file] is a newer build of this very app signed with the installed app's
     * certificate(s); otherwise why not (shown to the person).
     */
    private fun verifyApk(file: File): String? {
        val notSigned = "This update isn't signed by Roxy; not installing."
        if (!file.isFile) return "The downloaded update is missing; try again."
        val pm = context.packageManager
        @Suppress("DEPRECATION")
        val flags = PackageManager.GET_SIGNING_CERTIFICATES or PackageManager.GET_SIGNATURES
        val archive: PackageInfo = runCatching { pm.getPackageArchiveInfo(file.path, flags) }.getOrNull()
            ?: return "The download isn't a valid app package; not installing."
        if (archive.packageName != BuildConfig.APPLICATION_ID) {
            Timber.w("update is %s, not %s", archive.packageName, BuildConfig.APPLICATION_ID)
            return notSigned
        }
        val installed: PackageInfo = runCatching { pm.getPackageInfo(context.packageName, flags) }.getOrNull()
            ?: return notSigned
        val mine = certs(installed) ?: return notSigned
        val theirs = certs(archive) ?: return notSigned
        if (mine.isEmpty() || mine != theirs) {
            Timber.w("update signer mismatch")
            return notSigned
        }
        if (archive.longVersionCode <= installed.longVersionCode) {
            return "The server's update (${archive.longVersionCode}) isn't newer than this app (${installed.longVersionCode}); not installing."
        }
        return null
    }

    /**
     * The signing certificates as a set of SHA-256 hex digests: the APK's signers (v2/v3),
     * or the legacy signature list where signingInfo is not filled in (some Android 9 builds
     * leave it null for an archive). With no key rotation the history is just the one cert; after a rotation both sides must carry the same lineage.
     */
    private fun certs(info: PackageInfo): Set<String>? {
        val si = info.signingInfo
        @Suppress("DEPRECATION")
        val sigs: Array<Signature> = when {
            si != null && si.hasMultipleSigners() -> si.apkContentsSigners
            si != null -> si.signingCertificateHistory
            else -> info.signatures
        } ?: return null
        return sigs.map { s -> MessageDigest.getInstance("SHA-256").digest(s.toByteArray()).joinToString("") { "%02x".format(it) } }.toSet()
    }

    /**
     * [url] when it is a relative path into the server's /v1/apps/ (no scheme, host, "//",
     * "@", "..", backslash or control characters); null otherwise. The server cannot send the
     * app off to download from somewhere else.
     */
    private fun safeAppPath(url: String): String? {
        val u = url.trim()
        if (!u.startsWith("/v1/apps/")) return null
        if (u.contains("//") || u.contains('@') || u.contains('\\') || u.contains("..") || u.contains("://")) return null
        if (u.any { it.isWhitespace() || it.code < 0x20 || it.code == 0x7f }) return null
        if (u.substringBefore('?').contains('%')) return null
        return u
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
