package tv.roxy.tv.ui.phone

import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.asPaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.outlined.Bookmark
import androidx.compose.material.icons.outlined.GridView
import androidx.compose.material.icons.outlined.Home
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.NavDestination.Companion.hasRoute
import androidx.navigation.NavGraph.Companion.findStartDestination
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import androidx.navigation.toRoute
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.ui.nav.Route
import tv.roxy.tv.ui.theme.RoxyColors

private enum class Tab(val label: String, val route: Route) {
    Home("Home", Route.Home),
    Watchlist("Watchlist", Route.Watchlist),
    Browse("Browse", Route.Browse),
    Account("Account", Route.Account),
}

/** Height of the bottom bar's content (small, like most apps); the system inset is added on top. */
private val BarHeight = 54.dp

/**
 * Phone shell: a compact bottom bar (Home, Watchlist, Browse, Account) over a Navigation
 * Compose graph. Tabs are top-level destinations with saved state; detail pages and
 * Search push on top and hide the bar. Transitions are plain short fades so the system
 * back gesture does not zoom or slide anything.
 */
@Composable
fun PhoneRoot(onUnauthorized: () -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    val session by app.prefs.session.collectAsStateWithLifecycle()
    val nav = rememberNavController()
    val entry by nav.currentBackStackEntryAsState()
    val dest = entry?.destination
    val currentTab = when {
        dest == null -> null
        dest.hasRoute<Route.Home>() -> Tab.Home
        dest.hasRoute<Route.Watchlist>() -> Tab.Watchlist
        dest.hasRoute<Route.Browse>() -> Tab.Browse
        dest.hasRoute<Route.Account>() -> Tab.Account
        else -> null
    }
    val bottomInset = BarHeight + WindowInsets.navigationBars.asPaddingValues().calculateBottomPadding()

    fun openSeries(id: String, title: String) = nav.navigate(Route.Series(id, title))
    fun openMovie(id: String, title: String) = nav.navigate(Route.Movie(id, title))

    PhoneTheme {
        Box(modifier = Modifier.fillMaxSize().background(RoxyColors.background)) {
            NavHost(
                nav, startDestination = Route.Home, modifier = Modifier.fillMaxSize(),
                enterTransition = { fadeIn(tween(160)) },
                exitTransition = { fadeOut(tween(160)) },
                popEnterTransition = { fadeIn(tween(160)) },
                popExitTransition = { fadeOut(tween(160)) },
            ) {
                composable<Route.Home> {
                    PhoneHome(
                        onOpenSeries = { openSeries(it.id, it.title) },
                        onOpenMovie = { openMovie(it.id, it.title) },
                        onSearch = { nav.navigate(Route.Search) },
                        onUnauthorized = onUnauthorized,
                        bottomInset = bottomInset,
                    )
                }
                composable<Route.Watchlist> {
                    PhoneWatchlist(onOpenSeries = { openSeries(it.id, it.title) }, onOpenMovie = { openMovie(it.id, it.title) }, bottomInset = bottomInset)
                }
                composable<Route.Browse> {
                    PhoneBrowse(onOpenSeries = { openSeries(it.id, it.title) }, onOpenMovie = { openMovie(it.id, it.title) }, onSearch = { nav.navigate(Route.Search) }, bottomInset = bottomInset)
                }
                composable<Route.Account> {
                    PhoneAccount(bottomInset = bottomInset)
                }
                composable<Route.Search> {
                    PhoneSearch(onBack = { nav.popBackStack() }, onOpenSeries = { openSeries(it.id, it.title) }, onOpenMovie = { openMovie(it.id, it.title) })
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
                BottomBar(
                    current = currentTab,
                    userInitial = session.userName.firstOrNull()?.uppercaseChar()?.toString() ?: "?",
                    modifier = Modifier.align(Alignment.BottomCenter),
                    onSelect = { tab ->
                        if (tab == currentTab) return@BottomBar
                        nav.navigate(tab.route) {
                            popUpTo(nav.graph.findStartDestination().id) { saveState = true }
                            launchSingleTop = true
                            restoreState = true
                        }
                    },
                )
            }
        }
    }
}

/** Compact bottom bar: 22 dp icons, 10 sp labels, the Account tab as the profile avatar. */
@Composable
private fun BottomBar(current: Tab, userInitial: String, modifier: Modifier = Modifier, onSelect: (Tab) -> Unit) {
    Row(
        modifier = modifier.fillMaxWidth().background(Color(0xFA0B0B0F)).navigationBarsPadding().height(BarHeight),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Tab.entries.forEach { tab ->
            val selected = tab == current
            val tint = if (selected) Color.White else RoxyColors.muted
            Column(
                modifier = Modifier.weight(1f).fillMaxSize().clickable(interactionSource = remember { MutableInteractionSource() }, indication = null) { onSelect(tab) },
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = androidx.compose.foundation.layout.Arrangement.Center,
            ) {
                if (tab == Tab.Account) {
                    Avatar(userInitial, size = 22.dp, ring = selected)
                } else {
                    val icon: ImageVector = when (tab) {
                        Tab.Home -> if (selected) Icons.Filled.Home else Icons.Outlined.Home
                        Tab.Watchlist -> Icons.Outlined.Bookmark
                        else -> Icons.Outlined.GridView
                    }
                    Icon(icon, contentDescription = tab.label, tint = tint, modifier = Modifier.size(22.dp))
                }
                Text(tab.label, fontSize = 10.sp, color = tint, fontWeight = if (selected) FontWeight.SemiBold else FontWeight.Normal)
            }
        }
    }
}

/** Profile picture placeholder: a coloured disc with the user's initial. */
@Composable
fun Avatar(initial: String, size: androidx.compose.ui.unit.Dp, ring: Boolean = false, modifier: Modifier = Modifier) {
    Box(
        modifier = modifier.size(size).clip(CircleShape).background(if (ring) Color.White else RoxyColors.accent),
        contentAlignment = Alignment.Center,
    ) {
        Box(modifier = Modifier.size(if (ring) size - 3.dp else size).clip(CircleShape).background(RoxyColors.accent), contentAlignment = Alignment.Center) {
            Text(initial, color = Color.White, fontSize = (size.value * 0.5f).sp, fontWeight = FontWeight.Bold)
        }
    }
}
