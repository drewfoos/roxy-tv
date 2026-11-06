package tv.roxy.tv.player

import android.content.Context
import android.hardware.display.DisplayManager
import android.os.Handler
import android.os.Looper
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.async
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import timber.log.Timber
import tv.roxy.tv.tv.WatchNext
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.MediaSegment
import tv.roxy.tv.api.subtitleInfos
import tv.roxy.tv.data.PlayerSettings
import tv.roxy.tv.diagnostics.DeviceReport
import tv.roxy.tv.ui.PlayRequest
import tv.roxy.tv.ui.nextEpisode

/**
 * One playback: libmpv plus everything the controls need (position, tracks, segments,
 * the next-episode card, progress reports, the stats overlay), independent of where the
 * video is drawn. [PlayerActivity] hosts a session full-screen; the phone's episode page
 * hosts the same session in a 16:9 box that fills the screen when the phone turns. The
 * session is the one player; the hosts only differ in how they answer [Host].
 *
 * Runs on the main thread: [MpvPlayer] posts its listener calls there.
 */
class PlayerSession(
    private val context: Context,
    private val app: RoxyApp,
    val params: Params,
    private val scope: CoroutineScope,
    var host: Host,
) {
    /** What one playback starts with; the extras of [PlayerActivity.intent] in a bag. */
    data class Params(
        val url: String,
        val title: String,
        val subtitleLine: String,
        val itemId: String?,
        val sourceId: String?,
        val startSeconds: Int,
        val local: Boolean,
        val hwdec: String,
        val vo: String,
        val subMode: String,
        val forceSid: Int,
        /** mpv `alang` list (every spelling of each preferred language). */
        val audioLangs: List<String>,
        /** mpv `slang` list. */
        val subLangs: List<String>,
        val anime: Boolean,
        val showStats: Boolean,
    ) {
        companion object {
            fun from(req: PlayRequest, settings: PlayerSettings) = Params(
                url = req.url, title = req.title, subtitleLine = req.subtitleLine, itemId = req.item.id, sourceId = req.sourceId,
                startSeconds = req.startSeconds, local = req.local, hwdec = settings.hwdec, vo = settings.vo, subMode = settings.subMode,
                forceSid = 0, audioLangs = settings.mpvAudioLangs, subLangs = settings.mpvSubLangs, anime = req.item.isAnime, showStats = settings.showStats,
            )
        }
    }

    /** What a host (an activity or a page) does for the session. */
    interface Host {
        /** The back arrow / Back key with nothing of the player's own to close. */
        fun back()
        /** The Cast button: the phone opens its "Play on" picker; the TV has none. */
        fun cast() {}
        val pipSupported: Boolean get() = false
        val inPip: Boolean get() = false
        fun enterPip() {}
        /** Playing state changed: the activity keeps its PiP auto-enter in step. */
        fun playingChanged(playing: Boolean) {}
        /** The next episode took over the player (autoplay or Next): a page updates its text. */
        fun nowPlaying(req: PlayRequest) {}
        /** Page hosts: null when the player owns the screen; else whether it is fullscreen now. */
        val fullscreen: Boolean? get() = null
        fun toggleFullscreen() {}
    }

    companion object {
        /**
         * Watch-state calls go to the server one at a time, in the order they were made.
         * Moving on to the next episode sends "E1 is at 6:18" and "E1 is watched" together;
         * if the watched mark landed first, the late position turned E1 into a watched
         * episode that is also part-way through (Continue Watching showed it as watched).
         * app.scope runs launches at once on the main thread, so each takes the lock in
         * call order, and the lock is first come, first served.
         */
        private val reportLock = Mutex()
        private const val AUTO_HIDE_MS = 4000L
        private const val PROGRESS_INTERVAL_MS = 10_000L
        /** Seconds the next-episode card counts down before it plays (Netflix uses about this). */
        private const val NEXT_COUNTDOWN_S = 10
        /** A scrub commits this long after the last Left/Right press. */
        private const val SCRUB_COMMIT_MS = 650L
        /** Waits between reconnect attempts after the stream drops (then it waits for the server). */
        private val RECONNECT_DELAYS_MS = longArrayOf(0, 2_000, 4_000, 8_000, 15_000, 30_000)
        private const val LOST_CONNECTION = "Lost connection to Roxy. Reconnecting…"
        private const val WAITING_FOR_SERVER = "Can't reach Roxy. Playback resumes when it's back (or press play to retry)."
        /** Ticks (500 ms) of buffering before the stall is looked into, and before a reachable server gets a fresh stream. */
        private const val STALL_CHECK_TICKS = 16
        private const val STALL_RESTART_TICKS = 90
    }

    val player: MpvPlayer
    private val cpu = CpuSampler()
    private val main = Handler(Looper.getMainLooper())
    private val settings = app.prefs.settings.value
    private val isTv = DeviceReport.isTelevision(context)

    var statsLines by mutableStateOf<List<String>>(emptyList()); private set
    var fontLog by mutableStateOf<List<String>>(emptyList()); private set
    var showStats by mutableStateOf(params.showStats); private set
    var osd by mutableStateOf<String?>(null); private set
    private var osdJob: Job? = null
    var ui by mutableStateOf(PlayerUiState()); private set
    var controlsVisible by mutableStateOf(true); private set
    var sheet by mutableStateOf(Sheet.None)
    private var hideJob: Job? = null
    var trickplay by mutableStateOf<TrickplayPreview?>(null); private set
    var doubleTapFlash by mutableStateOf(0); private set // -10 / +10 while the ripple shows
    /** The SurfaceView the video draws into (set by [PlayerSurface]); null for a host without one yet. */
    @Volatile var surfaceView: android.view.SurfaceView? = null

    /**
     * Pin the surface's buffer at its current size while the window animates (the
     * picture-in-picture shrink and expand). The view may change size freely; the
     * compositor scales the fixed buffer into it, the way a decoder-fed surface is
     * scaled, and mpv never hears about a new size, so it never draws the one frame it
     * otherwise draws into the previous buffer. Unpinning returns the surface to the
     * view's size; when the two already agree nothing happens.
     */
    fun pinSurface(on: Boolean) {
        val sv = surfaceView ?: return
        if (on) { if (sv.width > 0 && sv.height > 0) sv.holder.setFixedSize(sv.width, sv.height) } else sv.holder.setSizeFromLayout()
    }
    private var flashJob: Job? = null
    private var lastSid = 0
    private var autoSkip = false
    private var segments: List<MediaSegment> = emptyList()
    /** Segments auto-skipped this playback, by type and start (an episode can have two of a type). */
    private val skipped = HashSet<String>()
    private fun segKey(s: MediaSegment) = "${s.type}@${s.startSec}"
    var title = params.title; private set
    var subtitleLine = params.subtitleLine; private set
    val hwdec = params.hwdec
    val vo = params.vo
    var itemId: String? = params.itemId; private set
    private var sourceId: String? = params.sourceId
    private var local = params.local

    // Quality: Original plays the file; a preset plays a server transcode of it. The choice
    // holds for the rest of this playback, next episodes included (downloads always play
    // their own file). A transcode is a live stream with no byte ranges: mpv can seek only
    // inside what it has buffered, so any other seek restarts the stream at the target.
    private var quality: QualityOption = QualityOption.Original
    /** True while [quality] came from the settings: then a file already smaller than it plays as-is. */
    private var qualityFromSettings = true
    private var transcoding = false
    private var directUrl = params.url
    private var sourceHeight = 0
    private var sourceDurationSec: Double? = null
    /** Where a restarted stream will begin; shown as the position until it plays. */
    @Volatile private var reloadTarget: Double? = null

    /**
     * Where a newly loaded file is meant to start, until mpv's clock gets there. mpv reports
     * 0 (or nothing) for a moment before the resume seek lands, and an intro marked from
     * 0:00 then flashed its Skip button on every resume; meanwhile the position shown is
     * the start and no segment is offered.
     */
    @Volatile private var pendingStart: Double? = null
    private var lastReportedMs = -1L
    @Volatile private var serverDownUntil = 0L
    @Volatile var destroyed = false; private set

    // Next-episode queue (Netflix): resolved in the background once an episode starts;
    // the card appears at the outro (or the last 15 s) and counts down when autoplay is on.
    private var autoplay = true
    private var skipWindowSec = 15
    private var nextCardEnabled = false
    private var next: PlayRequest? = null
    var nextCard by mutableStateOf<NextCard?>(null); private set
    private var nextCardDismissed = false
    private var countdownJob: Job? = null
    /** True from "load the next file" until it has loaded, so the old file's end events are ignored. */
    @Volatile private var switching = false
    /** This file's end was reported (EOF and END_FILE both arrive at the end). */
    private var endReported = false
    // The age rating card: what it says, and whether it is up (a few seconds per title).
    private var advisory: Advisory? = null
    private var advisoryUp = false
    private var advisoryJob: kotlinx.coroutines.Job? = null

    // Lost connection: the stream ended (or would not open) well before the end of the file.
    private var reconnectAttempts = 0
    private var reconnecting = false
    private var reconnectJob: Job? = null
    /** After the quick attempts: waits for the server to answer, then reopens at [waitingAt]. */
    private var waitJob: Job? = null
    private var waitingAt: Double? = null
    /** Half-second ticks the stream has been buffering with nothing else going on. */
    private var stallTicks = 0
    private var stallShown = false

    // D-pad scrubbing: Left/Right move a preview position (trickplay), one seek on release.
    private var scrubTarget: Double? = null
    private var scrubJob: Job? = null

    /**
     * The viewer's own track picks, carried to the next episode by language and kind (the
     * same mpv id can be a different track there). Null = the policy decides.
     */
    private data class TrackPick(val off: Boolean, val lang: String, val kind: String, val title: String)
    private var subPick: TrackPick? = null
    private var audioPick: TrackPick? = null

    private val glue: PlaybackGlue
    private var lastGlueUpdate = 0L
    private val displayManager = context.getSystemService(DisplayManager::class.java)
    private val displayListener = object : DisplayManager.DisplayListener {
        override fun onDisplayAdded(displayId: Int) {}
        override fun onDisplayRemoved(displayId: Int) {}
        // A frame-rate hint switched the TV's mode (or the user did): tell mpv the new rate.
        override fun onDisplayChanged(displayId: Int) {
            if (destroyed) return
            val fps = DeviceReport.refreshRate(context)
            Timber.i("display now %.3f Hz", fps)
            player.setDisplayFps(fps)
        }
    }

    val actions: PlayerActions

    init {
        autoSkip = settings.autoSkip
        autoplay = settings.autoplay
        skipWindowSec = settings.skipWindowSec
        nextCardEnabled = settings.nextCard
        val passthrough = AudioPassthrough.forSetting(context, settings.passthrough)
        player = MpvPlayer(
            context = context,
            options = MpvPlayer.Options(
                hwdec = params.hwdec, vo = params.vo, startSeconds = params.startSeconds, subMode = params.subMode, forceSid = params.forceSid,
                audioLangs = params.audioLangs, subLangs = params.subLangs, anime = params.anime,
                spdif = passthrough.map { it.mpv },
                outputKey = if (passthrough.isEmpty()) "" else AudioPassthrough.outputKey(context),
                smoothMotion = settings.smoothMotion,
                hdrNative = settings.hdrNative,
                hdrDisplay = DeviceReport.hdrDisplay(context),
                tv = isTv,
                lowRam = DeviceReport.lowRam(context),
                verboseLog = params.showStats,
            ),
            fontsDir = app.fontsDir,
            displayRefreshRate = DeviceReport.refreshRate(context),
        )
        player.listener = object : MpvPlayer.Listener {
            override fun onFileLoaded() {
                switching = false
                endReported = false
                if (reconnecting) { reconnecting = false; flashOsd(null) }
                stallTicks = 0; stallShown = false; waitingAt = null
                reconnectAttempts = 0
                applyTrackPicks()
                // Ask the display for the film's rate (24p on a TV that has it).
                player.containerFps()?.let { player.hintFrameRate(it.toFloat()) }
                itemId?.let { app.remote.reportState(it, title, 0, false) } // the remote bar shows it at once
                refreshUi(forceGlue = true)
            }

            override fun onEndFile() {
                if (switching || endReported) return // the old file giving way to the next one
                // A stream that ends with an error well before the end of the file (server
                // restarted, connection reset) is a dropped connection, not the end.
                val dur = knownDuration()
                val pos = player.timePosSeconds() ?: ui.positionSec
                if (!local && dur != null && pos < dur - 10.0) {
                    Timber.w("stream closed at %.1f of %.1f s: reconnecting", pos, dur)
                    reconnect(pos)
                    return
                }
                Timber.i("end of file")
                endReported = true
                reportProgress("end")
            }

            override fun onLoadFailed() {
                if (local) { switching = false; reloadTarget = null; flashOsd("This download could not be played", 6000); showControls(); return }
                val at = reloadTarget ?: pendingStart ?: player.timePosSeconds() ?: 0.0
                reconnect(at)
            }

            override fun onEofReached() {
                if (switching) return
                handleEof()
            }

            override fun onPauseChanged(paused: Boolean) {
                refreshUi(forceGlue = true)
                if (paused) showControls() else scheduleHide()
                // keep-open pauses at the end of the file: that is the end, not a pause.
                if (paused && atEnd()) return
                reportProgress(if (paused) "pause" else "resume")
            }

            override fun onBufferingChanged(buffering: Boolean) = refreshUi(forceGlue = true)

            override fun onHdrNative(active: Boolean) {
                if (active) flashOsd("Native HDR: styled subtitles are off for this video", 5000)
            }
        }
        glue = PlaybackGlue(context, object : PlaybackGlue.Controls {
            override fun play() { if (!retryAfterGivingUp()) player.setPaused(false); refreshUi(forceGlue = true) }
            override fun pause() { player.setPaused(true); refreshUi(forceGlue = true) }
            override fun seekTo(seconds: Double) = actions.seekTo(seconds)
            override fun seekBy(seconds: Int) = actions.seekBy(seconds)
            override fun next() = playNext()
            override fun stop() { reportProgress("stop"); host.back() }
            override fun duck(on: Boolean) = player.setVolume(if (on) 40 else 100)
            override fun isPlaying(): Boolean = !player.isPaused()
        })
        runCatching { displayManager?.registerDisplayListener(displayListener, main) }
        quality = QualityOption.byKey(if (onMeteredNetwork()) settings.qualityMobile else settings.qualityWifi)
        prepareAndPlay(params.url, params.sourceId, params.local, params.startSeconds)
        showAdvisory(params.itemId)
        app.remote.activePlayer = this
        app.server.playerActive.value = true

        actions = object : PlayerActions {
            // While waiting for the server, Play means "try now".
            override fun togglePause() { if (!retryAfterGivingUp()) player.togglePause() }
            override fun seekTo(seconds: Double) {
                cancelScrub()
                // A manual seek backwards into a segment re-arms its auto-skip.
                segments.filter { seconds < it.startSec }.forEach { skipped.remove(segKey(it)) }
                seek(seconds); refreshUi(forceGlue = true)
            }
            override fun seekBy(seconds: Int) { cancelScrub(); seekRelative(seconds); refreshUi(forceGlue = true) }
            override fun scrub(direction: Int, repeat: Int) = this@PlayerSession.scrub(direction, repeat)
            override fun commitScrub(): Boolean = this@PlayerSession.commitScrub()
            override fun setQuality(key: String) { changeQuality(QualityOption.byKey(key)) }
            override fun selectSubtitle(id: Int) {
                if (id > 0) lastSid = id
                player.setSubtitleTrack(id)
                subPick = pickOf(id, "sub")
                refreshUi()
            }
            override fun selectAudio(id: Int) {
                player.setAudioTrack(id)
                audioPick = pickOf(id, "audio")
                subPick = null // the subtitles follow the audio again (the policy just re-ran)
                refreshUi()
            }
            override fun setSpeed(speed: Double) { player.setSpeed(speed); refreshUi(forceGlue = true) }
            override fun toggleMute() { player.setMuted(!player.isMuted()); refreshUi() }
            override fun toggleCaptions() {
                // CC button: off <-> the last chosen track (or the first one).
                val current = player.currentTrackId("sub")
                val target = if (current > 0) { lastSid = current; 0 } else lastSid.takeIf { it > 0 } ?: ui.subtitleTracks.firstOrNull()?.id ?: 0
                player.setSubtitleTrack(target)
                subPick = pickOf(target, "sub")
                refreshUi()
            }
            override fun toggleAutoSkip() {
                autoSkip = !autoSkip
                app.scope.launch { app.prefs.saveSettings(app.prefs.settings.value.copy(autoSkip = autoSkip)) }
                refreshUi()
            }
            override fun toggleFill() { player.setFill(!player.isFill()); refreshUi() }
            override fun enterPip() { host.enterPip() }
            override fun toggleFullscreen() { host.toggleFullscreen() }
            override fun next() { playNext() }
            override fun back() { host.back() }
            override fun cast() { host.cast() }
        }
        scheduleHide()

        // Controls state (cheap now: tracks are cached until mpv says they changed), the
        // stats overlay (only while it is on) and periodic progress.
        scope.launch {
            var tick = 0
            while (!destroyed) {
                delay(500)
                tick++
                refreshUi()
                watchStall()
                if (tick % 2 != 0) continue
                if (showStats) {
                    val cores = Runtime.getRuntime().availableProcessors()
                    runCatching { player.snapshot() }.getOrNull()?.let { snap ->
                        statsLines = snap.lines(cpu.sample(), cores)
                        fontLog = snap.fontLog
                    }
                }
                if ((tick / 2) % (PROGRESS_INTERVAL_MS / 1000).toInt() == 0 && !player.isPaused()) reportProgress("tick")
            }
        }
    }

    // ---- gestures the surface reports ----

    fun onTap() { if (controlsVisible && sheet == Sheet.None) hideControls() else showControls() }

    fun onDoubleTap(back: Boolean) {
        actions.seekBy(if (back) -10 else 10)
        doubleTapFlash = if (back) -10 else 10
        flashJob?.cancel()
        flashJob = scope.launch { delay(700); doubleTapFlash = 0 }
    }

    fun openSheet(s: Sheet) { sheet = s; showControls() }

    /** The stats overlay; mpv logs verbosely only while it is up. */
    fun toggleStats() {
        showStats = !showStats
        player.setVerboseLog(showStats)
    }

    // ---- scrubbing (TV remote) ----

    /**
     * Move the preview position [direction] (-1 / +1) one step; the step grows while the key
     * is held ([repeat] = the key's repeat count): 10 s, then 30 s, then 60 s. The seek
     * happens once, [SCRUB_COMMIT_MS] after the last press, or on OK.
     */
    fun scrub(direction: Int, repeat: Int) {
        val step = when { repeat < 6 -> 10.0; repeat < 20 -> 30.0; else -> 60.0 }
        val dur = ui.durationSec
        val from = scrubTarget ?: ui.positionSec
        var t = (from + direction * step).coerceAtLeast(0.0)
        if (dur > 0) t = t.coerceAtMost((dur - 1.0).coerceAtLeast(0.0))
        scrubTarget = t
        showControls()
        refreshUi()
        scrubJob?.cancel()
        scrubJob = scope.launch { delay(SCRUB_COMMIT_MS); commitScrub() }
    }

    /** Seek to the scrubbed position now; false when nothing was being scrubbed. */
    fun commitScrub(): Boolean {
        val t = scrubTarget ?: return false
        scrubJob?.cancel()
        scrubTarget = null
        actions.seekTo(t)
        return true
    }

    private fun cancelScrub() { scrubJob?.cancel(); scrubTarget = null }

    /**
     * Flash the title's age rating and content descriptors for a few seconds once it
     * plays. Episodes take the show's (they carry none); a download uses its snapshot.
     */
    private fun showAdvisory(id: String?) {
        advisoryJob?.cancel()
        advisory = null; advisoryUp = false
        if (id == null) return
        advisoryJob = scope.launch {
            suspend fun lookup(itemId: String): tv.roxy.tv.api.Item? =
                app.downloads.entry(itemId)?.item ?: app.items[itemId] ?: runCatching { app.api.item(itemId) }.getOrNull()
            val item = lookup(id) ?: return@launch
            val show = if (item.officialRating.isNullOrBlank() || item.advisories.isEmpty()) item.seriesId?.let { lookup(it) } else null
            val rating = (item.officialRating?.takeIf { it.isNotBlank() } ?: show?.officialRating)?.takeIf { it.isNotBlank() } ?: return@launch
            val descriptors = item.advisories.ifEmpty { show?.advisories.orEmpty() }
            advisory = Advisory(tv.roxy.tv.ui.phone.ageLabel(rating), descriptors)
            // Wait for the picture, then hold it long enough to read.
            var waited = 0
            while ((player.timePosSeconds() == null || switching) && waited < 40) { delay(250); waited++ }
            // The player opens with its controls up, which would hide the card: start the
            // count once they have gone (or after 20 s, if someone keeps them up).
            waited = 0
            while (controlsVisible && waited < 80) { delay(250); waited++ }
            delay(500)
            advisoryUp = true; refreshUi()
            delay(6_000)
            advisoryUp = false; refreshUi()
        }
    }

    /**
     * Fetch what the server knows about the source (segments, sniffed subtitle languages,
     * trickplay) and start the file. For a download the snapshot answers instead, so
     * playback never waits on the network. Then resolve the episode after this one.
     */
    private fun prepareAndPlay(url: String, sourceId: String?, local: Boolean, start: Int) {
        directUrl = url
        pendingStart = start.toDouble()
        sourceHeight = 0
        sourceDurationSec = null
        reloadTarget = null
        transcoding = false
        val wantTranscode = !local && sourceId != null && !quality.isOriginal && app.prefs.session.value.isLoggedIn
        val snapshot = if (local) itemId?.let { app.downloads.entry(it)?.item } else null
        if (snapshot != null) {
            val src = snapshot.primarySource
            segments = src?.segments.orEmpty()
            // The Quality menu names the file by its resolution ("1080p", "4K") offline too.
            sourceHeight = src?.streams?.firstOrNull { it.type == "video" }?.height ?: 0
            sourceDurationSec = src?.durationMs?.let { it / 1000.0 }
            player.subtitleLanguageOverrides = subtitleInfos(src?.streams.orEmpty()).filter { it.detectedLanguage != null }.associate { it.mpvId to it.effectiveLanguage }
            player.play(url, start)
            // Seek previews are a bonus when the server happens to be reachable.
            if (sourceId != null && app.prefs.session.value.isLoggedIn) scope.launch {
                val m = runCatching { app.api.trickplay(sourceId) }.getOrNull()
                if (m != null && m.sheets > 0 && this@PlayerSession.sourceId == sourceId) trickplay = TrickplayPreview(app.api, sourceId, m)
            }
        } else if (sourceId != null && app.prefs.session.value.isLoggedIn) {
            // What the server knows about the file: skippable segments (intro/outro), its
            // size and length for the Quality menu, and whether a preferred quality helps.
            val source = scope.async { runCatching { app.api.source(sourceId) }.onFailure { Timber.w(it, "source %s", sourceId) }.getOrNull() }
            scope.launch {
                val src = source.await() ?: return@launch
                if (this@PlayerSession.sourceId != sourceId) return@launch
                segments = src.segments
                segments.forEach { Timber.i("segment %s %.1f-%.1f s (%s)", it.type, it.startSec, it.endSec, it.origin) }
                sourceHeight = src.streams.firstOrNull { it.type == "video" }?.height ?: 0
                sourceDurationSec = src.durationMs?.let { it / 1000.0 }
            }
            // Seek-preview thumbnails: ask once now and, if the server is still generating
            // them (202), a few more times while the episode plays.
            scope.launch {
                repeat(6) { attempt ->
                    val m = runCatching { app.api.trickplay(sourceId) }.getOrNull()
                    if (this@PlayerSession.sourceId != sourceId) return@launch
                    if (m != null && m.sheets > 0) { trickplay = TrickplayPreview(app.api, sourceId, m); return@launch }
                    delay(if (attempt == 0) 5_000 else 20_000)
                }
            }
            // Ask the server which language each subtitle track really is, then play.
            scope.launch {
                runCatching { app.api.subtitleStreams(sourceId) }
                    .onSuccess { subs ->
                        subs.forEach { Timber.i("server sub mpvId=%d idx=%d tag=%s detected=%s title=%s", it.mpvId, it.index, it.language, it.detectedLanguage, it.title) }
                        player.subtitleLanguageOverrides = subs.filter { it.detectedLanguage != null }.associate { it.mpvId to it.effectiveLanguage }
                    }
                    .onFailure { Timber.w(it, "could not fetch subtitle languages for %s", sourceId) }
                // A quality from the settings is for saving data: a file already smaller
                // than it plays as-is. One picked in the menu is always honoured.
                val use = wantTranscode && (!qualityFromSettings || worthTranscoding(quality, source.await()))
                if (wantTranscode && !use) Timber.i("preferred quality %s skipped: the file is already smaller", quality.key)
                transcoding = use
                val mt = if (use) runCatching { app.api.mediaToken(sourceId) }.getOrNull() else null
                player.play(if (use && mt != null) app.api.transcodeUrl(sourceId, quality.height, quality.kbps, start.toDouble(), mt) else url, start, transcoded = use && mt != null)
            }
        } else {
            player.play(url, start)
        }
        // What comes after this one (null for movies and the last episode).
        val id = itemId ?: return
        scope.launch {
            val n = nextEpisode(context, id)
            if (itemId == id) { next = n; Timber.i("next episode: %s", n?.item?.displayLabel ?: "none"); refreshUi() }
        }
    }

    /** Switch to the resolved next episode on the same player (no restart, no black gap). */
    fun playNext() {
        val req = next ?: return
        if (switching) return
        cancelScrub()
        countdownJob?.cancel()
        nextCard = null
        nextCardDismissed = false
        // Moving on counts as finishing this one (Netflix): report the last position and mark it watched.
        if (!endReported) reportProgress("end")
        itemId?.let { finished -> markWatched(finished) }
        Timber.i("next: %s start=%ds local=%s", req.item.displayLabel, req.startSeconds, req.local)
        switching = true
        next = null
        itemId = req.item.id
        sourceId = req.sourceId
        local = req.local
        title = req.title
        subtitleLine = req.subtitleLine
        lastReportedMs = -1
        skipped.clear()
        segments = emptyList()
        trickplay = null
        flashOsd(null)
        prepareAndPlay(req.url, req.sourceId, req.local, req.startSeconds)
        showAdvisory(req.item.id)
        refreshUi(forceGlue = true)
        host.nowPlaying(req)
    }

    private fun markWatched(id: String) {
        if (!app.prefs.session.value.isLoggedIn) return
        app.scope.launch { WatchNext.remove(app, id) }
        app.scope.launch {
            reportLock.withLock {
                val ok = System.currentTimeMillis() >= serverDownUntil && runCatching { app.api.setPlayed(id, true) }.isSuccess
                if (ok) app.items.userDataChanged()
                if (app.downloads.entry(id) != null || !ok || app.downloads.hasPendingProgress(id)) app.downloads.recordProgress(id, 0, reportedToServer = ok, played = true)
            }
        }
    }

    // ---- the end, and connections that end early ----

    /** The length the file really has: the server's for a transcode (the stream has none of its own). */
    private fun knownDuration(): Double? =
        ((if (transcoding) sourceDurationSec else null) ?: player.durationSeconds() ?: sourceDurationSec)?.takeIf { it > 0 }

    private fun atEnd(): Boolean {
        val d = knownDuration() ?: return false
        val p = player.timePosSeconds() ?: return false
        return p >= d - 2.0
    }

    /**
     * mpv ran out of data. Near the end that is the end of the episode. Well before it, the
     * server went away (restart, Wi-Fi drop) after ffmpeg's own reconnects gave up: do not
     * mark the episode watched or jump to the next one; reopen where it stopped.
     */
    private fun handleEof() {
        val pos = player.timePosSeconds() ?: 0.0
        val dur = knownDuration()
        if (!local && dur != null && pos < dur - 10.0) {
            Timber.w("stream ended at %.1f of %.1f s: reconnecting", pos, dur)
            reconnect(pos)
            return
        }
        Timber.i("reached the end")
        if (!endReported) { endReported = true; reportProgress("end") }
        // Ran out while the card was up (or with no outro marker): go on now.
        if (autoplay && next != null) playNext()
    }

    /**
     * Reopen the stream at [at] after a short wait, a few times (about a minute in all, enough
     * for a server restart). Then pause, say so, and wait for the server to answer again,
     * however long that takes; it picks up at [at] by itself, or at once on play.
     */
    private fun reconnect(at: Double) {
        if (destroyed) return
        reconnectJob?.cancel()
        waitJob?.cancel(); waitingAt = null
        if (reconnectAttempts >= RECONNECT_DELAYS_MS.size) {
            Timber.w("reconnects used up at %.1f s; waiting for the server", at)
            reconnecting = false
            switching = false
            reloadTarget = null
            reconnectAttempts = 0
            waitForServer(at)
            return
        }
        val wait = RECONNECT_DELAYS_MS[reconnectAttempts++]
        reconnecting = true
        switching = true // the dropped stream's end events are not the end of the episode
        reloadTarget = at
        flashOsd(LOST_CONNECTION, 0)
        refreshUi(forceGlue = true)
        // The last position goes in now (kept locally if the server is away), so leaving
        // the player while it reconnects still resumes here.
        reportProgress("reconnect", at)
        reconnectJob = scope.launch {
            delay(wait)
            if (destroyed) return@launch
            Timber.i("reconnect %d at %.1f s", reconnectAttempts, at)
            if (transcoding) { restartTranscode(at); return@launch }
            // A fresh media token: a long session can outlive the one in the old URL, and a
            // restarted server has forgotten the old one.
            val sid = sourceId
            val url = if (sid != null && !local) runCatching { app.api.streamUrl(sid, app.api.mediaToken(sid)) }.getOrNull() ?: directUrl else directUrl
            directUrl = url
            player.play(url, at.toInt())
        }
    }

    /** Paused on [at] until the server answers (checked every 5 s), then reopened there. */
    private fun waitForServer(at: Double) {
        waitJob?.cancel()
        waitingAt = at
        player.setPaused(true)
        flashOsd(WAITING_FOR_SERVER, 0)
        showControls()
        waitJob = scope.launch {
            while (!destroyed) {
                delay(5_000)
                if (app.api.reachable()) {
                    Timber.i("server back; resuming at %.1f s", at)
                    resumeAfterWaiting()
                    return@launch
                }
            }
        }
    }

    private fun resumeAfterWaiting() {
        val at = waitingAt ?: return
        waitJob?.cancel()
        waitingAt = null
        flashOsd(null)
        player.setPaused(false)
        reconnect(at)
    }

    /** Play pressed while waiting for the server: try at once. */
    private fun retryAfterGivingUp(): Boolean {
        if (reconnecting || waitingAt == null) return false
        resumeAfterWaiting()
        return true
    }

    /**
     * A stream that sits buffering: mpv's ffmpeg retries a dead server quietly for minutes.
     * After 8 s, ask the server: if it is gone say so; if it stays stuck for 45 s while the
     * server answers (a restarted server dropped the transcode), open a fresh stream.
     */
    private fun watchStall() {
        val stalled = !local && !reconnecting && !switching && waitingAt == null && !player.isPaused() && player.isBuffering() && player.timePosSeconds() != null
        if (!stalled) {
            if (stallShown) { stallShown = false; if (osd == LOST_CONNECTION) flashOsd(null) }
            stallTicks = 0
            return
        }
        stallTicks++
        if (stallTicks == STALL_CHECK_TICKS) scope.launch {
            if (!app.api.reachable() && stallTicks >= STALL_CHECK_TICKS) {
                Timber.w("stream stalled and the server is unreachable")
                stallShown = true
                flashOsd(LOST_CONNECTION, 0)
            }
        }
        if (stallTicks == STALL_RESTART_TICKS) scope.launch {
            if (app.api.reachable() && stallTicks >= STALL_RESTART_TICKS) {
                val at = player.timePosSeconds() ?: return@launch
                Timber.w("stream stuck at %.1f s with the server up: reopening", at)
                stallTicks = 0
                reconnect(at)
            }
        }
    }

    // ---- the viewer's track picks, across episodes ----

    private fun pickOf(id: Int, type: String): TrackPick? {
        if (id <= 0) return if (type == "sub") TrackPick(off = true, lang = "", kind = "", title = "") else null
        val t = player.tracksWithOverrides().firstOrNull { it.type == type && it.id == id } ?: return null
        return TrackPick(off = false, lang = t.lang, kind = if (type == "sub") player.subtitleClass(t) else "", title = t.title)
    }

    /** On a new file: the viewer's earlier picks, matched by language and kind (title first). */
    private fun applyTrackPicks() {
        val tracks = player.tracksWithOverrides()
        audioPick?.let { p ->
            val audio = tracks.filter { it.type == "audio" && it.lang == p.lang }
            val t = audio.firstOrNull { it.title == p.title } ?: audio.firstOrNull()
            if (t != null && t.id != player.currentTrackId("audio")) { Timber.i("audio pick carried over -> %d", t.id); player.setAudioTrack(t.id) }
        }
        subPick?.let { p ->
            if (p.off) { player.setSubtitleTrack(0); return }
            val subs = tracks.filter { it.type == "sub" && it.lang == p.lang && player.subtitleClass(it) == p.kind }
            val t = subs.firstOrNull { it.title == p.title } ?: subs.firstOrNull() ?: return
            Timber.i("subtitle pick carried over -> %d", t.id)
            player.setSubtitleTrack(t.id)
            lastSid = t.id
        }
    }

    /** Put the card up; with autoplay the countdown runs (pausing while the video is paused) and then plays. */
    private fun showNextCard(req: PlayRequest) {
        val ep = req.item
        val thumb = app.downloads.image(app.api, ep, "thumb", 448)
        nextCard = NextCard(
            title = listOf(ep.episodeCode, ep.title).filter { it.isNotEmpty() }.joinToString("  "),
            thumbUrl = thumb,
            secondsLeft = if (autoplay) NEXT_COUNTDOWN_S else null,
        )
        if (!autoplay) return
        countdownJob?.cancel()
        countdownJob = scope.launch {
            for (s in NEXT_COUNTDOWN_S - 1 downTo 0) {
                delay(1000)
                while (ui.paused && !destroyed) delay(250)
                nextCard = nextCard?.copy(secondsLeft = s) ?: return@launch
            }
            playNext()
        }
    }

    fun dismissNextCard() { countdownJob?.cancel(); nextCard = null; nextCardDismissed = true }

    /** Pulls position, tracks and state from mpv into the controls' state. */
    fun refreshUi(forceGlue: Boolean = false) {
        // Callbacks posted by mpv (e.g. the pause from onStop) can land after destroy.
        if (player.destroyed || destroyed) return
        val tracks = runCatching { player.tracks() }.getOrDefault(emptyList())
        val wasPaused = ui.paused
        val timePos = player.timePosSeconds()
        if (timePos != null && !switching) reloadTarget = null
        pendingStart?.let { want -> if (timePos != null && !switching && timePos >= want - 3.0) pendingStart = null }
        val settling = pendingStart != null
        // A download plays from the phone; while the server is reachable the lower qualities
        // stream a transcode from it, and the top entry goes back to the file here.
        val offline = sourceId == null || !app.prefs.session.value.isLoggedIn
        ui = PlayerUiState(
            title = title,
            subtitle = subtitleLine,
            positionSec = reloadTarget ?: pendingStart ?: timePos ?: 0.0,
            // A transcode stream has no length of its own; the source knows it.
            durationSec = (if (transcoding) sourceDurationSec else null) ?: player.durationSeconds() ?: sourceDurationSec ?: 0.0,
            qualityOptions = QualityOption.forSource(sourceHeight).let { if (offline) it.take(1) else it },
            qualityKey = if (transcoding) quality.key else QualityOption.Original.key,
            qualityNote = when {
                local && offline -> "Downloads play from this device at their original quality."
                local -> "Downloaded: the top quality plays from this phone; lower ones stream from the server."
                else -> null
            },
            bufferedSec = player.cacheEndSeconds() ?: 0.0,
            paused = player.isPaused(),
            buffering = (player.isBuffering() || reconnecting || (switching && reloadTarget != null)) && !player.isPaused(),
            scrubSec = scrubTarget,
            muted = player.isMuted(),
            speed = player.speed(),
            fillScreen = player.isFill(),
            pipSupported = host.pipSupported,
            hasNext = next != null,
            nextCardShown = nextCard != null,
            advisory = if (advisoryUp) advisory else null,
            segments = if (settling) emptyList() else segments,
            autoSkip = autoSkip,
            skipWindowSec = skipWindowSec,
            subtitleTracks = tracks.filter { it.type == "sub" },
            audioTracks = tracks.filter { it.type == "audio" },
            currentSid = player.currentTrackId("sub"),
            currentAid = player.currentTrackId("audio"),
            quality = runCatching { player.videoSummary() }.getOrDefault(""),
            fullscreen = host.fullscreen,
        )
        if (wasPaused != ui.paused) host.playingChanged(!ui.paused)
        // The system's now-playing state: on changes, and every few seconds to keep it honest.
        val now = android.os.SystemClock.elapsedRealtime()
        if (forceGlue || wasPaused != ui.paused || now - lastGlueUpdate > 5_000) {
            lastGlueUpdate = now
            glue.update(title, subtitleLine, ui.durationSec, ui.positionSec, !ui.paused, ui.buffering, ui.speed, ui.hasNext)
        }
        // Auto-skip: jump to the end of a segment the first time the playhead enters it.
        if (autoSkip && !ui.paused) {
            ui.activeSegment?.let { seg ->
                if (skipped.add(segKey(seg))) {
                    Timber.i("auto-skip %s -> %.1f s", seg.type, seg.endSec)
                    seek(seg.endSec)
                    flashOsd("Skipped ${seg.type}")
                }
            }
        }
        // Next-episode card. Where it appears depends on what follows the credits:
        //  - outro running to the end of the file: at the outro's start (Play now = skip credits);
        //  - something after the outro (post-credits scene, season-ender epilogue): the Skip
        //    outro button handles the credits and the card waits for the next-episode
        //    `preview` segment, or the last 15 s when there is none;
        //  - no markers at all: the last 15 s.
        // An auto-skipped outro that ends the file simply reaches EOF and onEofReached moves on.
        // The card is opt-in (settings): without it, autoplay still moves on at the end of the file.
        val n = next
        if (nextCardEnabled && n != null && nextCard == null && !nextCardDismissed && !switching && !ui.paused && ui.durationSec > 0 && !host.inPip) {
            val outro = segments.firstOrNull { it.type == "outro" }
            val preview = segments.firstOrNull { it.type == "preview" }
            val outroEndsFile = outro != null && outro.endSec >= ui.durationSec - 5.0
            val triggerAt = when {
                outroEndsFile -> if (autoSkip) Double.MAX_VALUE else outro!!.startSec
                preview != null -> preview.startSec
                else -> ui.durationSec - 15.0
            }
            if (ui.positionSec >= triggerAt) showNextCard(n)
        }
    }

    // ---- seeking and quality ----

    /** Every seek goes through here: a transcode restarts when the target is not buffered. */
    private fun seek(target: Double) {
        var t = target.coerceAtLeast(0.0)
        sourceDurationSec?.let { d -> t = t.coerceAtMost((d - 1.0).coerceAtLeast(0.0)) }
        if (transcoding && !buffered(t)) restartTranscode(t) else player.seekTo(t)
    }

    private fun seekRelative(seconds: Int) {
        if (!transcoding) { player.seekRelative(seconds); return }
        val from = reloadTarget ?: player.timePosSeconds() ?: return
        seek(from + seconds)
    }

    /**
     * Whether mpv can reach [t] in what it holds: ahead up to the end of the cache, behind
     * a little (the back buffer is 16-32 MiB, about half a minute of an 8 Mb/s transcode).
     */
    private fun buffered(t: Double): Boolean {
        if (reloadTarget != null) return false
        val pos = player.timePosSeconds() ?: return false
        val end = player.cacheEndSeconds() ?: return false
        return t >= pos - 15.0 && t <= end - 1.0
    }

    /** Ask the server for a new stream beginning at [at] (the old one is dropped; the server stops its ffmpeg). */
    private fun restartTranscode(at: Double) {
        val sid = sourceId ?: return
        Timber.i("transcode %s from %.1f s", quality.key, at)
        transcoding = true
        reloadTarget = at
        switching = true // the replaced stream's end is not the end of the episode
        val cached = app.api.cachedMediaToken(sid)
        if (cached != null) {
            player.play(app.api.transcodeUrl(sid, quality.height, quality.kbps, at, cached), 0, transcoded = true)
        } else {
            // A download playing offline-first has no media token yet: fetch one, then switch.
            scope.launch {
                runCatching { app.api.mediaToken(sid) }
                    .onSuccess { player.play(app.api.transcodeUrl(sid, quality.height, quality.kbps, at, it), 0, transcoded = true) }
                    .onFailure {
                        Timber.w(it, "media token")
                        // Server still away: count it as a failed attempt rather than sit "reconnecting".
                        if (reconnecting && !destroyed) reconnect(at)
                    }
            }
        }
        watchReload(at)
    }

    /** A stream that never loads (server busy: 503, or gone) must not leave the player stuck switching. */
    private fun watchReload(at: Double) {
        scope.launch {
            delay(20_000)
            if (!destroyed && switching && !reconnecting && reloadTarget == at) {
                Timber.w("reload at %.1f s did not start", at)
                switching = false
                reloadTarget = null
                flashOsd("The server could not start this quality", 4000)
            }
        }
    }

    private fun changeQuality(q: QualityOption) {
        if (sourceId == null || !app.prefs.session.value.isLoggedIn) return
        val current = if (transcoding) quality else QualityOption.Original
        if (q == current) return
        val pos = reloadTarget ?: player.timePosSeconds() ?: 0.0
        quality = q
        qualityFromSettings = false
        Timber.i("quality -> %s at %.1f s", q.key, pos)
        if (q.isOriginal) {
            transcoding = false
            reloadTarget = pos
            switching = true
            player.play(directUrl, pos.toInt(), transcoded = false)
            watchReload(pos)
        } else {
            restartTranscode(pos)
        }
        refreshUi()
    }

    /** Mobile data, a metered hotspot: the "on mobile data" default applies. */
    private fun onMeteredNetwork(): Boolean = runCatching {
        val cm = context.getSystemService(android.net.ConnectivityManager::class.java)
        cm.isActiveNetworkMetered
    }.getOrDefault(false)

    /**
     * Whether converting to [q] saves anything: the file's bitrate (size over length) is
     * clearly above the preset's, or, when that is unknown, the picture is taller.
     */
    private fun worthTranscoding(q: QualityOption, src: tv.roxy.tv.api.MediaSource?): Boolean {
        if (src == null) return true
        val kbps = sourceKbps(src)
        if (kbps > 0) return kbps > (q.kbps + 200) * 1.2
        val h = src.streams.firstOrNull { it.type == "video" }?.height ?: 0
        return h <= 0 || h > q.height * 1.06
    }

    private fun sourceKbps(src: tv.roxy.tv.api.MediaSource): Double {
        val ms = src.durationMs ?: 0L
        return if (src.size > 0 && ms > 0) src.size * 8.0 / (ms / 1000.0) / 1000.0 else 0.0
    }

    /** Play/pause from a key or button; after the reconnects gave up, it tries again instead. */
    fun playPause(play: Boolean? = null) {
        if (retryAfterGivingUp()) return
        if (play == null) player.togglePause() else player.setPaused(!play)
        showControls()
    }

    fun showControls() {
        if (host.inPip) return
        controlsVisible = true
        scheduleHide()
    }

    fun hideControls() {
        hideJob?.cancel()
        controlsVisible = false
        sheet = Sheet.None
    }

    /** Auto-hide unless paused or a sheet is open (YouTube behaviour). */
    private fun scheduleHide() {
        hideJob?.cancel()
        hideJob = scope.launch {
            delay(AUTO_HIDE_MS)
            if (!ui.paused && sheet == Sheet.None && scrubTarget == null) controlsVisible = false
        }
    }

    /** POST /v1/me/progress with the current position. Runs on the app scope so a final report survives the host. */
    fun reportProgress(reason: String, atSec: Double? = null) {
        val id = itemId ?: return
        if (!app.prefs.session.value.isLoggedIn) return
        val pos = atSec ?: runCatching { player.timePosSeconds() }.getOrNull() ?: return
        val positionMs = (pos * 1000).toLong()
        if (positionMs <= 0) return
        if (reason == "tick" && positionMs == lastReportedMs) return
        lastReportedMs = positionMs
        val audioIndex = runCatching { player.currentStreamIndex("audio") }.getOrNull()
        val subIndex = runCatching { player.currentStreamIndex("sub") }.getOrNull()
        app.remote.reportState(id, title, positionMs, reason == "pause" || reason == "stop" || (reason != "resume" && ui.paused))
        // A pause or stop is when the launcher's Watch Next row should show this title at this position.
        if (reason == "pause" || reason == "stop") {
            val durationMs = (ui.durationSec * 1000).toLong()
            app.scope.launch { (app.items[id] ?: runCatching { app.api.item(id) }.getOrNull())?.let { WatchNext.publish(app, it, positionMs, durationMs) } }
        }
        app.scope.launch {
            reportLock.withLock {
                // While the server is unreachable (playing a download away from home) skip the
                // network for a minute at a time instead of timing out on every tick.
                val skip = System.currentTimeMillis() < serverDownUntil
                val ok = !skip && runCatching { app.api.progress(id, positionMs, audioIndex, subIndex) }
                    .onSuccess { Timber.i("progress %s item=%s pos=%dms audio=%s sub=%s", reason, id, positionMs, audioIndex, subIndex) }
                    .onFailure {
                        Timber.w("progress %s item=%s pos=%dms failed: %s", reason, id, positionMs, it.message)
                        if (it !is tv.roxy.tv.api.ApiException) serverDownUntil = System.currentTimeMillis() + 60_000
                    }
                    .isSuccess
                // Downloaded items keep their position locally too (Downloads page offline); a
                // failed report is queued and replayed when the server is back. A success
                // clears this item's queued one, so an older position never replays over it.
                if (app.downloads.entry(id) != null || !ok || app.downloads.hasPendingProgress(id)) app.downloads.recordProgress(id, positionMs, reportedToServer = ok)
                // The server answers again: send what was saved while it was away.
                if (ok) app.downloads.syncProgress(app.api)
                // Watched state may have changed (finished, or a rewatch made it "in
                // progress"): every list and menu on screen reloads, like a menu action.
                if (ok && (reason == "stop" || reason == "end")) app.items.userDataChanged()
            }
        }
    }

    private fun flashOsd(text: String?, holdMs: Long = 2500) {
        osdJob?.cancel()
        osd = text
        if (text != null && holdMs > 0) {
            osdJob = scope.launch { delay(holdMs); osd = null }
        }
    }

    /** The host went to the background: report and pause so audio does not keep playing. */
    fun onStop() {
        commitScrub()
        reportProgress("stop")
        if (!host.inPip) player.setPaused(true)
    }

    // ---- driven from another device ("Play on") ----
    fun remotePause(paused: Boolean) { main.post { if (!destroyed) { player.setPaused(paused); refreshUi(forceGlue = true) } } }
    fun remoteToggle() { main.post { if (!destroyed) { player.togglePause(); refreshUi(forceGlue = true) } } }
    fun remoteSeekTo(seconds: Int) { main.post { if (!destroyed) actions.seekTo(seconds.toDouble()) } }
    fun remoteSeekBy(seconds: Int) { main.post { if (!destroyed) actions.seekBy(seconds) } }
    fun remoteNext() { main.post { if (!destroyed) playNext() } }
    fun remoteStop() { main.post { if (!destroyed) { reportProgress("stop"); host.back() } } }
    fun positionMs(): Long = ((runCatching { player.timePosSeconds() }.getOrNull() ?: 0.0) * 1000).toLong()

    /** Reports this player's state for "Play on" right away (after a handed-over play is up). */
    fun reportStateNow() { itemId?.let { app.remote.reportState(it, title, positionMs(), ui.paused) } }

    fun destroy() {
        if (destroyed) return
        destroyed = true
        if (app.remote.activePlayer === this) { app.remote.activePlayer = null; app.remote.reportState(null, "", 0, false) }
        countdownJob?.cancel(); hideJob?.cancel(); osdJob?.cancel(); flashJob?.cancel(); scrubJob?.cancel(); reconnectJob?.cancel(); waitJob?.cancel()
        app.server.playerActive.value = false
        runCatching { displayManager?.unregisterDisplayListener(displayListener) }
        glue.release()
        player.listener = null
        player.destroy()
    }
}
