package tv.roxy.tv.ui.tv

import androidx.activity.compose.BackHandler
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
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusProperties
import androidx.compose.ui.focus.focusRequester
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
import tv.roxy.tv.ui.playItem
import tv.roxy.tv.ui.theme.RoxyColors

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
        if (state.loading) Box(Modifier.fillMaxSize()) else PageMessage(state.error ?: "Not found", color = RoxyColors.error)
        return
    }
    fun play(item: Item) = scope.launch { playItem(context, item).onFailure { Timber.w(it, "play failed") } }

    val playable = state.episodes.filter { it.isPlayable }
    val next = playable.firstOrNull { (it.userData?.positionMs ?: 0) > 0 && it.userData?.played != true }
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
    TitlePage(title = series, play = next, api = app.api, actions = actions, tabs = listOf("Episodes", "Details", "More like this"), tab = tab, onTab = { tab = it }) { first ->
        when (tab) {
            0 -> EpisodesTab(
                seasons = state.seasons, selected = state.selectedSeason, onSelectSeason = vm::selectSeason,
                episodes = state.episodes, loading = state.episodesLoading, api = app.api, onPlay = ::play, first = first,
            )
            1 -> DetailsTab(series, app.api, first)
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
        if (state.loading) Box(Modifier.fillMaxSize()) else PageMessage(state.error ?: "Not found", color = RoxyColors.error)
        return
    }
    fun play(fromStart: Boolean = false) = scope.launch { playItem(context, movie, fromStart).onFailure { Timber.w(it, "play failed") } }

    val started = (movie.userData?.positionMs ?: 0L) > 0 && movie.userData?.played != true
    val actions = buildList {
        add(watchlistAction(app, movie, scope))
        add(TitleAction(key = "play", label = continueLabel(movie), icon = Icons.Outlined.PlayArrow, trailing = minutesLabel(movie)?.takeIf { it.endsWith("left") }, enabled = movie.isPlayable, onClick = { play() }))
        if (started) add(TitleAction(key = "restart", label = "Start over", icon = Icons.Filled.Replay, onClick = { play(fromStart = true) }))
    }
    var tab by rememberSaveable(movieId) { mutableStateOf(0) }
    TitlePage(title = movie, play = movie, api = app.api, actions = actions, tabs = listOf("Movie", "Details", "More like this"), tab = tab, onTab = { tab = it }) { first ->
        when (tab) {
            0 -> Row(modifier = Modifier.fillMaxSize()) {
                Spacer(Modifier.width(EpisodeListStart))
                Column(modifier = Modifier.padding(top = 27.dp)) {
                    EpisodeRow(movie, app.api, index = 0, onClick = { play() }, modifier = Modifier.focusRequester(first))
                }
            }
            1 -> DetailsTab(movie, app.api, first)
            else -> MoreLikeThisTab(movie, app.api, related = state.partOf, onOpenSeries = onOpenSeries, onOpenMovie = onOpenMovie, onOpenDiscover = onOpenDiscover, first = first)
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
        repeat(10) { withFrameNanos { }; if (runCatching { target.requestFocus() }.isSuccess) { pendingFocus = null; return@LaunchedEffect } }
        pendingFocus = null
    }
    val topHeight by animateDpAsState(if (folded) 0.dp else TopBlockHeight, Fold, label = "fold")
    var restored by remember { mutableStateOf(false) }
    LaunchedEffect(title.id) {
        if (restored) return@LaunchedEffect
        repeat(6) { withFrameNanos { }; if (memory.restore("play")) { restored = true; return@LaunchedEffect } }
    }

    Column(modifier = Modifier.fillMaxSize()) {
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
                    actions.forEach { a ->
                        TvListRow(
                            label = a.label, icon = a.icon, trailing = a.trailing, selected = true, height = TvDims.ListRowHeight,
                            onClick = a.onClick, onFocused = { folded = false },
                            modifier = Modifier.width(ActionWidth).focusKey(memory, a.key).focusProperties { down = memory.requester("tab/0") }.alpha(if (a.enabled) 1f else 0.5f),
                        )
                        Spacer(Modifier.height(2.dp))
                    }
                }
            }
        }
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
                // Measured at least 320 dp tall but placed at its top (a child that overflows its
                // constraints is otherwise centred, which drew the rows over the art).
                .layout { measurable, constraints ->
                    val floor = 320.dp.roundToPx()
                    val placeable = measurable.measure(constraints.copy(minHeight = 0, maxHeight = maxOf(constraints.maxHeight, floor)))
                    layout(placeable.width, minOf(placeable.height, constraints.maxHeight)) { placeable.placeRelative(0, 0) }
                }
                .clipToBounds()
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
private fun EpisodesTab(seasons: List<Item>, selected: Int, onSelectSeason: (Int) -> Unit, episodes: List<Item>, loading: Boolean, api: RoxyApi, onPlay: (Item) -> Unit, first: FocusRequester) {
    val firstEpisode = remember { FocusRequester() }
    var counts by remember(seasons) { mutableStateOf<Map<String, Int>>(emptyMap()) }
    LaunchedEffect(seasons) {
        for (s in seasons) {
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
                val n = if (i == selected && !loading) episodes.size else counts[s.id]
                val count = when (n) { null -> null; 1 -> "1 episode"; else -> "$n episodes" }
                TvListRow(
                    label = name, trailing = count, selected = i == selected, height = 44.dp, fontSize = 19.sp, startPadding = 16.dp,
                    onClick = { onSelectSeason(i) },
                    modifier = Modifier.fillMaxWidth()
                        .then(if (i == selected) Modifier.focusRequester(first) else Modifier)
                        .focusProperties { if (episodes.isNotEmpty()) right = firstEpisode },
                )
            }
        }
        if (episodes.isEmpty()) {
            Text(if (loading) "" else "No episodes in this season.", color = RoxyColors.muted, fontSize = 16.sp, modifier = Modifier.padding(top = 8.dp))
        } else {
            LazyColumn(modifier = Modifier.fillMaxHeight().width(TvDims.FocusBorder * 2 + EpisodeThumbWidth + 12.dp + 326.dp), contentPadding = PaddingValues(bottom = 24.dp)) {
                itemsIndexed(episodes, key = { _, ep -> ep.id }) { i, ep ->
                    EpisodeRow(ep, api, index = i, onClick = { if (ep.isPlayable) onPlay(ep) }, modifier = if (i == 0) Modifier.focusRequester(firstEpisode) else Modifier)
                }
            }
        }
    }
}

/** One episode (or the movie itself): still with minutes and progress, "E1 - Title", the audio, two lines of synopsis. */
@Composable
private fun EpisodeRow(ep: Item, api: RoxyApi, index: Int, onClick: () -> Unit, modifier: Modifier = Modifier) {
    val placeholder = !ep.isPlayable
    val imageType = if (ep.isEpisode) "thumb" else "backdrop"
    val code = if (ep.isEpisode) episodeShort(ep) else ""
    val name = if (code.isEmpty()) ep.title else "$code - ${ep.title}"
    val audio = audioBadge(ep) ?: ep.availability?.takeIf { placeholder }?.replaceFirstChar { it.uppercase() }
    TvCard(onClick = onClick, modifier = modifier.alpha(if (placeholder) 0.5f else 1f)) {
        Row(modifier = Modifier.width(EpisodeThumbWidth + 12.dp + 326.dp)) {
            Box(modifier = Modifier.width(EpisodeThumbWidth).aspectRatio(16f / 9f)) {
                // Watched, as on the phone: the still dimmed, a replay arrow, "Watched" in the corner.
                val watched = ep.userData?.played == true
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

/** Facts about the title: the full synopsis, genres, year, studio, status, audio, cast. Focusable so it can be scrolled up to. */
@Composable
private fun DetailsTab(item: Item, api: RoxyApi, first: FocusRequester, loadDetails: suspend () -> ItemDetails? = { api.itemDetails(item.id) }) {
    var details by remember(item.id) { mutableStateOf<ItemDetails?>(null) }
    LaunchedEffect(item.id) { details = runCatching { loadDetails() }.getOrNull() }
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
    var similar by remember(item.id) { mutableStateOf<List<DiscoverItem>?>(null) }
    LaunchedEffect(item.id) { similar = runCatching { api.similar(item.id) }.getOrDefault(emptyList()) }
    val cards: List<Any> = related + similar.orEmpty().filter { s -> related.none { it.id == s.libraryItemId } }
    if (similar != null && cards.isEmpty()) { PageMessage("Nothing like this in the library yet."); return }
    LazyVerticalGrid(
        columns = GridCells.Fixed(6),
        contentPadding = PaddingValues(start = PageStart - TvDims.FocusBorder, top = 23.dp, end = PageStart, bottom = 24.dp),
        modifier = Modifier.fillMaxSize(),
    ) {
        itemsIndexed(cards, key = { _, c -> if (c is Item) c.id else "tmdb:${(c as DiscoverItem).tmdbId}" }) { i, c ->
            val firstMod = if (i == 0) Modifier.focusRequester(first) else Modifier
            when (c) {
                is Item -> PosterCard(c, api, width = TvDims.PosterCardWidth, captionHeight = TvDims.PosterCaption, onClick = { if (c.isMovie) onOpenMovie(c) else onOpenSeries(c) }, modifier = firstMod)
                is DiscoverItem -> UrlPosterCard(
                    title = c.title, url = c.poster, width = TvDims.PosterCardWidth, captionHeight = TvDims.PosterCaption, dim = !c.inLibrary, modifier = firstMod,
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


/**
 * A title the library does not hold (from More like this): TMDB's art and copy in the
 * same frame as a library title, with Open in library when it does exist, Request when
 * requests are on, or the request's status; Details under the strip.
 */
@Composable
fun TvDiscoverTitle(type: String, tmdbId: Int, onOpenSeries: (Item) -> Unit, onOpenMovie: (Item) -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    val scope = rememberCoroutineScope()
    var detail by remember(type, tmdbId) { mutableStateOf<DiscoverDetail?>(null) }
    var error by remember(type, tmdbId) { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    suspend fun load() { runCatching { app.api.discoverDetail(type, tmdbId) }.onSuccess { detail = it; error = null }.onFailure { error = it.message ?: "Could not load this title" } }
    LaunchedEffect(type, tmdbId) { load() }
    val d = detail
    if (d == null) {
        if (error != null) PageMessage(error!!, color = RoxyColors.error) else Box(Modifier.fillMaxSize())
        return
    }
    val stub = Item(
        id = "tmdb:$type:$tmdbId", type = if (d.isMovie) "movie" else "series", title = d.title, year = d.year, overview = d.overview,
        tagline = d.tagline, genres = d.genres, officialRating = d.officialRating, status = d.status, seasonCount = d.seasonCount.takeIf { it > 0 },
        communityRating = d.rating, hasMedia = false,
    )
    val request = d.request
    val actions = buildList {
        val libraryId = d.libraryItemId
        when {
            libraryId != null -> add(TitleAction(key = "play", label = "Open in library", icon = Icons.Outlined.PlayArrow, onClick = {
                val lib = Item(id = libraryId, type = stub.type, title = d.title)
                if (lib.isMovie) onOpenMovie(lib) else onOpenSeries(lib)
            }))
            request != null -> add(TitleAction(key = "play", label = "Requested  \u00b7  " + request.status.replaceFirstChar { it.uppercase() }, icon = Icons.Outlined.Check, enabled = false, onClick = {}))
            d.requestsEnabled -> add(TitleAction(key = "play", label = if (busy) "Requesting\u2026" else "Request", icon = Icons.Outlined.Add, enabled = !busy, onClick = {
                scope.launch {
                    busy = true
                    runCatching { app.api.discoverRequest(d.type, d.tmdbId, d.missingSeasons.map { it.number }) }.onFailure { error = it.message }
                    load()
                    busy = false
                }
            }))
            else -> add(TitleAction(key = "play", label = "Not in the library", icon = Icons.Outlined.Info, enabled = false, onClick = {}))
        }
    }
    var tab by rememberSaveable(type, tmdbId) { mutableStateOf(0) }
    TitlePage(title = stub, play = null, api = app.api, actions = actions, tabs = listOf("Details"), tab = tab, onTab = { tab = it }, backdropUrl = d.backdrop ?: d.poster, logoUrl = d.logo) { first ->
        DetailsTab(stub, app.api, first, loadDetails = { app.api.discoverMoreDetails(d.type, d.tmdbId) })
    }
}
