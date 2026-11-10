package tv.roxy.tv.player

import android.content.Context
import android.content.Intent
import android.os.Bundle
import android.view.KeyEvent
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.ui.Modifier
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.WindowInsetsControllerCompat
import androidx.lifecycle.lifecycleScope
import kotlinx.coroutines.launch
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
        const val EXTRA_START_EXPLICIT = "startExplicit" // the requester chose EXTRA_START (see PlayRequest.explicitStart)
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
        const val EXTRA_SUBS_OFF = "subsOff" // the item was last watched with subtitles off
        const val EXTRA_SUB_INDEX = "subIndex"     // the item's saved subtitle stream (server numbering)
        const val EXTRA_AUDIO_INDEX = "audioIndex" // the item's saved audio stream
        const val EXTRA_SERIES_ID = "seriesId"     // the episode's show: "Go to Details" opens it without a lookup
        const val EXTRA_SERIES_TITLE = "seriesTitle"
        const val EXTRA_EPISODE = "episode"        // the item is an episode (else a film)

        /** Saved state: the item on screen and where it was, for a rebuilt player (see [restartPlan]). */
        private const val STATE_ITEM_ID = "player.itemId"
        private const val STATE_POSITION = "player.positionSec"

        /**
         * The item's saved subtitle and audio streams, as the request carried them. Found on
         * the TV: only "subtitles off" was passed here, so a saved track (Alya: "Dialogue")
         * never reached the player opened from a page; only the next episode in a row had it.
         */
        fun savedStreams(intent: Intent): Pair<Int?, Int?> =
            intent.getIntExtra(EXTRA_SUB_INDEX, 0).takeIf { it > 0 } to intent.getIntExtra(EXTRA_AUDIO_INDEX, 0).takeIf { it > 0 }

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
            explicitStart: Boolean = false,
            subtitlesOff: Boolean = false,
            subtitleIndex: Int? = null,
            audioIndex: Int? = null,
            seriesId: String? = null,
            seriesTitle: String? = null,
            episode: Boolean = false,
        ): Intent = Intent(context, PlayerActivity::class.java)
            .putExtra(EXTRA_SERIES_ID, seriesId)
            .putExtra(EXTRA_SERIES_TITLE, seriesTitle)
            .putExtra(EXTRA_EPISODE, episode)
            .putExtra(EXTRA_SUBS_OFF, subtitlesOff)
            .putExtra(EXTRA_SUB_INDEX, subtitleIndex?.takeIf { it > 0 } ?: 0)
            .putExtra(EXTRA_AUDIO_INDEX, audioIndex?.takeIf { it > 0 } ?: 0)
            .putExtra(EXTRA_START_EXPLICIT, explicitStart)
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
            intent(context, req.url, req.title, req.item.id, req.sourceId, req.startSeconds, settings, req.subtitleLine, req.local, anime = req.item.isAnime, explicitStart = req.explicitStart,
                subtitlesOff = req.item.userData?.subtitleIndex == -1, subtitleIndex = req.item.userData?.subtitleIndex, audioIndex = req.item.userData?.audioIndex,
                seriesId = req.item.seriesId, seriesTitle = req.item.seriesTitle, episode = req.item.isEpisode)

        /**
         * The Intent that describes what the player shows now, after autoplay or Next moved
         * it on to [req]. Never an explicit start: it is only the item, and a rebuild (see
         * [restartPlan]) asks the server where it is.
         */
        fun nowPlayingIntent(context: Context, req: PlayRequest, settings: PlayerSettings): Intent =
            intent(context, req.copy(explicitStart = false), settings)

        /** Saved with the activity's state: the item on screen (the next episode after autoplay) and its position. */
        fun saveState(out: Bundle, itemId: String?, positionSec: Int?) {
            if (itemId == null) return
            out.putString(STATE_ITEM_ID, itemId)
            if (positionSec != null) out.putInt(STATE_POSITION, positionSec)
        }

        /** A rebuilt player resolves [itemId] again; [fallbackStartSec] is used if the server can't say. */
        data class Restart(val itemId: String, val fallbackStartSec: Int)

        /**
         * What a player being created opens. A first start (no saved state) plays its Intent
         * as given. A rebuild (process death, a recreation) got the ORIGINAL Intent back
         * (setIntent is not kept by the system), which names the first episode with its old
         * start, or 0 after Play from start: replayed, episode 1 reopened there and its 10 s
         * reports overwrote its saved place. So a rebuild plays the item that was on screen,
         * resolved again so the server's position wins, never the Intent's explicit start.
         * Null: open [intent] as it is (a first start, or nothing to resolve: a bare URL).
         */
        fun restartPlan(intent: Intent, saved: Bundle?): Restart? {
            saved ?: return null
            val id = saved.getString(STATE_ITEM_ID) ?: intent.getStringExtra(EXTRA_ITEM_ID) ?: return null
            val fallback = if (saved.containsKey(STATE_POSITION)) saved.getInt(STATE_POSITION) else intent.getIntExtra(EXTRA_START, 0)
            return Restart(id, fallback)
        }

        /** Where "Go to Details" goes: a show's page for an episode, the film's own for a film. */
        data class DetailsTarget(val id: String, val movie: Boolean, val title: String)

        fun detailsTarget(item: tv.roxy.tv.api.Item): DetailsTarget? =
            if (item.isEpisode) item.seriesId?.let { DetailsTarget(it, movie = false, title = item.seriesTitle.orEmpty()) }
            else DetailsTarget(item.id, movie = true, title = item.title)

        /**
         * The details page for the player's [intent], read from its extras when they say
         * enough (the series id of an episode, or a film), else from [lookup] (the cache,
         * then the server). Null when neither knows: the player then stays open.
         */
        suspend fun resolveDetails(intent: Intent, itemId: String, lookup: suspend (String) -> tv.roxy.tv.api.Item?): DetailsTarget? {
            if (intent.getStringExtra(EXTRA_ITEM_ID) == itemId) {
                val seriesId = intent.getStringExtra(EXTRA_SERIES_ID)
                if (intent.getBooleanExtra(EXTRA_EPISODE, false) && seriesId != null) return DetailsTarget(seriesId, movie = false, title = intent.getStringExtra(EXTRA_SERIES_TITLE).orEmpty())
            }
            return lookup(itemId)?.let { detailsTarget(it) }
        }
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

    /** A rebuild's lookup of the item it was showing (see [restartPlan]); a new request cancels it. */
    private var restartJob: kotlinx.coroutines.Job? = null

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        // No FLAG_KEEP_SCREEN_ON here: PlayerSurface keeps the screen on only while playing.
        WindowCompat.setDecorFitsSystemWindows(window, false)
        WindowInsetsControllerCompat(window, window.decorView).apply {
            hide(WindowInsetsCompat.Type.systemBars())
            systemBarsBehavior = WindowInsetsControllerCompat.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
        }

        app = RoxyApp.get(this)
        restoredState = savedInstanceState
        val restart = restartPlan(intent, savedInstanceState)
        if (restart != null) restartJob = lifecycleScope.launch { restart(restart) }
        else if (!startSession(intent)) { finish(); return }
        setContent {
            // A new session gets a new SurfaceView: the old one's holder belongs to the old player.
            shown.value?.let { s -> androidx.compose.runtime.key(s) { PlayerSurface(s, modifier = Modifier.fillMaxSize()) } }
        }
    }

    /** The state this instance was rebuilt from: saved again as it was while no session is up yet. */
    private var restoredState: Bundle? = null

    /** A rebuilt player: the item it showed, at the server's position (see [restartPlan]). */
    private suspend fun restart(r: Restart) {
        Timber.i("player rebuilt: resolving %s again", r.itemId)
        val seed = app.items[r.itemId] ?: tv.roxy.tv.api.Item(
            id = r.itemId, type = if (intent.getBooleanExtra(EXTRA_EPISODE, false)) "episode" else "movie", title = intent.getStringExtra(EXTRA_TITLE) ?: "",
        )
        val req = tv.roxy.tv.ui.playRequest(this, seed).onFailure { Timber.w(it, "rebuilt player: lookup failed") }.getOrNull()
        val next = when {
            req != null -> nowPlayingIntent(this, req, app.prefs.settings.value)
            // Server away: the Intent's file if it is the same item, at the last known place.
            intent.getStringExtra(EXTRA_ITEM_ID) == r.itemId -> Intent(intent).putExtra(EXTRA_START, r.fallbackStartSec).putExtra(EXTRA_START_EXPLICIT, false)
            else -> null
        }
        restartJob = null
        if (next == null || isFinishing) { finish(); return }
        setIntent(next)
        if (!startSession(next)) finish()
    }

    override fun onSaveInstanceState(outState: Bundle) {
        super.onSaveInstanceState(outState)
        if (::session.isInitialized && !session.destroyed) saveState(outState, session.itemId, (session.positionMs() / 1000).toInt())
        else restoredState?.let { r -> saveState(outState, r.getString(STATE_ITEM_ID), if (r.containsKey(STATE_POSITION)) r.getInt(STATE_POSITION) else null) }
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
            // Already open: a request that chose a position (Play from start, a "Play on"
            // handover) jumps there; a plain repeat (a Watch Next card, Play again) resumes,
            // since this player's own position is fresher than the saved one it carries.
            if (intent.getBooleanExtra(EXTRA_START_EXPLICIT, false)) {
                val start = intent.getIntExtra(EXTRA_START, 0)
                Timber.i("play request for the item already playing: jump to %ds", start)
                session.actions.seekTo(start.toDouble())
            } else {
                Timber.i("play request for the item already playing: resume it")
            }
            session.playPause(play = true)
            return
        }
        Timber.i("new play request: %s", url.substringBefore('?'))
        restartJob?.cancel(); restartJob = null // a rebuild's lookup is superseded
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
            subtitlesOff = intent.getBooleanExtra(EXTRA_SUBS_OFF, false),
            subtitleIndex = savedStreams(intent).first,
            audioIndex = savedStreams(intent).second,
        )
        session = PlayerSession(this, app, params, lifecycleScope, host)
        shown.value = session
        return true
    }

    /** What every session of this activity calls back. */
    internal val host = object : PlayerSession.Host {
        override fun back() = finish()
        override fun ended() = finish()
        override fun openDetails(itemId: String?) = this@PlayerActivity.openDetails(itemId)
        override val pipSupported: Boolean get() = this@PlayerActivity.pipSupported
        override val inPip: Boolean get() = isInPictureInPictureMode
        override fun enterPip() = this@PlayerActivity.enterPip()
        override fun playingChanged(playing: Boolean) = updatePipParams(playing)
        // Autoplay / Next moved on: the Intent names the item on screen now, so anything
        // that reads it (a rebuild, the same-item check of a new request) sees this one.
        override fun nowPlaying(req: PlayRequest) = setIntent(nowPlayingIntent(this@PlayerActivity, req, RoxyApp.get(this@PlayerActivity).prefs.settings.value))
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
        if (!pipSupported || !::session.isInitialized) return
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
        if (!::session.isInitialized) return
        if (isInPictureInPictureMode) session.hideControls() else session.showControls()
    }

    override fun onKeyDown(keyCode: Int, event: KeyEvent): Boolean {
        if (!::session.isInitialized) return super.onKeyDown(keyCode, event)
        // A held key repeats: the keys that act once per press drop their repeats (see dropsRepeat).
        if (dropsRepeat(keyCode, event.repeatCount)) return true
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
                // Up a level: a list (Audio, Subtitles...) back to Settings, Settings closed.
                if (session.sheet != Sheet.None) { session.openSheet(if (session.sheet == Sheet.Settings) Sheet.None else Sheet.Settings); return true }
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

    override fun onStart() { super.onStart(); RoxyApp.get(this).remote.setForeground(true); RoxyApp.get(this).server.setForeground(true) }

    override fun onStop() {
        if (::session.isInitialized) session.onStop()
        RoxyApp.get(this).remote.setForeground(false)
        RoxyApp.get(this).server.setForeground(false)
        super.onStop()
    }

    override fun onDestroy() {
        if (::session.isInitialized) session.destroy()
        super.onDestroy()
    }

    /**
     * "Go to Details": the page of the show this episode is from (or the film itself), in the
     * app under the player. Coming from that page, closing the player shows it; otherwise the
     * app opens it (roxytv://open/<id>, see MainActivity.pendingOpen). The video pauses first
     * (the lookup can take seconds); the show comes from the Intent when it says, and only
     * otherwise from a lookup. When nothing can say, the player stays open (paused, with a
     * message) instead of closing on nothing.
     */
    private fun openDetails(itemId: String?) {
        val app = RoxyApp.get(this)
        if (::session.isInitialized) session.playPause(play = false)
        if (itemId == null) { finish(); return }
        lifecycleScope.launch {
            val target = resolveDetails(intent, itemId) { id ->
                app.items[id] ?: kotlinx.coroutines.withTimeoutOrNull(3_000) { runCatching { app.api.item(id) }.getOrNull() }
            }
            if (target == null) {
                Timber.w("go to details: %s not found", itemId)
                android.widget.Toast.makeText(applicationContext, "Couldn't open the details right now.", android.widget.Toast.LENGTH_LONG).show()
                if (::session.isInitialized) session.showControls()
                return@launch
            }
            val uri = tv.roxy.tv.tv.WatchNext.detailsUri(target.id, movie = target.movie, title = target.title)
            startActivity(Intent(Intent.ACTION_VIEW, uri, this@PlayerActivity, tv.roxy.tv.MainActivity::class.java)
                .addFlags(Intent.FLAG_ACTIVITY_REORDER_TO_FRONT or Intent.FLAG_ACTIVITY_SINGLE_TOP))
            finish()
        }
    }
}
