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
import tv.roxy.tv.ui.Splash
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
import androidx.navigation.toRoute
import androidx.tv.material3.Surface
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.api.ApiException
import tv.roxy.tv.diagnostics.DeviceReport
import tv.roxy.tv.ui.detail.MovieScreen
import tv.roxy.tv.ui.detail.SeriesScreen
import tv.roxy.tv.ui.home.HomeScreen
import tv.roxy.tv.ui.login.LoginScreen
import tv.roxy.tv.ui.setup.SetupWizard
import tv.roxy.tv.ui.nav.Route
import tv.roxy.tv.ui.phone.PhoneRoot
import tv.roxy.tv.ui.profiles.ProfilePicker
import tv.roxy.tv.ui.settings.SettingsScreen
import tv.roxy.tv.ui.theme.RoxyColors
import tv.roxy.tv.ui.theme.RoxyTheme

class MainActivity : ComponentActivity() {

    /** True while the activity is the floating picture-in-picture window (the episode page's player). */
    var inPip by mutableStateOf(false)
        private set

    /** Runs on the PiP callback itself, before the surface resizes (a Compose effect would be a frame late). */
    var onPipChanged: ((Boolean) -> Unit)? = null

    /** Android 15+: the enter animation is about to start. Pages hide their chrome and pin the video here. */
    var pipTransitioning by mutableStateOf(false)
        private set

    override fun onPictureInPictureUiStateChanged(pipState: android.app.PictureInPictureUiState) {
        super.onPictureInPictureUiStateChanged(pipState)
        if (android.os.Build.VERSION.SDK_INT >= 35 && pipState.isTransitioningToPip) {
            pipTransitioning = true
            onPipChanged?.invoke(true)
        }
    }

    /** The page's player tells us whether a video is playing, so swiping home shrinks it. */
    private var pipAutoEnter: Boolean? = null
    private var pipSource: android.graphics.Rect? = null

    val pipSupported: Boolean by lazy {
        !isTelevision(this) && packageManager.hasSystemFeature(PackageManager.FEATURE_PICTURE_IN_PICTURE)
    }

    private fun pipParams(autoEnter: Boolean): android.app.PictureInPictureParams {
        val b = android.app.PictureInPictureParams.Builder().setAspectRatio(android.util.Rational(16, 9))
        pipSource?.let { b.setSourceRectHint(it) }
        if (android.os.Build.VERSION.SDK_INT >= 31) {
            // Auto-enter makes the swipe-home transition a seamless shrink of the live
            // surface; the source rect is the video box, so the shrink starts from it.
            b.setAutoEnterEnabled(autoEnter).setSeamlessResizeEnabled(true)
        }
        return b.build()
    }

    /** Called by the page's player: keep the system's PiP parameters current. */
    fun updatePip(playing: Boolean, source: android.graphics.Rect?) {
        if (!pipSupported || isDestroyed) return
        val changed = pipAutoEnter != playing || pipSource != source
        pipAutoEnter = playing; pipSource = source
        if (changed) runCatching { setPictureInPictureParams(pipParams(autoEnter = playing)) }
    }

    /** Leave PiP support behind when the page closes, so other screens never shrink. */
    fun clearPip() {
        if (!pipSupported || isDestroyed) return
        pipAutoEnter = null; pipSource = null
        runCatching { setPictureInPictureParams(android.app.PictureInPictureParams.Builder().setAutoEnterEnabled(false).build()) }
    }

    fun enterPip() {
        if (!pipSupported) return
        runCatching { enterPictureInPictureMode(pipParams(autoEnter = pipAutoEnter == true)) }.onFailure { Timber.w(it, "PiP failed") }
    }

    override fun onUserLeaveHint() {
        super.onUserLeaveHint()
        // Android 12+ auto-enters through the params; older versions need the explicit call.
        if (pipSupported && pipAutoEnter == true && android.os.Build.VERSION.SDK_INT < 31) enterPip()
    }

    override fun onPictureInPictureModeChanged(isInPictureInPictureMode: Boolean, newConfig: Configuration) {
        super.onPictureInPictureModeChanged(isInPictureInPictureMode, newConfig)
        // Entering: already announced by the transition-start callback on Android 15+;
        // older versions hear it here. Leaving: always here.
        if (!isInPictureInPictureMode || !pipTransitioning) onPipChanged?.invoke(isInPictureInPictureMode)
        pipTransitioning = false
        inPip = isInPictureInPictureMode
    }

    override fun onStart() { super.onStart(); RoxyApp.get(this).remote.setForeground(true) }
    override fun onStop() { RoxyApp.get(this).remote.setForeground(false); super.onStop() }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        Timber.i("Device report:\n%s", DeviceReport.full(this))
        val tv = isTelevision(this)
        // The TV UI is a landscape, D-pad layout; the phone browses in portrait like Netflix
        // (the player is landscape on both, set in the manifest).
        requestedOrientation = if (tv) ActivityInfo.SCREEN_ORIENTATION_SENSOR_LANDSCAPE else ActivityInfo.SCREEN_ORIENTATION_PORTRAIT
        if (!tv) enableEdgeToEdge(SystemBarStyle.dark(AndroidColor.TRANSPARENT), SystemBarStyle.dark(AndroidColor.TRANSPARENT))
        setContent {
            RoxyTheme {
                Surface(modifier = Modifier.fillMaxSize().background(RoxyColors.background)) {
                    AppRoot(tv)
                }
            }
        }
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
                }
                .onFailure { t ->
                    if (t is ApiException && t.isUnauthorized) {
                        Timber.w("stored token rejected (401); logging out")
                        app.prefs.clearSession()
                    } else Timber.w("auth/me check failed (%s); keeping session", t.message)
                }
            Timber.d("token check done for %s", token.take(6))
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
    // Keyed by profile so every screen's ViewModel starts fresh after a switch.
    key(session.profileId) {
        if (tv) TvRoot(onUnauthorized) else PhoneRoot(onUnauthorized)
    }
}

/** The TV graph: Home with the hero and rows, detail pages, Settings from the hero's button. */
@Composable
private fun TvRoot(onUnauthorized: () -> Unit) {
    val nav = rememberNavController()
    NavHost(nav, startDestination = Route.Home) {
        composable<Route.Home> {
            HomeScreen(
                onOpenSeries = { nav.navigate(Route.Series(it.id, it.title)) },
                onOpenMovie = { nav.navigate(Route.Movie(it.id, it.title)) },
                onOpenSettings = { nav.navigate(Route.Settings) },
                onUnauthorized = onUnauthorized,
            )
        }
        composable<Route.Series> { entry ->
            val r = entry.toRoute<Route.Series>()
            SeriesScreen(
                r.id, r.title,
                onOpenMovie = { nav.navigate(Route.Movie(it.id, it.title)) },
                onOpenSeries = { nav.navigate(Route.Series(it.id, it.title)) },
            )
        }
        composable<Route.Movie> { entry ->
            val r = entry.toRoute<Route.Movie>()
            MovieScreen(r.id, r.title, onOpenSeries = { nav.navigate(Route.Series(it.id, it.title)) })
        }
        composable<Route.Settings> {
            SettingsScreen(onLoggedOut = { nav.popBackStack(Route.Home, inclusive = false) })
        }
    }
}
