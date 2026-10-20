package tv.roxy.tv.player

import android.content.Context
import android.os.Handler
import android.os.Looper
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import timber.log.Timber
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
        private const val AUTO_HIDE_MS = 4000L
        private const val PROGRESS_INTERVAL_MS = 10_000L
        /** Seconds the next-episode card counts down before it plays (Netflix uses about this). */
        private const val NEXT_COUNTDOWN_S = 10
    }

    val player: MpvPlayer
    private val cpu = CpuSampler()
    private val main = Handler(Looper.getMainLooper())

    var statsLines by mutableStateOf<List<String>>(emptyList()); private set
    var fontLog by mutableStateOf<List<String>>(emptyList()); private set
    var showStats by mutableStateOf(params.showStats)
    var osd by mutableStateOf<String?>(null); private set
    private var osdJob: Job? = null
    var ui by mutableStateOf(PlayerUiState()); private set
    var controlsVisible by mutableStateOf(true); private set
    var sheet by mutableStateOf(Sheet.None)
    private var hideJob: Job? = null
    var trickplay by mutableStateOf<TrickplayPreview?>(null); private set
    var doubleTapFlash by mutableStateOf(0); private set // -10 / +10 while the ripple shows
    var zoomLevel by mutableStateOf(1f); private set
    var zoomHud by mutableStateOf(false); private set // "1.5×" readout shown (fingers down, or just after)
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
    private var zoomHudJob: Job? = null
    private var flashJob: Job? = null
    private var lastSid = 0
    private var autoSkip = false
    private var segments: List<MediaSegment> = emptyList()
    private val skipped = HashSet<String>() // segment types auto-skipped this playback
    var title = params.title; private set
    var subtitleLine = params.subtitleLine; private set
    val hwdec = params.hwdec
    val vo = params.vo
    var itemId: String? = params.itemId; private set
    private var sourceId: String? = params.sourceId
    private var local = params.local
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

    val actions: PlayerActions

    init {
        val settings = app.prefs.settings.value
        autoSkip = settings.autoSkip
        autoplay = settings.autoplay
        skipWindowSec = settings.skipWindowSec
        nextCardEnabled = settings.nextCard
        player = MpvPlayer(
            context = context,
            options = MpvPlayer.Options(
                hwdec = params.hwdec, vo = params.vo, startSeconds = params.startSeconds, subMode = params.subMode, forceSid = params.forceSid,
                audioLangs = params.audioLangs, subLangs = params.subLangs, anime = params.anime,
            ),
            fontsDir = app.fontsDir,
            displayRefreshRate = DeviceReport.refreshRate(context),
        )
        player.listener = object : MpvPlayer.Listener {
            override fun onFileLoaded() { switching = false; itemId?.let { app.remote.reportState(it, title, 0, false) } } // the remote bar shows it at once

            override fun onEndFile() {
                if (switching) return // the old file giving way to the next episode
                Timber.i("end of file")
                reportProgress("end")
            }

            override fun onEofReached() {
                if (switching) return
                Timber.i("reached the end")
                reportProgress("end")
                // Ran out while the card was up (or with no outro marker): go on now.
                if (autoplay && next != null) main.post { if (!destroyed) playNext() }
            }

            override fun onPauseChanged(paused: Boolean) {
                main.post { if (player.destroyed || destroyed) return@post; refreshUi(); if (paused) showControls() else scheduleHide() }
                reportProgress(if (paused) "pause" else "resume")
            }
        }
        prepareAndPlay(params.url, params.sourceId, params.local, params.startSeconds)
        app.remote.activePlayer = this

        actions = object : PlayerActions {
            override fun togglePause() = player.togglePause()
            override fun seekTo(seconds: Double) {
                // A manual seek backwards into a segment re-arms its auto-skip.
                segments.filter { seconds < it.startSec }.forEach { skipped.remove(it.type) }
                player.seekTo(seconds); refreshUi()
            }
            override fun seekBy(seconds: Int) { player.seekRelative(seconds); refreshUi() }
            override fun selectSubtitle(id: Int) { if (id > 0) lastSid = id; player.setSubtitleTrack(id); refreshUi() }
            override fun selectAudio(id: Int) { player.setAudioTrack(id); refreshUi() }
            override fun setSpeed(speed: Double) { player.setSpeed(speed); refreshUi() }
            override fun toggleMute() { player.setMuted(!player.isMuted()); refreshUi() }
            override fun toggleCaptions() {
                // CC button: off <-> the last chosen track (or the first one).
                val current = player.currentTrackId("sub")
                if (current > 0) { lastSid = current; player.setSubtitleTrack(0) } else {
                    val target = lastSid.takeIf { it > 0 } ?: ui.subtitleTracks.firstOrNull()?.id ?: 0
                    player.setSubtitleTrack(target)
                }
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

        // Controls state (fast), stats sampling (cheap; only rendered when the overlay is on)
        // and periodic progress.
        scope.launch {
            var tick = 0
            while (!destroyed) {
                delay(500)
                tick++
                refreshUi()
                if (tick % 2 != 0) continue
                val cores = Runtime.getRuntime().availableProcessors()
                val snap = runCatching { player.snapshot() }.getOrNull() ?: continue
                statsLines = snap.lines(cpu.sample(), cores)
                fontLog = snap.fontLog
                if ((tick / 2) % (PROGRESS_INTERVAL_MS / 1000).toInt() == 0 && snap.paused != true) reportProgress("tick")
            }
        }
    }

    // ---- gestures the surface reports ----

    fun onTap() { if (controlsVisible && sheet == Sheet.None) hideControls() else showControls() }

    fun onDoubleTap(back: Boolean) {
        player.seekRelative(if (back) -10 else 10)
        refreshUi()
        doubleTapFlash = if (back) -10 else 10
        flashJob?.cancel()
        flashJob = scope.launch { delay(700); doubleTapFlash = 0 }
    }

    fun onZoom(factor: Double, x: Double, y: Double) = player.setZoom(factor, x, y)

    fun onZoomLevel(level: Float, active: Boolean) {
        zoomLevel = level
        zoomHud = true
        zoomHudJob?.cancel()
        if (!active) zoomHudJob = scope.launch { delay(1400); zoomHud = false }
    }

    fun onPinchStart() = hideControls()

    fun openSheet(s: Sheet) { sheet = s; showControls() }

    fun toggleStats() { showStats = !showStats }

    /**
     * Fetch what the server knows about the source (segments, sniffed subtitle languages,
     * trickplay) and start the file. For a download the snapshot answers instead, so
     * playback never waits on the network. Then resolve the episode after this one.
     */
    private fun prepareAndPlay(url: String, sourceId: String?, local: Boolean, start: Int) {
        val snapshot = if (local) itemId?.let { app.downloads.entry(it)?.item } else null
        if (snapshot != null) {
            val src = snapshot.primarySource
            segments = src?.segments.orEmpty()
            player.subtitleLanguageOverrides = subtitleInfos(src?.streams.orEmpty()).filter { it.detectedLanguage != null }.associate { it.mpvId to it.effectiveLanguage }
            player.play(url, start)
            // Seek previews are a bonus when the server happens to be reachable.
            if (sourceId != null && app.prefs.session.value.isLoggedIn) scope.launch {
                val m = runCatching { app.api.trickplay(sourceId) }.getOrNull()
                if (m != null && m.sheets > 0 && this@PlayerSession.sourceId == sourceId) trickplay = TrickplayPreview(app.api, sourceId, m)
            }
        } else if (sourceId != null && app.prefs.session.value.isLoggedIn) {
            // Skippable segments (intro/outro) from the server's chapter analysis.
            scope.launch {
                runCatching { app.api.source(sourceId).segments }
                    .onSuccess { segs -> if (this@PlayerSession.sourceId == sourceId) { segments = segs; segs.forEach { Timber.i("segment %s %.1f-%.1f s (%s)", it.type, it.startSec, it.endSec, it.origin) } } }
                    .onFailure { Timber.w(it, "segments for %s", sourceId) }
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
                player.play(url, start)
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
        countdownJob?.cancel()
        nextCard = null
        nextCardDismissed = false
        // Moving on counts as finishing this one (Netflix): report the last position and mark it watched.
        reportProgress("end")
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
        refreshUi()
        host.nowPlaying(req)
    }

    private fun markWatched(id: String) {
        if (!app.prefs.session.value.isLoggedIn) return
        app.scope.launch {
            val ok = System.currentTimeMillis() >= serverDownUntil && runCatching { app.api.setPlayed(id, true) }.isSuccess
            if (app.downloads.entry(id) != null || !ok) app.downloads.recordProgress(id, 0, reportedToServer = ok, played = true)
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
    fun refreshUi() {
        // Callbacks posted by mpv (e.g. the pause from onStop) can land after destroy.
        if (player.destroyed || destroyed) return
        val tracks = runCatching { player.tracks() }.getOrDefault(emptyList())
        val wasPaused = ui.paused
        ui = PlayerUiState(
            title = title,
            subtitle = subtitleLine,
            positionSec = player.timePosSeconds() ?: 0.0,
            durationSec = player.durationSeconds() ?: 0.0,
            bufferedSec = player.cacheEndSeconds() ?: 0.0,
            paused = player.isPaused(),
            muted = player.isMuted(),
            speed = player.speed(),
            fillScreen = player.isFill(),
            pipSupported = host.pipSupported,
            hasNext = next != null,
            nextCardShown = nextCard != null,
            segments = segments,
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
        // Auto-skip: jump to the end of a segment the first time the playhead enters it.
        if (autoSkip && !ui.paused) {
            ui.activeSegment?.let { seg ->
                if (skipped.add(seg.type)) {
                    Timber.i("auto-skip %s -> %.1f s", seg.type, seg.endSec)
                    player.seekTo(seg.endSec)
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
            if (!ui.paused && sheet == Sheet.None) controlsVisible = false
        }
    }

    /** POST /v1/me/progress with the current position. Runs on the app scope so a final report survives the host. */
    fun reportProgress(reason: String) {
        val id = itemId ?: return
        if (!app.prefs.session.value.isLoggedIn) return
        val pos = runCatching { player.timePosSeconds() }.getOrNull() ?: return
        val positionMs = (pos * 1000).toLong()
        if (positionMs <= 0) return
        if (reason == "tick" && positionMs == lastReportedMs) return
        lastReportedMs = positionMs
        val audioIndex = runCatching { player.currentStreamIndex("audio") }.getOrNull()
        val subIndex = runCatching { player.currentStreamIndex("sub") }.getOrNull()
        app.remote.reportState(id, title, positionMs, reason == "pause" || reason == "stop" || (reason != "resume" && ui.paused))
        app.scope.launch {
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
            // failed report is queued and replayed when the server is back.
            if (app.downloads.entry(id) != null || !ok) app.downloads.recordProgress(id, positionMs, reportedToServer = ok)
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
        reportProgress("stop")
        if (!host.inPip) player.setPaused(true)
    }

    // ---- driven from another device ("Play on") ----
    fun remotePause(paused: Boolean) { main.post { if (!destroyed) { player.setPaused(paused); refreshUi() } } }
    fun remoteToggle() { main.post { if (!destroyed) { player.togglePause(); refreshUi() } } }
    fun remoteSeekTo(seconds: Int) { main.post { if (!destroyed) actions.seekTo(seconds.toDouble()) } }
    fun remoteSeekBy(seconds: Int) { main.post { if (!destroyed) actions.seekBy(seconds) } }
    fun remoteNext() { main.post { if (!destroyed) playNext() } }
    fun remoteStop() { main.post { if (!destroyed) { reportProgress("stop"); host.back() } } }
    fun positionMs(): Long = ((runCatching { player.timePosSeconds() }.getOrNull() ?: 0.0) * 1000).toLong()

    fun destroy() {
        if (destroyed) return
        destroyed = true
        if (app.remote.activePlayer === this) { app.remote.activePlayer = null; app.remote.reportState(null, "", 0, false) }
        countdownJob?.cancel(); hideJob?.cancel(); osdJob?.cancel(); flashJob?.cancel(); zoomHudJob?.cancel()
        player.listener = null
        player.destroy()
    }
}
