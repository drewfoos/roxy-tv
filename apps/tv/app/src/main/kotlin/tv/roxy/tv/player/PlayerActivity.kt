package tv.roxy.tv.player

import android.content.Context
import android.content.Intent
import android.os.Bundle
import android.view.KeyEvent
import android.view.SurfaceView
import android.view.WindowManager
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.WindowInsetsControllerCompat
import androidx.lifecycle.lifecycleScope
import androidx.tv.material3.Text
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.data.PlayerSettings
import tv.roxy.tv.diagnostics.DeviceReport
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.text.font.FontWeight

private val uuidRegex = Regex("^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$")

/**
 * Full-screen libmpv player. Launched by the app through [intent], and by the diagnostics
 * scripts (tools/spike-*.ps1) over adb with the same extras, so every extra stays optional
 * and falls back to the saved settings.
 */
class PlayerActivity : ComponentActivity() {

    companion object {
        const val EXTRA_URL = "url"
        const val EXTRA_TITLE = "title"
        const val EXTRA_HWDEC = "hwdec"
        const val EXTRA_VO = "vo"
        const val EXTRA_START = "start"      // seconds; adb: --ei start 600
        const val EXTRA_SUBMODE = "submode"  // full | signs | none; adb: --es submode full
        const val EXTRA_SID = "sid"          // force an mpv subtitle id; adb: --ei sid 2
        const val EXTRA_SOURCE_ID = "sourceId" // Roxy source id: enables server-sniffed sub languages
        const val EXTRA_ITEM_ID = "itemId"   // Roxy item id: enables progress reporting
        const val EXTRA_ALANG = "alang"      // comma-separated audio language order
        const val EXTRA_STATS = "stats"      // show the stats overlay from the start; adb: --ez stats true
        const val EXTRA_SUBTITLE_LINE = "subtitleLine" // second title line (series + SxxEyy, or year)
        private const val AUTO_HIDE_MS = 4000L

        private const val PROGRESS_INTERVAL_MS = 10_000L

        fun intent(
            context: Context,
            url: String,
            title: String,
            itemId: String?,
            sourceId: String?,
            startSeconds: Int,
            settings: PlayerSettings,
            subtitleLine: String = "",
        ): Intent = Intent(context, PlayerActivity::class.java)
            .putExtra(EXTRA_URL, url)
            .putExtra(EXTRA_TITLE, title)
            .putExtra(EXTRA_SUBTITLE_LINE, subtitleLine)
            .putExtra(EXTRA_ITEM_ID, itemId)
            .putExtra(EXTRA_SOURCE_ID, sourceId)
            .putExtra(EXTRA_START, startSeconds)
            .putExtra(EXTRA_HWDEC, settings.hwdec)
            .putExtra(EXTRA_VO, settings.vo)
            .putExtra(EXTRA_SUBMODE, settings.subMode)
            .putExtra(EXTRA_ALANG, settings.audioLangs.joinToString(","))
            .putExtra(EXTRA_STATS, settings.showStats)
    }

    private lateinit var app: RoxyApp
    private lateinit var player: MpvPlayer
    private val cpu = CpuSampler()

    private var statsLines by mutableStateOf<List<String>>(emptyList())
    private var fontLog by mutableStateOf<List<String>>(emptyList())
    private var showStats by mutableStateOf(false)
    private var osd by mutableStateOf<String?>(null)
    private var osdJob: Job? = null
    private var ui by mutableStateOf(PlayerUiState())
    private var controlsVisible by mutableStateOf(true)
    private var sheet by mutableStateOf(Sheet.None)
    private var hideJob: Job? = null
    private var trickplay by mutableStateOf<TrickplayPreview?>(null)
    private var doubleTapFlash by mutableStateOf(0) // -10 / +10 while the ripple shows
    private var zoomLevel by mutableStateOf(1f)
    private var zoomActive by mutableStateOf(false)
    private var flashJob: Job? = null
    private var lastSid = 0
    private var autoSkip = false
    private var segments: List<tv.roxy.tv.api.MediaSegment> = emptyList()
    private val skipped = HashSet<String>() // segment types auto-skipped this playback
    private val isTv: Boolean by lazy {
        (getSystemService(Context.UI_MODE_SERVICE) as android.app.UiModeManager).currentModeType == android.content.res.Configuration.UI_MODE_TYPE_TELEVISION
    }
    private val pipSupported: Boolean by lazy {
        !isTv && packageManager.hasSystemFeature(android.content.pm.PackageManager.FEATURE_PICTURE_IN_PICTURE)
    }
    private var title = ""
    private var subtitleLine = ""
    private var hwdec = ""
    private var vo = ""
    private var itemId: String? = null
    private var lastReportedMs = -1L

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        WindowCompat.setDecorFitsSystemWindows(window, false)
        WindowInsetsControllerCompat(window, window.decorView).apply {
            hide(WindowInsetsCompat.Type.systemBars())
            systemBarsBehavior = WindowInsetsControllerCompat.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
        }

        app = RoxyApp.get(this)
        val settings = app.prefs.settings.value

        val url = intent.getStringExtra(EXTRA_URL) ?: run { finish(); return }
        title = intent.getStringExtra(EXTRA_TITLE) ?: ""
        subtitleLine = intent.getStringExtra(EXTRA_SUBTITLE_LINE) ?: ""
        hwdec = intent.getStringExtra(EXTRA_HWDEC) ?: settings.hwdec
        vo = intent.getStringExtra(EXTRA_VO) ?: settings.vo
        val start = intent.getIntExtra(EXTRA_START, 0)
        val subMode = intent.getStringExtra(EXTRA_SUBMODE) ?: settings.subMode
        val forceSid = intent.getIntExtra(EXTRA_SID, 0)
        val audioLangs = intent.getStringExtra(EXTRA_ALANG)
            ?.split(',')?.map { it.trim() }?.filter { it.isNotEmpty() }?.takeIf { it.isNotEmpty() }
            ?.let { settings.copy(audioLangs = it).mpvAudioLangs }
            ?: settings.mpvAudioLangs
        showStats = intent.getBooleanExtra(EXTRA_STATS, settings.showStats)
        autoSkip = settings.autoSkip
        itemId = intent.getStringExtra(EXTRA_ITEM_ID)
        val sourceId = intent.getStringExtra(EXTRA_SOURCE_ID)
            ?: url.substringAfterLast("/stream/").substringBefore('?').takeIf { it.matches(uuidRegex) }

        player = MpvPlayer(
            context = this,
            options = MpvPlayer.Options(hwdec = hwdec, vo = vo, startSeconds = start, subMode = subMode, forceSid = forceSid, audioLangs = audioLangs),
            fontsDir = app.fontsDir,
            displayRefreshRate = DeviceReport.refreshRate(this),
        )
        player.listener = object : MpvPlayer.Listener {
            override fun onEndFile() {
                Timber.i("end of file")
                reportProgress("end")
            }

            override fun onPauseChanged(paused: Boolean) {
                runOnUiThread { refreshUi(); if (paused) showControls() else scheduleHide() }
                reportProgress(if (paused) "pause" else "resume")
            }
        }
        if (sourceId != null && app.prefs.session.value.isLoggedIn) {
            // Skippable segments (intro/outro) from the server's chapter analysis.
            lifecycleScope.launch {
                runCatching { app.api.source(sourceId).segments }
                    .onSuccess { segs -> segments = segs; segs.forEach { Timber.i("segment %s %.1f-%.1f s (%s)", it.type, it.startSec, it.endSec, it.origin) } }
                    .onFailure { Timber.w(it, "segments for %s", sourceId) }
            }
            // Seek-preview thumbnails: ask once now and, if the server is still generating
            // them (202), a few more times while the episode plays.
            lifecycleScope.launch {
                repeat(6) { attempt ->
                    val m = runCatching { app.api.trickplay(sourceId) }.getOrNull()
                    if (m != null && m.sheets > 0) { trickplay = TrickplayPreview(app.api, sourceId, m); return@launch }
                    delay(if (attempt == 0) 5_000 else 20_000)
                }
            }
            // Ask the server which language each subtitle track really is, then play.
            lifecycleScope.launch {
                runCatching { app.api.subtitleStreams(sourceId) }
                    .onSuccess { subs ->
                        subs.forEach { Timber.i("server sub mpvId=%d idx=%d tag=%s detected=%s title=%s", it.mpvId, it.index, it.language, it.detectedLanguage, it.title) }
                        player.subtitleLanguageOverrides = subs.filter { it.detectedLanguage != null }.associate { it.mpvId to it.effectiveLanguage }
                    }
                    .onFailure { Timber.w(it, "could not fetch subtitle languages for %s", sourceId) }
                player.play(url)
            }
        } else {
            player.play(url)
        }

        val actions = object : PlayerActions {
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
            override fun enterPip() { this@PlayerActivity.enterPip() }
            override fun next() { /* next-episode queue arrives with the series player session */ }
            override fun back() = finish()
        }
        scheduleHide()

        setContent {
            Box(modifier = Modifier.fillMaxSize().background(Color.Black)) {
                AndroidView(
                    modifier = Modifier.fillMaxSize(),
                    factory = { ctx ->
                        SurfaceView(ctx).apply { holder.addCallback(player) }
                    },
                )
                // Tap anywhere on the video: toggle the controls (phone); keys are handled in onKeyDown.
                // Tap: toggle the controls. Double-tap: seek -10 s on the left half, +10 s on
                // the right (YouTube), with a flash saying how far.
                Box(
                    modifier = Modifier.fillMaxSize().pinchZoom(
                        onZoom = { f, x, y -> player.setZoom(f, x, y) },
                        onLevel = { level, active -> zoomLevel = level; zoomActive = active },
                        onPinchStart = { hideControls() },
                    ).pointerInput(Unit) {
                        detectTapGestures(
                            onTap = { if (controlsVisible && sheet == Sheet.None) hideControls() else showControls() },
                            onDoubleTap = { pos ->
                                val back = pos.x < size.width / 2
                                player.seekRelative(if (back) -10 else 10)
                                refreshUi()
                                doubleTapFlash = if (back) -10 else 10
                                flashJob?.cancel()
                                flashJob = lifecycleScope.launch { delay(700); doubleTapFlash = 0 }
                            },
                        )
                    },
                )
                // Zoom readout: "1.5×" while pinching, and kept on screen while zoomed in.
                if (zoomActive || zoomLevel > 1.01f) {
                    Box(modifier = Modifier.fillMaxSize()) {
                        Text(
                            "%.1f×".format(zoomLevel),
                            color = Color.White, fontSize = 15.sp, fontWeight = FontWeight.SemiBold,
                            modifier = Modifier.align(Alignment.TopCenter).padding(top = 28.dp).background(Color(0x99000000), RoundedCornerShape(14.dp)).padding(horizontal = 12.dp, vertical = 5.dp),
                        )
                    }
                }
                if (doubleTapFlash != 0) {
                    Box(modifier = Modifier.fillMaxSize()) {
                        Row(
                            verticalAlignment = Alignment.CenterVertically,
                            modifier = Modifier
                                .align(if (doubleTapFlash < 0) Alignment.CenterStart else Alignment.CenterEnd)
                                .padding(horizontal = 96.dp)
                                .background(Color(0x66000000), CircleShape)
                                .padding(horizontal = 22.dp, vertical = 16.dp),
                        ) {
                            Text(if (doubleTapFlash < 0) "◀◀ 10 s" else "10 s ▶▶", color = Color.White, fontSize = 18.sp, fontWeight = FontWeight.SemiBold)
                        }
                    }
                }
                PlayerControls(
                    state = ui,
                    visible = controlsVisible,
                    sheet = sheet,
                    onSheet = { s -> sheet = s; showControls() },
                    onInteraction = { showControls() },
                    actions = actions,
                    trickplay = trickplay,
                )
                osd?.let { text ->
                    Box(
                        modifier = Modifier
                            .align(Alignment.TopEnd)
                            .padding(32.dp)
                            .background(Color(0xB3000000), RoundedCornerShape(8.dp))
                            .padding(horizontal = 16.dp, vertical = 10.dp),
                    ) {
                        Text(text, color = Color.White, fontSize = 18.sp)
                    }
                }
                if (showStats) {
                    Column(
                        modifier = Modifier
                            .align(Alignment.TopStart)
                            .padding(24.dp)
                            .background(Color(0xAA000000))
                            .padding(12.dp),
                    ) {
                        Text("$title   [hwdec=$hwdec vo=$vo]", color = Color.White, fontSize = 14.sp, fontFamily = FontFamily.Monospace)
                        statsLines.forEach { Text(it, color = Color(0xFFE8EAED), fontSize = 13.sp, fontFamily = FontFamily.Monospace) }
                    }
                    Column(
                        modifier = Modifier
                            .align(Alignment.BottomStart)
                            .padding(24.dp)
                            .background(Color(0x99000000))
                            .padding(8.dp),
                    ) {
                        Text("libass / decoder log (newest last)", color = Color(0xFFFBBC04), fontSize = 11.sp, fontFamily = FontFamily.Monospace)
                        fontLog.takeLast(12).forEach { Text(it, color = Color(0xFFBDC1C6), fontSize = 10.sp, fontFamily = FontFamily.Monospace, maxLines = 1) }
                    }
                }
            }
        }

        // Controls state (fast), stats sampling (cheap; only rendered when the overlay is on)
        // and periodic progress.
        lifecycleScope.launch {
            var tick = 0
            while (true) {
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

    /** Pulls position, tracks and state from mpv into the controls' state. */
    private fun refreshUi() {
        val tracks = runCatching { player.tracks() }.getOrDefault(emptyList())
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
            pipSupported = pipSupported,
            segments = segments,
            autoSkip = autoSkip,
            subtitleTracks = tracks.filter { it.type == "sub" },
            audioTracks = tracks.filter { it.type == "audio" },
            currentSid = player.currentTrackId("sub"),
            currentAid = player.currentTrackId("audio"),
            quality = runCatching { player.videoSummary() }.getOrDefault(""),
        )
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
    }

    private fun showControls() {
        if (isInPictureInPictureMode) return
        controlsVisible = true
        scheduleHide()
    }

    /** Phone only: shrink to a floating window (the PiP button, and leaving the app while playing). */
    private fun enterPip() {
        if (!pipSupported) return
        hideControls()
        val params = android.app.PictureInPictureParams.Builder()
            .setAspectRatio(android.util.Rational(16, 9))
            .build()
        runCatching { enterPictureInPictureMode(params) }.onFailure { Timber.w(it, "PiP failed") }
    }

    override fun onUserLeaveHint() {
        super.onUserLeaveHint()
        if (pipSupported && !ui.paused) enterPip()
    }

    override fun onPictureInPictureModeChanged(isInPictureInPictureMode: Boolean, newConfig: android.content.res.Configuration) {
        super.onPictureInPictureModeChanged(isInPictureInPictureMode, newConfig)
        if (isInPictureInPictureMode) hideControls() else showControls()
    }

    private fun hideControls() {
        hideJob?.cancel()
        controlsVisible = false
        sheet = Sheet.None
    }

    /** Auto-hide unless paused or a sheet is open (YouTube behaviour). */
    private fun scheduleHide() {
        hideJob?.cancel()
        hideJob = lifecycleScope.launch {
            delay(AUTO_HIDE_MS)
            if (!ui.paused && sheet == Sheet.None) controlsVisible = false
        }
    }

    /** POST /v1/me/progress with the current position. Runs on the app scope so a final report survives finish(). */
    private fun reportProgress(reason: String) {
        val id = itemId ?: return
        if (!app.prefs.session.value.isLoggedIn) return
        val pos = runCatching { player.timePosSeconds() }.getOrNull() ?: return
        val positionMs = (pos * 1000).toLong()
        if (positionMs <= 0) return
        if (reason == "tick" && positionMs == lastReportedMs) return
        lastReportedMs = positionMs
        val audioIndex = runCatching { player.currentStreamIndex("audio") }.getOrNull()
        val subIndex = runCatching { player.currentStreamIndex("sub") }.getOrNull()
        app.scope.launch {
            runCatching { app.api.progress(id, positionMs, audioIndex, subIndex) }
                .onSuccess { Timber.i("progress %s item=%s pos=%dms audio=%s sub=%s", reason, id, positionMs, audioIndex, subIndex) }
                .onFailure { Timber.w("progress %s item=%s pos=%dms failed: %s", reason, id, positionMs, it.message) }
        }
    }

    private fun flashOsd(text: String?, holdMs: Long = 2500) {
        osdJob?.cancel()
        osd = text
        if (text != null && holdMs > 0) {
            osdJob = lifecycleScope.launch { delay(holdMs); osd = null }
        }
    }

    override fun onKeyDown(keyCode: Int, event: KeyEvent): Boolean {
        // Media keys and the diagnostics shortcuts work regardless of the control layer.
        when (keyCode) {
            KeyEvent.KEYCODE_MEDIA_PLAY_PAUSE, KeyEvent.KEYCODE_MEDIA_PLAY, KeyEvent.KEYCODE_MEDIA_PAUSE,
            KeyEvent.KEYCODE_SPACE -> { player.togglePause(); showControls(); return true }
            KeyEvent.KEYCODE_MEDIA_REWIND -> { player.seekRelative(-10); showControls(); return true }
            KeyEvent.KEYCODE_MEDIA_FAST_FORWARD -> { player.seekRelative(10); showControls(); return true }
            KeyEvent.KEYCODE_MENU, KeyEvent.KEYCODE_I -> { showStats = !showStats; return true }
            KeyEvent.KEYCODE_CAPTIONS -> { sheet = Sheet.Subtitles; showControls(); return true }
            KeyEvent.KEYCODE_MEDIA_AUDIO_TRACK, KeyEvent.KEYCODE_A -> { sheet = Sheet.Audio; showControls(); return true }
            KeyEvent.KEYCODE_BACK -> {
                if (sheet != Sheet.None) { sheet = Sheet.None; showControls(); return true }
                if (controlsVisible) { hideControls(); return true }
                return super.onKeyDown(keyCode, event)
            }
        }
        if (!controlsVisible) {
            // Hidden: the first D-pad press reveals the controls (YouTube on TV); Left/Right
            // also seek straight away so a quick skip does not need two presses.
            when (keyCode) {
                KeyEvent.KEYCODE_DPAD_LEFT -> { player.seekRelative(-10); showControls(); return true }
                KeyEvent.KEYCODE_DPAD_RIGHT -> { player.seekRelative(10); showControls(); return true }
                KeyEvent.KEYCODE_DPAD_CENTER, KeyEvent.KEYCODE_ENTER, KeyEvent.KEYCODE_DPAD_UP, KeyEvent.KEYCODE_DPAD_DOWN -> { showControls(); return true }
            }
        } else {
            showControls() // any navigation keeps them on screen
        }
        return super.onKeyDown(keyCode, event) // Compose focus handles the rest
    }

    override fun onStop() {
        if (::player.isInitialized) {
            reportProgress("stop")
            // Leaving without PiP (screen off, task switch): pause so audio does not keep playing.
            if (!isInPictureInPictureMode) player.setPaused(true)
        }
        super.onStop()
    }

    override fun onDestroy() {
        if (::player.isInitialized) player.destroy()
        super.onDestroy()
    }
}
