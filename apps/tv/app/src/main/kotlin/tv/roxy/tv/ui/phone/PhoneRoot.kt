package tv.roxy.tv.ui.phone

import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import tv.roxy.tv.api.Item
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.ExitTransition
import androidx.compose.animation.EnterTransition
import androidx.navigation.NavDestination
import androidx.navigation.NavBackStackEntry
import androidx.compose.animation.slideOutHorizontally
import androidx.compose.animation.slideInHorizontally
import androidx.compose.animation.AnimatedContentTransitionScope
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
import androidx.compose.runtime.DisposableEffect
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
    Browse("Browse", Route.Browse),
    Watchlist("My Lists", Route.Watchlist),
    Account("Account", Route.Account),
}

/**
 * Every destination paints its own black page. The slide transitions put one page over
 * another, and a page that let the background colour show through from the host would
 * be see-through everywhere but its images.
 */
@Composable
private fun Page(content: @Composable () -> Unit) {
    Box(modifier = Modifier.fillMaxSize().background(RoxyColors.background)) { content() }
}

private fun NavDestination.isTab() = Tab.entries.any { hasRoute(it.route::class) }

/** Push/pop slide timing: quick with a soft landing. */
private fun <T> pushSpec() = tween<T>(300, easing = FastOutSlowInEasing)

/** Both ends of a navigation are bottom-bar tabs, so the pages swap in place rather than push. */
private fun AnimatedContentTransitionScope<NavBackStackEntry>.tabToTab() = initialState.destination.isTab() && targetState.destination.isTab()

/** Height of the bottom bar's content (small, like most apps); the system inset is added on top. */
private val BarHeight = 56.dp

/**
 * Phone shell: a compact bottom bar (Home, Watchlist, Browse, Account) over a Navigation
 * Compose graph. Tabs are top-level destinations with saved state; detail pages and
 * Search push on top and hide the bar. Tabs swap with a short fade; pushed pages slide
 * in from the right and the predictive back gesture drags them out the same way.
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

    // The tapped item goes into the cache first, so the page can paint its header from it
    // while it slides in.
    fun openSeries(item: Item) { app.items.put(item); nav.navigate(Route.Series(item.id, item.title)) }
    fun openMovie(item: Item) { app.items.put(item); nav.navigate(Route.Movie(item.id, item.title)) }
    fun openEpisode(item: Item) { app.items.put(item); nav.navigate(Route.Episode(item.id, item.title)) }
    // Every play on the phone (hero, Resume pill, downloads, episode rows) lands on the player page.
    DisposableEffect(nav) {
        app.openPlayerPage = { item -> openEpisode(item) }
        app.openArrSettings = { nav.navigate(Route.ArrSettings) }
        onDispose { app.openPlayerPage = null; app.openArrSettings = null }
    }

    PhoneTheme {
        Box(modifier = Modifier.fillMaxSize().background(RoxyColors.background)) {
            NavHost(
                nav, startDestination = Route.Home, modifier = Modifier.fillMaxSize(),
                // Tab to tab: a short fade. Pushing a page (series, movie, search, details):
                // it slides in from the right over the page beneath, which stays put, like
                // Crunchyroll. Back slides it out to the right again. The predictive back
                // gesture gets the same slide explicitly, otherwise Navigation's default
                // shrinks the page while the finger moves.
                enterTransition = { if (tabToTab()) fadeIn(tween(160)) else slideInHorizontally(pushSpec()) { it } },
                exitTransition = { if (tabToTab()) fadeOut(tween(160)) else ExitTransition.KeepUntilTransitionsFinished },
                popEnterTransition = { if (tabToTab()) fadeIn(tween(160)) else EnterTransition.None },
                popExitTransition = { if (tabToTab()) fadeOut(tween(160)) else slideOutHorizontally(pushSpec()) { it } },
                predictivePopEnterTransition = { _ -> EnterTransition.None },
                predictivePopExitTransition = { _ -> slideOutHorizontally(pushSpec()) { it } },
            ) {
                composable<Route.Home> {
                    Page {
                        PhoneHome(
                            onOpenSeries = { openSeries(it) },
                            onOpenMovie = { openMovie(it) },
                            onSearch = { nav.navigate(Route.Search) },
                            onDownloads = { nav.navigate(Route.Downloads) },
                            onOpenDownloadedTitle = { nav.navigate(Route.DownloadedTitle(it)) },
                            onUnauthorized = onUnauthorized,
                            bottomInset = bottomInset,
                        )
                    }
                }
                composable<Route.Downloads> {
                    Page {
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
                }
                composable<Route.DownloadedTitle> { e ->
                    Page {
                        PhoneDownloadedTitle(titleId = e.toRoute<Route.DownloadedTitle>().id, onBack = { nav.popBackStack() })
                    }
                }
                composable<Route.Discover> {
                    Page {
                        PhoneDiscover(onOpen = { nav.navigate(Route.DiscoverDetail(it.type, it.tmdbId)) }, bottomInset = bottomInset)
                    }
                }
                composable<Route.DiscoverDetail> { e ->
                    Page {
                        val r = e.toRoute<Route.DiscoverDetail>()
                        PhoneDiscoverDetail(
                            type = r.type, tmdbId = r.tmdbId,
                            onBack = { nav.popBackStack() },
                            onOpenSeries = { id, title -> nav.navigate(Route.Series(id, title)) }, // Discover has no library item to seed from
                            onOpenMovie = { id, title -> nav.navigate(Route.Movie(id, title)) },
                            onMoreDetails = { title -> nav.navigate(Route.DiscoverMoreDetails(r.type, r.tmdbId, title)) },
                            onOpenPerson = { c -> nav.navigate(Route.Person(c.tmdbId, c.name)) },
                        )
                    }
                }
                composable<Route.DiscoverMoreDetails> { e ->
                    Page {
                        val r = e.toRoute<Route.DiscoverMoreDetails>()
                        PhoneDetails(itemId = "", fallbackTitle = r.title, onBack = { nav.popBackStack() }, discover = r.type to r.tmdbId,
                            onOpenPerson = { c -> nav.navigate(Route.Person(c.tmdbId, c.name)) })
                    }
                }
                composable<Route.Watchlist> {
                    Page {
                        PhoneWatchlist(
                            onOpenSeries = { openSeries(it) }, onOpenMovie = { openMovie(it) }, bottomInset = bottomInset, onSearch = { nav.navigate(Route.Search) },
                            onOpenDownloadedTitle = { nav.navigate(Route.DownloadedTitle(it)) },
                            onBrowse = {
                                nav.navigate(Tab.Browse.route) {
                                    popUpTo(nav.graph.findStartDestination().id) { saveState = true }
                                    launchSingleTop = true
                                    restoreState = true
                                }
                            },
                        )
                    }
                }
                composable<Route.Browse> {
                    Page {
                        PhoneBrowse(onOpenSeries = { openSeries(it) }, onOpenMovie = { openMovie(it) }, onSearch = { nav.navigate(Route.Search) }, bottomInset = bottomInset)
                    }
                }
                composable<Route.Account> {
                    Page {
                        PhoneAccount(bottomInset = bottomInset, onOpenDownloads = { nav.navigate(Route.Downloads) }, onOpenArrSettings = { nav.navigate(Route.ArrSettings) }, onOpenFanart = { nav.navigate(Route.FanartSettings) }, onOpenTranscoding = { nav.navigate(Route.TranscodingSettings) }, onOpenWebhooks = { nav.navigate(Route.Webhooks) }, onOpenLegal = { nav.navigate(Route.Legal(it)) })
                    }
                }
                composable<Route.ArrSettings> {
                    Page {
                        PhoneArrSettings(onBack = { nav.popBackStack() })
                    }
                }
                composable<Route.TranscodingSettings> {
                    Page {
                        PhoneTranscodingSettings(onBack = { nav.popBackStack() })
                    }
                }
                composable<Route.FanartSettings> {
                    Page {
                        PhoneFanartSettings(onBack = { nav.popBackStack() })
                    }
                }
                composable<Route.Webhooks> {
                    Page {
                        PhoneWebhooks(onBack = { nav.popBackStack() })
                    }
                }
                composable<Route.Legal> { e ->
                    Page {
                        PhoneLegal(doc = e.toRoute<Route.Legal>().doc, onBack = { nav.popBackStack() })
                    }
                }
                composable<Route.Search> {
                    Page {
                        PhoneSearch(onBack = { nav.popBackStack() }, onOpenSeries = { openSeries(it) }, onOpenMovie = { openMovie(it) })
                    }
                }
                composable<Route.Series> { e ->
                    Page {
                        val r = e.toRoute<Route.Series>()
                        PhoneSeries(
                            seriesId = r.id, fallbackTitle = r.title,
                            onBack = { nav.popBackStack() },
                            onOpenMovie = { openMovie(it) },
                            onOpenSeries = { openSeries(it) },
                            onOpenDetails = { nav.navigate(Route.Details(it.id, it.title)) },
                            onOpenEpisode = { openEpisode(it) },
                            onOpenDiscover = { nav.navigate(Route.DiscoverDetail(it.type, it.tmdbId)) },
                        )
                    }
                }
                composable<Route.Episode> { e ->
                    Page {
                        val r = e.toRoute<Route.Episode>()
                        PhoneEpisode(episodeId = r.id, fallbackTitle = r.title, onBack = { nav.popBackStack() }, onOpenEpisode = { openEpisode(it) }, onOpenPerson = { nav.navigate(Route.Person(it.tmdbId, it.name)) })
                    }
                }
                composable<Route.Movie> { e ->
                    Page {
                        val r = e.toRoute<Route.Movie>()
                        PhoneMovie(movieId = r.id, fallbackTitle = r.title, onBack = { nav.popBackStack() }, onOpenSeries = { openSeries(it) }, onOpenDetails = { nav.navigate(Route.Details(it.id, it.title)) }, onOpenMovie = { openMovie(it) }, onOpenDiscover = { nav.navigate(Route.DiscoverDetail(it.type, it.tmdbId)) })
                    }
                }
                composable<Route.Details> { e ->
                    Page {
                        val r = e.toRoute<Route.Details>()
                        PhoneDetails(itemId = r.id, fallbackTitle = r.title, onBack = { nav.popBackStack() }, onOpenPerson = { nav.navigate(Route.Person(it.tmdbId, it.name)) })
                    }
                }
                composable<Route.Person> { e ->
                    Page {
                        val r = e.toRoute<Route.Person>()
                        PhonePerson(
                            tmdbId = r.tmdbId, fallbackName = r.name, onBack = { nav.popBackStack() },
                            onOpenLibrary = { c -> val id = c.libraryItemId ?: return@PhonePerson; if (c.isMovie) nav.navigate(Route.Movie(id, c.title)) else nav.navigate(Route.Series(id, c.title)) },
                            onOpenDiscover = { c -> nav.navigate(Route.DiscoverDetail(c.type, c.tmdbId)) },
                        )
                    }
                }
            }
            if (currentTab != null) {
                val update by app.updates.state.collectAsStateWithLifecycle()
                Column(modifier = Modifier.align(Alignment.BottomCenter)) {
                    UpdateBar(update, onInstall = { app.updates.install() }, onDismiss = { app.updates.dismiss() })
                    RemoteBar()
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

private val NavBarGrey = Color(0xFF23252B)

/** Compact bottom bar: 26 dp icons, 11 sp labels, white tabs with the selected one in Roxy blue, the Account tab as the profile avatar. */
@Composable
private fun BottomBar(current: Tab, profileName: String, avatarUrl: String?, modifier: Modifier = Modifier, onSelect: (Tab) -> Unit) {
    Row(
        // Crunchyroll-style gray bar over the black page, so the tabs read at a glance; the accent stays Roxy blue.
        modifier = modifier.fillMaxWidth().background(NavBarGrey).navigationBarsPadding().height(BarHeight),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Tab.entries.forEach { tab ->
            val selected = tab == current
            val tint = if (selected) RoxyColors.accent else Color.White
            Column(
                modifier = Modifier.weight(1f).fillMaxSize().clickable(interactionSource = remember { MutableInteractionSource() }, indication = null) { onSelect(tab) },
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = androidx.compose.foundation.layout.Arrangement.Center,
            ) {
                if (tab == Tab.Account) {
                    Box(modifier = Modifier.size(28.dp).clip(CircleShape).background(if (selected) RoxyColors.accent else Color.Transparent), contentAlignment = Alignment.Center) {
                        AvatarCircle(name = profileName, imageUrl = avatarUrl, size = if (selected) 24.dp else 26.dp)
                    }
                } else {
                    val icon: ImageVector = when (tab) {
                        Tab.Home -> if (selected) Icons.Filled.Home else Icons.Outlined.Home
                        Tab.Discover -> if (selected) Icons.Filled.Explore else Icons.Outlined.Explore
                        Tab.Watchlist -> Icons.Outlined.Bookmark
                        else -> Icons.Outlined.GridView
                    }
                    Icon(icon, contentDescription = tab.label, tint = tint, modifier = Modifier.size(26.dp))
                }
                Text(tab.label, fontSize = 11.sp, color = tint, fontWeight = if (selected) FontWeight.SemiBold else FontWeight.Normal)
            }
        }
    }
}

