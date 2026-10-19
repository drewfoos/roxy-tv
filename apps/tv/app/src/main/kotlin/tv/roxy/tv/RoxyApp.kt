package tv.roxy.tv

import android.app.Application
import android.content.Context
import coil3.ImageLoader
import coil3.PlatformContext
import coil3.SingletonImageLoader
import coil3.network.okhttp.OkHttpNetworkFetcherFactory
import coil3.request.crossfade
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import timber.log.Timber
import tv.roxy.tv.api.RoxyApi
import tv.roxy.tv.data.Prefs
import tv.roxy.tv.data.Updates
import tv.roxy.tv.data.ItemCache
import tv.roxy.tv.data.downloads.Downloads
import java.io.File

class RoxyApp : Application(), SingletonImageLoader.Factory {

    /** Process-wide scope for work that must outlive an activity (final progress report, logout). */
    val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)

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

    /** Items the lists have shown, so a detail page can draw its header before its own fetch lands. */
    val items = ItemCache()

    /** Directory holding the bundled fallback fonts for libass (copied from assets). */
    lateinit var fontsDir: File
        private set

    override fun onCreate() {
        super.onCreate()
        Timber.plant(Timber.DebugTree())
        fontsDir = Fonts.install(this)
        prefs = Prefs(this, scope)
        api = RoxyApi(prefs)
        downloads = Downloads(this, prefs, scope)
        updates = Updates(this, api, prefs, scope)
        Timber.i("Roxy TV started. server=%s user=%s fontsDir=%s", prefs.session.value.serverUrl, prefs.session.value.userName, fontsDir)
    }

    /** Coil loads images through the API's OkHttp client, so every image request carries the Bearer token. */
    override fun newImageLoader(context: PlatformContext): ImageLoader =
        ImageLoader.Builder(context)
            .components { add(OkHttpNetworkFetcherFactory(callFactory = { api.http })) }
            .crossfade(true)
            .build()

    companion object {
        fun get(context: Context): RoxyApp = context.applicationContext as RoxyApp
    }
}

/** Copies every font in the assets fonts folder into filesDir/fonts once, so libmpv can read them from disk. */
object Fonts {
    fun install(context: Context): File {
        val dir = File(context.filesDir, "fonts").apply { mkdirs() }
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
        return dir
    }
}
