package tv.roxy.tv.player

import android.content.Context
import android.view.SurfaceHolder
import dev.jdtech.mpv.MPVLib
import timber.log.Timber
import java.io.File
import java.util.ArrayDeque

/**
 * Thin wrapper over libmpv (dev.jdtech.mpv:libmpv); grew out of the Phase 1a player spike.
 *
 * Wiring follows Findroid's MPVPlayer and mpv-android's BaseMPVView:
 *  - options are set before init(), surface is attached in surfaceCreated and the vo is
 *    (re)enabled there, surfaceDestroyed sets vo=null and detaches.
 *  - the file is loaded only once a surface exists.
 *
 * Font setup (fonts.conf + FONTCONFIG_FILE, aliases, colour-emoji rejection) and the
 * track policy were validated on real hardware; change them only with the test matrix.
 */
class MpvPlayer(
    private val context: Context,
    private val options: Options,
    private val fontsDir: File,
    displayRefreshRate: Float,
) : SurfaceHolder.Callback, MPVLib.EventObserver, MPVLib.LogObserver {

    data class Options(
        /** "mediacodec-copy" | "mediacodec" | "no" */
        val hwdec: String = "mediacodec-copy",
        /** "gpu" | "gpu-next" */
        val vo: String = "gpu",
        /** "audiotrack" | "aaudio" */
        val ao: String = "audiotrack",
        /** Start position in seconds (skip intros when checking subtitle rendering). */
        val startSeconds: Int = 0,
        /** Preferred audio languages, first wins. Owner default: English dub, else Japanese. */
        val audioLangs: List<String> = listOf("eng", "en", "jpn", "ja"),
        /** Preferred subtitle languages. */
        val subLangs: List<String> = listOf("eng", "en"),
        /**
         * Which subtitle track to pick among the preferred language: "full" = the dialogue
         * track (never a signs/songs-only track), "signs" = signs/songs only, "none".
         * The real app derives this from the chosen audio and the user's profile.
         */
        val subMode: String = "full",
        /** Explicit subtitle track id (mpv numbering); overrides the policy when > 0. */
        val forceSid: Int = 0,
    )

    /**
     * Language overrides keyed by mpv subtitle id, from Roxy Server's content sniffing.
     * Set before the file loads; the policy consults it instead of the container tag.
     */
    @Volatile
    var subtitleLanguageOverrides: Map<Int, String> = emptyMap()

    /** One entry of mpv's track-list. */
    data class Track(val id: Int, val type: String, val lang: String, val title: String, val codec: String, val isDefault: Boolean, val isForced: Boolean)

    interface Listener {
        fun onFileLoaded() {}
        fun onEndFile() {}
        fun onPauseChanged(paused: Boolean) {}
    }

    var listener: Listener? = null

    private val configDir = File(context.filesDir, "mpv").apply { mkdirs() }
    private val cacheDir = File(context.cacheDir, "mpv").apply { mkdirs() }

    // fontconfig inside libass only reads the file named by FONTCONFIG_FILE (there is no
    // /etc/fonts on Android), so write ours and export the variable before libmpv loads.
    // Without it libass logs "No usable fontconfig configuration file found" and the
    // aliases below are silently ignored.
    private val mpv: MPVLib = run {
        val conf = writeFontsConf(configDir, cacheDir)
        android.system.Os.setenv("FONTCONFIG_FILE", conf.path, true)
        android.system.Os.setenv("FONTCONFIG_PATH", conf.parent, true)
        MPVLib.create(context) ?: error("MPVLib.create returned null")
    }
    private var surfaceReady = false
    private var pendingUrl: String? = null

    /** Last libass / font related log lines, newest last. */
    private val fontLog = ArrayDeque<String>()
    private val fontLogMax = 40

    private val observed = listOf(
        "pause" to MPVLib.MpvFormat.MPV_FORMAT_FLAG,
        "paused-for-cache" to MPVLib.MpvFormat.MPV_FORMAT_FLAG,
        "eof-reached" to MPVLib.MpvFormat.MPV_FORMAT_FLAG,
        "time-pos" to MPVLib.MpvFormat.MPV_FORMAT_DOUBLE,
        "duration" to MPVLib.MpvFormat.MPV_FORMAT_DOUBLE,
    )

    init {
        fun opt(name: String, value: String) {
            val rc = mpv.setOptionString(name, value)
            if (rc < 0) Timber.w("mpv option %s=%s rejected (%d)", name, value, rc)
        }

        opt("config", "yes")
        opt("config-dir", configDir.path)
        opt("gpu-shader-cache-dir", cacheDir.path)
        opt("icc-cache-dir", cacheDir.path)

        opt("profile", "fast")
        opt("vo", options.vo)
        opt("gpu-context", "android")
        opt("opengl-es", "yes")
        opt("hwdec", options.hwdec)
        opt("hwdec-codecs", "h264,hevc,mpeg4,mpeg2video,vp8,vp9,av1")
        opt("ao", options.ao)
        opt("audio-set-media-role", "yes")

        opt("video-sync", "audio")
        opt("display-fps-override", "%.3f".format(displayRefreshRate))

        opt("tls-verify", "no")
        opt("cache", "yes")
        opt("cache-pause-initial", "yes")
        opt("demuxer-max-bytes", "64MiB")
        opt("demuxer-max-back-bytes", "32MiB")

        // Subtitles: render ASS exactly as authored, never override styles; fonts from our dir.
        opt("sub-fonts-dir", fontsDir.path)
        opt("sub-ass-override", "no")
        // mpv's default sid=auto only picks a track that is flagged default/forced or
        // matches slang; fansub MKVs often flag nothing, so prefer English subs explicitly.
        // Audio: English dub first (owner's preference), Japanese otherwise. With English
        // audio the right subtitle companion is the "Signs/Songs" track, which dual-audio
        // releases usually flag default; with Japanese audio it must be the full dialogue
        // track instead. The real app derives the sub choice from the chosen audio.
        opt("slang", options.subLangs.joinToString(","))
        opt("alang", options.audioLangs.joinToString(","))
        if (options.startSeconds > 0) opt("start", options.startSeconds.toString())
        opt("sub-scale-with-window", "yes")
        opt("sub-use-margins", "no")
        opt("embeddedfonts", "yes")

        opt("force-window", "no")
        opt("idle", "yes")
        opt("keep-open", "always")
        opt("save-position-on-quit", "no")
        opt("ytdl", "no")
        opt("input-default-bindings", "no")
        opt("msg-level", "all=v,ass/libass=v,sub=v")

        mpv.init()
        mpv.addObserver(this)
        mpv.addLogObserver(this)

        for ((name, fmt) in observed) mpv.observeProperty(name, fmt)
        Timber.i("mpv initialised: hwdec=%s vo=%s ao=%s", options.hwdec, options.vo, options.ao)
    }

    // ---- playback control ----

    fun play(url: String) {
        pendingUrl = url
        if (surfaceReady) loadPending()
    }

    private fun loadPending() {
        val url = pendingUrl ?: return
        pendingUrl = null
        Timber.i("loadfile %s", url.substringBefore("?"))
        mpv.command(arrayOf("loadfile", url, "replace"))
        mpv.setPropertyBoolean("pause", false)
    }

    fun togglePause() {
        val paused = mpv.getPropertyBoolean("pause") ?: false
        mpv.setPropertyBoolean("pause", !paused)
    }

    fun seekRelative(seconds: Int) = mpv.command(arrayOf("seek", seconds.toString(), "relative"))

    fun cycleSubtitle() = mpv.command(arrayOf("cycle", "sub"))

    fun cycleAudio() = mpv.command(arrayOf("cycle", "audio"))

    // ---- direct control (used by the on-screen controls) ----

    fun setPaused(paused: Boolean) = mpv.setPropertyBoolean("pause", paused)

    fun seekTo(seconds: Double) = mpv.command(arrayOf("seek", "%.3f".format(java.util.Locale.ROOT, seconds), "absolute"))

    /** mpv subtitle id (1-based) or 0 for off. */
    fun setSubtitleTrack(id: Int) {
        if (id <= 0) mpv.setPropertyString("sid", "no") else mpv.setPropertyInt("sid", id)
    }

    fun setAudioTrack(id: Int) {
        if (id <= 0) mpv.setPropertyString("aid", "no") else mpv.setPropertyInt("aid", id)
    }

    fun setSpeed(speed: Double) = mpv.setPropertyDouble("speed", speed)

    fun isMuted(): Boolean = bool("mute") ?: false

    fun setMuted(muted: Boolean) = mpv.setPropertyBoolean("mute", muted)

    /** Crop-to-fill (panscan 1) vs letterbox (0): the "fullscreen" button on phones. */
    fun setFill(fill: Boolean) = mpv.setPropertyDouble("panscan", if (fill) 1.0 else 0.0)

    fun isFill(): Boolean = (dbl("panscan") ?: 0.0) > 0.5

    /**
     * Pinch zoom: [factor] 1..8 (mpv's video-zoom is log2), [panX]/[panY] as fractions of
     * the scaled video size. Reset with (1, 0, 0).
     */
    fun setZoom(factor: Double, panX: Double, panY: Double) {
        mpv.setPropertyDouble("video-zoom", kotlin.math.log2(factor.coerceIn(1.0, 8.0)))
        mpv.setPropertyDouble("video-pan-x", panX)
        mpv.setPropertyDouble("video-pan-y", panY)
    }

    fun speed(): Double = dbl("speed") ?: 1.0

    /** Current mpv track id for "sub" or "audio" (0 when off / none). */
    fun currentTrackId(type: String): Int = int("current-tracks/$type/id") ?: 0

    /** Demuxer cache end in seconds (how far ahead playback can go without buffering). */
    fun cacheEndSeconds(): Double? = dbl("demuxer-cache-time")

    fun videoSummary(): String {
        val w = int("video-params/w") ?: return ""
        val h = int("video-params/h") ?: return ""
        val codec = str("video-format") ?: ""
        val hw = str("hwdec-current")?.takeIf { it != "no" && it.isNotEmpty() }
        return "${h}p ${codec.uppercase()}" + (if (hw != null) " · hardware" else " · software")
    }

    // ---- track selection ----

    fun tracks(): List<Track> {
        val n = mpv.getPropertyInt("track-list/count") ?: 0
        return (0 until n).mapNotNull { i ->
            val p = "track-list/$i"
            val type = str("$p/type") ?: return@mapNotNull null
            Track(
                id = int("$p/id") ?: return@mapNotNull null,
                type = type,
                lang = str("$p/lang").orEmpty().lowercase(),
                title = str("$p/title").orEmpty(),
                codec = str("$p/codec").orEmpty(),
                isDefault = bool("$p/default") ?: false,
                isForced = bool("$p/forced") ?: false,
            )
        }
    }

    private val signsRegex = Regex("""(?i)\b(signs?|songs?|s&s|signs/songs|forced)\b""")
    private val fullRegex = Regex("""(?i)\b(full|dialog(ue)?|complete)\b""")

    /**
     * Picks the subtitle track by language and title. Release groups do not flag "full
     * dialogue" vs "signs & songs" in any machine-readable way; the title is the only
     * signal, and the file's default flag usually points at the signs track (for dub
     * viewers), which is useless for checking dialogue rendering.
     */
    fun chooseSubtitle(all: List<Track>, mode: String, langs: List<String>): Track? {
        if (mode == "none") return null
        val subs = all.filter { it.type == "sub" }
        if (subs.isEmpty()) return null
        val wantSigns = mode == "signs"
        fun score(t: Track): Int {
            var s = 0
            val li = langs.indexOf(t.lang)
            s += if (li >= 0) (100 - li * 10) else if (t.lang.isEmpty()) 20 else 0
            val signs = signsRegex.containsMatchIn(t.title)
            val full = fullRegex.containsMatchIn(t.title)
            s += when {
                wantSigns && signs -> 50
                wantSigns && !signs -> -50
                !wantSigns && signs -> -60
                !wantSigns && full -> 30
                else -> 0
            }
            if (t.codec == "ass" || t.codec == "ssa") s += 5 // styled over plain when both exist
            if (t.isDefault) s += 1
            return s
        }
        return subs.maxByOrNull { score(it) }
    }

    private fun applyTrackPolicy() {
        val overrides = subtitleLanguageOverrides
        val all = tracks().map { t ->
            val o = overrides[t.id]
            if (t.type == "sub" && o != null && o != t.lang) t.copy(lang = o) else t
        }
        all.forEach { Timber.i("track %s id=%d lang=%s codec=%s default=%s title=%s", it.type, it.id, it.lang, it.codec, it.isDefault, it.title) }
        if (options.forceSid > 0) {
            mpv.setPropertyInt("sid", options.forceSid)
            Timber.i("subtitle forced -> sid=%d", options.forceSid)
            return
        }
        val sub = chooseSubtitle(all, options.subMode, options.subLangs)
        if (sub == null) {
            if (options.subMode == "none") mpv.setPropertyString("sid", "no")
        } else {
            mpv.setPropertyInt("sid", sub.id)
            Timber.i("subtitle policy %s -> sid=%d '%s' (%s/%s)", options.subMode, sub.id, sub.title, sub.lang, sub.codec)
        }
    }

    // ---- playback state (for progress reporting / OSD) ----

    fun timePosSeconds(): Double? = dbl("time-pos")

    fun durationSeconds(): Double? = dbl("duration")

    fun isPaused(): Boolean = bool("pause") ?: false

    /** ffmpeg stream index of the selected track of [type] ("audio" | "sub"); matches Roxy's stream `index`. */
    fun currentStreamIndex(type: String): Int? = int("current-tracks/$type/ff-index")

    // ---- stats ----

    fun snapshot(): MpvStats = MpvStats(
        hwdecCurrent = str("hwdec-current"),
        videoCodec = str("video-codec"),
        videoFormat = str("video-format"),
        pixelFormat = str("video-params/pixelformat"),
        width = int("video-params/w"),
        height = int("video-params/h"),
        containerFps = dbl("container-fps"),
        estimatedVfFps = dbl("estimated-vf-fps"),
        displayFps = dbl("display-fps"),
        frameDropCount = int("frame-drop-count"),
        decoderFrameDropCount = int("decoder-frame-drop-count"),
        voDelayedFrameCount = int("vo-delayed-frame-count"),
        mistimedFrameCount = int("mistimed-frame-count"),
        avSync = dbl("avsync"),
        timePos = dbl("time-pos"),
        duration = dbl("duration"),
        paused = bool("pause"),
        pausedForCache = bool("paused-for-cache"),
        cacheDuration = dbl("demuxer-cache-duration"),
        cacheSpeedBytes = int("cache-speed"),
        subCodec = str("current-tracks/sub/codec"),
        subTitle = str("current-tracks/sub/title"),
        subLang = str("current-tracks/sub/lang"),
        audioCodec = str("audio-codec-name"),
        audioParams = str("audio-params/format"),
        audioChannels = str("audio-params/channel-count"),
        vo = str("current-vo"),
        ao = str("current-ao"),
        videoOutParams = str("video-out-params/colormatrix"),
        gamma = str("video-out-params/gamma"),
        primaries = str("video-out-params/primaries"),
        fontLog = synchronized(fontLog) { fontLog.toList() },
    )

    private fun str(p: String) = mpv.getPropertyString(p)
    private fun int(p: String) = mpv.getPropertyInt(p)
    private fun dbl(p: String) = mpv.getPropertyDouble(p)
    private fun bool(p: String) = mpv.getPropertyBoolean(p)

    // ---- surface ----

    override fun surfaceCreated(holder: SurfaceHolder) {
        Timber.d("surfaceCreated")
        mpv.attachSurface(holder.surface)
        mpv.setPropertyString("force-window", "yes")
        surfaceReady = true
        if (pendingUrl != null) loadPending() else mpv.setPropertyString("vo", options.vo)
    }

    override fun surfaceChanged(holder: SurfaceHolder, format: Int, width: Int, height: Int) {
        Timber.d("surfaceChanged %dx%d", width, height)
        mpv.setPropertyString("android-surface-size", "${width}x$height")
    }

    override fun surfaceDestroyed(holder: SurfaceHolder) {
        Timber.d("surfaceDestroyed")
        surfaceReady = false
        mpv.setPropertyString("vo", "null")
        mpv.setPropertyString("force-window", "no")
        mpv.detachSurface()
    }

    // ---- events ----

    override fun eventProperty(property: String) {}
    override fun eventProperty(property: String, value: Long) {}
    override fun eventProperty(property: String, value: Double) {}
    override fun eventProperty(property: String, value: String) {}
    override fun eventProperty(property: String, value: Boolean) {
        when (property) {
            "pause" -> listener?.onPauseChanged(value)
            "eof-reached" -> if (value) listener?.onEndFile()
        }
    }

    override fun event(eventId: Int) {
        when (eventId) {
            MPVLib.MpvEvent.MPV_EVENT_FILE_LOADED -> {
                Timber.i("file loaded: codec=%s hwdec=%s start-opt=%s time-pos=%s", str("video-codec"), str("hwdec-current"), str("start"), str("time-pos"))
                // Belt and braces: the `start` option is set before init, but seek anyway if
                // the file came up at 0 (seen on the refactored player; cheap to be sure).
                if (options.startSeconds > 0 && (dbl("time-pos") ?: 0.0) < options.startSeconds - 2.0) {
                    seekTo(options.startSeconds.toDouble())
                }
                applyTrackPolicy()
                listener?.onFileLoaded()
            }
            MPVLib.MpvEvent.MPV_EVENT_END_FILE -> listener?.onEndFile()
        }
    }

    override fun logMessage(prefix: String, level: Int, text: String) {
        val line = text.trimEnd()
        if (line.isEmpty()) return
        val interesting = prefix.contains("libass") || prefix.contains("sub") ||
            line.contains("fontselect") || line.contains("font provider") ||
            line.contains("Glyph") || line.contains("attachment", ignoreCase = true) ||
            line.contains("hwdec", ignoreCase = true) || line.contains("Using hardware decoding") ||
            line.contains("Could not open", ignoreCase = true)
        if (interesting) {
            synchronized(fontLog) {
                fontLog.addLast("[$prefix] $line")
                while (fontLog.size > fontLogMax) fontLog.removeFirst()
            }
        }
        if (level <= MPVLib.MpvLogLevel.MPV_LOG_LEVEL_WARN) Timber.w("mpv[%s] %s", prefix, line)
        else Timber.v("mpv[%s] %s", prefix, line)
    }

    // ---- lifecycle ----

    fun destroy() {
        runCatching { mpv.removeLogObserver(this) }
        runCatching { mpv.removeObserver(this) }
        runCatching { mpv.command(arrayOf("stop")) }
        mpv.destroy()
        Timber.i("mpv destroyed")
    }

    /** fontconfig fallback so libass can also find fonts via fontconfig if sub-fonts-dir is ignored. */
    private fun writeFontsConf(configDir: File, cacheDir: File): File {
        val conf = File(configDir, "fonts.conf")
        val fcCache = File(cacheDir, "fontconfig").apply { mkdirs() }
        // Aliases map the proprietary families subtitles ask for (Arial, Calibri, ...) onto
        // the metric-compatible or closest bundled font, so libass does not fall through to
        // whatever the OS has. Only used when the MKV does not attach the real font.
        fun alias(from: String, vararg to: String) =
            "<alias binding=\"same\"><family>$from</family><prefer>" + to.joinToString("") { "<family>$it</family>" } + "</prefer></alias>"
        val aliases = listOf(
            alias("Arial", "Liberation Sans", "Open Sans"),
            alias("Arial Narrow", "Liberation Sans Narrow", "Liberation Sans"),
            alias("Helvetica", "Liberation Sans", "Open Sans"),
            alias("Helvetica Neue", "Open Sans", "Liberation Sans"),
            alias("Calibri", "Carlito", "Open Sans"),
            alias("Segoe UI", "Open Sans", "Noto Sans"),
            alias("Verdana", "DejaVu Sans", "Noto Sans"),
            alias("Tahoma", "DejaVu Sans", "Noto Sans"),
            alias("Trebuchet MS", "Fira Sans", "Open Sans"),
            alias("Myriad Pro", "Source Sans 3", "Open Sans"),
            alias("Cronos Pro", "Cabin", "Source Sans 3"),
            alias("Gill Sans", "Cabin", "Lato"),
            alias("Futura", "Montserrat"),
            alias("Century Gothic", "Montserrat"),
            alias("Roboto", "Roboto", "Open Sans"),
            alias("Times New Roman", "Liberation Serif", "Noto Serif"),
            alias("MS Gothic", "Noto Sans CJK JP"),
            alias("MS PGothic", "Noto Sans CJK JP"),
            alias("Meiryo", "Noto Sans CJK JP"),
            alias("Yu Gothic", "Noto Sans CJK JP"),
            // Default family = the glyph-fallback chain libass walks for missing glyphs.
            // Order: text fonts, then CJK, then the Noto symbol/emoji set (cleaner outlines,
            // newer emoji), then Symbola as the widest last-resort net, then math/music.
            alias("sans-serif", "Open Sans", "Noto Sans", "DejaVu Sans", "Noto Sans CJK JP", "Noto Sans Symbols", "Noto Sans Symbols 2", "Noto Emoji", "Symbola", "Noto Sans Math", "Noto Music"),
        )
        // Colour bitmap emoji fonts (CBDT/sbix) cannot be rasterised by libass; if
        // fontconfig offers one for a missing glyph the glyph is lost. Hide them so
        // fallback reaches the bundled monochrome Noto Emoji / Symbols fonts instead.
        val reject = listOf(
            "/system/fonts/NotoColorEmoji*.ttf",
            "/system/fonts/*ColorEmoji*.ttf",
            "/system/fonts/*Emoji*.ttf",
            // The OS CJK collection sorts its SC/TC faces ahead of our JP one for kana and
            // CJK punctuation; anime subs want Japanese glyph forms, so use the bundled JP.
            "/system/fonts/NotoSansCJK*.ttc",
            "/system/fonts/NotoSerifCJK*.ttc",
        )
        // Assembled line by line: the XML declaration must be the first byte of the file,
        // and a trimIndent'ed template with multi-line insertions does not guarantee that.
        val xml = buildString {
            appendLine("<?xml version=\"1.0\"?>")
            appendLine("<!DOCTYPE fontconfig SYSTEM \"fonts.dtd\">")
            appendLine("<fontconfig>")
            appendLine("  <dir>${fontsDir.path}</dir>")
            appendLine("  <dir>/system/fonts</dir>")
            appendLine("  <cachedir>${fcCache.path}</cachedir>")
            appendLine("  <selectfont><rejectfont>")
            reject.forEach { appendLine("    <glob>$it</glob>") }
            appendLine("  </rejectfont></selectfont>")
            aliases.forEach { appendLine("  $it") }
            appendLine("</fontconfig>")
        }
        conf.writeText(xml)
        return conf
    }
}
