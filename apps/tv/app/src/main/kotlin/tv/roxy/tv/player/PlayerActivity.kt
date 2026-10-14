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

        private const val PROGRESS_INTERVAL_MS = 10_000L

        fun intent(
            context: Context,
            url: String,
            title: String,
            itemId: String?,
            sourceId: String?,
            startSeconds: Int,
            settings: PlayerSettings,
        ): Intent = Intent(context, PlayerActivity::class.java)
            .putExtra(EXTRA_URL, url)
            .putExtra(EXTRA_TITLE, title)
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
    private var title = ""
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
                runOnUiThread { flashOsd(if (paused) "Paused" else null) }
                reportProgress(if (paused) "pause" else "resume")
            }
        }
        if (sourceId != null && app.prefs.session.value.isLoggedIn) {
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

        setContent {
            Box(modifier = Modifier.fillMaxSize().background(Color.Black)) {
                AndroidView(
                    modifier = Modifier.fillMaxSize(),
                    factory = { ctx ->
                        SurfaceView(ctx).apply { holder.addCallback(player) }
                    },
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

        // Stats sampling (cheap; only rendered when the overlay is on) and periodic progress.
        lifecycleScope.launch {
            var tick = 0
            while (true) {
                delay(1000)
                tick++
                val cores = Runtime.getRuntime().availableProcessors()
                val snap = runCatching { player.snapshot() }.getOrNull() ?: continue
                statsLines = snap.lines(cpu.sample(), cores)
                fontLog = snap.fontLog
                if (tick % (PROGRESS_INTERVAL_MS / 1000).toInt() == 0 && snap.paused != true) reportProgress("tick")
            }
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

    private fun seekOsd() {
        val pos = player.timePosSeconds() ?: return
        val dur = player.durationSeconds()
        flashOsd(fmtTime(pos) + (dur?.let { " / " + fmtTime(it) } ?: ""))
    }

    private fun fmtTime(d: Double): String {
        val s = d.toLong()
        return if (s >= 3600) "%d:%02d:%02d".format(s / 3600, (s % 3600) / 60, s % 60) else "%d:%02d".format(s / 60, s % 60)
    }

    override fun onKeyDown(keyCode: Int, event: KeyEvent): Boolean {
        when (keyCode) {
            KeyEvent.KEYCODE_DPAD_CENTER, KeyEvent.KEYCODE_ENTER,
            KeyEvent.KEYCODE_MEDIA_PLAY_PAUSE, KeyEvent.KEYCODE_MEDIA_PLAY, KeyEvent.KEYCODE_MEDIA_PAUSE,
            KeyEvent.KEYCODE_SPACE -> { player.togglePause(); return true }
            KeyEvent.KEYCODE_DPAD_LEFT, KeyEvent.KEYCODE_MEDIA_REWIND -> { player.seekRelative(-10); seekOsd(); return true }
            KeyEvent.KEYCODE_DPAD_RIGHT, KeyEvent.KEYCODE_MEDIA_FAST_FORWARD -> { player.seekRelative(10); seekOsd(); return true }
            KeyEvent.KEYCODE_DPAD_UP, KeyEvent.KEYCODE_MENU, KeyEvent.KEYCODE_I -> { showStats = !showStats; return true }
            KeyEvent.KEYCODE_DPAD_DOWN, KeyEvent.KEYCODE_CAPTIONS -> { player.cycleSubtitle(); flashOsd("Subtitle: " + (player.snapshot().subTitle ?: "cycled")); return true }
            KeyEvent.KEYCODE_MEDIA_AUDIO_TRACK, KeyEvent.KEYCODE_A -> { player.cycleAudio(); flashOsd("Audio: " + (player.snapshot().audioCodec ?: "cycled")); return true }
        }
        return super.onKeyDown(keyCode, event)
    }

    override fun onStop() {
        if (::player.isInitialized) reportProgress("stop")
        super.onStop()
    }

    override fun onDestroy() {
        if (::player.isInitialized) player.destroy()
        super.onDestroy()
    }
}
