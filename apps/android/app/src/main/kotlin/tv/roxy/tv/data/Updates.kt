package tv.roxy.tv.data

import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.content.pm.PackageInfo
import android.content.pm.PackageInstaller
import android.content.pm.PackageManager
import android.content.pm.Signature
import android.os.Build
import androidx.core.content.ContextCompat
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
import java.util.zip.ZipFile

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
    /** Compilation profiles for this build, one per range of Android versions (newer servers). */
    val profiles: List<AppProfile> = emptyList(),
)

/**
 * A compilation profile (.dm) published with a build. Installed together with the APK, it
 * lets Android compile the update ahead of time at install, so it runs smoothly from its
 * first launch instead of stuttering until the device compiles it in the background.
 */
@Serializable
data class AppProfile(
    val minApi: Int,
    val maxApi: Int,
    val size: Long = 0,
    val sha256: String = "",
    val url: String = "",
)

/** Where an update stands: offered, downloading, ready to install, or failed. */
sealed interface UpdateState {
    data object None : UpdateState
    data class Available(val update: AppUpdate) : UpdateState
    data class Downloading(val update: AppUpdate, val fraction: Float) : UpdateState
    /**
     * [profile]: the compilation profile to install with it, when one was published and fetched.
     * [installing]: handed to Android and not answered yet (being copied into the installer,
     * or waiting for the confirmation to open); Install does nothing meanwhile.
     */
    data class Ready(val update: AppUpdate, val file: File, val profile: File? = null, val installing: Boolean = false) : UpdateState
    data class Failed(val update: AppUpdate, val message: String) : UpdateState
}

/**
 * Checks the server for a newer build of this app (same applicationId, higher
 * versionCode) once per launch, downloads it on request and installs it through Android's
 * package installer (which asks the person to confirm), together with the build's
 * compilation profile for this Android version when one is published. Self-hosted apps have no store, so the server is the update channel:
 * `apps/android/tools/publish-apk.sh` puts a build there after `gradlew assemblePerf`.
 *
 * The server is not trusted with what gets installed: the download URL must be one of the
 * server's own /v1/apps/ paths, a published SHA-256 must match, and before the installer
 * opens, the APK must be this app (same package), signed with the same certificate as the
 * installed one, and newer. Anything else is deleted and refused. A profile must match its
 * published checksum and hold nothing but a profile (no compiled code); a profile that is
 * missing or refused only means the update installs without it, as before.
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
                // A second press while the first is still being handed over would start a
                // second install (and a second confirmation).
                if (s.installing) return
                val busy = s.copy(installing = true)
                _state.value = busy
                // Checked again: the file sat in the cache since the last look.
                scope.launch {
                    val why = withContext(Dispatchers.IO) { verifyApk(s.file) }
                    if (why == null) installPackage(busy) else { s.file.delete(); _state.value = UpdateState.Failed(s.update, why) }
                }
                return
            }
            else -> return
        }
        _state.value = UpdateState.Downloading(u, 0f)
        scope.launch {
            runCatching { download(u) }
                .onSuccess { f ->
                    val profile = runCatching { downloadProfile(u) }.onFailure { Timber.w(it, "update profile skipped") }.getOrNull()
                    val ready = UpdateState.Ready(u, f, profile, installing = true)
                    _state.value = ready
                    installPackage(ready)
                }
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
     * The compilation profile for this Android version, downloaded and checked: its checksum
     * must be published and match, and it must hold only a profile. Null when the build has
     * none for this version. Throws when one is offered but refused (the caller installs
     * without it).
     */
    private suspend fun downloadProfile(u: AppUpdate): File? = withContext(Dispatchers.IO) {
        val p = u.profiles.firstOrNull { Build.VERSION.SDK_INT in it.minApi..it.maxApi } ?: return@withContext null
        val path = safeAppPath(p.url) ?: throw UpdateRejected("profile from an unexpected address")
        val expectSha = p.sha256.trim().lowercase()
        if (!expectSha.matches(Regex("[0-9a-f]{64}"))) throw UpdateRejected("profile without a valid checksum")
        val target = File(File(context.cacheDir, "updates"), "roxy-tv-update.dm")
        val digest = MessageDigest.getInstance("SHA-256")
        val req = Request.Builder().url(api.baseUrl + path).get().build()
        api.http.newCall(req).execute().use { resp ->
            if (!resp.isSuccessful) throw IOException("profile: HTTP ${resp.code}")
            target.outputStream().use { out ->
                val buf = ByteArray(64 * 1024)
                var done = 0L
                resp.body.byteStream().use { input ->
                    while (true) {
                        val n = input.read(buf)
                        if (n < 0) break
                        done += n
                        if (done > MAX_PROFILE_BYTES) throw UpdateRejected("profile too large")
                        out.write(buf, 0, n)
                        digest.update(buf, 0, n)
                    }
                }
            }
        }
        val got = digest.digest().joinToString("") { "%02x".format(it) }
        if (got != expectSha) { target.delete(); throw UpdateRejected("profile checksum mismatch") }
        if (!isProfileOnly(target)) { target.delete(); throw UpdateRejected("profile holds more than a profile") }
        target
    }

    /**
     * Whether [file] is a .dm holding only a profile. A .dm may also carry verification data
     * for the code (a vdex), which a hostile server could use to weaken the device's checks
     * of the app; the build's own profiles never have one, so anything but the profile
     * ("primary.prof") and its metadata ("primary.profm") is refused.
     */
    private fun isProfileOnly(file: File): Boolean = runCatching {
        ZipFile(file).use { zip ->
            val names = zip.entries().toList().map { it.name }
            "primary.prof" in names && names.all { it == "primary.prof" || it == "primary.profm" } && names.size == names.toSet().size
        }
    }.getOrDefault(false)

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

    // ---- installing ----

    /**
     * The session last handed to Android and what it installs. Android's answers carry their
     * session's id: one about any other session (an earlier attempt, since abandoned) is
     * ignored, so it can neither open a confirmation nor be taken for this one's result.
     * Set on the IO thread before the commit and read on the main thread by the receiver.
     */
    private class Attempt(val sessionId: Int, val ready: UpdateState.Ready)
    @Volatile private var current: Attempt? = null
    private var receiverRegistered = false

    /** [ready] as the screen should show it: Install offered again (installing = false). */
    private fun settle(ready: UpdateState.Ready) {
        if (_state.value == ready) _state.value = ready.copy(installing = false)
    }

    /** Android's answers about a session: confirm with the person, done, or failed. */
    private val statusReceiver = object : BroadcastReceiver() {
        override fun onReceive(ctx: Context, intent: Intent) {
            val status = intent.getIntExtra(PackageInstaller.EXTRA_STATUS, PackageInstaller.STATUS_FAILURE)
            val message = intent.getStringExtra(PackageInstaller.EXTRA_STATUS_MESSAGE)
            val sessionId = intent.getIntExtra(PackageInstaller.EXTRA_SESSION_ID, -1)
            val attempt = current
            if (attempt == null || attempt.sessionId != sessionId) {
                Timber.i("update: status %d for session %d ignored (current: %s)", status, sessionId, attempt?.sessionId)
                return
            }
            val ready = attempt.ready
            when (status) {
                PackageInstaller.STATUS_PENDING_USER_ACTION -> {
                    // Install is offered again from here: someone who leaves the confirmation
                    // with Home instead of Cancel can start over (the old session is abandoned).
                    settle(ready)
                    @Suppress("DEPRECATION")
                    val confirm = intent.getParcelableExtra<Intent>(Intent.EXTRA_INTENT) ?: return
                    runCatching { context.startActivity(confirm.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)) }
                        .onFailure { Timber.w(it, "could not open the install confirmation") }
                }
                PackageInstaller.STATUS_SUCCESS -> Timber.i("update installed") // this process is replaced right after
                PackageInstaller.STATUS_FAILURE_ABORTED -> { Timber.i("update install cancelled"); current = null; settle(ready) } // Install offers it again
                else -> {
                    Timber.w("update install failed: %d %s", status, message)
                    current = null
                    if (ready.profile != null) {
                        // Refused with the profile (an Android that does not take one): the
                        // update matters more than the profile, so install it alone.
                        val alone = ready.copy(profile = null, installing = true)
                        _state.value = alone
                        scope.launch { installPackage(alone) }
                    } else {
                        _state.value = UpdateState.Failed(ready.update, "Android couldn't install it" + (message?.let { " ($it)" } ?: ""))
                    }
                }
            }
        }
    }

    /**
     * Installs [ready] with a package installer session: the APK as base.apk and its profile
     * as base.dm, so Android compiles the update ahead of time while installing it. Android
     * asks the person to confirm (see [statusReceiver]). Sessions left from an earlier
     * attempt (a confirmation left open, a previous run of the app) are abandoned first, so
     * only one is ever waiting. If the session cannot be used at all, the APK alone goes to
     * Android's installer screen, as before. [ready] is the state shown meanwhile
     * (installing = true).
     */
    private suspend fun installPackage(ready: UpdateState.Ready) {
        val started = withContext(Dispatchers.IO) {
            runCatching {
                if (!receiverRegistered) {
                    ContextCompat.registerReceiver(context, statusReceiver, IntentFilter(ACTION_INSTALL_STATUS), ContextCompat.RECEIVER_NOT_EXPORTED)
                    receiverRegistered = true
                }
                val installer = context.packageManager.packageInstaller
                current = null
                for (old in installer.mySessions) {
                    runCatching { installer.abandonSession(old.sessionId) }.onSuccess { Timber.i("update: abandoned earlier session %d", old.sessionId) }
                }
                val params = PackageInstaller.SessionParams(PackageInstaller.SessionParams.MODE_FULL_INSTALL).apply {
                    setAppPackageName(BuildConfig.APPLICATION_ID)
                    setSize(ready.file.length() + (ready.profile?.length() ?: 0))
                }
                val id = installer.createSession(params)
                try {
                    installer.openSession(id).use { session ->
                        fun put(name: String, file: File) {
                            file.inputStream().use { input ->
                                session.openWrite(name, 0, file.length()).use { out -> input.copyTo(out, 256 * 1024); session.fsync(out) }
                            }
                        }
                        put("base.apk", ready.file)
                        ready.profile?.let { put("base.dm", it) }
                        val flags = PendingIntent.FLAG_UPDATE_CURRENT or (if (Build.VERSION.SDK_INT >= 31) PendingIntent.FLAG_MUTABLE else 0)
                        val pending = PendingIntent.getBroadcast(context, id, Intent(ACTION_INSTALL_STATUS).setPackage(context.packageName), flags)
                        // Before the commit: the first answer can arrive as soon as it returns.
                        current = Attempt(id, ready)
                        session.commit(pending.intentSender)
                    }
                    Timber.i("update session %d committed (profile: %s)", id, ready.profile != null)
                } catch (t: Throwable) {
                    if (current?.sessionId == id) current = null
                    runCatching { installer.abandonSession(id) }
                    throw t
                }
            }.onFailure { Timber.w(it, "install session failed; using the installer screen") }.isSuccess
        }
        if (!started) {
            settle(ready)
            launchInstaller(ready.file)
        }
    }

    private fun launchInstaller(file: File) {
        val uri = FileProvider.getUriForFile(context, "${BuildConfig.APPLICATION_ID}.files", file)
        val intent = Intent(Intent.ACTION_VIEW)
            .setDataAndType(uri, "application/vnd.android.package-archive")
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_GRANT_READ_URI_PERMISSION)
        runCatching { context.startActivity(intent) }.onFailure { Timber.w(it, "could not open the installer") }
    }

    private companion object {
        const val MIN_INTERVAL_MS = 6 * 60 * 60 * 1000L
        const val ACTION_INSTALL_STATUS = "${BuildConfig.APPLICATION_ID}.UPDATE_INSTALL_STATUS"
        /** The build's profiles are ~15 KB; anything near this is not one. */
        const val MAX_PROFILE_BYTES = 8L shl 20
    }
}
