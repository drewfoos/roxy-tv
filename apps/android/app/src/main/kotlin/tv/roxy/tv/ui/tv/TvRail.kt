package tv.roxy.tv.ui.tv

import androidx.compose.animation.core.animateDpAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.border
import androidx.compose.foundation.shape.CircleShape
import tv.roxy.tv.BuildConfig
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.BookmarkBorder
import androidx.compose.material.icons.outlined.GridView
import androidx.compose.material.icons.outlined.Explore
import androidx.compose.material.icons.outlined.Home
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.focus.FocusDirection
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusProperties
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.onKeyEvent
import androidx.compose.ui.input.key.onPreviewKeyEvent
import androidx.compose.ui.input.key.type
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.outlined.SystemUpdate
import androidx.compose.material3.Icon
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.style.TextAlign
import androidx.tv.material3.Text
import tv.roxy.tv.data.UpdateState
import androidx.activity.compose.BackHandler
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.ui.components.widthPx
import tv.roxy.tv.ui.nav.Route
import tv.roxy.tv.ui.profiles.AvatarCircle
import tv.roxy.tv.ui.theme.RoxyColors

/** The rail's pages, top to bottom as on Crunchyroll's TV app. */
enum class RailPage(val label: String, val icon: ImageVector, val route: Route, val routeName: String) {
    Search("Search", Icons.Outlined.Search, Route.Search, Route.Search.serializer().descriptor.serialName),
    Home("Home", Icons.Outlined.Home, Route.Home, Route.Home.serializer().descriptor.serialName),
    Watchlist("My Lists", Icons.Outlined.BookmarkBorder, Route.Watchlist, Route.Watchlist.serializer().descriptor.serialName),
    // History lives in My Lists now; Discover (TMDB and requests) took its place.
    Discover("Discover", Icons.Outlined.Explore, Route.Discover, Route.Discover.serializer().descriptor.serialName),
    Browse("Browse", Icons.Outlined.GridView, Route.Browse, Route.Browse.serializer().descriptor.serialName),
    Settings("Settings", Icons.Outlined.Settings, Route.Settings, Route.Settings.serializer().descriptor.serialName),
}

/**
 * The left rail over the page: a column of icons with the profile's avatar on top. Focus
 * moving into it (Left from a page's first column) widens it to show the labels over a
 * dimmed page; OK on an item opens that page, OK on the avatar goes back to "Who's
 * watching?". [current] is null on title pages, which have no rail.
 */
@Composable
fun TvShell(current: RailPage?, onSelect: (RailPage) -> Unit, onSwitchProfile: () -> Unit, content: @Composable () -> Unit) {
    // The rail's items can take focus only while it is open. It opens on a Left press the
    // page cannot use (the focused element is already at its left edge), so focus never
    // wanders into it when a page's focused element goes away.
    var railOpen by remember { mutableStateOf(false) }
    val entry = remember { PageEntry() }
    val focusManager = LocalFocusManager.current
    // Hands focus to the page's own entry element; the page may still be composing, so it
    // is tried over a few frames. Bumped by the rail when it closes and when a page is (re)entered.
    var enterTick by remember { mutableStateOf(0) }
    LaunchedEffect(enterTick, current) {
        if (current == null || enterTick == 0) return@LaunchedEffect
        repeat(15) { withFrameNanos { }; if (entry.owner == current && runCatching { entry.enter() }.getOrDefault(false)) return@LaunchedEffect }
    }
    // Closing the rail hands focus straight from the rail to the page's element: the page
    // may take focus again first (closing), the entry moves focus there, and only then does
    // the rail close. Closing it first left focus nowhere for a frame, the page's first
    // element (the search field, Home's hero) lit up, and then the highlight jumped.
    var closing by remember { mutableStateOf(false) }
    LaunchedEffect(closing) {
        if (!closing) return@LaunchedEffect
        FocusMemory.muteBriefly()
        for (attempt in 0 until 15) {
            withFrameNanos { }
            if (entry.owner == current && runCatching { entry.enter() }.getOrDefault(false)) break
        }
        railOpen = false
        closing = false
    }
    val app = RoxyApp.get(androidx.compose.ui.platform.LocalContext.current)
    val update by app.updates.state.collectAsStateWithLifecycle()
    CompositionLocalProvider(LocalPageEntry provides entry, LocalPageOwner provides current) {
        Box(modifier = Modifier.fillMaxSize()) {
            Box(
                modifier = Modifier
                    .fillMaxSize()
                    // While the rail is open the page cannot take focus, so Up / Down on the
                    // rail never land on a page element that happens to be nearer.
                    .focusProperties { canFocus = !railOpen || closing }
                    .onPreviewKeyEvent { ev ->
                        // Left with nowhere to go inside the page opens the rail.
                        if (current == null || railOpen || ev.type != KeyEventType.KeyDown || ev.key != Key.DirectionLeft) return@onPreviewKeyEvent false
                        if (focusManager.moveFocus(FocusDirection.Left)) true else { railOpen = true; true }
                    }
                    // Back opens the rail right away. Taken on the way back up from the focused
                    // element (a text field being typed in keeps its Back), before Compose's own
                    // handling at the root, which turns Back into "drop focus".
                    .onKeyEvent { ev ->
                        if (current == null || railOpen || ev.key != Key.Back) return@onKeyEvent false
                        if (ev.type == KeyEventType.KeyUp) railOpen = true
                        true
                    },
            ) { content() }
            // Under the rail, and hidden while it is open so the rail's Update row shows.
            if (current != null && update !is UpdateState.None && !railOpen) {
                TvUpdateNotice(update, modifier = Modifier.align(Alignment.BottomCenter))
            }
            if (current != null) Rail(
                current, open = railOpen, onSelect = { p -> FocusMemory.muteBriefly(); railOpen = false; onSelect(p); enterTick++ }, onSwitchProfile = onSwitchProfile,
                onClosed = { closing = true },
            )
            // Back on a rail page opens the rail, also when nothing on the page has focus (a
            // card that went away). Registered after the page so it comes before NavHost's own
            // Back. A text field being typed in takes its Back first; Back on the rail itself
            // is in Rail.
            BackHandler(enabled = current != null && !railOpen) { railOpen = true }
        }
    }
}

/**
 * "Roxy TV 0.5.15 is available - press Back for the menu, then Update": a notice, not a
 * control. Buttons on a card floating over the page could not be reached with the remote;
 * the rail's Update row (always reachable with Back) is where the update is started. Shown
 * for a while when a version first appears, and throughout a download, an install that
 * waits, or a failure.
 */
@Composable
private fun TvUpdateNotice(state: UpdateState, modifier: Modifier = Modifier) {
    val update = when (state) {
        is UpdateState.Available -> state.update
        is UpdateState.Downloading -> state.update
        is UpdateState.Ready -> state.update
        is UpdateState.Failed -> state.update
        UpdateState.None -> return
    }
    // "Available" leaves the screen after a while; the rail keeps the way to it.
    var shown by remember(update.versionCode) { mutableStateOf(true) }
    LaunchedEffect(update.versionCode, state is UpdateState.Available) {
        shown = true
        if (state is UpdateState.Available) { kotlinx.coroutines.delay(15_000); shown = false }
    }
    if (!shown) return
    val title = when (state) {
        is UpdateState.Downloading -> "Downloading Roxy TV ${update.versionName}  \u00b7  ${(state.fraction * 100).toInt()}%"
        is UpdateState.Ready -> "Roxy TV ${update.versionName} is ready to install"
        is UpdateState.Failed -> "The update didn't download"
        else -> "Roxy TV ${update.versionName} is available"
    }
    val hint = when (state) {
        is UpdateState.Downloading -> "You can keep watching. The installer opens when it's done."
        is UpdateState.Ready -> "Press Back to open the menu, then choose Install update."
        is UpdateState.Failed -> "${state.message.trimEnd('.')}. Press Back for the menu, then Retry update."
        else -> "Press Back to open the menu, then choose Update Roxy TV."
    }
    val shape = RoundedCornerShape(16.dp)
    Row(
        modifier = modifier.padding(bottom = 28.dp).width(640.dp).clip(shape).background(RoxyColors.surface.copy(alpha = 0.95f))
            .border(1.dp, Color.White.copy(alpha = 0.1f), shape).padding(horizontal = 20.dp, vertical = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(modifier = Modifier.size(44.dp).clip(CircleShape).background(RoxyColors.accent.copy(alpha = 0.16f)), contentAlignment = Alignment.Center) {
            Icon(Icons.Outlined.SystemUpdate, contentDescription = null, tint = RoxyColors.accent, modifier = Modifier.size(24.dp))
        }
        Column(modifier = Modifier.weight(1f).padding(start = 16.dp)) {
            Text(title, color = RoxyColors.onBackground, fontSize = 17.sp, fontWeight = FontWeight.Bold, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(hint, color = RoxyColors.muted, fontSize = 14.sp, lineHeight = 18.sp, maxLines = 2, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 2.dp))
            if (state is UpdateState.Downloading) {
                Box(modifier = Modifier.padding(top = 8.dp).fillMaxWidth().height(4.dp).clip(RoundedCornerShape(2.dp)).background(Color.White.copy(alpha = 0.12f))) {
                    Box(modifier = Modifier.fillMaxWidth(state.fraction.coerceIn(0f, 1f)).fillMaxHeight().background(RoxyColors.accent))
                }
            }
        }
    }
}

private val RailExpand = tween<androidx.compose.ui.unit.Dp>(150)

@Composable
private fun Rail(current: RailPage, open: Boolean, onSelect: (RailPage) -> Unit, onSwitchProfile: () -> Unit, onClosed: () -> Unit) {
    val app = RoxyApp.get(androidx.compose.ui.platform.LocalContext.current)
    val session by app.prefs.session.collectAsStateWithLifecycle()
    val expanded = open
    val width by animateDpAsState(if (expanded) TvDims.RailExpandedWidth else TvDims.RailWidth, RailExpand, label = "rail")
    // The icons sit at x = 40 dp collapsed and 52 dp expanded (measured); the row padding moves them.
    val iconStart by animateDpAsState(if (expanded) 40.dp else 28.dp, RailExpand, label = "railIcon")
    val requesters = remember { RailPage.entries.associateWith { FocusRequester() } }
    // Opening lands focus on the page's own item.
    LaunchedEffect(open) {
        if (!open) return@LaunchedEffect
        repeat(6) { withFrameNanos { }; if (runCatching { requesters.getValue(current).requestFocus() }.getOrDefault(false)) return@LaunchedEffect }
    }
    // Focus leaving the rail for good (the page took it) closes it. A move between two of
    // its items reports no focus for an instant, so the loss has to last a couple of frames,
    // and only counts once the rail has held focus since it opened.
    var railFocused by remember { mutableStateOf(false) }
    var held by remember(open) { mutableStateOf(false) }
    LaunchedEffect(open, railFocused) {
        if (!open) return@LaunchedEffect
        if (railFocused) { held = true; return@LaunchedEffect }
        if (!held) return@LaunchedEffect
        repeat(3) { withFrameNanos { } }
        onClosed()
    }

    if (expanded) Box(modifier = Modifier.fillMaxSize().background(Color.Black.copy(alpha = 0.6f)))
    Column(
        modifier = Modifier
            .width(width)
            .fillMaxHeight()
            .then(if (expanded) Modifier.background(RoxyColors.surface) else Modifier)
            .onFocusChanged { railFocused = it.hasFocus }
            // Right goes back into the page. Back goes to Home, and on Home it is left to the
            // system, which closes the app (the rail is where Back ends up, so the next press leaves).
            .onPreviewKeyEvent { ev ->
                when {
                    ev.key == Key.DirectionRight && ev.type == KeyEventType.KeyDown -> { onClosed(); true }
                    ev.key == Key.Back && current != RailPage.Home -> { if (ev.type == KeyEventType.KeyUp) onSelect(RailPage.Home); true }
                    else -> false
                }
            }
            // Items take focus only while the rail is open.
            .focusProperties { canFocus = open; left = FocusRequester.Cancel },
    ) {
        Spacer(Modifier.height(30.dp))
        // Avatar, name and "Switch Profile" (the name only when expanded).
        TvFocusable(onClick = onSwitchProfile, modifier = Modifier.fillMaxWidth().focusProperties { up = FocusRequester.Cancel }, focusedBackground = RoxyColors.accent) { focused ->
            Row(modifier = Modifier.fillMaxWidth().height(72.dp).padding(start = iconStart - 8.dp), verticalAlignment = Alignment.CenterVertically) {
                val avatarUrl = if (session.profileHasAvatar && session.profileId != null) app.api.profileAvatarUrl(session.profileId!!, widthPx(40.dp) * 2, session.profileAvatarKey) else null
                AvatarCircle(name = session.profileName, imageUrl = avatarUrl, size = 40.dp)
                if (expanded) {
                    Column(modifier = Modifier.padding(start = 16.dp)) {
                        Text(session.profileName, color = if (focused) OnAccent else RoxyColors.onBackground, fontSize = 22.sp, fontWeight = FontWeight.Bold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        Text("Switch Profile", color = if (focused) OnAccent else RoxyColors.muted, fontSize = 13.sp, maxLines = 1)
                    }
                }
            }
        }
        Spacer(Modifier.height(39.dp))
        val update by app.updates.state.collectAsStateWithLifecycle()
        RailPage.entries.forEach { page ->
            // Settings stands apart at the bottom, as far from the others as Search is from the profile.
            if (page == RailPage.Settings) {
                Spacer(Modifier.height(39.dp))
                // An update waiting: a way to it that always works (Back opens the rail). The
                // card at the bottom floats over the page, where the D-pad could not reach it.
                if (update !is UpdateState.None) {
                    val label = when (val u = update) {
                        is UpdateState.Downloading -> "Updating ${(u.fraction * 100).toInt()}%"
                        is UpdateState.Ready -> "Install update"
                        is UpdateState.Failed -> "Retry update"
                        else -> "Update Roxy TV"
                    }
                    TvListRow(
                        label = if (expanded) label else "", icon = Icons.Outlined.SystemUpdate, selected = true,
                        height = TvDims.RailRow, startPadding = iconStart, fontSize = 20.sp,
                        onClick = { if (update !is UpdateState.Downloading) app.updates.install() },
                        modifier = Modifier.fillMaxWidth(),
                    )
                }
            }
            TvListRow(
                label = if (expanded) page.label else "",
                icon = page.icon,
                selected = page == current,
                height = TvDims.RailRow,
                startPadding = iconStart,
                fontSize = 20.sp,
                onClick = { onSelect(page) },
                modifier = Modifier.fillMaxWidth().focusRequester(requesters.getValue(page)).focusProperties { if (page == RailPage.entries.last()) down = FocusRequester.Cancel },
            )
        }
    }
}
