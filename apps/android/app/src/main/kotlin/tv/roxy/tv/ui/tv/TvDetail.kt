package tv.roxy.tv.ui.tv

import androidx.activity.compose.BackHandler
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.onPreviewKeyEvent
import androidx.compose.ui.input.key.type
import androidx.compose.animation.core.animateDpAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.wrapContentHeight
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.itemsIndexed
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.ArrowBack
import androidx.compose.material.icons.filled.Replay
import androidx.compose.material.icons.outlined.Bookmark
import androidx.compose.material.icons.outlined.BookmarkBorder
import androidx.compose.material.icons.outlined.PlayArrow
import androidx.compose.material3.Icon
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clipToBounds
import androidx.compose.foundation.focusGroup
import androidx.compose.ui.focus.FocusDirection
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusProperties
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.focusRestorer
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.layout.layout
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.tv.material3.Text
import coil3.compose.AsyncImage
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.DiscoverItem
import tv.roxy.tv.api.DiscoverDetail
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Check
import androidx.compose.material.icons.outlined.Info
import tv.roxy.tv.api.Item
import tv.roxy.tv.api.ItemDetails
import tv.roxy.tv.api.RoxyApi
import tv.roxy.tv.ui.components.ProgressBar
import tv.roxy.tv.ui.components.RoxyImage
import tv.roxy.tv.ui.components.widthPx
import tv.roxy.tv.ui.detail.MovieViewModel
import tv.roxy.tv.ui.detail.SeriesViewModel
import tv.roxy.tv.ui.phone.OnUserDataChanged
import tv.roxy.tv.ui.phone.audioBadge
import tv.roxy.tv.ui.phone.languageName
import tv.roxy.tv.ui.phone.setWatchlist
import tv.roxy.tv.ui.phone.markWatched
import tv.roxy.tv.ui.playItem
import tv.roxy.tv.ui.theme.RoxyColors
import tv.roxy.tv.ui.components.tryFocus
import tv.roxy.tv.ui.components.retryEachFrame

/*
 * Title pages, as Crunchyroll's TV app lays them out: full-bleed key art with the logo,
 * facts and synopsis over its dark left side, text actions under them, a tab strip at the
 * bottom edge. Moving focus below the strip folds the art away so the strip sits at the
 * top and the episodes (season picker left, list right) fill the screen; Up brings it back.
 */

/** The key-art block above the tab strip. */
private val TopBlockHeight = 452.dp
/** Text and actions start here (no rail on a title page). */
private val PageStart = 40.dp
/** The actions and the season picker share this width. */
private val ActionWidth = 339.dp
private val SeasonPickerWidth = 305.dp
/** The episode list's left edge: right of the season column. */
private val EpisodeListStart = 385.dp
private val EpisodeThumbWidth = 192.dp

/**
 * The height the content keeps while it sits under the art, off screen: enough for its first
 * row (the first episode, the first similar title), which Down from the tabs must reach. It
 * was 320 dp, three episode rows composed and measured that nobody could see yet: a 26 ms
 * frame on the budget TV as the episodes arrived. The rest compose as the page folds up.
 */
private val ContentFloor = 140.dp
private val Fold = tween<Dp>(220)

/** One text action under the synopsis: "Add to Watchlist", "CONTINUE: E2 ... 20m left". */
private class TitleAction(val key: String, val label: String, val icon: ImageVector, val trailing: String? = null, val enabled: Boolean = true, val onClick: () -> Unit)

/** Series: episodes by season, details, and the franchise's films plus similar titles. */
@Composable
fun TvSeries(seriesId: String, fallbackTitle: String, onOpenSeries: (Item) -> Unit, onOpenMovie: (Item) -> Unit, onOpenDiscover: (type: String, tmdbId: Int) -> Unit = { _, _ -> }) {
    val context = LocalContext.current
    val app = RoxyApp.get(context)
    val vm: SeriesViewModel = viewModel(key = "series:$seriesId") { SeriesViewModel(app.api, seriesId, initial = app.items[seriesId]) }
    val state by vm.state.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { vm.load() }
    OnUserDataChanged { vm.load() }

    val series = state.series
    if (series == null) {
        if (state.loading) Box(Modifier.fillMaxSize()) else tv.roxy.tv.ui.components.LoadError(state.error ?: "Not found", onRetry = { vm.load() })
        return
    }
    fun play(item: Item) = scope.launch { playItem(context, item).onFailure { Timber.w(it, "play failed") } }

    val playable = state.episodes.filter { it.isPlayable }
    // Across all seasons (the list below shows one); the season's own pick until it is known.
    val next = state.upNext ?: playable.firstOrNull { (it.userData?.positionMs ?: 0) > 0 }
        ?: playable.firstOrNull { it.userData?.played != true }
        ?: playable.firstOrNull()
    val actions = listOf(
        watchlistAction(app, series, scope),
        TitleAction(
            key = "play",
            label = when {
                next == null -> if (state.episodesLoading || state.loading) "Play" else "Nothing to play yet"
                else -> continueLabel(next)
            },
            icon = Icons.Outlined.PlayArrow,
            trailing = next?.let { minutesLabel(it)?.takeIf { l -> l.endsWith("left") } },
            enabled = next != null,
            onClick = { next?.let { play(it) } },
        ),
    )
    var tab by rememberSaveable(seriesId) { mutableStateOf(0) }
    // An "Extras" tab only for a show with trailers, featurettes and the like.
    val tabs = listOf("Episodes", "Details") + (if (state.extras.isNotEmpty()) listOf("Extras") else emptyList()) + "More like this"
    TitlePage(title = series, play = next, api = app.api, actions = actions, tabs = tabs, tab = tab.coerceIn(0, tabs.lastIndex), onTab = { tab = it }) { first ->
        when (tabs[tab.coerceIn(0, tabs.lastIndex)]) {
            "Episodes" -> EpisodesTab(
                seasons = state.seasons, selected = state.selectedSeason, onSelectSeason = vm::selectSeason,
                episodes = state.episodes, loading = state.episodesLoading, api = app.api, onPlay = ::play, first = first,
                onPlayFromStart = { ep -> scope.launch { playItem(context, ep, fromStart = true).onFailure { Timber.w(it, "play failed") } } },
                // A group of specials numbers by place, a film's row not at all (rowNumbersFor).
                sectionCounts = state.sectionCounts, sectionLabels = state.sectionLabels,
                incompleteOf = state.episodesExpected.takeIf { state.episodesIncomplete }, error = state.episodesError, onRetry = vm::retryEpisodes,
                code = { ep -> if (!ep.isEpisode) "" else if (state.rowNumbers.containsKey(ep.id)) state.rowNumbers[ep.id]?.let { "E$it" } ?: "" else episodeShort(ep) },
            )
            "Details" -> DetailsTab(series, app.api, first)
            "Extras" -> ExtrasTab(state.extras, app.api, first)
            else -> MoreLikeThisTab(series, app.api, related = state.related.mapNotNull { if (it.inLibrary) it.item else null }, onOpenSeries = onOpenSeries, onOpenMovie = onOpenMovie, onOpenDiscover = onOpenDiscover, first = first)
        }
    }
}

/** Movie: the film itself under its tab, details, and the series it belongs to plus similar titles. */
@Composable
fun TvMovie(movieId: String, fallbackTitle: String, onOpenSeries: (Item) -> Unit, onOpenMovie: (Item) -> Unit, onOpenDiscover: (type: String, tmdbId: Int) -> Unit = { _, _ -> }) {
    val context = LocalContext.current
    val app = RoxyApp.get(context)
    val vm: MovieViewModel = viewModel(key = "movie:$movieId") { MovieViewModel(app.api, movieId, initial = app.items[movieId]) }
    val state by vm.state.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { vm.load() }
    OnUserDataChanged { vm.load() }

    val movie = state.movie
    if (movie == null) {
        if (state.loading) Box(Modifier.fillMaxSize()) else tv.roxy.tv.ui.components.LoadError(state.error ?: "Not found", onRetry = { vm.load() })
        return
    }
    fun play(fromStart: Boolean = false) = scope.launch { playItem(context, movie, fromStart).onFailure { Timber.w(it, "play failed") } }

    val started = (movie.userData?.positionMs ?: 0L) > 0
    val actions = buildList {
        add(watchlistAction(app, movie, scope))
        add(TitleAction(key = "play", label = continueLabel(movie), icon = Icons.Outlined.PlayArrow, trailing = minutesLabel(movie)?.takeIf { it.endsWith("left") }, enabled = movie.isPlayable, onClick = { play() }))
        if (started) add(TitleAction(key = "restart", label = "Start over", icon = Icons.Filled.Replay, onClick = { play(fromStart = true) }))
    }
    var tab by rememberSaveable(movieId) { mutableStateOf(0) }
    val tabs = listOf("Movie", "Details") + (if (state.extras.isNotEmpty()) listOf("Extras") else emptyList()) + "More like this"
    TitlePage(title = movie, play = movie, api = app.api, actions = actions, tabs = tabs, tab = tab.coerceIn(0, tabs.lastIndex), onTab = { tab = it }) { first ->
        when (tabs[tab.coerceIn(0, tabs.lastIndex)]) {
            "Movie" -> Row(modifier = Modifier.fillMaxSize()) {
                // No seasons on a movie: the film takes the seasons' place at the left, under its tab.
                Spacer(Modifier.width(PageStart - TvDims.FocusBorder))
                Column(modifier = Modifier.padding(top = 27.dp)) {
                    var menu by remember { mutableStateOf(false) }
                    if (menu) TvItemMenu(movie, onPlayFromStart = { play(fromStart = true) }, onDismiss = { menu = false })
                    EpisodeRow(movie, app.api, index = 0, onClick = { play() }, onLongClick = { if (movie.isPlayable) menu = true }, modifier = Modifier.focusRequester(first))
                    if (movie.isPlayable) Text("Hold OK for options", color = TvSubtle, fontSize = 14.sp, modifier = Modifier.padding(start = TvDims.FocusBorder, top = 10.dp))
                }
            }
            "Details" -> DetailsTab(movie, app.api, first)
            "Extras" -> ExtrasTab(state.extras, app.api, first)
            else -> MoreLikeThisTab(movie, app.api, related = state.partOf, onOpenSeries = onOpenSeries, onOpenMovie = onOpenMovie, onOpenDiscover = onOpenDiscover, first = first)
        }
    }
}

/** A title's trailers, featurettes and other extras, one row each; OK plays one. */
@Composable
private fun ExtrasTab(extras: List<Item>, api: RoxyApi, first: FocusRequester) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    Row(modifier = Modifier.fillMaxSize()) {
        Spacer(Modifier.width(PageStart - TvDims.FocusBorder))
        androidx.compose.foundation.lazy.LazyColumn(contentPadding = androidx.compose.foundation.layout.PaddingValues(top = 27.dp, bottom = 48.dp)) {
            items(extras.size, key = { extras[it].id }) { i ->
                val x = extras[i]
                EpisodeRow(
                    x, api, index = i,
                    onClick = { scope.launch { playItem(context, x).onFailure { Timber.w(it, "play failed") } } },
                    modifier = if (i == 0) Modifier.focusRequester(first) else Modifier,
                )
            }
        }
    }
}

private fun watchlistAction(app: RoxyApp, item: Item, scope: kotlinx.coroutines.CoroutineScope): TitleAction {
    val saved = item.userData?.isFavorite == true
    return TitleAction(
        key = "watchlist",
        label = if (saved) "Remove from Watchlist" else "Add to Watchlist",
        icon = if (saved) Icons.Outlined.Bookmark else Icons.Outlined.BookmarkBorder,
        onClick = { scope.launch { app.setWatchlist(item.id, !saved) } },
    )
}

/**
 * The page's frame: the folding key-art block with the copy and [actions], the tab strip,
 * then [content] for the chosen tab. Focus lands on the play action once the page is up.
 */
@Composable
private fun TitlePage(
    title: Item, play: Item?, api: RoxyApi, actions: List<TitleAction>, tabs: List<String>, tab: Int, onTab: (Int) -> Unit,
    backdropUrl: String? = null, logoUrl: String? = null,
    content: @Composable (first: FocusRequester) -> Unit,
) {
    val memory = rememberFocusMemory()
    // Attached by the content to its first focusable (the season picker, the details
    // block, the first similar title): Down from a tab goes straight to it.
    val contentFocus = remember { FocusRequester() }
    // Folded = the art is away and the tab strip sits at the top (Crunchyroll's). Down
    // on a tab folds and enters the content; Up on a tab while folded unfolds and goes
    // back to the actions. Focus landing on an action unfolds too (Back from the list).
    var folded by remember { mutableStateOf(false) }
    var pendingFocus by remember { mutableStateOf<FocusRequester?>(null) }
    LaunchedEffect(pendingFocus) {
        val target = pendingFocus ?: return@LaunchedEffect
        retryEachFrame("title page: pending focus") { target.tryFocus() }
        pendingFocus = null
    }
    val topHeight by animateDpAsState(if (folded) 0.dp else TopBlockHeight, Fold, label = "fold")
    var restored by remember { mutableStateOf(false) }
    // The page arrives in two frames: the art, copy and actions first, then the tabs and the
    // content (which start at the bottom edge, mostly off screen) one frame later. In one
    // frame it was 26 ms on the budget TV; split, each half fits a frame.
    var below by remember(title.id) { mutableStateOf(false) }
    LaunchedEffect(title.id) { withFrameNanos { }; below = true }
    LaunchedEffect(title.id) {
        if (restored) return@LaunchedEffect
        if (retryEachFrame("title page: play action") { memory.restore("play") }) restored = true
    }

    // Back from the list (folded) goes up to the actions first, as on Crunchyroll; a second
    // Back leaves the page. Taken before Compose sees it: left alone, the first Back only
    // dropped focus (nothing highlighted, still on the page).
    Column(
        modifier = Modifier.fillMaxSize().onPreviewKeyEvent { ev ->
            if (ev.key != Key.Back || !folded) return@onPreviewKeyEvent false
            if (ev.type == KeyEventType.KeyUp) {
                folded = false
                pendingFocus = memory.requester("play")
            }
            true
        },
    ) {
        Box(modifier = Modifier.fillMaxWidth().height(topHeight).clipToBounds().focusProperties { canFocus = !folded }) {
            Box(modifier = Modifier.fillMaxWidth().wrapContentHeight(Alignment.Top, unbounded = true).height(TopBlockHeight)) {
                val art = backdropUrl ?: api.imageUrl(title, "backdrop", widthPx(960.dp)) ?: play?.let { api.imageUrl(it, "thumb", widthPx(960.dp)) }
                RoxyImage(art, "", modifier = Modifier.fillMaxSize(), placeholderTextSize = 0)
                // Scrims sized to the strips they tint (the copy's column, bottom 40 %), not the
                // whole block; the left one stays near-solid under the text so the facts and
                // synopsis read over bright art.
                Box(modifier = Modifier.fillMaxWidth(0.62f).fillMaxHeight().background(Brush.horizontalGradient(0f to RoxyColors.background.copy(alpha = 0.97f), 0.55f to RoxyColors.background.copy(alpha = 0.85f), 1f to Color.Transparent)))
                Box(modifier = Modifier.fillMaxWidth().fillMaxHeight(0.4f).align(Alignment.BottomCenter).background(Brush.verticalGradient(0f to Color.Transparent, 1f to RoxyColors.background)))
                Column(modifier = Modifier.padding(start = PageStart, top = 45.dp)) {
                    Box(modifier = Modifier.height(125.dp).widthIn(max = 480.dp), contentAlignment = Alignment.BottomStart) {
                        val logo = logoUrl ?: api.imageUrl(title, "logo", widthPx(480.dp))
                        if (logo != null) AsyncImage(model = logo, contentDescription = title.title, contentScale = ContentScale.Fit, alignment = Alignment.BottomStart, modifier = Modifier.width(240.dp).heightIn(max = 125.dp))
                        else Text(title.title, fontSize = 34.sp, lineHeight = 38.sp, fontWeight = FontWeight.Bold, maxLines = 2, overflow = TextOverflow.Ellipsis)
                    }
                    Spacer(Modifier.height(12.dp))
                    FactsLine(title, play, fontSize = 16.sp)
                    Spacer(Modifier.height(14.dp))
                    Text(title.overview.orEmpty(), color = TvCopy, fontSize = 16.sp, lineHeight = 20.sp, minLines = 3, maxLines = 3, overflow = TextOverflow.Ellipsis, style = TvShadowed, modifier = Modifier.widthIn(max = 440.dp))
                    Spacer(Modifier.height(47.dp))
                    // Down goes back to the tab showing (Extras after a trip up from Extras), not
                    // always the first one.
                    actions.forEach { a ->
                        TvListRow(
                            label = a.label, icon = a.icon, trailing = a.trailing, selected = true, height = TvDims.ListRowHeight,
                            onClick = a.onClick, onFocused = { folded = false },
                            modifier = Modifier.width(ActionWidth).focusKey(memory, a.key).focusProperties { down = memory.requester("tab/$tab") }.alpha(if (a.enabled) 1f else 0.5f),
                        )
                        Spacer(Modifier.height(2.dp))
                    }
                }
            }
        }
        if (!below) { Spacer(Modifier.height(23.dp + TvDims.TabHeight)); return@Column }
        Row(modifier = Modifier.padding(start = PageStart, top = 23.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            tabs.forEachIndexed { i, t ->
                TvTab(
                    t, selected = i == tab, onClick = { onTab(i) }, onFocused = { onTab(i) },
                    onDown = { folded = true; pendingFocus = contentFocus },
                    onUp = { if (folded) { folded = false; pendingFocus = memory.requester("play") } else pendingFocus = memory.requester("play") },
                    modifier = Modifier.focusKey(memory, "tab/$i"),
                )
            }
        }
        // Under the art the content gets only a sliver of height; it keeps a working height
        // anyway (overflowing the screen) so its first rows exist for Down from the tabs to reach.
        Box(
            modifier = Modifier
                .fillMaxWidth()
                .weight(1f)
                // Measured at least ContentFloor tall but placed at its top (a child that overflows
                // its constraints is otherwise centred, which drew the rows over the art).
                .layout { measurable, constraints ->
                    val floor = ContentFloor.roundToPx()
                    val placeable = measurable.measure(constraints.copy(minHeight = 0, maxHeight = maxOf(constraints.maxHeight, floor)))
                    layout(placeable.width, minOf(placeable.height, constraints.maxHeight)) { placeable.placeRelative(0, 0) }
                }
                .clipToBounds()
                // Up out of the content goes back to the tab showing (Episodes from the seasons,
                // Movie from the film); the nearest tab above was often Details or More like this.
                .focusProperties {
                    onExit = {
                        if (requestedFocusDirection == FocusDirection.Up) {
                            cancelFocusChange()
                            pendingFocus = memory.requester("tab/$tab")
                        }
                    }
                }
                .focusGroup(),
        ) { content(contentFocus) }
    }
}

/**
 * Every season as a row at the left ("Season 1: 11 episodes"), the focused season's
 * episodes at the right; Right from a season goes to its first episode. Counts come
 * from one small request per season, made once the tab opens.
 */
@Composable
private fun EpisodesTab(seasons: List<Item>, selected: Int, onSelectSeason: (Int) -> Unit, episodes: List<Item>, loading: Boolean, api: RoxyApi, onPlay: (Item) -> Unit, first: FocusRequester, onPlayFromStart: (Item) -> Unit = {}, code: (Item) -> String = { if (it.isEpisode) episodeShort(it) else "" }, sectionCounts: Map<String, Int> = emptyMap(), sectionLabels: Map<String, String> = emptyMap(), incompleteOf: Int? = null, error: String? = null, onRetry: () -> Unit = {}) {
    val firstEpisode = remember { FocusRequester() }
    var menuFor by remember { mutableStateOf<Item?>(null) }
    menuFor?.let { ep ->
        val season = seasons.getOrNull(selected)?.takeIf { ep.parentId == it.id }
        TvItemMenu(ep, season = season, onPlayFromStart = { onPlayFromStart(ep) }, onDismiss = { menuFor = null })
    }
    // Keyed by the season ids: a reload brings new Item objects (fresh watch state) for the
    // same seasons, and counting them again cost one request per season every time.
    val seasonIds = remember(seasons) { seasons.joinToString(",") { it.id } }
    var counts by remember(seasonIds) { mutableStateOf<Map<String, Int>>(emptyMap()) }
    LaunchedEffect(seasonIds) {
        for (s in seasons) {
            // An entry of the season list that is not a season (a group of specials, a film):
            // its count came with the list; asking for its "season's" episodes said 0.
            if (s.id in sectionCounts) continue
            val n = runCatching { api.items(type = "episode", parent = s.id, limit = 1).total }.getOrNull() ?: continue
            counts = counts + (s.id to n)
        }
    }
    Row(modifier = Modifier.fillMaxSize().padding(top = 27.dp)) {
        Column(modifier = Modifier.width(EpisodeListStart).padding(start = PageStart, end = EpisodeListStart - PageStart - SeasonPickerWidth)) {
            if (seasons.isEmpty()) {
                TvListRow(label = "Episodes", selected = true, height = 44.dp, fontSize = 20.sp, startPadding = 16.dp, onClick = {}, modifier = Modifier.fillMaxWidth().focusRequester(first).focusProperties { if (episodes.isNotEmpty()) { down = firstEpisode; right = firstEpisode } })
            }
            seasons.forEachIndexed { i, s ->
                val name = s.title.ifEmpty { "Season ${s.indexAired ?: i + 1}" }
                val n = if (i == selected && !loading) episodes.size else counts[s.id] ?: sectionCounts[s.id]
                val count = sectionLabels[s.id] ?: when (n) { null -> null; 1 -> "1 episode"; else -> "$n episodes" }
                TvListRow(
                    label = name, trailing = count, selected = i == selected, height = 44.dp, fontSize = 19.sp, startPadding = 16.dp,
                    onClick = { onSelectSeason(i) },
                    modifier = Modifier.fillMaxWidth()
                        .then(if (i == selected) Modifier.focusRequester(first) else Modifier),
                )
            }
            if (episodes.any { it.isPlayable }) Text("Hold OK on an episode for options", color = TvSubtle, fontSize = 14.sp, modifier = Modifier.padding(start = 16.dp, top = 14.dp))
        }
        Column {
            // Part of the list did not load (episodeRows): said, with a retry, rather than the rows
            // passing for the whole season.
            // Three states: loaded; part loaded (how much, Try again); failed (Try again).
            val problem = when {
                loading -> null
                error != null -> "Couldn't load the episodes. $error"
                incompleteOf != null -> "Some episodes could not be loaded (${episodes.size} of $incompleteOf)."
                else -> null
            }
            if (problem != null) {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(16.dp), modifier = Modifier.padding(bottom = 12.dp)) {
                    Text(problem, color = RoxyColors.onBackground, fontSize = 17.sp, modifier = Modifier.widthIn(max = 560.dp))
                    // The button leaves the screen as the retry starts: focus goes to the
                    // season on screen first, or it was dropped (nothing focused on the TV).
                    TvButton("Try again", onClick = { runCatching { first.requestFocus() }; onRetry() }, modifier = if (episodes.isEmpty()) Modifier.focusRequester(firstEpisode) else Modifier)
                }
            }
            if (episodes.isEmpty()) {
                if (problem == null) {
                    if (loading) EpisodeRowsSkeleton()
                    else Text("No episodes in this season.", color = RoxyColors.muted, fontSize = 16.sp, modifier = Modifier.padding(top = 8.dp))
                }
            } else {
                // Right from the seasons comes back to the episode last focused (the first one
                // for a new season). It used to aim at episode 1 only, a dead end once scrolled.
                LazyColumn(modifier = Modifier.fillMaxHeight().width(TvDims.FocusBorder * 2 + EpisodeThumbWidth + 12.dp + 326.dp).focusRestorer(firstEpisode), contentPadding = PaddingValues(bottom = 24.dp)) {
                    itemsIndexed(episodes, key = { _, ep -> ep.id }) { i, ep ->
                        EpisodeRow(ep, api, index = i, code = code(ep), onClick = { if (ep.isPlayable) onPlay(ep) }, onLongClick = { if (ep.isPlayable) menuFor = ep }, modifier = if (i == 0) Modifier.focusRequester(firstEpisode) else Modifier)
                    }
                }
            }
        }
    }
}

/** Episode rows while they load (a season picked, a retry): the rows' shapes, breathing. */
@Composable
private fun EpisodeRowsSkeleton() {
    val pulse = rememberSkeletonPulse()
    Column(verticalArrangement = Arrangement.spacedBy(20.dp), modifier = Modifier.padding(start = TvDims.FocusBorder, top = TvDims.FocusBorder)) {
        repeat(3) {
            Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                SkeletonBlock(pulse, Modifier.width(EpisodeThumbWidth).aspectRatio(16f / 9f))
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    SkeletonBlock(pulse, Modifier.width(220.dp).height(18.dp))
                    SkeletonBlock(pulse, Modifier.width(120.dp).height(13.dp))
                    SkeletonBlock(pulse, Modifier.width(300.dp).height(13.dp))
                    SkeletonBlock(pulse, Modifier.width(260.dp).height(13.dp))
                }
            }
        }
    }
}

/** One episode (or the movie itself): still with minutes and progress, "E1 - Title", the audio, two lines of synopsis. */
@Composable
private fun EpisodeRow(ep: Item, api: RoxyApi, index: Int, onClick: () -> Unit, modifier: Modifier = Modifier, onLongClick: (() -> Unit)? = null, code: String = if (ep.isEpisode) episodeShort(ep) else "") {
    val placeholder = !ep.isPlayable
    // An episode's still; an extra's frame (its "thumb", made on request) before any
    // backdrop; a film's backdrop. Extras asked for a backdrop only and showed none.
    val imageType = if (ep.isEpisode || ep.images["thumb"] != null) "thumb" else "backdrop"
    val name = if (code.isEmpty()) ep.title else "$code - ${ep.title}"
    val audio = audioBadge(ep) ?: ep.availability?.takeIf { placeholder }?.replaceFirstChar { it.uppercase() }
    TvCard(onClick = onClick, onLongClick = onLongClick, modifier = modifier.alpha(if (placeholder) 0.5f else 1f)) {
        Row(modifier = Modifier.width(EpisodeThumbWidth + 12.dp + 326.dp)) {
            Box(modifier = Modifier.width(EpisodeThumbWidth).aspectRatio(16f / 9f)) {
                // Watched, as on the phone: the still dimmed, a replay arrow, "Watched" in the corner.
                val watched = ep.isWatched
                RoxyImage(api.imageUrl(ep, imageType, widthPx(EpisodeThumbWidth)), code.ifEmpty { ep.title }, modifier = Modifier.fillMaxSize().alpha(if (watched) 0.45f else 1f))
                if (watched) {
                    ReplayBadge(size = 44.dp, modifier = Modifier.align(Alignment.Center))
                    MinutesBadge("Watched", modifier = Modifier.align(Alignment.BottomEnd).padding(bottom = 3.dp))
                } else {
                    minutesLabel(ep)?.let { MinutesBadge(it, modifier = Modifier.align(Alignment.BottomEnd).padding(bottom = 3.dp)) }
                    ep.progressFraction?.takeIf { it < 1f }?.let { ProgressBar(it, modifier = Modifier.align(Alignment.BottomCenter)) }
                }
            }
            Column(modifier = Modifier.padding(start = 12.dp, top = 10.dp)) {
                Text(name, color = RoxyColors.onBackground, fontSize = 19.sp, lineHeight = 24.sp, fontWeight = FontWeight.Medium, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text(audio ?: "", color = TvSubtle, fontSize = 14.sp, lineHeight = 18.sp, maxLines = 1)
                Text(ep.overview.orEmpty(), color = TvSubtle, fontSize = 16.sp, lineHeight = 21.sp, maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
        }
    }
}

/**
 * Tabs switch as focus passes over them, so a sweep along the tab strip recreated Details and
 * More like this and fetched both again each time. Their answers are kept for a while here.
 */
private object TabCache {
    private val map = object : LinkedHashMap<String, Any>(32, 0.75f, true) {
        override fun removeEldestEntry(eldest: MutableMap.MutableEntry<String, Any>?) = size > 40
    }
    @Synchronized fun get(key: String): Any? = map[key]
    @Synchronized fun put(key: String, value: Any) { map[key] = value }
}

/** Facts about the title: the full synopsis, genres, year, studio, status, audio, cast. Focusable so it can be scrolled up to. */
@Composable
private fun DetailsTab(item: Item, api: RoxyApi, first: FocusRequester, loadDetails: suspend () -> ItemDetails? = { api.itemDetails(item.id) }) {
    var details by remember(item.id) { mutableStateOf(TabCache.get("details/${item.id}") as? ItemDetails) }
    LaunchedEffect(item.id) {
        if (details != null) return@LaunchedEffect
        details = runCatching { loadDetails() }.getOrNull()?.also { TabCache.put("details/${item.id}", it) }
    }
    val facts = buildList {
        item.genres.takeIf { it.isNotEmpty() }?.let { add("Genres" to it.joinToString(", ")) }
        (item.year ?: item.premiered?.take(4)?.toIntOrNull())?.let { add("Year" to it.toString()) }
        item.studio?.takeIf { it.isNotBlank() }?.let { add("Studio" to it) }
        item.status?.takeIf { it.isNotBlank() }?.let { add("Status" to it.replaceFirstChar { c -> c.uppercase() }) }
        item.officialRating?.takeIf { it.isNotBlank() }?.let { add("Rating" to it) }
        item.communityRating?.let { add("Score" to "%.1f".format(it)) }
        item.audioLanguages.takeIf { it.isNotEmpty() }?.let { add("Audio" to it.joinToString(", ") { l -> languageName(l) }) }
        item.seasonCount?.takeIf { it > 0 }?.let { add("Seasons" to it.toString()) }
        item.runtimeMinutes?.let { add("Runtime" to "$it min") }
        details?.tmdb?.cast?.takeIf { it.isNotEmpty() }?.let { add("Cast" to it.take(8).joinToString(", ") { c -> c.name }) }
    }
    TvFocusable(onClick = {}, modifier = Modifier.fillMaxSize().focusRequester(first)) { _ ->
        Row(modifier = Modifier.fillMaxSize().padding(start = PageStart, top = 27.dp, end = PageStart)) {
            Text(item.overview.orEmpty(), color = RoxyColors.onBackground, fontSize = 17.sp, lineHeight = 24.sp, modifier = Modifier.weight(1f).fillMaxHeight().verticalScroll(rememberScrollState()).padding(bottom = 24.dp))
            Column(modifier = Modifier.width(ActionWidth).padding(start = 40.dp)) {
                facts.forEach { (k, v) ->
                    Text(k.uppercase(), color = RoxyColors.muted, fontSize = 12.sp, fontWeight = FontWeight.Bold, letterSpacing = 1.sp)
                    Text(v, color = RoxyColors.onBackground, fontSize = 16.sp, lineHeight = 20.sp, maxLines = 2, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(bottom = 10.dp))
                }
            }
        }
    }
}

/**
 * The franchise's other titles in the library ([related]: a series' films, a film's
 * series), then TMDB's similar titles; those not in the library are dimmed and open the
 * Discover page (synopsis, facts, a request).
 */
@Composable
private fun MoreLikeThisTab(item: Item, api: RoxyApi, related: List<Item>, onOpenSeries: (Item) -> Unit, onOpenMovie: (Item) -> Unit, onOpenDiscover: (type: String, tmdbId: Int) -> Unit, first: FocusRequester) {
    @Suppress("UNCHECKED_CAST")
    var similar by remember(item.id) { mutableStateOf(TabCache.get("similar/${item.id}") as? List<DiscoverItem>) }
    LaunchedEffect(item.id) {
        if (similar != null) return@LaunchedEffect
        similar = runCatching { api.similar(item.id) }.onSuccess { TabCache.put("similar/${item.id}", it) }.getOrDefault(emptyList())
    }
    val cards: List<Any> = related + similar.orEmpty().filter { s -> related.none { it.id == s.libraryItemId } }
    val context = LocalContext.current
    var titleMenuFor by remember { mutableStateOf<Item?>(null) } // hold OK on a poster: its menu
    titleMenuFor?.let { t -> TvTitleMenu(t, onDismiss = { titleMenuFor = null }, onOpen = { if (t.isMovie) onOpenMovie(t) else onOpenSeries(t) }) }
    if (similar != null && cards.isEmpty()) { PageMessage("Nothing like this in the library yet."); return }
    LazyVerticalGrid(
        columns = GridCells.Fixed(6),
        contentPadding = PaddingValues(start = PageStart - TvDims.FocusBorder, top = 23.dp, end = PageStart, bottom = 24.dp),
        modifier = Modifier.fillMaxSize(),
    ) {
        itemsIndexed(cards, key = { _, c -> if (c is Item) c.id else "tmdb:${(c as DiscoverItem).tmdbId}" }) { i, c ->
            val firstMod = if (i == 0) Modifier.focusRequester(first) else Modifier
            when (c) {
                is Item -> PosterCard(c, api, width = TvDims.PosterCardWidth, captionHeight = TvDims.PosterCaption, onClick = { if (c.isMovie) onOpenMovie(c) else onOpenSeries(c) }, modifier = firstMod, onLongClick = { titleMenuFor = c })
                is DiscoverItem -> UrlPosterCard(
                    title = c.title, url = c.poster, width = TvDims.PosterCardWidth, captionHeight = TvDims.PosterCaption, modifier = firstMod,
                    note = if (c.inLibrary) null else "Not in library",
                    onClick = {
                        val id = c.libraryItemId
                        if (id != null) { val stub = Item(id = id, type = if (c.type == "movie") "movie" else "series", title = c.title); if (stub.isMovie) onOpenMovie(stub) else onOpenSeries(stub) }
                        else onOpenDiscover(c.type, c.tmdbId)
                    },
                )
            }
        }
    }
}
