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
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Explore
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.outlined.Explore
import androidx.compose.material.icons.outlined.Bookmark
import androidx.compose.material.icons.outlined.GridView
import androidx.compose.material.icons.outlined.Home
import androidx.compose.material.icons.outlined.SystemUpdate
import androidx.compose.ui.text.style.TextOverflow
import tv.roxy.tv.data.UpdateState
import tv.roxy.tv.data.downloads.formatBytes
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
import tv.roxy.tv.ui.components.widthPx
import tv.roxy.tv.ui.nav.Route
import tv.roxy.tv.ui.profiles.AvatarCircle
import tv.roxy.tv.ui.theme.RoxyColors

private enum class Tab(val label: String, val route: Route) {
    Home("Home", Route.Home),
    Discover("Discover", Route.Discover),
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
        dest.hasRoute<Route.Discover>() -> Tab.Discover
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
                        onDownloads = { nav.navigate(Route.Downloads) },
                        onOpenDownloadedTitle = { nav.navigate(Route.DownloadedTitle(it)) },
                        onUnauthorized = onUnauthorized,
                        bottomInset = bottomInset,
                    )
                }
                composable<Route.Downloads> {
                    PhoneDownloads(
                        onBack = { nav.popBackStack() },
                        onOpenTitle = { nav.navigate(Route.DownloadedTitle(it)) },
                        onBrowse = {
                            nav.navigate(Tab.Browse.route) {
                                popUpTo(nav.graph.findStartDestination().id) { saveState = true }
                                launchSingleTop = true
                                restoreState = true
                            }
                        },
                    )
                }
                composable<Route.DownloadedTitle> { e ->
                    PhoneDownloadedTitle(titleId = e.toRoute<Route.DownloadedTitle>().id, onBack = { nav.popBackStack() })
                }
                composable<Route.Discover> {
                    PhoneDiscover(onOpen = { nav.navigate(Route.DiscoverDetail(it.type, it.tmdbId)) }, bottomInset = bottomInset)
                }
                composable<Route.DiscoverDetail> { e ->
                    val r = e.toRoute<Route.DiscoverDetail>()
                    PhoneDiscoverDetail(
                        type = r.type, tmdbId = r.tmdbId,
                        onBack = { nav.popBackStack() },
                        onOpenSeries = { id, title -> openSeries(id, title) },
                        onOpenMovie = { id, title -> openMovie(id, title) },
                    )
                }
                composable<Route.Watchlist> {
                    PhoneWatchlist(onOpenSeries = { openSeries(it.id, it.title) }, onOpenMovie = { openMovie(it.id, it.title) }, bottomInset = bottomInset)
                }
                composable<Route.Browse> {
                    PhoneBrowse(onOpenSeries = { openSeries(it.id, it.title) }, onOpenMovie = { openMovie(it.id, it.title) }, onSearch = { nav.navigate(Route.Search) }, bottomInset = bottomInset)
                }
                composable<Route.Account> {
                    PhoneAccount(bottomInset = bottomInset, onOpenDownloads = { nav.navigate(Route.Downloads) }, onOpenArrSettings = { nav.navigate(Route.ArrSettings) }, onOpenWebhooks = { nav.navigate(Route.Webhooks) }, onOpenLegal = { nav.navigate(Route.Legal(it)) })
                }
                composable<Route.ArrSettings> {
                    PhoneArrSettings(onBack = { nav.popBackStack() })
                }
                composable<Route.Webhooks> {
                    PhoneWebhooks(onBack = { nav.popBackStack() })
                }
                composable<Route.Legal> { e ->
                    PhoneLegal(doc = e.toRoute<Route.Legal>().doc, onBack = { nav.popBackStack() })
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
                        onOpenDetails = { nav.navigate(Route.Details(it.id, it.title)) },
                    )
                }
                composable<Route.Movie> { e ->
                    val r = e.toRoute<Route.Movie>()
                    PhoneMovie(movieId = r.id, fallbackTitle = r.title, onBack = { nav.popBackStack() }, onOpenSeries = { openSeries(it.id, it.title) }, onOpenDetails = { nav.navigate(Route.Details(it.id, it.title)) })
                }
                composable<Route.Details> { e ->
                    val r = e.toRoute<Route.Details>()
                    PhoneDetails(itemId = r.id, fallbackTitle = r.title, onBack = { nav.popBackStack() })
                }
            }
            if (currentTab != null) {
                val update by app.updates.state.collectAsStateWithLifecycle()
                Column(modifier = Modifier.align(Alignment.BottomCenter)) {
                    UpdateBar(update, onInstall = { app.updates.install() }, onDismiss = { app.updates.dismiss() })
                    BottomBar(
                        current = currentTab,
                        profileName = session.profileName.ifEmpty { session.userName },
                        avatarUrl = session.profileId?.takeIf { session.profileHasAvatar }?.let { app.api.profileAvatarUrl(it, widthPx(22.dp) * 3, session.profileAvatarKey.ifEmpty { session.profileAvatarVersion.toString() }) },
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
}

/**
 * A slim strip above the bottom bar when the server has a newer build: "Roxy 0.3.0 is
 * available" with Update and a close; shows download progress, then "Install" once the
 * APK is down (the installer opens by itself; this is the way back if it was dismissed).
 */
@Composable
private fun UpdateBar(state: UpdateState, onInstall: () -> Unit, onDismiss: () -> Unit) {
    if (state is UpdateState.None) return
    val update = when (state) {
        is UpdateState.Available -> state.update
        is UpdateState.Downloading -> state.update
        is UpdateState.Ready -> state.update
        is UpdateState.Failed -> state.update
        UpdateState.None -> return
    }
    Column(modifier = Modifier.fillMaxWidth().background(RoxyColors.surfaceHigh)) {
        Row(modifier = Modifier.fillMaxWidth().padding(start = PhonePad, end = 8.dp, top = 10.dp, bottom = 10.dp), verticalAlignment = Alignment.CenterVertically) {
            Icon(Icons.Outlined.SystemUpdate, contentDescription = null, tint = RoxyColors.accent, modifier = Modifier.size(22.dp))
            Column(modifier = Modifier.weight(1f).padding(start = 12.dp, end = 16.dp)) {
                val title = when (state) {
                    is UpdateState.Downloading -> "Downloading Roxy ${update.versionName}  ·  ${(state.fraction * 100).toInt()}%"
                    is UpdateState.Ready -> "Roxy ${update.versionName} is ready to install"
                    is UpdateState.Failed -> "Update failed  ·  ${state.message}"
                    else -> "Roxy ${update.versionName} is available"
                }
                Text(title, fontSize = 13.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground, maxLines = 1, overflow = TextOverflow.Ellipsis)
                val sub = listOfNotNull(update.notes.takeIf { it.isNotBlank() }, update.size.takeIf { it > 0 }?.let { formatBytes(it) }).joinToString("  ·  ")
                if (sub.isNotEmpty() && state !is UpdateState.Downloading) Text(sub, fontSize = 11.sp, color = RoxyColors.muted, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            if (state !is UpdateState.Downloading) {
                Text(
                    when (state) { is UpdateState.Ready -> "Install"; is UpdateState.Failed -> "Retry"; else -> "Update" },
                    fontSize = 13.sp, fontWeight = FontWeight.Bold, color = OnBrand,
                    modifier = Modifier.clip(RoundedCornerShape(16.dp)).background(RoxyColors.accent).clickable(onClick = onInstall).padding(horizontal = 16.dp, vertical = 8.dp),
                )
                Icon(Icons.Filled.Close, contentDescription = "Not now", tint = RoxyColors.muted, modifier = Modifier.padding(start = 10.dp).size(32.dp).clip(CircleShape).clickable(onClick = onDismiss).padding(7.dp))
            }
        }
        if (state is UpdateState.Downloading) {
            Box(modifier = Modifier.fillMaxWidth().height(2.dp).background(ChipGrey)) {
                Box(modifier = Modifier.fillMaxHeight().fillMaxWidth(state.fraction.coerceIn(0.02f, 1f)).background(RoxyColors.accent))
            }
        }
    }
}

/** Compact bottom bar: 22 dp icons, 10 sp labels, the Account tab as the profile avatar. */
@Composable
private fun BottomBar(current: Tab, profileName: String, avatarUrl: String?, modifier: Modifier = Modifier, onSelect: (Tab) -> Unit) {
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
                    Box(modifier = Modifier.size(24.dp).clip(CircleShape).background(if (selected) Color.White else Color.Transparent), contentAlignment = Alignment.Center) {
                        AvatarCircle(name = profileName, imageUrl = avatarUrl, size = if (selected) 20.dp else 22.dp)
                    }
                } else {
                    val icon: ImageVector = when (tab) {
                        Tab.Home -> if (selected) Icons.Filled.Home else Icons.Outlined.Home
                        Tab.Discover -> if (selected) Icons.Filled.Explore else Icons.Outlined.Explore
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

