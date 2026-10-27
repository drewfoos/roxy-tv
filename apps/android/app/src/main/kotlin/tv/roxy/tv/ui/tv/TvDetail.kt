package tv.roxy.tv.ui.tv

import androidx.activity.compose.BackHandler
import androidx.compose.animation.core.animateDpAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
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
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.lazy.items
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
import androidx.compose.ui.focus.focusProperties
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.layout.ContentScale
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
fun TvSeries(seriesId: String, fallbackTitle: String, onOpenSeries: (Item) -> Unit, onOpenMovie: (Item) -> Unit) {
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
    TitlePage(title = series, play = next, api = app.api, actions = actions, tabs = listOf("Episodes", "Details", "More like this"), tab = tab, onTab = { tab = it }) {
        when (tab) {
            0 -> EpisodesTab(
                seasons = state.seasons, selected = state.selectedSeason, onSelectSeason = vm::selectSeason,
                episodes = state.episodes, loading = state.episodesLoading, api = app.api, onPlay = ::play,
            )
            1 -> DetailsTab(series, app.api)
            else -> MoreLikeThisTab(series, app.api, related = state.related.mapNotNull { if (it.inLibrary) it.item else null }, onOpenSeries = onOpenSeries, onOpenMovie = onOpenMovie)
        }
    }
}

/** Movie: the film itself under its tab, details, and the series it belongs to plus similar titles. */
@Composable
fun TvMovie(movieId: String, fallbackTitle: String, onOpenSeries: (Item) -> Unit, onOpenMovie: (Item) -> Unit) {
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
    TitlePage(title = movie, play = movie, api = app.api, actions = actions, tabs = listOf("Movie", "Details", "More like this"), tab = tab, onTab = { tab = it }) {
        when (tab) {
            0 -> Row(modifier = Modifier.fillMaxSize()) {
                Spacer(Modifier.width(EpisodeListStart))
                Column(modifier = Modifier.padding(top = 27.dp)) {
                    EpisodeRow(movie, app.api, index = 0, onClick = { play() })
                }
            }
            1 -> DetailsTab(movie, app.api)
            else -> MoreLikeThisTab(movie, app.api, related = state.partOf, onOpenSeries = onOpenSeries, onOpenMovie = onOpenMovie)
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
    content: @Composable () -> Unit,
) {
    val memory = rememberFocusMemory()
    var lowerFocused by remember { mutableStateOf(false) }
    val topHeight by animateDpAsState(if (lowerFocused) 0.dp else TopBlockHeight, Fold, label = "fold")
    var restored by remember { mutableStateOf(false) }
    LaunchedEffect(title.id) {
        if (restored) return@LaunchedEffect
        repeat(6) { withFrameNanos { }; if (memory.restore("play")) { restored = true; return@LaunchedEffect } }
    }

    Column(modifier = Modifier.fillMaxSize()) {
        Box(modifier = Modifier.fillMaxWidth().height(topHeight).clipToBounds()) {
            Box(modifier = Modifier.fillMaxWidth().wrapContentHeight(Alignment.Top, unbounded = true).height(TopBlockHeight)) {
                val art = api.imageUrl(title, "backdrop", widthPx(960.dp)) ?: play?.let { api.imageUrl(it, "thumb", widthPx(960.dp)) }
                RoxyImage(art, "", modifier = Modifier.fillMaxSize(), placeholderTextSize = 0)
                Box(modifier = Modifier.fillMaxSize().background(Brush.horizontalGradient(0f to RoxyColors.background.copy(alpha = 0.92f), 0.5f to Color.Transparent)))
                Box(modifier = Modifier.fillMaxSize().background(Brush.verticalGradient(0.6f to Color.Transparent, 1f to RoxyColors.background)))
                Column(modifier = Modifier.padding(start = PageStart, top = 45.dp)) {
                    Box(modifier = Modifier.height(125.dp).widthIn(max = 480.dp), contentAlignment = Alignment.BottomStart) {
                        val logo = api.imageUrl(title, "logo", widthPx(480.dp))
                        if (logo != null) AsyncImage(model = logo, contentDescription = title.title, contentScale = ContentScale.Fit, alignment = Alignment.BottomStart, modifier = Modifier.width(240.dp).heightIn(max = 125.dp))
                        else Text(title.title, fontSize = 34.sp, lineHeight = 38.sp, fontWeight = FontWeight.Bold, maxLines = 2, overflow = TextOverflow.Ellipsis)
                    }
                    Spacer(Modifier.height(12.dp))
                    FactsLine(title, play, fontSize = 16.sp)
                    Spacer(Modifier.height(14.dp))
                    Text(title.overview.orEmpty(), color = TvSubtle, fontSize = 16.sp, lineHeight = 20.sp, minLines = 3, maxLines = 3, overflow = TextOverflow.Ellipsis, modifier = Modifier.widthIn(max = 440.dp))
                    Spacer(Modifier.height(47.dp))
                    actions.forEach { a ->
                        TvListRow(
                            label = a.label, icon = a.icon, trailing = a.trailing, selected = true, height = TvDims.ListRowHeight,
                            onClick = a.onClick, modifier = Modifier.width(ActionWidth).focusKey(memory, a.key).focusProperties { down = memory.requester("tab/0") }.alpha(if (a.enabled) 1f else 0.5f),
                        )
                        Spacer(Modifier.height(2.dp))
                    }
                }
            }
        }
        Row(modifier = Modifier.padding(start = PageStart, top = 23.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            tabs.forEachIndexed { i, t -> TvTab(t, selected = i == tab, onClick = { onTab(i) }, onFocused = { onTab(i) }, modifier = Modifier.focusKey(memory, "tab/$i")) }
        }
        Box(modifier = Modifier.fillMaxWidth().weight(1f).onFocusChanged { lowerFocused = it.hasFocus }) { content() }
    }
}

/** Season picker (left) and the season's episodes (right). OK on the picker lists the seasons in its place. */
@Composable
private fun EpisodesTab(seasons: List<Item>, selected: Int, onSelectSeason: (Int) -> Unit, episodes: List<Item>, loading: Boolean, api: RoxyApi, onPlay: (Item) -> Unit) {
    var picking by rememberSaveable { mutableStateOf(false) }
    BackHandler(enabled = picking) { picking = false }
    val seasonName = seasons.getOrNull(selected)?.let { it.title.ifEmpty { "Season ${it.indexAired ?: selected + 1}" } }
    Row(modifier = Modifier.fillMaxSize().padding(top = 27.dp)) {
        Column(modifier = Modifier.width(EpisodeListStart).padding(start = PageStart, end = EpisodeListStart - PageStart - SeasonPickerWidth)) {
            if (picking) {
                seasons.forEachIndexed { i, s ->
                    TvListRow(
                        label = s.title.ifEmpty { "Season ${s.indexAired ?: i + 1}" }, selected = i == selected, height = 36.dp, fontSize = 20.sp, startPadding = 16.dp,
                        onClick = { onSelectSeason(i); picking = false }, modifier = Modifier.fillMaxWidth(),
                    )
                }
            } else {
                TvListRow(
                    label = if (seasonName != null) "Season: $seasonName" else "Episodes", trailing = "${episodes.size} Episodes".takeIf { episodes.isNotEmpty() },
                    selected = true, height = 36.dp, fontSize = 20.sp, startPadding = 16.dp,
                    onClick = { if (seasons.size > 1) picking = true }, modifier = Modifier.fillMaxWidth(),
                )
            }
        }
        if (episodes.isEmpty()) {
            Text(if (loading) "" else "No episodes in this season.", color = RoxyColors.muted, fontSize = 16.sp, modifier = Modifier.padding(top = 8.dp))
        } else {
            LazyColumn(modifier = Modifier.fillMaxHeight().width(TvDims.FocusBorder * 2 + EpisodeThumbWidth + 12.dp + 326.dp), contentPadding = PaddingValues(bottom = 24.dp)) {
                items(episodes, key = { it.id }) { ep -> EpisodeRow(ep, api, index = episodes.indexOf(ep), onClick = { if (ep.isPlayable) onPlay(ep) }) }
            }
        }
    }
}

/** One episode (or the movie itself): still with minutes and progress, "E1 - Title", the audio, two lines of synopsis. */
@Composable
private fun EpisodeRow(ep: Item, api: RoxyApi, index: Int, onClick: () -> Unit) {
    val placeholder = !ep.isPlayable
    val imageType = if (ep.isEpisode) "thumb" else "backdrop"
    val code = if (ep.isEpisode) episodeShort(ep) else ""
    val name = if (code.isEmpty()) ep.title else "$code - ${ep.title}"
    val audio = audioBadge(ep) ?: ep.availability?.takeIf { placeholder }?.replaceFirstChar { it.uppercase() }
    TvCard(onClick = onClick, modifier = Modifier.alpha(if (placeholder) 0.5f else 1f)) {
        Row(modifier = Modifier.width(EpisodeThumbWidth + 12.dp + 326.dp)) {
            Box(modifier = Modifier.width(EpisodeThumbWidth).aspectRatio(16f / 9f)) {
                RoxyImage(api.imageUrl(ep, imageType, widthPx(EpisodeThumbWidth)), code.ifEmpty { ep.title }, modifier = Modifier.fillMaxSize())
                minutesLabel(ep)?.let { MinutesBadge(it, modifier = Modifier.align(Alignment.BottomEnd).padding(bottom = 3.dp)) }
                ep.progressFraction?.takeIf { it < 1f }?.let { ProgressBar(it, modifier = Modifier.align(Alignment.BottomCenter)) }
            }
            Column(modifier = Modifier.padding(start = 12.dp, top = 10.dp)) {
                Text(name, color = RoxyColors.onBackground, fontSize = 19.sp, lineHeight = 24.sp, fontWeight = FontWeight.Medium, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text(audio ?: "", color = RoxyColors.muted, fontSize = 14.sp, lineHeight = 18.sp, maxLines = 1)
                Text(ep.overview.orEmpty(), color = RoxyColors.muted, fontSize = 16.sp, lineHeight = 20.sp, maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
        }
    }
}

/** Facts about the title: the full synopsis, genres, year, studio, status, audio, cast. Focusable so it can be scrolled up to. */
@Composable
private fun DetailsTab(item: Item, api: RoxyApi) {
    var details by remember(item.id) { mutableStateOf<ItemDetails?>(null) }
    LaunchedEffect(item.id) { details = runCatching { api.itemDetails(item.id) }.getOrNull() }
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
    TvFocusable(onClick = {}, modifier = Modifier.fillMaxSize()) { _ ->
        Row(modifier = Modifier.fillMaxSize().padding(start = PageStart, top = 27.dp, end = PageStart)) {
            Text(item.overview.orEmpty(), color = TvSubtle, fontSize = 16.sp, lineHeight = 22.sp, maxLines = 14, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
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
 * series), then TMDB's similar titles; those not in the library are dimmed and inert.
 */
@Composable
private fun MoreLikeThisTab(item: Item, api: RoxyApi, related: List<Item>, onOpenSeries: (Item) -> Unit, onOpenMovie: (Item) -> Unit) {
    var similar by remember(item.id) { mutableStateOf<List<DiscoverItem>?>(null) }
    LaunchedEffect(item.id) { similar = runCatching { api.similar(item.id) }.getOrDefault(emptyList()) }
    val cards: List<Any> = related + similar.orEmpty().filter { s -> related.none { it.id == s.libraryItemId } }
    if (similar != null && cards.isEmpty()) { PageMessage("Nothing like this in the library yet."); return }
    LazyVerticalGrid(
        columns = GridCells.Fixed(6),
        contentPadding = PaddingValues(start = PageStart - TvDims.FocusBorder, top = 23.dp, end = PageStart, bottom = 24.dp),
        modifier = Modifier.fillMaxSize(),
    ) {
        items(cards, key = { c -> if (c is Item) c.id else "tmdb:${(c as DiscoverItem).tmdbId}" }) { c ->
            when (c) {
                is Item -> PosterCard(c, api, width = TvDims.PosterCardWidth, captionHeight = TvDims.PosterCaption, onClick = { if (c.isMovie) onOpenMovie(c) else onOpenSeries(c) })
                is DiscoverItem -> UrlPosterCard(
                    title = c.title, url = c.poster, width = TvDims.PosterCardWidth, captionHeight = TvDims.PosterCaption, dim = !c.inLibrary,
                    note = if (c.inLibrary) null else "Not in library",
                    onClick = { c.libraryItemId?.let { id -> val stub = Item(id = id, type = c.type.let { t -> if (t == "movie") "movie" else "series" }, title = c.title); if (stub.isMovie) onOpenMovie(stub) else onOpenSeries(stub) } },
                )
            }
        }
    }
}
