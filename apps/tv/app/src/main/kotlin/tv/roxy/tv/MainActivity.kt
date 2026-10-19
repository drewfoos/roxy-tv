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
import tv.roxy.tv.ui.nav.Route
import tv.roxy.tv.ui.phone.PhoneRoot
import tv.roxy.tv.ui.profiles.ProfilePicker
import tv.roxy.tv.ui.settings.SettingsScreen
import tv.roxy.tv.ui.theme.RoxyColors
import tv.roxy.tv.ui.theme.RoxyTheme

class MainActivity : ComponentActivity() {

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

/** Login when there is no session; otherwise the TV or phone navigation graph. The stored token is validated with GET /v1/auth/me. */
@Composable
private fun AppRoot(tv: Boolean) {
    val app = RoxyApp.get(LocalContext.current)
    val session by app.prefs.session.collectAsStateWithLifecycle()

    LaunchedEffect(session.token) {
        val token = session.token ?: return@LaunchedEffect
        runCatching { app.api.me() }
            .onSuccess {
                Timber.i("session ok: %s @ %s", it.name ?: "api-key", session.serverUrl)
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

    if (!session.isLoggedIn) {
        LoginScreen()
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
