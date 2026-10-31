package tv.roxy.tv.player

import android.content.Context
import android.content.Intent
import android.os.Bundle
import android.view.KeyEvent
import android.view.WindowManager
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.ui.Modifier
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.WindowInsetsControllerCompat
import androidx.lifecycle.lifecycleScope
import timber.log.Timber
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.data.Languages
import tv.roxy.tv.data.PlayerSettings
import tv.roxy.tv.ui.PlayRequest

private val uuidRegex = Regex("^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$")

/**
 * Full-screen host of a [PlayerSession] (movies, the TV, and the diagnostics scripts in
 * tools/spike-*.ps1, which launch it over adb with the same extras, so every extra stays
 * optional and falls back to the saved settings). The phone's episode page hosts the
 * same session in its own layout; this activity only adds the window, keys and PiP.
 */
class PlayerActivity : ComponentActivity() {

    companion object {
        const val EXTRA_URL = "url"
        const val EXTRA_TITLE = "title"
        const val EXTRA_HWDEC = "hwdec"
        const val EXTRA_VO = "vo"
        const val EXTRA_START = "start"      // seconds; adb: --ei start 600
        const val EXTRA_SUBMODE = "submode"  // full | signs | foreign | none; adb: --es submode full
        const val EXTRA_SID = "sid"          // force an mpv subtitle id; adb: --ei sid 2
        const val EXTRA_SOURCE_ID = "sourceId" // Roxy source id: enables server-sniffed sub languages
        const val EXTRA_ITEM_ID = "itemId"   // Roxy item id: enables progress reporting
        const val EXTRA_ALANG = "alang"      // comma-separated audio language order
        const val EXTRA_STATS = "stats"      // show the stats overlay from the start; adb: --ez stats true
        const val EXTRA_SUBTITLE_LINE = "subtitleLine" // second title line (series + SxxEyy, or year)
        const val EXTRA_LOCAL = "local"      // url is a downloaded file: segments and sub languages come from the download snapshot
        const val EXTRA_ANIME = "anime"      // the title is anime: smart subtitles pair a dub with signs & songs
        const val EXTRA_SLANG = "slang"      // preferred subtitle language, ISO 639-2

        fun intent(
            context: Context,
            url: String,
            title: String,
            itemId: String?,
            sourceId: String?,
            startSeconds: Int,
            settings: PlayerSettings,
            subtitleLine: String = "",
            local: Boolean = false,
            anime: Boolean = false,
        ): Intent = Intent(context, PlayerActivity::class.java)
            .putExtra(EXTRA_ANIME, anime)
            .putExtra(EXTRA_SLANG, settings.subLang)
            .putExtra(EXTRA_URL, url)
            .putExtra(EXTRA_TITLE, title)
            .putExtra(EXTRA_SUBTITLE_LINE, subtitleLine)
            .putExtra(EXTRA_LOCAL, local)
            .putExtra(EXTRA_ITEM_ID, itemId)
            .putExtra(EXTRA_SOURCE_ID, sourceId)
            .putExtra(EXTRA_START, startSeconds)
            .putExtra(EXTRA_HWDEC, settings.hwdec)
            .putExtra(EXTRA_VO, settings.vo)
            .putExtra(EXTRA_SUBMODE, settings.subMode)
            .putExtra(EXTRA_ALANG, settings.audioLangs.joinToString(","))
            .putExtra(EXTRA_STATS, settings.showStats)

        fun intent(context: Context, req: PlayRequest, settings: PlayerSettings): Intent =
            intent(context, req.url, req.title, req.item.id, req.sourceId, req.startSeconds, settings, req.subtitleLine, req.local, anime = req.item.isAnime)
    }

    private lateinit var app: RoxyApp
    private lateinit var session: PlayerSession
    /** The session the content draws; replaced when a new play request arrives (see onNewIntent). */
    private val shown = androidx.compose.runtime.mutableStateOf<PlayerSession?>(null)
    private val isTv: Boolean by lazy {
        (getSystemService(Context.UI_MODE_SERVICE) as android.app.UiModeManager).currentModeType == android.content.res.Configuration.UI_MODE_TYPE_TELEVISION
    }
    private val pipSupported: Boolean by lazy {
        !isTv && packageManager.hasSystemFeature(android.content.pm.PackageManager.FEATURE_PICTURE_IN_PICTURE)
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        WindowCompat.setDecorFitsSystemWindows(window, false)
        WindowInsetsControllerCompat(window, window.decorView).apply {
            hide(WindowInsetsCompat.Type.systemBars())
            systemBarsBehavior = WindowInsetsControllerCompat.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
        }

        app = RoxyApp.get(this)
        if (!startSession(intent)) { finish(); return }
        setContent {
            // A new session gets a new SurfaceView: the old one's holder belongs to the old player.
            shown.value?.let { s -> androidx.compose.runtime.key(s) { PlayerSurface(s, modifier = Modifier.fillMaxSize()) } }
        }
    }

    /**
     * Another title while the player is open (or stopped in the background): a Watch Next
     * card, "Play on" from the phone, a new pick in the app. singleTask brings this instance
     * back, so the request arrives here; without this the old episode would carry on.
     */
    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        val url = intent.getStringExtra(EXTRA_URL) ?: return
        val same = ::session.isInitialized && intent.getStringExtra(EXTRA_ITEM_ID)?.let { it == session.itemId } == true
        if (same) {
            Timber.i("play request for the item already playing: resume it")
            session.playPause(play = true)
            return
        }
        Timber.i("new play request: %s", url.substringBefore('?'))
        setIntent(intent)
        if (::session.isInitialized) { session.reportProgress("stop"); session.destroy() }
        if (!startSession(intent)) finish()
    }

    /** Build a session from [intent]'s extras; false when it names nothing to play. */
    private fun startSession(intent: Intent): Boolean {
        val settings = app.prefs.settings.value
        val url = intent.getStringExtra(EXTRA_URL) ?: return false
        val audioLangs = intent.getStringExtra(EXTRA_ALANG)
            ?.split(',')?.map { it.trim() }?.filter { it.isNotEmpty() }?.takeIf { it.isNotEmpty() }
            ?.let { settings.copy(audioLangs = it).mpvAudioLangs }
            ?: settings.mpvAudioLangs
        val params = PlayerSession.Params(
            url = url,
            title = intent.getStringExtra(EXTRA_TITLE) ?: "",
            subtitleLine = intent.getStringExtra(EXTRA_SUBTITLE_LINE) ?: "",
            itemId = intent.getStringExtra(EXTRA_ITEM_ID),
            sourceId = intent.getStringExtra(EXTRA_SOURCE_ID) ?: url.substringAfterLast("/stream/").substringBefore('?').takeIf { it.matches(uuidRegex) },
            startSeconds = intent.getIntExtra(EXTRA_START, 0),
            local = intent.getBooleanExtra(EXTRA_LOCAL, false),
            hwdec = intent.getStringExtra(EXTRA_HWDEC) ?: settings.hwdec,
            vo = intent.getStringExtra(EXTRA_VO) ?: settings.vo,
            subMode = intent.getStringExtra(EXTRA_SUBMODE) ?: settings.subMode,
            forceSid = intent.getIntExtra(EXTRA_SID, 0),
            audioLangs = audioLangs,
            subLangs = Languages.mpvCodes(intent.getStringExtra(EXTRA_SLANG) ?: settings.subLang),
            anime = intent.getBooleanExtra(EXTRA_ANIME, false),
            showStats = intent.getBooleanExtra(EXTRA_STATS, settings.showStats),
        )
        val host = object : PlayerSession.Host {
            override fun back() = finish()
            override val pipSupported: Boolean get() = this@PlayerActivity.pipSupported
            override val inPip: Boolean get() = isInPictureInPictureMode
            override fun enterPip() = this@PlayerActivity.enterPip()
            override fun playingChanged(playing: Boolean) = updatePipParams(playing)
        }
        session = PlayerSession(this, app, params, lifecycleScope, host)
        shown.value = session
        return true
    }

    private fun pipParams(autoEnter: Boolean): android.app.PictureInPictureParams {
        val b = android.app.PictureInPictureParams.Builder().setAspectRatio(android.util.Rational(16, 9))
        if (android.os.Build.VERSION.SDK_INT >= 31) {
            // Auto-enter makes the swipe-home transition a seamless shrink of the live
            // surface; without it the system takes a screenshot after the fact (a lag and
            // a black frame). Seamless resize keeps the video drawing while it animates.
            b.setAutoEnterEnabled(autoEnter).setSeamlessResizeEnabled(true)
        }
        return b.build()
    }

    private var pipAutoEnter: Boolean? = null

    /** Keep the system's PiP parameters current: auto-enter only while playing. */
    private fun updatePipParams(playing: Boolean) {
        if (!pipSupported || isDestroyed) return
        if (pipAutoEnter == playing) return
        pipAutoEnter = playing
        runCatching { setPictureInPictureParams(pipParams(autoEnter = playing)) }
    }

    /** Phone only: shrink to a floating window (the PiP button, and leaving the app while playing). */
    private fun enterPip() {
        if (!pipSupported) return
        session.hideControls()
        runCatching { enterPictureInPictureMode(pipParams(autoEnter = !session.ui.paused)) }.onFailure { Timber.w(it, "PiP failed") }
    }

    override fun onUserLeaveHint() {
        super.onUserLeaveHint()
        // Android 12+ auto-enters through the params set in updatePipParams(); older
        // versions need the explicit call here.
        if (pipSupported && ::session.isInitialized && !session.ui.paused && android.os.Build.VERSION.SDK_INT < 31) enterPip()
    }

    override fun onPictureInPictureModeChanged(isInPictureInPictureMode: Boolean, newConfig: android.content.res.Configuration) {
        super.onPictureInPictureModeChanged(isInPictureInPictureMode, newConfig)
        if (isInPictureInPictureMode) session.hideControls() else session.showControls()
    }

    override fun onKeyDown(keyCode: Int, event: KeyEvent): Boolean {
        if (!::session.isInitialized) return super.onKeyDown(keyCode, event)
        // Media keys and the diagnostics shortcuts work regardless of the control layer.
        // PLAY and PAUSE are explicit (HDMI-CEC, Bluetooth remotes and Assistant send them):
        // "pause" on a paused video must not start it.
        when (keyCode) {
            KeyEvent.KEYCODE_MEDIA_PLAY_PAUSE, KeyEvent.KEYCODE_SPACE -> { session.playPause(); return true }
            KeyEvent.KEYCODE_MEDIA_PLAY -> { session.playPause(play = true); return true }
            KeyEvent.KEYCODE_MEDIA_PAUSE -> { session.playPause(play = false); return true }
            KeyEvent.KEYCODE_MEDIA_STOP -> { session.reportProgress("stop"); finish(); return true }
            KeyEvent.KEYCODE_MEDIA_NEXT -> { session.actions.next(); session.showControls(); return true }
            KeyEvent.KEYCODE_MEDIA_PREVIOUS -> { session.actions.seekTo(0.0); session.showControls(); return true }
            KeyEvent.KEYCODE_MEDIA_REWIND -> { session.scrub(-1, event.repeatCount); return true }
            KeyEvent.KEYCODE_MEDIA_FAST_FORWARD -> { session.scrub(1, event.repeatCount); return true }
            KeyEvent.KEYCODE_MENU, KeyEvent.KEYCODE_I -> { session.toggleStats(); return true }
            KeyEvent.KEYCODE_CAPTIONS -> { session.openSheet(Sheet.Subtitles); return true }
            KeyEvent.KEYCODE_MEDIA_AUDIO_TRACK, KeyEvent.KEYCODE_A -> { session.openSheet(Sheet.Audio); return true }
            KeyEvent.KEYCODE_BACK -> {
                if (session.sheet != Sheet.None) { session.openSheet(Sheet.None); return true }
                if (session.controlsVisible) { session.hideControls(); return true }
                return super.onKeyDown(keyCode, event)
            }
        }
        if (!session.controlsVisible) {
            // Hidden: the first D-pad press reveals the controls (YouTube on TV); Left/Right
            // also start a scrub, and the controls open with focus on the seek bar, so the
            // following presses keep scrubbing instead of walking the buttons.
            when (keyCode) {
                KeyEvent.KEYCODE_DPAD_LEFT -> { session.scrub(-1, event.repeatCount); return true }
                KeyEvent.KEYCODE_DPAD_RIGHT -> { session.scrub(1, event.repeatCount); return true }
                KeyEvent.KEYCODE_DPAD_CENTER, KeyEvent.KEYCODE_ENTER, KeyEvent.KEYCODE_DPAD_UP, KeyEvent.KEYCODE_DPAD_DOWN -> { session.showControls(); return true }
            }
        } else {
            session.showControls() // any navigation keeps them on screen
        }
        return super.onKeyDown(keyCode, event) // Compose focus handles the rest
    }

    override fun onStart() { super.onStart(); RoxyApp.get(this).remote.setForeground(true) }

    override fun onStop() {
        if (::session.isInitialized) session.onStop()
        RoxyApp.get(this).remote.setForeground(false)
        super.onStop()
    }

    override fun onDestroy() {
        if (::session.isInitialized) session.destroy()
        super.onDestroy()
    }
}
