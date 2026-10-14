package tv.roxy.tv

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.tv.material3.Surface
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.api.ApiException
import tv.roxy.tv.diagnostics.DeviceReport
import tv.roxy.tv.ui.detail.MovieScreen
import tv.roxy.tv.ui.detail.SeriesScreen
import tv.roxy.tv.ui.home.HomeScreen
import tv.roxy.tv.ui.login.LoginScreen
import tv.roxy.tv.ui.nav.NavHost
import tv.roxy.tv.ui.nav.Screen
import tv.roxy.tv.ui.nav.rememberNavController
import tv.roxy.tv.ui.settings.SettingsScreen
import tv.roxy.tv.ui.theme.RoxyColors
import tv.roxy.tv.ui.theme.RoxyTheme

class MainActivity : ComponentActivity() {

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        Timber.i("Device report:\n%s", DeviceReport.full(this))
        setContent {
            RoxyTheme {
                Surface(modifier = Modifier.fillMaxSize().background(RoxyColors.background)) {
                    AppRoot()
                }
            }
        }
    }
}

/** Login when there is no session; otherwise the back-stack. The stored token is validated with GET /v1/auth/me. */
@Composable
private fun AppRoot() {
    val app = RoxyApp.get(LocalContext.current)
    val session by app.prefs.session.collectAsStateWithLifecycle()

    LaunchedEffect(session.token) {
        val token = session.token ?: return@LaunchedEffect
        runCatching { app.api.me() }
            .onSuccess { Timber.i("session ok: %s @ %s", it.name ?: "api-key", session.serverUrl) }
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

    val nav = rememberNavController(Screen.Home)
    NavHost(nav) { screen ->
        when (screen) {
            Screen.Home -> HomeScreen(
                onOpenSeries = { nav.push(Screen.Series(it.id, it.title)) },
                onOpenMovie = { nav.push(Screen.Movie(it.id, it.title)) },
                onOpenSettings = { nav.push(Screen.Settings) },
                onUnauthorized = { app.scope.launch { app.prefs.clearSession() } },
            )
            is Screen.Series -> SeriesScreen(
                screen.id, screen.title,
                onOpenMovie = { nav.push(Screen.Movie(it.id, it.title)) },
                onOpenSeries = { nav.push(Screen.Series(it.id, it.title)) },
            )
            is Screen.Movie -> MovieScreen(screen.id, screen.title, onOpenSeries = { nav.push(Screen.Series(it.id, it.title)) })
            Screen.Settings -> SettingsScreen(onLoggedOut = { while (nav.pop()) Unit })
        }
    }
}
