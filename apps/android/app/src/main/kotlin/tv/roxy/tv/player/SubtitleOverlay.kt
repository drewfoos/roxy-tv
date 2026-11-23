package tv.roxy.tv.player

import android.content.Context
import android.os.Handler
import android.os.HandlerThread
import android.view.SurfaceHolder
import dev.jdtech.mpv.MPVLib
import timber.log.Timber
import tv.roxy.tv.api.Attachment
import java.io.File
import java.net.HttpURLConnection
import java.net.URL
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/**
 * Subtitles over a picture that mpv does not draw.
 *
 * Native HDR hands the video to the device's own video hardware (mpv's mediacodec_embed:
 * the decoder fills the surface, the screen gets HDR as it is, in the file's full size).
 * mpv has nothing to draw subtitles on there, so they used to be off. This is a second,
 * small mpv that plays nothing and draws only subtitles (libass, with the same settings
 * and fonts as the player's own), on a see-through surface the page lays exactly over the
 * picture.
 *
 * How it keeps time: it holds a second of silence, paused, so its own clock stands still,
 * and the subtitle's delay is set to minus the film's position: the line for "now" is
 * always the one at its time zero. Nothing has to be kept in step (two clocks, seeks, the
 * speed, pauses): the film's position is read on every tick, and each film frame is one
 * redraw.
 *
 * What it draws is the server's copy of the shown track as a file of its own (text tracks:
 * styled ones keep their styles), read whole, so any position can be drawn at once. A
 * styled track's fonts are the video file's attachments, which this mpv does not have:
 * they are fetched into a folder of their own first.
 */
class SubtitleOverlay(
    context: Context,
    private val cacheDir: File,
    private val player: MpvPlayer,
    private val attachments: () -> List<Attachment>,
) : SurfaceHolder.Callback, MPVLib.EventObserver {

    private val mpv: MPVLib = MPVLib.create(context.applicationContext) ?: error("MPVLib.create returned null")
    private val thread = HandlerThread("subtitle-overlay").apply { start() }
    private val handler = Handler(thread.looper)
    @Volatile private var destroyed = false
    private var surfaceReady = false
    /** What is shown or being loaded: the files, and how many fonts were known when they were asked for. */
    private var want: Pair<OverlayFiles, Int>? = null
    private var loaded = false
    private var lastTime = Double.NaN
    private var ticks = 0
    private val silence: File by lazy { writeSilence(File(cacheDir, "overlay-silence.wav")) }

    // Declared before the start below: the first tick runs on the other thread at once.
    private val tick = object : Runnable {
        override fun run() {
            if (destroyed) return
            if (surfaceReady) {
                if (ticks++ % SYNC_EVERY == 0) syncSubtitle()
                if (loaded) {
                    val t = player.timePosSeconds()
                    if (t != null && t != lastTime) {
                        lastTime = t
                        mpv.setPropertyDouble("sub-delay", -t)
                    }
                }
            }
            handler.postDelayed(this, TICK_MS)
        }
    }

    init {
        fun opt(name: String, value: String) { mpv.setOptionString(name, value) }
        opt("config", "no")
        opt("vo", "null")
        opt("gpu-context", "android")
        opt("opengl-es", "yes")
        opt("profile", "fast")
        // Nothing but the subtitles is drawn: the rest of the surface stays see-through.
        opt("background", "color")
        opt("background-color", "#00000000")
        // mpv cannot read the refresh rate on Android, and without one it redraws a paused
        // screen ten times a second at most (vo.c): a line then came up to a tenth of a
        // second late, and moving typesetting stuttered.
        opt("display-fps-override", "%.3f".format(java.util.Locale.ROOT, refreshRate(context)))
        opt("ao", "null")
        opt("pause", "yes")
        opt("idle", "yes")
        opt("keep-open", "always")
        opt("force-window", "no")
        opt("cache", "no")
        opt("ytdl", "no")
        opt("load-scripts", "no")
        opt("input-default-bindings", "no")
        opt("osd-level", "0")
        opt("sub-auto", "no")
        opt("audio-display", "no")
        // As the player's own (MpvPlayer): the script's styles, inside the picture.
        opt("sub-ass-override", "no")
        opt("sub-scale-with-window", "yes")
        opt("sub-use-margins", "no")
        opt("embeddedfonts", "yes")
        opt("msg-level", "all=warn,sub=info")
        mpv.init()
        mpv.addObserver(this)
        handler.post(tick)
        Timber.i("subtitle overlay: started")
    }

    /** Follows what the player shows: another track, none, another file, fonts that became known. */
    private fun syncSubtitle() {
        val files = player.overlaySubtitle()
        val now = files?.let { it to if (it.fonts != null) attachments().count { a -> isFontAttachment(a.filename, a.mime) } else 0 }
        if (now == want) return
        want = now
        loaded = false
        lastTime = Double.NaN
        if (now == null) {
            Timber.i("subtitle overlay: nothing to show")
            mpv.command(arrayOf("stop"))
            return
        }
        Timber.i("subtitle overlay: loading %s (%d fonts)", now.first.subtitle.substringBefore('?'), now.second)
        mpv.setPropertyString("http-header-fields", now.first.header ?: "")
        mpv.command(arrayOf("loadfile", silence.path, "replace"))
    }

    override fun event(eventId: Int) {
        if (eventId != MPVLib.MpvEvent.MPV_EVENT_FILE_LOADED) return
        handler.post {
            val w = want ?: return@post
            if (destroyed) return@post
            // Before the track: libass reads the folder when the track's renderer is made.
            val fonts = fetchFonts(w.first)
            if (destroyed || w != want) return@post
            mpv.setPropertyString("sub-fonts-dir", fonts?.path ?: "")
            runCatching { mpv.command(arrayOf("sub-add", w.first.subtitle, "select")) }
                .onFailure { Timber.w(it, "subtitle overlay: sub-add") }
            loaded = true
            Timber.i("subtitle overlay: subtitle loaded")
        }
    }

    override fun eventProperty(property: String) {}
    override fun eventProperty(property: String, value: Long) {}
    override fun eventProperty(property: String, value: Double) {}
    override fun eventProperty(property: String, value: String) {}
    override fun eventProperty(property: String, value: Boolean) {}

    /**
     * The video file's fonts in a folder for this file, fetched once (a film's are some
     * tens of megabytes at most). Null when the track needs none. A font that cannot be
     * fetched is left out: its lines are drawn in the nearest font the device has.
     */
    private fun fetchFonts(files: OverlayFiles): File? {
        val base = files.fonts ?: return null
        val fonts = attachments().filter { isFontAttachment(it.filename, it.mime) }
        if (fonts.isEmpty()) return null
        val root = File(cacheDir, "overlay-fonts")
        val dir = File(root, (files.sourceId ?: "x").filter { it.isLetterOrDigit() || it == '-' }.ifEmpty { "x" })
        dir.mkdirs()
        dir.setLastModified(System.currentTimeMillis())
        for (a in fonts) {
            if (destroyed) return dir
            val name = a.index.toString() + "-" + a.filename.substringAfterLast('/').filter { it.isLetterOrDigit() || it in "._- " }.takeLast(80)
            val file = File(dir, name)
            if (file.length() > 0) continue
            val part = File(dir, "$name.part")
            runCatching {
                val c = URL(base + a.index + files.query).openConnection() as HttpURLConnection
                c.connectTimeout = 10_000; c.readTimeout = 20_000
                files.header?.let { h -> c.setRequestProperty(h.substringBefore(':').trim(), h.substringAfter(':').trim()) }
                try {
                    if (c.responseCode != 200) error("HTTP ${c.responseCode}")
                    c.inputStream.use { input -> part.outputStream().use { input.copyTo(it) } }
                } finally { c.disconnect() }
                if (!part.renameTo(file)) error("rename")
            }.onFailure { part.delete(); Timber.w("subtitle overlay: font %s: %s", a.filename, it.message) }
        }
        // The folders of the last few files are kept; older ones go.
        root.listFiles { f -> f.isDirectory }?.sortedByDescending { it.lastModified() }?.drop(KEEP_FONT_FOLDERS)?.forEach { it.deleteRecursively() }
        return dir
    }

    override fun surfaceCreated(holder: SurfaceHolder) {
        val surface = holder.surface
        val frame = holder.surfaceFrame
        handler.post {
            if (destroyed) return@post
            if (frame.width() > 0 && frame.height() > 0) mpv.setPropertyString("android-surface-size", "${frame.width()}x${frame.height()}")
            mpv.attachSurface(surface)
            mpv.setPropertyString("force-window", "yes")
            mpv.setPropertyString("vo", "gpu")
            surfaceReady = true
            ticks = 0
            Timber.i("subtitle overlay: surface %dx%d", frame.width(), frame.height())
        }
    }

    override fun surfaceChanged(holder: SurfaceHolder, format: Int, width: Int, height: Int) {
        handler.post { if (!destroyed) mpv.setPropertyString("android-surface-size", "${width}x$height") }
    }

    /** The surface is gone when this returns: mpv has to have let go of it by then. */
    override fun surfaceDestroyed(holder: SurfaceHolder) {
        val done = CountDownLatch(1)
        handler.postAtFrontOfQueue {
            if (!destroyed) releaseSurface()
            done.countDown()
        }
        done.await(2, TimeUnit.SECONDS)
    }

    private fun releaseSurface() {
        if (!surfaceReady) return
        surfaceReady = false
        mpv.setPropertyString("vo", "null")
        mpv.setPropertyString("force-window", "no")
        mpv.detachSurface()
    }

    /** Stops reading the player before it is destroyed; returns once no more reads are made. */
    fun destroy() {
        if (destroyed) return
        val done = CountDownLatch(1)
        handler.postAtFrontOfQueue {
            releaseSurface()
            destroyed = true
            handler.removeCallbacksAndMessages(null)
            done.countDown()
            runCatching { mpv.removeObserver(this) }
            runCatching { mpv.command(arrayOf("stop")) }
            runCatching { mpv.destroy() }.onFailure { Timber.w(it, "subtitle overlay: destroy") }
            thread.quitSafely()
            Timber.i("subtitle overlay: destroyed")
        }
        done.await(3, TimeUnit.SECONDS)
    }

    internal companion object {
        const val TICK_MS = 16L
        /** The player is asked what it shows every this many ticks (a quarter of a second). */
        const val SYNC_EVERY = 15
        const val KEEP_FONT_FOLDERS = 3

        fun refreshRate(context: Context): Float {
            val display = (context.getSystemService(Context.DISPLAY_SERVICE) as? android.hardware.display.DisplayManager)
                ?.getDisplay(android.view.Display.DEFAULT_DISPLAY)
            return display?.refreshRate?.takeIf { it >= 10f } ?: 60f
        }

        fun writeSilence(file: File): File {
            val wav = silenceWav()
            if (file.length() != wav.size.toLong()) file.writeBytes(wav)
            return file
        }
    }
}
