package tv.roxy.tv

import android.app.Application
import android.content.Context
import coil3.ImageLoader
import coil3.PlatformContext
import coil3.SingletonImageLoader
import coil3.network.okhttp.OkHttpNetworkFetcherFactory
import coil3.request.crossfade
import kotlinx.coroutines.CoroutineExceptionHandler
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import timber.log.Timber
import tv.roxy.tv.api.RoxyApi
import tv.roxy.tv.data.Prefs
import tv.roxy.tv.data.Updates
import tv.roxy.tv.data.ItemCache
import tv.roxy.tv.api.Item
import tv.roxy.tv.data.RemotePlay
import tv.roxy.tv.data.downloads.Downloads
import java.io.File

class RoxyApp : Application(), SingletonImageLoader.Factory {

    /**
     * Process-wide scope for work that must outlive an activity (final progress report, logout).
     * A failure in one launch is logged, never fatal: a server restart mid-call must not crash the app.
     */
    val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate + CoroutineExceptionHandler { _, t -> Timber.e(t, "uncaught in app scope") })

    lateinit var prefs: Prefs
        private set

    lateinit var api: RoxyApi
        private set

    /** Offline downloads: the index, the files and the queue (see data/downloads). */
    lateinit var downloads: Downloads
        private set

    /** App updates published on the server (bar above the nav, install through the system). */
    lateinit var updates: Updates
        private set

    /** Whether Roxy Server is answering (the "Can't reach Roxy" banner, reloads when it is back). */
    lateinit var server: tv.roxy.tv.data.ServerStatus
        private set

    /** Items the lists have shown, so a detail page can draw its header before its own fetch lands. */
    val items = ItemCache()

    /** "Play on": this device as a player others drive, and as a remote for them. */
    lateinit var remote: RemotePlay
        private set

    /**
     * The phone's in-app player page, registered by the phone shell. Every "play" on the
     * phone opens it (the video in a box with the page under it, fullscreen by turning
     * the phone); without it (the TV) playback goes to the full-screen activity.
     */
    @Volatile var openPlayerPage: ((item: Item, fromStart: Boolean, startMs: Long?) -> Unit)? = null

    /** Opens Account → Server → Sonarr & Radarr (set by the phone layout), for "set up requests" prompts. */
    @Volatile var openArrSettings: (() -> Unit)? = null
    /** Phone: an admin's Identify / Edit details for a title ("identify" | "edit"), set by the phone UI. */
    var openTitleAdmin: ((tv.roxy.tv.api.Item, String) -> Unit)? = null

    /** Directory holding the bundled fallback fonts for libass (copied from assets). */
    lateinit var fontsDir: File
        private set

    override fun onCreate() {
        super.onCreate()
        // DebugTree builds a stack trace per call to name the tag; fine while debugging, too
        // costly on a TV for the player's logging. Other builds log info and above under one tag.
        if (BuildConfig.DEBUG) Timber.plant(Timber.DebugTree()) else Timber.plant(InfoTree)
        tv.roxy.tv.diagnostics.CrashReports.install(this)
        fontsDir = Fonts.install(this)
        prefs = Prefs(this, scope)
        api = RoxyApi(prefs)
        downloads = Downloads(this, prefs, scope)
        updates = Updates(this, api, prefs, scope)
        remote = RemotePlay(this, api, prefs, scope)
        server = tv.roxy.tv.data.ServerStatus(this, api, prefs, scope)
        // Back after a restart or a Wi-Fi drop: positions saved meanwhile go up, waiting downloads resume.
        server.onBack = { scope.launch { downloads.syncProgress(api); downloads.kick() } }
        if (prefs.session.value.isLoggedIn) tv.roxy.tv.diagnostics.CrashReports.sendPending(this, api, scope)
        Timber.i("Roxy TV started. server=%s user=%s fontsDir=%s", prefs.session.value.serverUrl, prefs.session.value.userName, fontsDir)
    }

    /** Coil loads images through the API's OkHttp client, so every image request carries the Bearer token. */
    override fun newImageLoader(context: PlatformContext): ImageLoader =
        ImageLoader.Builder(context)
            .components { add(OkHttpNetworkFetcherFactory(callFactory = { api.http })) }
            // The fade-in is one animation per card as thumbnails land: a phone shrugs, a TV's processor stutters.
            .crossfade(!isTelevision(this))
            .build()

    companion object {
        fun get(context: Context): RoxyApp = context.applicationContext as RoxyApp
    }
}

/**
 * Copies every font in the assets fonts folder into filesDir/fonts once per app version, so
 * libmpv can read them from disk. A stamp file holds the version that copied them: an
 * unchanged install skips the whole folder (51 fonts, about 39 MB, used to be inflated and
 * rewritten on the main thread at every cold start because the size check never matched
 * compressed assets).
 */
object Fonts {
    fun install(context: Context): File {
        val dir = File(context.filesDir, "fonts").apply { mkdirs() }
        val stamp = File(dir, ".installed")
        val version = BuildConfig.VERSION_CODE.toString()
        if (runCatching { stamp.readText() }.getOrNull() == version) return dir
        val assets = context.assets
        val names = runCatching { assets.list("fonts")?.toList().orEmpty() }.getOrDefault(emptyList())
        for (name in names) {
            if (!name.endsWith(".ttf") && !name.endsWith(".ttc") && !name.endsWith(".otf")) continue
            val target = File(dir, name)
            val size = runCatching { assets.openFd("fonts/$name").use { it.length } }.getOrNull()
            if (target.exists() && size != null && target.length() == size) continue
            assets.open("fonts/$name").use { input ->
                target.outputStream().use { output -> input.copyTo(output) }
            }
            Timber.i("Installed font %s (%d bytes)", name, target.length())
        }
        runCatching { stamp.writeText(version) }
        return dir
    }
}

/** Release / perf logging: info and above, one fixed tag, no stack walk per line. */
private object InfoTree : Timber.Tree() {
    override fun isLoggable(tag: String?, priority: Int): Boolean = priority >= android.util.Log.INFO
    override fun log(priority: Int, tag: String?, message: String, t: Throwable?) {
        android.util.Log.println(priority, "Roxy", message)
        if (t != null) android.util.Log.println(priority, "Roxy", android.util.Log.getStackTraceString(t))
    }
}
