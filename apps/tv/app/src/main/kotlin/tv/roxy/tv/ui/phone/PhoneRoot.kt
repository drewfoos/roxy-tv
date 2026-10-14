package tv.roxy.tv.ui.phone

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.asPaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.outlined.Movie
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material.icons.outlined.Tv
import androidx.compose.material3.Icon
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.NavigationBarItemDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.navigation.NavDestination.Companion.hasRoute
import androidx.navigation.NavGraph.Companion.findStartDestination
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import androidx.navigation.toRoute
import tv.roxy.tv.ui.nav.Route
import tv.roxy.tv.ui.theme.RoxyColors

private enum class Tab(val label: String, val icon: ImageVector, val route: Route) {
    Home("Home", Icons.Filled.Home, Route.Home),
    Series("Series", Icons.Outlined.Tv, Route.SeriesLibrary),
    Movies("Movies", Icons.Outlined.Movie, Route.MovieLibrary),
    Settings("Settings", Icons.Outlined.Settings, Route.Settings),
}

/** Height of the bottom bar's content; the system navigation inset is added on top. */
private val BarHeight = 80.dp

/**
 * Phone shell: Netflix-style bottom bar over a Navigation Compose graph. The four tabs
 * are top-level destinations (state saved and restored when switching); detail pages
 * push on top and hide the bar.
 */
@Composable
fun PhoneRoot(onUnauthorized: () -> Unit) {
    val nav = rememberNavController()
    val entry by nav.currentBackStackEntryAsState()
    val dest = entry?.destination
    val currentTab = when {
        dest == null -> null
        dest.hasRoute<Route.Home>() -> Tab.Home
        dest.hasRoute<Route.SeriesLibrary>() -> Tab.Series
        dest.hasRoute<Route.MovieLibrary>() -> Tab.Movies
        dest.hasRoute<Route.Settings>() -> Tab.Settings
        else -> null
    }
    val bottomInset = BarHeight + WindowInsets.navigationBars.asPaddingValues().calculateBottomPadding()

    fun openSeries(id: String, title: String) = nav.navigate(Route.Series(id, title))
    fun openMovie(id: String, title: String) = nav.navigate(Route.Movie(id, title))

    PhoneTheme {
        Box(modifier = Modifier.fillMaxSize().background(RoxyColors.background)) {
            NavHost(nav, startDestination = Route.Home, modifier = Modifier.fillMaxSize()) {
                composable<Route.Home> {
                    PhoneHome(
                        onOpenSeries = { openSeries(it.id, it.title) },
                        onOpenMovie = { openMovie(it.id, it.title) },
                        onUnauthorized = onUnauthorized,
                        bottomInset = bottomInset,
                    )
                }
                composable<Route.SeriesLibrary> {
                    PhoneBrowse(type = "series", title = "Series", onOpen = { openSeries(it.id, it.title) }, bottomInset = bottomInset)
                }
                composable<Route.MovieLibrary> {
                    PhoneBrowse(type = "movie", title = "Movies", onOpen = { openMovie(it.id, it.title) }, bottomInset = bottomInset)
                }
                composable<Route.Settings> {
                    PhoneSettings(bottomInset = bottomInset)
                }
                composable<Route.Series> { e ->
                    val r = e.toRoute<Route.Series>()
                    PhoneSeries(
                        seriesId = r.id, fallbackTitle = r.title,
                        onBack = { nav.popBackStack() },
                        onOpenMovie = { openMovie(it.id, it.title) },
                        onOpenSeries = { openSeries(it.id, it.title) },
                    )
                }
                composable<Route.Movie> { e ->
                    val r = e.toRoute<Route.Movie>()
                    PhoneMovie(movieId = r.id, fallbackTitle = r.title, onBack = { nav.popBackStack() }, onOpenSeries = { openSeries(it.id, it.title) })
                }
            }
            if (currentTab != null) {
                NavigationBar(
                    modifier = Modifier.align(Alignment.BottomCenter),
                    containerColor = Color(0xFA0B0B0F),
                ) {
                    Tab.entries.forEach { tab ->
                        NavigationBarItem(
                            selected = tab == currentTab,
                            onClick = {
                                if (tab == currentTab) return@NavigationBarItem
                                nav.navigate(tab.route) {
                                    popUpTo(nav.graph.findStartDestination().id) { saveState = true }
                                    launchSingleTop = true
                                    restoreState = true
                                }
                            },
                            icon = { Icon(tab.icon, contentDescription = tab.label) },
                            label = { Text(tab.label, fontSize = 11.sp) },
                            colors = NavigationBarItemDefaults.colors(
                                selectedIconColor = Color.White,
                                selectedTextColor = Color.White,
                                unselectedIconColor = RoxyColors.muted,
                                unselectedTextColor = RoxyColors.muted,
                                indicatorColor = Color.Transparent,
                            ),
                        )
                    }
                }
            }
        }
    }
}
