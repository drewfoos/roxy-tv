package tv.roxy.tv

import android.app.UiModeManager
import android.content.Context
import android.content.pm.ActivityInfo
import android.content.pm.PackageManager
import android.content.res.Configuration
import android.graphics.Color as AndroidColor
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.SystemBarStyle
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import tv.roxy.tv.ui.CompatGate
import tv.roxy.tv.ui.Splash
import tv.roxy.tv.ui.components.ServerBanner
import kotlinx.coroutines.delay
import androidx.compose.runtime.setValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.key
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.rememberNavController
import androidx.navigation.NavGraph.Companion.findStartDestination
import androidx.navigation.toRoute
import androidx.tv.material3.Surface
import androidx.tv.material3.SurfaceDefaults
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.api.ApiException
import tv.roxy.tv.diagnostics.DeviceReport
import tv.roxy.tv.ui.tv.TvMovie
import tv.roxy.tv.ui.tv.TvDiscoverTitle
import tv.roxy.tv.ui.tv.TvSeries
import tv.roxy.tv.ui.tv.TvMyLists
import tv.roxy.tv.ui.tv.TvDiscover
import tv.roxy.tv.ui.tv.TvBrowse
import tv.roxy.tv.ui.tv.TvSearch
import tv.roxy.tv.ui.tv.TvSettings
import tv.roxy.tv.api.Item
import tv.roxy.tv.ui.tv.RailPage
import tv.roxy.tv.ui.tv.TvHome
import tv.roxy.tv.ui.tv.TvShell
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.NavDestination.Companion.hasRoute
import androidx.compose.runtime.rememberCoroutineScope
import tv.roxy.tv.ui.login.LoginScreen
import tv.roxy.tv.tv.WatchNext
import tv.roxy.tv.ui.playItem
import tv.roxy.tv.ui.setup.SetupWizard
import tv.roxy.tv.ui.nav.Route
import tv.roxy.tv.ui.phone.PhoneRoot
import tv.roxy.tv.ui.profiles.ProfilePicker
import tv.roxy.tv.ui.theme.RoxyColors
import tv.roxy.tv.ui.theme.RoxyTheme

class MainActivity : ComponentActivity() {

    private companion object {
        /** How long after the window lost the focus the app looks at who has it (see [onWindowFocusChanged]). */
        const val LEAVING_LOOK_MS = 50L
    }

    /** True while the activity is the floating picture-in-picture window (the episode page's player). */
    var inPip by mutableStateOf(false)
        private set

    /**
     * The app is being left with a video playing, as far as can be told: it is on its way
     * into the PiP window, which the system makes by shrinking the app's window AS IT IS,
     * cropping it little by little to where the picture is. Whatever else is in the window
     * rides along until it is cropped away: the player's controls over the picture, and the
     * page under it (the series' name and the episode's sat along the bottom of the small
     * window for the last third of the way). So while this is set the page draws nothing
     * but the picture; it keeps its layout, because the system crops to where the picture
     * is in it.
     *
     * Set by whatever says so first: the window losing the focus to something that is not
     * the app's own ([leavingLook]: the Home gesture's only sign), the activity pausing, the
     * leave hint, Android 15's "transitioning". Cleared when the app is back: the window has
     * the focus again, the activity resumes outside PiP, PiP ends, or the page is touched.
     */
    var pipLeaving by mutableStateOf(false)
        private set

    override fun onPictureInPictureUiStateChanged(pipState: android.app.PictureInPictureUiState) {
        super.onPictureInPictureUiStateChanged(pipState)
        if (android.os.Build.VERSION.SDK_INT >= 35 && pipState.isTransitioningToPip && pip.armed) pipLeaving = true
    }

    override fun onResume() {
        super.onResume()
        // Left towards PiP and back without one (the system did not make the window).
        if (!isInPictureInPictureMode) pipLeaving = false
    }

    override fun onPause() {
        // Home by the button, another app over this one: with a video playing the PiP window
        // is next. (Split screen on Android 9 pauses the app that is merely not in use.)
        if (pip.armed && !isInMultiWindowMode) pipLeaving = true
        super.onPause()
    }

    /** The page is in use after all: a touch in the app's own window. */
    override fun dispatchTouchEvent(ev: android.view.MotionEvent): Boolean {
        if (ev.actionMasked == android.view.MotionEvent.ACTION_DOWN && pipLeaving && !isInPictureInPictureMode) pipLeaving = false
        return super.dispatchTouchEvent(ev)
    }

    /** What the system is told about the PiP window; the page with the player sets it. */
    private val pip = tv.roxy.tv.player.PipSettings()

    val pipSupported: Boolean by lazy {
        !isTelevision(this) && packageManager.hasSystemFeature(PackageManager.FEATURE_PICTURE_IN_PICTURE)
    }

    private fun pipParams(p: tv.roxy.tv.player.PipSettings.Params): android.app.PictureInPictureParams {
        val b = android.app.PictureInPictureParams.Builder()
            .setAspectRatio(android.util.Rational(p.shape.first, p.shape.second))
            // The picture's place in the window: the system shrinks the window cropped to it,
            // and grows the PiP window back towards it.
            .setSourceRectHint(p.source)
        if (android.os.Build.VERSION.SDK_INT >= 31) {
            // Auto-enter makes the swipe-home transition a seamless shrink of the live
            // surface. Seamless resize: the video is one buffer that the system scales (see
            // PictureSurface), so the window can change size without a cross-fade.
            b.setAutoEnterEnabled(p.autoEnter).setSeamlessResizeEnabled(true)
        }
        return b.build()
    }

    private fun sendPip(p: tv.roxy.tv.player.PipSettings.Params?) {
        if (p == null || !pipSupported || isDestroyed) return
        runCatching { setPictureInPictureParams(pipParams(p)) }.onFailure { Timber.w(it, "PiP settings refused") }
    }

    /** A page with a player takes the PiP settings over (see [tv.roxy.tv.player.PipSettings]); [onLeaving]: see [onWindowFocusChanged]. */
    fun claimPip(owner: Any, onLeaving: () -> Unit) = pip.claim(owner, onLeaving)

    /** The picture's shape (see playerBoxShape), so the PiP window has no bands either. */
    fun setPipShape(owner: Any, shape: Pair<Int, Int>) = sendPip(pip.shape(owner, shape))

    /** Called by the page's player: whether it plays, and where the picture is in the (whole) window. */
    fun updatePip(owner: Any, playing: Boolean, source: android.graphics.Rect?) {
        val decor = window?.decorView
        sendPip(pip.playing(owner, playing, source, decor?.width ?: 0, decor?.height ?: 0))
    }

    /** Leave PiP support behind when the page closes, so other screens never shrink. */
    fun clearPip(owner: Any) {
        if (!pip.clear(owner)) return
        pipLeaving = false
        if (!pipSupported || isDestroyed) return
        runCatching { setPictureInPictureParams(android.app.PictureInPictureParams.Builder().setAutoEnterEnabled(false).build()) }
    }

    /**
     * The Home gesture gives the app nothing to go by while the launcher shrinks its window
     * (about a second: no pause, no leave hint; Android 15's "transitioning" arrives when the
     * shrinking is over). The one thing that does change, a tenth of a second in, is the
     * window's focus. With a video playing that is taken as "being left for the PiP
     * window": the player's controls go at once, and the rest of the page a moment later
     * ([leavingLook]). It is also what a pulled-down notification shade looks like: the
     * same happens under it, and is undone when the focus is back.
     */
    override fun onWindowFocusChanged(hasFocus: Boolean) {
        super.onWindowFocusChanged(hasFocus)
        val decor = window?.decorView
        decor?.removeCallbacks(leavingLook)
        if (hasFocus) {
            if (!isInPictureInPictureMode) pipLeaving = false
            return
        }
        pip.leaving()
        // A menu or sheet of the page's own is a window too and takes the focus the same
        // way; its own "I have the focus" is a message or two behind this one. A moment for
        // it to arrive, and a second look for a slow one.
        decor?.postDelayed(leavingLook, LEAVING_LOOK_MS)
        decor?.postDelayed(leavingLook, LEAVING_LOOK_MS * 5)
    }

    private val leavingLook = Runnable {
        if (isInPictureInPictureMode || isDestroyed) return@Runnable
        val own = ownWindowFocused()
        if (own == true) pipLeaving = false
        else if (pip.leftByFocus(hasWindowFocus(), isInMultiWindowMode, own)) pipLeaving = true
    }

    /** Whether another window of this app (a dialog, a menu) has the focus; null when it cannot be told. */
    private fun ownWindowFocused(): Boolean? {
        if (android.os.Build.VERSION.SDK_INT < 29) return null
        val decor = window?.decorView
        return runCatching { android.view.inspector.WindowInspector.getGlobalWindowViews().any { it !== decor && it.hasWindowFocus() } }.getOrNull()
    }

    /** The settings page (a window of its own) lost the focus with a video playing: it has closed itself, and the app is being left. */
    fun pipLeavingNow(owner: Any) {
        if (pip.owns(owner) && pip.armed && !isInPictureInPictureMode) pipLeaving = true
    }

    fun enterPip() {
        if (!pipSupported) return
        runCatching { enterPictureInPictureMode(pipParams(pip.params())) }.onFailure { Timber.w(it, "PiP failed") }
    }

    override fun onUserLeaveHint() {
        super.onUserLeaveHint()
        if (!pipSupported || !pip.armed) return
        // The app is being left with a video playing: it is about to shrink.
        pipLeaving = true
        // Android 12+ auto-enters through the params; older versions need the explicit call.
        if (android.os.Build.VERSION.SDK_INT < 31) enterPip()
    }

    override fun onPictureInPictureModeChanged(isInPictureInPictureMode: Boolean, newConfig: Configuration) {
        super.onPictureInPictureModeChanged(isInPictureInPictureMode, newConfig)
        // In the PiP window the page is the picture alone anyway; out of it, it is all there
        // again from the first frame (the system grows the window back with the page in it).
        pipLeaving = false
        inPip = isInPictureInPictureMode
    }

    /** An item the launcher's Watch Next card asked for (roxytv://play/<id>); played once the app is signed in. */
    var pendingPlayId by mutableStateOf<String?>(null)

    /**
     * A TV's sign-in link (http://server/link/CODE, for the saved server only) opened in the
     * app: opens Account > Sign in a TV, where the code must still be typed from the TV.
     */
    var pendingLinkCode by mutableStateOf<String?>(null)

    /** A title's page the player asked for ("Go to Details", roxytv://open/<id>). */
    var pendingOpen by mutableStateOf<WatchNext.OpenDetails?>(null)

    override fun onNewIntent(intent: android.content.Intent) {
        super.onNewIntent(intent)
        WatchNext.itemIdFrom(intent.data)?.let { pendingPlayId = it }
        WatchNext.detailsFrom(intent.data)?.let { pendingOpen = it }
        linkCodeFrom(intent.data)?.let { pendingLinkCode = it }
    }

    /**
     * The code from a sign-in link, only when the link points at the server this app uses
     * (same host, case-insensitive). A link to any other host, or with no server saved yet,
     * is ignored: anyone can send a link, and approving it would sign THEIR TV in to this
     * account. The code itself is never trusted either (PhoneLinkTv makes the person type
     * the code from the TV's screen).
     */
    private fun linkCodeFrom(uri: android.net.Uri?): String? {
        uri ?: return null
        val scheme = uri.scheme?.lowercase()
        if (scheme != "http" && scheme != "https") return null
        val segs = uri.pathSegments ?: return null
        if (segs.size < 2 || segs[segs.size - 2] != "link") return null
        val saved = tv.roxy.tv.data.ServerAddress.hostOf(RoxyApp.get(this).prefs.session.value.serverUrl)
        val host = uri.host?.lowercase()?.removePrefix("[")?.removeSuffix("]")?.trimEnd('.')
        if (saved == null || host == null || host != saved) {
            Timber.w("ignoring a sign-in link for another host")
            return null
        }
        return segs.last().uppercase().filter { it.isLetterOrDigit() }.takeIf { it.length >= 6 }
    }

    override fun onStart() { super.onStart(); RoxyApp.get(this).remote.setForeground(true); RoxyApp.get(this).server.setForeground(true) }
    override fun onStop() { RoxyApp.get(this).remote.setForeground(false); RoxyApp.get(this).server.setForeground(false); super.onStop() }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        // MediaCodecList and display queries take a few hundred ms on a TV's SoC: not on the main thread.
        val ctx = this
        Thread({ runCatching { Timber.i("Device report:\n%s", DeviceReport.full(ctx)) } }, "device-report").start()
        val tv = isTelevision(this)
        // The link that launched the app, once. A rebuild (process death while the player was
        // up, a configuration change) gets the same launch Intent again: reading it then
        // replayed the Watch Next card, so Back from the player opened it once more.
        if (savedInstanceState == null) {
            WatchNext.itemIdFrom(intent?.data)?.let { pendingPlayId = it }
            WatchNext.detailsFrom(intent?.data)?.let { pendingOpen = it }
            linkCodeFrom(intent?.data)?.let { pendingLinkCode = it }
        }
        // The TV UI is a landscape, D-pad layout; the phone browses in portrait like Netflix
        // (the player is landscape on both, set in the manifest).
        requestedOrientation = if (tv) ActivityInfo.SCREEN_ORIENTATION_SENSOR_LANDSCAPE else ActivityInfo.SCREEN_ORIENTATION_PORTRAIT
        if (!tv) enableEdgeToEdge(SystemBarStyle.dark(AndroidColor.TRANSPARENT), SystemBarStyle.dark(AndroidColor.TRANSPARENT))
        setContent {
            RoxyTheme {
                // The Surface paints the page colour itself; a second background modifier
                // would be another full-screen fill on every frame.
                Surface(modifier = Modifier.fillMaxSize(), colors = SurfaceDefaults.colors(containerColor = RoxyColors.background)) {
                    androidx.compose.foundation.layout.Box(Modifier.fillMaxSize()) {
                        CompatGate(tv) { AppRoot(tv) }
                        // Above every page: "Can't reach Roxy" while the server is away. The phone
                        // says it its own way (a strip over the bottom bar, offline pages).
                        if (tv) ServerBanner(tv)
                    }
                }
            }
        }
        // The content is opaque, so the window's own background is one more full-screen
        // fill the GPU can skip (a TV's fill rate is the frame budget's bottleneck).
        window.setBackgroundDrawable(null)
    }
}

/** True on Android TV / Google TV (leanback), false on phones and tablets. */
fun isTelevision(context: Context): Boolean {
    val uiMode = context.getSystemService(Context.UI_MODE_SERVICE) as UiModeManager
    return uiMode.currentModeType == Configuration.UI_MODE_TYPE_TELEVISION ||
        context.packageManager.hasSystemFeature(PackageManager.FEATURE_LEANBACK)
}

/**
 * Splash (hat breathing while the stored session is checked), then Login (which has the
 * Terms of Use checkbox), then "Who's watching?", then the TV or phone graph. The stored token is validated with
 * GET /v1/auth/me.
 */
@Composable
private fun AppRoot(tv: Boolean) {
    val app = RoxyApp.get(LocalContext.current)
    val session by app.prefs.session.collectAsStateWithLifecycle()
    var splash by remember { mutableStateOf(true) }

    LaunchedEffect(session.token) {
        val started = System.currentTimeMillis()
        val token = session.token
        if (token != null) {
            runCatching { app.api.me() }
                .onSuccess {
                    Timber.i("session ok: %s @ %s", it.name ?: "api-key", session.serverUrl)
                    app.prefs.setAdmin(it.isAdmin)
                    // Where else the server answers (home address, Tailscale): the app moves between them.
                    if (app.prefs.setAddresses(it.addresses, it.serverId)) app.server.checkSoon()
                    // Terms are agreed by the checkbox on the sign-in screen; record that with the
                    // server once (it keeps the version per login), and mirror what it knows.
                    runCatching { app.api.legalStatus() }.onSuccess { st ->
                        if (st.accepted) app.prefs.setAcceptedTerms(st.version)
                        else if (session.acceptedTerms.isNotEmpty()) runCatching { app.api.acceptLegal(st.version) }.onSuccess { app.prefs.setAcceptedTerms(st.version) }
                    }
                    // Positions watched offline (downloads) reach the server now; queued downloads resume.
                    app.downloads.syncProgress(app.api)
                    app.downloads.kick()
                    app.updates.check()
                    // Subtitle files a download could not get are asked for again (last: nothing waits on it).
                    app.downloads.retryMissingSidecars(app.api)
                }
                .onFailure { t ->
                    if (t is ApiException && t.isUnauthorized) {
                        Timber.w("stored token rejected (401); logging out")
                        app.prefs.clearSession()
                    } else Timber.w("auth/me check failed (%s); keeping session", t.message)
                }
            Timber.d("token check done")
        }
        // At least a beat of the hat, so a warm start never flashes from the icon into Home.
        val remaining = 1200 - (System.currentTimeMillis() - started)
        if (remaining > 0) delay(remaining)
        splash = false
    }

    if (splash) {
        Splash()
        return
    }
    if (!session.isLoggedIn) {
        LoginScreen()
        return
    }
    // Signed in by the setup wizard, which isn't finished: carry on where it was.
    if (session.setupInProgress) {
        SetupWizard(serverUrl = session.serverUrl, serverName = "")
        return
    }
    // "Who's watching?" until a profile is chosen (and again after Switch profile).
    if (!session.hasProfile) {
        ProfilePicker()
        return
    }

    val onUnauthorized = { app.scope.launch { app.prefs.clearSession() }; Unit }
    // The launcher's Watch Next card: resume that title now that the session is ready.
    val activity = LocalContext.current as? MainActivity
    val pendingPlay = activity?.pendingPlayId
    LaunchedEffect(pendingPlay) {
        if (activity == null || pendingPlay == null) return@LaunchedEffect
        // Clearing the id restarts this effect, so the play runs on the app's scope, not here.
        activity.pendingPlayId = null
        app.scope.launch {
            val item = app.items[pendingPlay] ?: runCatching { app.api.item(pendingPlay) }.getOrNull() ?: return@launch
            playItem(activity, item).onFailure { Timber.w(it, "watch next play failed") }
        }
    }
    // Launcher Watch Next rows are per profile: another profile's go when this one is in use.
    LaunchedEffect(session.profileId) { WatchNext.keepFor(app, session.profileId) }
    // Keyed by profile so every screen's ViewModel starts fresh after a switch.
    key(session.profileId) {
        if (tv) TvRoot(onUnauthorized) else PhoneRoot(onUnauthorized)
    }
}

/**
 * The TV graph: the rail's pages (Home, Watchlist, History, Browse, Search, Settings) are
 * switched by the rail and keep Home under them, so Back from any of them is Home; title
 * pages (series, movie) push on top and have no rail, as on Crunchyroll.
 */
@Composable
private fun TvRoot(onUnauthorized: () -> Unit) {
    val nav = rememberNavController()
    val app = RoxyApp.get(LocalContext.current)
    val scope = rememberCoroutineScope()
    val entry by nav.currentBackStackEntryAsState()
    // Matched by the route's serial name (what Navigation stores for a typed route); a class
    // name would not survive R8.
    val routeName = entry?.destination?.route
    val page = RailPage.entries.firstOrNull { routeName == it.routeName }
    var homeTick by remember { mutableStateOf(0) }
    fun go(p: RailPage) {
        // Read the page at call time: the rail's click handlers can outlive a composition.
        val now = nav.currentBackStackEntry?.destination?.route?.let { r -> RailPage.entries.firstOrNull { it.routeName == r } }
        if (p == now) { if (p == RailPage.Home) homeTick++; return }
        nav.navigate(p.route) { popUpTo(nav.graph.findStartDestination().id) { inclusive = false }; launchSingleTop = true }
    }
    val openSeries = { it: Item -> nav.navigate(Route.Series(it.id, it.title)) }
    val openMovie = { it: Item -> nav.navigate(Route.Movie(it.id, it.title)) }
    val openDiscover = { type: String, tmdbId: Int -> nav.navigate(Route.DiscoverDetail(type, tmdbId)) }
    // The player's "Go to Details": the page under the player is usually that title's
    // already (it was played from there), and then closing the player was enough.
    val activity = LocalContext.current as? MainActivity
    val open = activity?.pendingOpen
    LaunchedEffect(open) {
        if (activity == null || open == null) return@LaunchedEffect
        activity.pendingOpen = null
        val here = nav.currentBackStackEntry
        val showing = here?.arguments?.getString("id") == open.id && (here.destination.route?.let { r -> r.contains(if (open.movie) "Movie" else "Series") } == true)
        if (!showing) nav.navigate(if (open.movie) Route.Movie(open.id, open.title) else Route.Series(open.id, open.title))
    }
    TvShell(current = page, onSelect = ::go, onSwitchProfile = { scope.launch { app.prefs.clearProfile() } }) {
        // No page transitions on the TV: navigation-compose's default is a 700 ms crossfade,
        // two full pages blended for ~40 frames on every page change (measured: p90 40 ms).
        NavHost(
            nav, startDestination = Route.Home,
            enterTransition = { androidx.compose.animation.EnterTransition.None },
            exitTransition = { androidx.compose.animation.ExitTransition.None },
            popEnterTransition = { androidx.compose.animation.EnterTransition.None },
            popExitTransition = { androidx.compose.animation.ExitTransition.None },
        ) {
            composable<Route.Home> { TvHome(onOpenSeries = openSeries, onOpenMovie = openMovie, onUnauthorized = onUnauthorized, refreshTick = homeTick) }
            composable<Route.Series> { entry ->
                val r = entry.toRoute<Route.Series>()
                TvSeries(r.id, r.title, onOpenSeries = openSeries, onOpenMovie = openMovie, onOpenDiscover = openDiscover)
            }
            composable<Route.Movie> { entry ->
                val r = entry.toRoute<Route.Movie>()
                TvMovie(r.id, r.title, onOpenSeries = openSeries, onOpenMovie = openMovie, onOpenDiscover = openDiscover)
            }
            composable<Route.DiscoverDetail> { entry ->
                val r = entry.toRoute<Route.DiscoverDetail>()
                TvDiscoverTitle(r.type, r.tmdbId, onOpenSeries = openSeries, onOpenMovie = openMovie, onOpenDiscover = openDiscover)
            }
            composable<Route.Watchlist> { TvMyLists(onOpenSeries = openSeries, onOpenMovie = openMovie) }
            composable<Route.Discover> { TvDiscover(onOpenSeries = openSeries, onOpenMovie = openMovie, onOpenDiscover = { type, tmdbId -> openDiscover(type, tmdbId) }) }
            composable<Route.Browse> { TvBrowse(onOpenSeries = openSeries, onOpenMovie = openMovie) }
            composable<Route.Search> { TvSearch(onOpenSeries = openSeries, onOpenMovie = openMovie) }
            composable<Route.Settings> {
                TvSettings(onSwitchProfile = { scope.launch { app.prefs.clearProfile() } }, onLoggedOut = { nav.popBackStack(Route.Home, inclusive = false) })
            }
        }
    }
}
