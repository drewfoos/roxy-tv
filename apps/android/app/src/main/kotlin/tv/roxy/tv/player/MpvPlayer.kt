package tv.roxy.tv.player

import android.content.Context
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.view.Surface
import android.view.SurfaceHolder
import dev.jdtech.mpv.MPVLib
import timber.log.Timber
import tv.roxy.tv.data.Languages
import java.io.File
import java.util.ArrayDeque

/**
 * Thin wrapper over libmpv (dev.jdtech.mpv:libmpv 1.0.0 = mpv 0.41.0, libplacebo 7.360.1);
 * grew out of the Phase 1a player spike.
 *
 * Wiring follows mpv-android's BaseMPVView and Findroid's MPVPlayer:
 *  - options are set before init(); the surface is attached in surfaceCreated and the vo is
 *    (re)enabled there; surfaceDestroyed sets vo=null and detaches.
 *  - the file is loaded only once a surface exists.
 *  - libmpv calls back on its own event thread while holding a lock: nothing here blocks
 *    in a callback, and every [Listener] call is posted to the main thread.
 *
 * Font setup (fonts.conf + FONTCONFIG_FILE, aliases, colour-emoji rejection) and the
 * track policy were validated on real hardware; change them only with the test matrix.
 * The 2026-10-31 audit (docs/research/tv-player-audit-2026-10-31.md) explains the rest.
 */
class MpvPlayer(
    private val context: Context,
    private val options: Options,
    private val fontsDir: File,
    displayRefreshRate: Float,
) : SurfaceHolder.Callback, MPVLib.EventObserver, MPVLib.LogObserver {

    companion object {
        /** "<output>|<codec>" pairs whose passthrough hung the audio output in this run. */
        private val brokenSpdif: MutableSet<String> = java.util.Collections.synchronizedSet(mutableSetOf())
        private const val BROKEN_KEY = "passthroughFailed"

        /** Forget the codecs passthrough failed on, for every sound output (the setting was touched). */
        fun resetPassthroughMemory(context: Context) {
            brokenSpdif.clear()
            val prefs = context.getSharedPreferences("audio", Context.MODE_PRIVATE)
            prefs.edit().apply { prefs.all.keys.filter { it.startsWith(BROKEN_KEY) }.forEach { remove(it) } }.apply()
        }

        /** The mpv `hwdec` value for a setting: "auto" = zero-copy MediaCodec, copy mode as the fallback. */
        fun mpvHwdec(setting: String): String = when (setting) {
            "auto", "" -> "mediacodec,mediacodec-copy"
            else -> setting
        }

        /** ?mt= / api_key= / token= (and similar) query values, and auth headers, in a log line. */
        private val SECRET_QUERY = Regex("""(?i)([?&;](?:mt|api_key|apikey|token|access_token|secret)=)[^&\s"'<>]+""")
        private val SECRET_HEADER = Regex("""(?i)((?:X-Roxy-Media-Token|X-Api-Key|Authorization|X-Emby-Token)\s*:\s*)[^\s,"']+""")
        /** ffmpeg / mpv saying the server refused the open with 400/401/403 (e.g. "HTTP error 401 Unauthorized"); not 404. */
        private val HTTP_4XX = Regex("""(?i)(HTTP error 40[013]\b|Server returned 40[013]\b|\b40[013] (?:Unauthorized|Forbidden|Bad Request))""")

        /** [line] with token values replaced by "***", for logcat, Timber and the subtitle/font diagnostics. */
        fun redactSecrets(line: String): String =
            if (line.isEmpty()) line else line.replace(SECRET_QUERY, "$1***").replace(SECRET_HEADER, "$1***")
    }

    data class Options(
        /** "auto" (mediacodec, then mediacodec-copy) | "mediacodec" | "mediacodec-copy" | "no" */
        val hwdec: String = "auto",
        /** "gpu"; "gpu-next" only from the adb bench extra (its libplacebo leaks GL syncs, see the audit). */
        val vo: String = "gpu",
        /** mpv `ao` list. aaudio freezes video after seeks with libmpv 1.0.0 (Findroid #1246). */
        val ao: String = "audiotrack,opensles",
        /** Start position in seconds (skip intros when checking subtitle rendering). */
        val startSeconds: Int = 0,
        /** Preferred audio languages, first wins. Owner default: English dub, else Japanese. */
        val audioLangs: List<String> = listOf("eng", "en", "jpn", "ja"),
        /** Preferred subtitle languages (every spelling of the one preference). */
        val subLangs: List<String> = listOf("eng", "en"),
        /** Anime (AniList-matched): a dub in your language pairs with the signs & songs track. */
        val anime: Boolean = false,
        /**
         * Which subtitle track to pick among the preferred language: "full" = the dialogue
         * track whatever the audio, "signs" = signs & songs over an anime dub, "foreign" =
         * subtitles only on foreign audio, "none" = off. See [chooseSubtitle].
         */
        val subMode: String = "full",
        /** Explicit subtitle track id (mpv numbering) for the first file only; overrides the policy when > 0. */
        val forceSid: Int = 0,
        /** mpv `audio-spdif` codecs sent untouched to a soundbar (empty = decode everything here). */
        val spdif: List<String> = emptyList(),
        /** Which HDMI sound output [spdif] was detected on; a passthrough failure is remembered per output. */
        val outputKey: String = "",
        /** Frame interpolation (display-resample + oversample): smooths 24 fps on a 60 Hz panel, costs GPU. */
        val smoothMotion: Boolean = false,
        /** HDR video goes to the screen as HDR (decoder straight to the surface) at the cost of styled subtitles. */
        val hdrNative: Boolean = false,
        /** The display takes HDR10/HLG; [hdrNative] only applies when it does. */
        val hdrDisplay: Boolean = false,
        /** A TV: frame-rate hints may switch the display mode (a short blank), which a TV wants. */
        val tv: Boolean = false,
        /** Little RAM (under 3 GB): smaller demuxer buffers. */
        val lowRam: Boolean = false,
        /** mpv logs at verbose level (the stats overlay's decoder log); otherwise warnings only. */
        val verboseLog: Boolean = false,
    )

    /**
     * Language overrides keyed by mpv subtitle id, from Roxy Server's content sniffing.
     * Set before the file loads; the policy consults it instead of the container tag.
     */
    @Volatile
    var subtitleLanguageOverrides: Map<Int, String> = emptyMap()

    /** One entry of mpv's track-list. */
    data class Track(val id: Int, val type: String, val lang: String, val title: String, val codec: String, val isDefault: Boolean, val isForced: Boolean)

    /** Every call arrives on the main thread. */
    interface Listener {
        fun onFileLoaded() {}
        /** The loaded file finished or was replaced (also when the next episode loads over it). */
        fun onEndFile() {}
        /** The newest file could not be opened (server down, 404, unreadable). */
        fun onLoadFailed() {}
        /** Playback ran to the end of what mpv could read (keep-open holds the last frame). */
        fun onEofReached() {}
        fun onPauseChanged(paused: Boolean) {}
        /** Waiting on the network, a seek, or a (re)load; the controls show a spinner. */
        fun onBufferingChanged(buffering: Boolean) {}
        /** Native HDR output switched on (true) for this file: styled subtitles are not drawn. */
        fun onHdrNative(active: Boolean) {}
        /**
         * Passthrough never started for this file's audio (see [watchPassthrough]); the codec
         * is decoded here from now on. True: the listener reopens the file at [atSec] itself,
         * as one of its own reloads, so the old file's end is not taken for a lost
         * connection or the end of the episode. False: the player reopens it.
         */
        fun onPassthroughFailed(atSec: Double): Boolean = false
    }

    var listener: Listener? = null

    private val main = Handler(Looper.getMainLooper())

    /** Runs [block] on the main thread unless the player is gone by then. */
    private fun post(block: () -> Unit) { main.post { if (!destroyed) block() } }

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
        MPVLib.create(context.applicationContext) ?: error("MPVLib.create returned null")
    }
    private var surfaceReady = false
    private var surface: Surface? = null
    private var pendingUrl: String? = null

    /** Last libass / decoder / audio log lines, newest last (the stats overlay). */
    private val fontLog = ArrayDeque<String>()
    private val fontLogMax = 40

    // Observed state. libmpv sends each property once at observe time and then on change
    // (compared in the requested format), so these mirror mpv without a JNI read.
    @Volatile private var pausedFlag = false
    @Volatile private var cacheFlag = false
    @Volatile private var seekingFlag = false
    @Volatile private var idleFlag = true
    @Volatile private var lastBuffering = false

    private val observed = listOf(
        "pause" to MPVLib.MpvFormat.MPV_FORMAT_FLAG,
        "paused-for-cache" to MPVLib.MpvFormat.MPV_FORMAT_FLAG,
        "seeking" to MPVLib.MpvFormat.MPV_FORMAT_FLAG,
        "core-idle" to MPVLib.MpvFormat.MPV_FORMAT_FLAG,
        "eof-reached" to MPVLib.MpvFormat.MPV_FORMAT_FLAG,
        "track-list" to MPVLib.MpvFormat.MPV_FORMAT_NONE,
        "video-params/gamma" to MPVLib.MpvFormat.MPV_FORMAT_STRING,
    )

    private val audioPrefs = context.getSharedPreferences("audio", Context.MODE_PRIVATE)
    private val brokenKey = BROKEN_KEY + (if (options.outputKey.isNotEmpty()) "|" + options.outputKey else "")

    private fun isBroken(codec: String): Boolean =
        "${options.outputKey}|$codec" in brokenSpdif || codec in (audioPrefs.getStringSet(brokenKey, emptySet()) ?: emptySet())

    /**
     * Passthrough codecs for this player: the setting's list minus any that already hung
     * this sound output (see [watchPassthrough]); `adb shell setprop debug.roxy.spdif
     * ac3,dts` (or `none`) overrides it for bench tests.
     */
    private var spdif: List<String> = sysprop("debug.roxy.spdif")?.let { if (it == "none") emptyList() else it.split(',') }
        ?: options.spdif.filter { !isBroken(it) }

    private fun sysprop(name: String): String? = runCatching {
        Class.forName("android.os.SystemProperties").getMethod("get", String::class.java).invoke(null, name) as String
    }.getOrNull()?.trim()?.takeIf { it.isNotEmpty() }

    /** The vo in use: [Options.vo], or mediacodec_embed while a file plays as native HDR. */
    @Volatile private var activeVo: String = options.vo
    @Volatile private var hdrActive = false

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
        // "auto" lists the zero-copy path first: frames stay on the GPU (AImageReader interop)
        // and libass still draws over them. Copy mode cannot carry 10-bit frames on many SoCs.
        // mpv's own hwdec=auto would never pick it (auto-safe since 0.40).
        opt("hwdec", mpvHwdec(options.hwdec))
        opt("hwdec-codecs", "h264,hevc,mpeg4,mpeg2video,vp8,vp9,av1")
        // gpu-next's film grain shader flips planes on GLES with this libplacebo (Findroid #1322).
        if (options.vo == "gpu-next") opt("vd-lavc-film-grain", "cpu")
        opt("ao", sysprop("debug.roxy.ao") ?: options.ao)
        opt("audio-set-media-role", "yes")
        if (spdif.isNotEmpty()) opt("audio-spdif", spdif.joinToString(","))
        // Bench switch for audio work on a device: `adb shell setprop debug.roxy.af <filters>`.
        sysprop("debug.roxy.af")?.let { opt("af", it) }

        if (options.smoothMotion) {
            // Blends frames to the display's rate: 24 fps on a 60 Hz panel without 3:2 judder.
            opt("video-sync", "display-resample")
            opt("interpolation", "yes")
            opt("tscale", "oversample")
        } else {
            opt("video-sync", "audio")
        }
        // mpv cannot read the refresh rate on Android; the session updates it after a mode switch.
        opt("display-fps-override", "%.3f".format(java.util.Locale.ROOT, displayRefreshRate))

        opt("cache", "yes")
        opt("cache-pause-initial", "yes")
        opt("demuxer-max-bytes", if (options.lowRam) "32MiB" else "64MiB")
        opt("demuxer-max-back-bytes", if (options.lowRam) "16MiB" else "32MiB")
        // mpv already turns on ffmpeg's HTTP reconnect; this covers resets and timeouts too.
        opt("stream-lavf-o", "reconnect_on_network_error=1")

        // Subtitles: render ASS exactly as authored, never override styles; fonts from our dir.
        opt("sub-fonts-dir", fontsDir.path)
        opt("sub-ass-override", "no")
        // mpv's default sid=auto only picks a track that is flagged default/forced or
        // matches slang; fansub MKVs often flag nothing, so prefer English subs explicitly.
        // Audio: English dub first (owner's preference), Japanese otherwise. The subtitle
        // choice is derived from the chosen audio in [applyTrackPolicy].
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
        // Verbose logs cost a JNI call and a logcat line each, and mpv's own lines carry the
        // stream URL with its media token; only the stats overlay asks for them.
        opt("msg-level", logLevel(options.verboseLog))

        mpv.init()
        Timber.i("mpv initialised: hwdec=%s vo=%s ao=%s spdif=%s smooth=%s lowRam=%s", mpvHwdec(options.hwdec), options.vo, options.ao, spdif, options.smoothMotion, options.lowRam)
    }

    private fun logLevel(verbose: Boolean) = if (verbose) "all=v,ass/libass=v,sub=v" else "all=warn,vd=info,ad=info,ao=info,sub=info"

    /** Verbose mpv logging while the stats overlay is up. */
    fun setVerboseLog(on: Boolean) = setStr("msg-level", logLevel(on))

    // ---- playback control ----

    /** Start position for the next loaded file; the first file takes it from [Options.startSeconds]. */
    @Volatile private var startSeconds: Int = options.startSeconds

    /** The pending file is a server transcode (starts where the server began it, keeps the file's clock). */
    @Volatile private var pendingTranscode = false

    /**
     * Load [url]; [start] applies to this file only (the next-episode queue plays several files on one player).
     * [transcoded]: a server transcode stream, which already begins at the wanted position
     * and carries the original file's timestamps; mpv must not rebase them to 0.
     */
    /**
     * [sid]: the subtitle track to open the file on (0 = none; null = the policy picks once
     * it is open). A transcode numbers its tracks itself: never preselected.
     */
    fun play(url: String, start: Int = options.startSeconds, transcoded: Boolean = false, sid: Int? = null) {
        if (destroyed) return
        startSeconds = if (transcoded) 0 else start
        pendingTranscode = transcoded
        pendingSid = if (transcoded) null else sid
        pendingUrl = url
        if (surfaceReady) loadPending()
    }

    @Volatile private var pendingSid: Int? = null
    @Volatile private var preselectedSid: Int? = null
    private var lastUrl: String? = null
    private var lastTranscoded = false

    // Load bookkeeping: each loadfile is followed by START_FILE for it (after the END_FILE
    // of whatever it replaces). An END_FILE between START_FILE and FILE_LOADED with no newer
    // load queued means the newest file failed to open.
    private val queuedLoads = java.util.concurrent.atomic.AtomicInteger(0)
    @Volatile private var loading = false

    /**
     * The media token travels as a header (X-Roxy-Media-Token), not in the URL: libmpv's
     * binding writes every URL it opens to the device log. A server from before 2026-10-31
     * only reads ?mt=, so one failed open falls back to the URL form for this player.
     */
    @Volatile private var tokenInHeader = true
    @Volatile private var lastLoadUsedHeader = false
    /**
     * The open that is under way was refused by the server with a 4xx (401/403/...), seen in
     * mpv's log. Only that means "an older server that wants ?mt="; a timeout, a 5xx or a
     * broken file must not push the token into the URL (and so into the device log).
     */
    @Volatile private var lastOpenHttp4xx = false

    private fun loadPending() {
        // A surface coming back with nothing to load must not touch the frame-rate gate:
        // the gate is reset on the new file's START_FILE (in event order, see FrameRateGate).
        val url = pendingUrl ?: return
        pendingUrl = null
        frameRateGate.loadRequested()
        // The passthrough watch belongs to the file it was armed for: left running, it
        // reopened THIS file at the old one's position.
        stopPassthroughWatch()
        lastUrl = url
        lastTranscoded = pendingTranscode
        Timber.i("loadfile %s start=%ds transcoded=%s", url.substringBefore("?"), startSeconds, pendingTranscode)
        val query = url.substringAfter('?', "").split('&').filter { it.isNotEmpty() }
        val token = if (tokenInHeader && url.startsWith("http")) query.firstOrNull { it.startsWith("mt=") }?.removePrefix("mt=") else null
        val loadUrl = if (token == null) url else {
            val rest = query.filterNot { it.startsWith("mt=") }
            url.substringBefore('?') + (if (rest.isEmpty()) "" else "?" + rest.joinToString("&"))
        }
        lastLoadUsedHeader = token != null
        lastOpenHttp4xx = false
        mpv.setPropertyString("http-header-fields", if (token != null) "X-Roxy-Media-Token: $token" else "")
        // `start` is a file-local option: set it as a property right before the load.
        mpv.setPropertyString("start", if (startSeconds > 0) startSeconds.toString() else "none")
        // A transcode started mid-file has its first frame at e.g. 300 s; keep that clock so
        // the position, subtitles and skip segments line up with the original file.
        mpv.setPropertyString("rebase-start-time", if (pendingTranscode) "no" else "yes")
        // The subtitle track chosen before the load: mpv opens on it (no switch, so no
        // going back for its subtitles). The policy then leaves this file alone.
        preselectedSid = pendingSid
        pendingSid = null
        preselectedSid?.let { mpv.setPropertyString("sid", if (it == 0) "no" else it.toString()) }
        queuedLoads.incrementAndGet()
        mpv.command(arrayOf("loadfile", loadUrl, "replace"))
        mpv.setPropertyBoolean("pause", false)
    }

    fun togglePause() = setPaused(!pausedFlag)

    /** Exact relative seek (the default for relative seeks lands on a keyframe, 7 or 13 s for a "10"). */
    fun seekRelative(seconds: Int) = cmd("seek", seconds.toString(), "relative+exact")

    fun cycleSubtitle() = cmd("cycle", "sub")

    fun cycleAudio() = cmd("cycle", "audio")

    // ---- direct control (used by the on-screen controls) ----

    fun setPaused(paused: Boolean) { if (!destroyed) mpv.setPropertyBoolean("pause", paused) }

    fun seekTo(seconds: Double) = cmd("seek", "%.3f".format(java.util.Locale.ROOT, seconds), "absolute")

    /** Fast seek to the keyframe before [seconds]: the picture follows a scrub without decoding every frame. */
    fun seekToKeyframe(seconds: Double) = cmd("seek", "%.3f".format(java.util.Locale.ROOT, seconds), "absolute+keyframes")

    /** mpv subtitle id (1-based) or 0 for off. */
    fun setSubtitleTrack(id: Int) {
        if (destroyed) return
        // Already on it: setting it again is not free (mpv re-reads the track's subtitles).
        if (currentTrackId("sub") == id.coerceAtLeast(0)) return
        if (id <= 0) mpv.setPropertyString("sid", "no") else mpv.setPropertyInt("sid", id)
    }

    fun setAudioTrack(id: Int) {
        if (destroyed) return
        if (id <= 0) mpv.setPropertyString("aid", "no") else mpv.setPropertyInt("aid", id)
        // Subtitles follow the audio: switching to the Japanese track brings the dialogue
        // subs in, switching back to the dub drops to signs & songs (or off).
        if (options.subMode != "none") {
            val all = tracksWithOverrides()
            val lang = all.firstOrNull { it.type == "audio" && it.id == id }?.let { audioLangOf(it) }.orEmpty()
            val sub = chooseSubtitle(all, options.subMode, options.subLangs, lang, options.anime)
            setSubtitleTrack(sub?.id ?: 0)
            Timber.i("subtitle policy after audio change (%s) -> %s", lang, sub?.id ?: "off")
        }
    }

    fun setSpeed(speed: Double) { if (!destroyed) mpv.setPropertyDouble("speed", speed) }

    fun isMuted(): Boolean = bool("mute") ?: false

    fun setMuted(muted: Boolean) { if (!destroyed) mpv.setPropertyBoolean("mute", muted) }

    /** 0..100; audio focus ducking uses it. */
    fun setVolume(percent: Int) { if (!destroyed) mpv.setPropertyInt("volume", percent) }

    /** Crop-to-fill (panscan 1) vs letterbox (0): the "fullscreen" button on phones. */
    fun setFill(fill: Boolean) { if (!destroyed) mpv.setPropertyDouble("panscan", if (fill) 1.0 else 0.0) }

    fun isFill(): Boolean = (dbl("panscan") ?: 0.0) > 0.5

    /** Display aspect (width/height) of the current video, or null before it is known. */
    fun videoAspect(): Double? = dbl("video-params/aspect")?.takeIf { it > 0.0 }

    fun speed(): Double = dbl("speed") ?: 1.0

    /** Current mpv track id for "sub" or "audio" (0 when off / none). */
    fun currentTrackId(type: String): Int = int("current-tracks/$type/id") ?: 0

    /** Demuxer cache end in seconds (how far ahead playback can go without buffering). */
    fun cacheEndSeconds(): Double? = dbl("demuxer-cache-time")

    /** The file's frame rate (container), for the display's frame-rate hint. */
    fun containerFps(): Double? = dbl("container-fps")?.takeIf { it > 1.0 }

    /** Tell mpv the display's refresh rate (it cannot read it on Android). */
    fun setDisplayFps(fps: Float) = setStr("display-fps-override", "%.3f".format(java.util.Locale.ROOT, fps))

    private var summaryCache: Pair<Long, String>? = null

    /** "1080p H264 · hardware"; re-read at most every 3 s (four JNI reads). */
    fun videoSummary(): String {
        val now = android.os.SystemClock.elapsedRealtime()
        summaryCache?.let { (at, s) -> if (now - at < 3000 && s.isNotEmpty()) return s }
        val w = int("video-params/w")
        val h = int("video-params/h")
        val s = if (w == null || h == null) "" else {
            val codec = str("video-format") ?: ""
            val hw = str("hwdec-current")?.takeIf { it != "no" && it.isNotEmpty() }
            "${h}p ${codec.uppercase()}" + (if (hw != null) " · hardware" else " · software")
        }
        summaryCache = now to s
        return s
    }

    // ---- track selection ----

    // track-list is observed with no value: a change only drops the cache, and the next
    // read builds it again (about seven JNI reads per track, so not twice a second).
    // The list is kept with the generation it was read at (see GenerationCache): checking the
    // counter and then writing the cache let a change land in between and stay unseen.
    private val trackCache = GenerationCache<List<Track>>()

    fun tracks(): List<Track> {
        if (destroyed) return emptyList()
        trackCache.get()?.let { return it }
        val gen = trackCache.generation()
        val n = mpv.getPropertyInt("track-list/count") ?: 0
        val list = (0 until n).mapNotNull { i ->
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
        trackCache.put(gen, list)
        return list
    }

    /** [tracks] with the server's sniffed subtitle languages in place of the container tags. */
    fun tracksWithOverrides(): List<Track> {
        val overrides = subtitleLanguageOverrides
        return tracks().map { t ->
            val o = overrides[t.id]
            if (t.type == "sub" && o != null && o != t.lang) t.copy(lang = o) else t
        }
    }

    // Signs & songs tracks go by many names: "Signs/Songs", "Forced", "[FOR DUB]" (FMA:B),
    // "English [ForcedNarrative]" (Netflix rips), "Dubtitles".
    private val signsRegex = Regex("""(?i)\b(signs?|songs?|s&s|signs/songs|forced\w*|for dub|dubtitles?)\b""")
    private val fullRegex = Regex("""(?i)\b(full|dialog(ue)?|complete)\b""")

    /** "signs" / "full" / "" from a subtitle track's title: carries a viewer's pick to the next episode. */
    fun subtitleClass(t: Track): String = when {
        signsRegex.containsMatchIn(t.title) || t.isForced -> "signs"
        fullRegex.containsMatchIn(t.title) -> "full"
        else -> ""
    }

    private fun sameLang(a: String, b: String): Boolean = a.isNotEmpty() && b.isNotEmpty() && Languages.canonical(a) == Languages.canonical(b)

    /**
     * Picks the subtitle track from the language, the title, and (smart mode) what the
     * audio is. Release groups do not flag "full dialogue" vs "signs & songs" in any
     * machine-readable way, so the title is the signal; the file's default flag usually
     * points at the signs track for dub viewers.
     *
     * Smart mode: audio already in your language → anime gets the signs & songs track in
     * that language if there is one, anything else gets a forced track if there is one,
     * else no subtitles. Audio in another language → the dialogue track in your language.
     */
    fun chooseSubtitle(all: List<Track>, mode: String, langs: List<String>, audioLang: String = "", anime: Boolean = false): Track? {
        if (mode == "none") return null
        val subs = all.filter { it.type == "sub" }
        if (subs.isEmpty()) return null
        val subLang = langs.firstOrNull().orEmpty()
        val mine = subs.filter { sameLang(it.lang, subLang) }
        fun signs(t: Track) = signsRegex.containsMatchIn(t.title) || t.isForced
        fun full(t: Track) = fullRegex.containsMatchIn(t.title)
        fun styled(t: Track) = if (t.codec == "ass" || t.codec == "ssa") 5 else 0

        val dubbed = audioLang.isNotEmpty() && sameLang(audioLang, subLang)
        if (dubbed && mode != "full") {
            // Audio already in the viewer's language. "signs" pairs an anime dub with its
            // signs & songs track; both "signs" and "foreign" otherwise show at most a
            // forced track. Only "full" carries on to the dialogue track below.
            val pool = mine.ifEmpty { subs.filter { it.lang.isEmpty() } }
            if (anime && (mode == "signs" || mode == "smart")) {
                pool.filter { signs(it) }.maxByOrNull { styled(it) + (if (it.isDefault) 1 else 0) }?.let { return it }
            }
            return pool.firstOrNull { it.isForced }
        }
        // Dialogue in the preferred language (or the best guess when no track is tagged).
        fun score(t: Track): Int {
            var sc = 0
            sc += when { sameLang(t.lang, subLang) -> 100; t.lang.isEmpty() -> 20; else -> 0 }
            sc += when { signs(t) -> -60; full(t) -> 30; else -> 0 }
            sc += styled(t)
            if (t.isDefault) sc += 1
            return sc
        }
        return subs.maxByOrNull { score(it) }
    }

    /** The language mpv ended up playing, from the current audio track. */
    private fun currentAudioLang(): String {
        val aid = currentTrackId("audio")
        return tracks().firstOrNull { it.type == "audio" && it.id == aid }?.let { audioLangOf(it) }.orEmpty()
    }

    /**
     * An audio track's language: its tag, or, untagged (common on fansub dual-audio
     * releases), a language named in its title ("English 5.1", "Japanese").
     */
    fun audioLangOf(t: Track): String {
        if (t.lang.isNotEmpty() && t.lang != "und") return t.lang
        return Languages.all.firstOrNull { l -> Regex("""(?i)\b${Regex.escape(l.name)}\b""").containsMatchIn(t.title) }?.iso3.orEmpty()
    }

    /** The adb/diagnostics subtitle override; used for the first file only. */
    private var forceSid = options.forceSid

    private fun applyTrackPolicy() {
        val all = tracksWithOverrides()
        all.forEach { Timber.i("track %s id=%d lang=%s codec=%s default=%s title=%s", it.type, it.id, it.lang, it.codec, it.isDefault, it.title) }
        if (forceSid > 0) {
            setSubtitleTrack(forceSid)
            Timber.i("subtitle forced -> sid=%d", forceSid)
            forceSid = 0
            return
        }
        preselectedSid?.let { sid ->
            preselectedSid = null
            Timber.i("subtitle chosen before the load: sid=%s (now %d)", sid, currentTrackId("sub"))
            return
        }
        val audioLang = currentAudioLang()
        val sub = chooseSubtitle(all, options.subMode, options.subLangs, audioLang, options.anime)
        Timber.i("subtitle policy %s: audio=%s anime=%s -> %s", options.subMode, audioLang.ifEmpty { "?" }, options.anime, sub?.let { "sid=${it.id} '${it.title}' (${it.lang}/${it.codec})" } ?: "off")
        setSubtitleTrack(sub?.id ?: 0)
    }

    // ---- playback state (for progress reporting / OSD) ----

    fun timePosSeconds(): Double? = dbl("time-pos")

    fun durationSeconds(): Double? = dbl("duration")

    fun isPaused(): Boolean = pausedFlag

    /** Waiting on the network, a seek or a load (not a pause the viewer asked for). */
    fun isBuffering(): Boolean = lastBuffering

    /** ffmpeg stream index of the selected track of [type] ("audio" | "sub"); matches Roxy's stream `index`. */
    fun currentStreamIndex(type: String): Int? = int("current-tracks/$type/ff-index")

    /**
     * The mpv id of the file's own [type] track at stream [index] (the server's numbering,
     * what [currentStreamIndex] reports), or null: none, or only an added (external) one.
     */
    fun trackIdForStreamIndex(type: String, index: Int): Int? {
        val n = int("track-list/count") ?: return null
        for (i in 0 until n) {
            if (str("track-list/$i/type") != type || bool("track-list/$i/external") == true) continue
            if (int("track-list/$i/ff-index") == index) return int("track-list/$i/id")
        }
        return null
    }

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

    // Every read and command checks [destroyed] first: after destroy() the native handle is gone.
    private fun str(p: String) = if (destroyed) null else mpv.getPropertyString(p)
    private fun int(p: String) = if (destroyed) null else mpv.getPropertyInt(p)
    private fun dbl(p: String) = if (destroyed) null else mpv.getPropertyDouble(p)
    private fun bool(p: String) = if (destroyed) null else mpv.getPropertyBoolean(p)
    private fun setStr(p: String, v: String) { if (!destroyed) mpv.setPropertyString(p, v) }
    private fun cmd(vararg args: String) { if (!destroyed) mpv.command(arrayOf(*args)) }

    // ---- surface ----
    //
    // The same three calls mpv-android makes. mpv 0.41 syncs the window's buffer
    // geometry on every resize itself (vo/context_android, "fix surface tearing on
    // resize"); the one frame it still draws into the previous buffer is why a host
    // should pin the surface's size across animated transitions (see PlayerSession.pinSurface)
    // rather than resize it mid-flight.

    override fun surfaceCreated(holder: SurfaceHolder) {
        if (destroyed) return
        Timber.d("surfaceCreated")
        // The size goes in before the vo comes up: the vo lays the picture out for the size
        // it sees at creation, and after a page/app switch that was the previous surface's.
        val frame = holder.surfaceFrame
        if (frame.width() > 0 && frame.height() > 0) applySurfaceSize(frame.width(), frame.height())
        surface = holder.surface
        mpv.attachSurface(holder.surface)
        mpv.setPropertyString("force-window", "yes")
        surfaceReady = true
        // Always bring the vo back: surfaceDestroyed set it to null, and a file loaded while
        // the surface was away (a reload, the next episode) would otherwise play with no picture.
        mpv.setPropertyString("vo", activeVo)
        applyFrameRate()
        loadPending()
    }

    override fun surfaceChanged(holder: SurfaceHolder, format: Int, width: Int, height: Int) {
        if (destroyed) return
        Timber.d("surfaceChanged %dx%d", width, height)
        applySurfaceSize(width, height)
        // mpv draws its first frame at a new size into a buffer that is still the old size
        // (the buffer was taken before the size changed). Playing, the next frame replaces it;
        // paused, it stayed: after a rotation the picture was a zoomed-in corner, or squeezed
        // into part of the screen. Once the resize has settled, draw the frame again.
        main.removeCallbacks(redrawAfterResize)
        main.postDelayed(redrawAfterResize, 150)
    }

    /** Setting the size again makes mpv lay out and draw the current frame once more. */
    private val redrawAfterResize = Runnable {
        val (w, h) = lastSurface ?: return@Runnable
        if (!destroyed && surfaceReady) mpv.setPropertyString("android-surface-size", "${w}x$h")
    }

    override fun surfaceDestroyed(holder: SurfaceHolder) {
        if (destroyed || !surfaceReady) return
        Timber.d("surfaceDestroyed")
        releaseSurface()
    }

    private fun releaseSurface() {
        main.removeCallbacks(redrawAfterResize)
        surfaceReady = false
        mpv.setPropertyString("vo", "null")
        mpv.setPropertyString("force-window", "no")
        mpv.detachSurface()
        surface = null
    }

    /** The last size handed to mpv; [redrawAfterResize] sets it again. */
    private var lastSurface: Pair<Int, Int>? = null

    private fun applySurfaceSize(width: Int, height: Int) {
        mpv.setPropertyString("android-surface-size", "${width}x$height")
        lastSurface = width to height
    }

    // ---- display frame rate ----

    private var frameRate = 0f
    private val frameRateGate = FrameRateGate()

    /**
     * Ask the display for a mode that fits [fps] (24p on a TV that has it, a 120 Hz phone
     * dropping to 120 = 5 x 24). 0 clears the request. TVs may switch modes (a moment of
     * black, as every TV app does); phones only switch when it is seamless.
     */
    fun hintFrameRate(fps: Float) {
        frameRateGate.hint(fps)?.let { frameRate = it; applyFrameRate() }
    }

    private fun applyFrameRate() {
        if (Build.VERSION.SDK_INT < 30) return
        val s = surface?.takeIf { it.isValid } ?: return
        runCatching {
            if (Build.VERSION.SDK_INT >= 31) {
                s.setFrameRate(frameRate, Surface.FRAME_RATE_COMPATIBILITY_FIXED_SOURCE,
                    if (options.tv) Surface.CHANGE_FRAME_RATE_ALWAYS else Surface.CHANGE_FRAME_RATE_ONLY_IF_SEAMLESS)
            } else {
                s.setFrameRate(frameRate, Surface.FRAME_RATE_COMPATIBILITY_FIXED_SOURCE)
            }
        }.onFailure { Timber.w(it, "setFrameRate %.3f", frameRate) }
    }

    // ---- native HDR ----

    /**
     * HDR10/HLG straight to the screen: hardware decoding into the surface with
     * mediacodec_embed, so the TV's own HDR pipeline shows it. libass cannot draw there,
     * so this is opt-in (settings) and the controls say subtitles are off.
     */
    private fun maybeNativeHdr(gamma: String) {
        if (!options.hdrNative || !options.hdrDisplay || hdrActive) return
        if (gamma != "pq" && gamma != "hlg") return
        Timber.i("native HDR output for %s video", gamma)
        hdrActive = true
        activeVo = "mediacodec_embed"
        mpv.setPropertyString("hwdec", "mediacodec")
        if (surfaceReady) mpv.setPropertyString("vo", activeVo)
        listener?.onHdrNative(true)
    }

    /** A new file starts from the normal (tone-mapped, subtitled) output. */
    private fun leaveNativeHdr() {
        if (!hdrActive) return
        hdrActive = false
        activeVo = options.vo
        mpv.setPropertyString("hwdec", mpvHwdec(options.hwdec))
        if (surfaceReady) mpv.setPropertyString("vo", activeVo)
        listener?.onHdrNative(false)
    }

    // ---- events (libmpv's event thread: record, then post) ----

    override fun eventProperty(property: String) {
        if (property == "track-list") trackCache.invalidate()
    }
    override fun eventProperty(property: String, value: Long) {}
    override fun eventProperty(property: String, value: Double) {}
    override fun eventProperty(property: String, value: String) {
        if (property == "video-params/gamma") post { maybeNativeHdr(value) }
    }
    override fun eventProperty(property: String, value: Boolean) {
        when (property) {
            "pause" -> { pausedFlag = value; post { listener?.onPauseChanged(value) } }
            "paused-for-cache" -> cacheFlag = value
            "seeking" -> seekingFlag = value
            "core-idle" -> idleFlag = value
            "eof-reached" -> { ended = value; if (value) post { listener?.onEofReached() } }
        }
        // core-idle is also true at the end of a file (keep-open holds it): not buffering.
        val buffering = cacheFlag || seekingFlag || (idleFlag && !pausedFlag && !ended)
        if (buffering != lastBuffering) {
            lastBuffering = buffering
            post { listener?.onBufferingChanged(buffering) }
        }
    }

    /** eof-reached: the file has played out (keep-open holds the last frame). */
    @Volatile private var ended = false

    override fun event(eventId: Int) {
        when (eventId) {
            MPVLib.MpvEvent.MPV_EVENT_START_FILE -> {
                queuedLoads.updateAndGet { (it - 1).coerceAtLeast(0) }
                loading = true
                ended = false
                post { frameRateGate.fileStarted(); leaveNativeHdr() }
            }
            MPVLib.MpvEvent.MPV_EVENT_PLAYBACK_RESTART -> post { frameRateGate.firstFrame()?.let { frameRate = it; applyFrameRate() } }
            MPVLib.MpvEvent.MPV_EVENT_FILE_LOADED -> {
                loading = false
                post { onFileLoaded() }
            }
            MPVLib.MpvEvent.MPV_EVENT_END_FILE -> {
                val failed = loading && queuedLoads.get() == 0
                loading = false
                if (failed && lastLoadUsedHeader && tokenInHeader && lastOpenHttp4xx) {
                    // An older server: it only takes the token in the URL.
                    Timber.w("open with the token header failed; retrying with it in the URL")
                    tokenInHeader = false
                    post { if (pendingUrl == null) { pendingUrl = lastUrl; if (surfaceReady) loadPending() } }
                } else if (failed) {
                    Timber.w("the file failed to open: %s", lastUrl?.substringBefore("?"))
                    post { listener?.onLoadFailed() }
                } else {
                    post { listener?.onEndFile() }
                }
            }
        }
    }

    /** Main thread. */
    private fun onFileLoaded() {
        Timber.i("file loaded: codec=%s hwdec=%s start-opt=%s time-pos=%s", str("video-codec"), str("hwdec-current"), str("start"), str("time-pos"))
        summaryCache = null
        // Belt and braces: the `start` property is set before the load, but seek anyway if
        // the file came up at 0 (seen on the refactored player; cheap to be sure).
        if (startSeconds > 0 && (dbl("time-pos") ?: 0.0) < startSeconds - 2.0) {
            seekTo(startSeconds.toDouble())
        }
        applyTrackPolicy()
        listener?.onFileLoaded()
        if (spdif.isNotEmpty()) watchPassthrough()
    }

    private var watchdog: Runnable? = null
    private var passthroughWatch: PassthroughWatch? = null

    private fun stopPassthroughWatch() {
        watchdog?.let { main.removeCallbacks(it) }
        watchdog = null
        passthroughWatch?.cancel()
        passthroughWatch = null
    }

    /**
     * Safety net: some TVs never finish opening a passthrough audio output for a format
     * (the budget TV with a soundbar on E-AC3). mpv then waits on the audio clock and the picture
     * stays black. If the position hasn't moved after 6 s of the player actually trying
     * to play (time paused by the viewer, buffering or seeking does not count), that codec
     * is decoded here from then on, for this sound output, and the file reopens where it was.
     */
    private fun watchPassthrough() {
        stopPassthroughWatch()
        val codec = str("current-tracks/audio/codec")?.lowercase()
        if (codec == null || codec !in spdif) return
        val url = lastUrl ?: return
        val w = PassthroughWatch(url, lastTranscoded, dbl("time-pos") ?: 0.0)
        passthroughWatch = w
        val r = object : Runnable {
            override fun run() {
                if (destroyed || passthroughWatch !== w) return
                when (val v = w.poll(dbl("time-pos"), pausedFlag || cacheFlag || seekingFlag)) {
                    PassthroughWatch.Verdict.Wait -> main.postDelayed(this, PassthroughWatch.POLL_MS)
                    PassthroughWatch.Verdict.Done -> {}
                    is PassthroughWatch.Verdict.Failed -> passthroughFailed(w, codec, v.at)
                }
            }
        }
        watchdog = r
        main.postDelayed(r, PassthroughWatch.POLL_MS)
    }

    private fun passthroughFailed(w: PassthroughWatch, codec: String, at: Double) {
        Timber.w("passthrough of %s did not start (pos %.1f); decoding it on the TV instead", codec, at)
        passthroughWatch = null; watchdog = null
        brokenSpdif += "${options.outputKey}|$codec"
        // Remembered for this sound output, so later plays decode it at once.
        audioPrefs.edit().putStringSet(brokenKey, (audioPrefs.getStringSet(brokenKey, emptySet()).orEmpty() + codec)).apply()
        spdif = spdif - codec
        mpv.setPropertyString("audio-spdif", spdif.joinToString(","))
        // The session reopens it as one of its reloads; replaced behind its back, the old
        // file's END_FILE looked like a dropped stream (a false "Lost connection" and a
        // reconnect) or, with no length known, the end of the episode.
        if (listener?.onPassthroughFailed(at) == true) return
        play(w.url, at.toInt(), w.transcoded)
    }

    override fun logMessage(prefix: String, level: Int, text: String) {
        // Every line is scrubbed of media tokens / keys before it is kept or logged.
        val line = redactSecrets(text.trimEnd())
        if (line.isEmpty()) return
        if (HTTP_4XX.containsMatchIn(line)) lastOpenHttp4xx = true
        val interesting = prefix.contains("libass") || prefix.contains("sub") ||
            line.contains("fontselect") || line.contains("font provider") ||
            line.contains("Glyph") || line.contains("attachment", ignoreCase = true) ||
            line.contains("hwdec", ignoreCase = true) || line.contains("Using hardware decoding") ||
            line.contains("Could not open", ignoreCase = true) || prefix.startsWith("ao") || prefix.startsWith("af")
        if (interesting) {
            synchronized(fontLog) {
                fontLog.addLast("[$prefix] $line")
                while (fontLog.size > fontLogMax) fontLog.removeFirst()
            }
        }
        // Only warnings and errors reach logcat (mpv prints its own copy when verbose).
        if (level <= MPVLib.MpvLogLevel.MPV_LOG_LEVEL_WARN) Timber.w("mpv[%s] %s", prefix, line)
    }

    // ---- lifecycle ----

    /** True once [destroy] ran; every property read after that returns null. */
    @Volatile var destroyed = false
        private set

    /**
     * Tear down. The surface is let go here on the main thread (while the window still has
     * it); mpv itself is destroyed on a background thread, because mpv_terminate_destroy
     * waits for its demuxer and a read stuck on a dead server would freeze the UI (ANR).
     */
    fun destroy() {
        if (destroyed) return
        stopPassthroughWatch()
        if (surfaceReady) runCatching {
            if (frameRate != 0f) { frameRate = 0f; applyFrameRate() }
            releaseSurface()
        }
        destroyed = true
        listener = null
        main.removeCallbacksAndMessages(null)
        val m = mpv
        val self = this
        Thread({
            runCatching { m.removeLogObserver(self) }
            runCatching { m.removeObserver(self) }
            runCatching { m.command(arrayOf("stop")) }
            runCatching { m.destroy() }.onFailure { Timber.w(it, "mpv destroy") }
            Timber.i("mpv destroyed")
        }, "mpv-destroy").start()
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

    // Last in the class, on purpose: mpv calls back on its own thread, and MPVLib adds the
    // observer and dispatches to it under one lock, so that thread is only guaranteed to see
    // the fields written BEFORE addObserver. Fields declared below the old place of this
    // (trackGen, queuedLoads, ...) were written after it: on the TV (ARM) the event thread
    // still saw trackGen as null 20 minutes in, at the first track-list change of a teardown
    // ("Go to Details"), and the NullPointerException aborted the app (it came back at Home).
    // Kotlin runs initialisers in order: here, every field is set. Keep no field below.
    init {
        mpv.addObserver(this)
        mpv.addLogObserver(this)
        for ((name, fmt) in observed) mpv.observeProperty(name, fmt)
    }
}
