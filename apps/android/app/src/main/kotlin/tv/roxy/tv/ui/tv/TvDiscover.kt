package tv.roxy.tv.ui.tv

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.gestures.LocalBringIntoViewSpec
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.itemsIndexed
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.focusProperties
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.flow.collectLatest
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.DiscoverItem
import tv.roxy.tv.api.DiscoverRow
import tv.roxy.tv.api.Item
import tv.roxy.tv.ui.phone.DiscoverViewModel
import tv.roxy.tv.ui.theme.RoxyColors
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Check
import androidx.compose.material.icons.outlined.Info
import androidx.compose.material.icons.outlined.PlayArrow
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.Alignment
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.text.font.FontStyle
import coil3.compose.AsyncImage
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import tv.roxy.tv.api.DiscoverDetail
import tv.roxy.tv.api.DiscoverSeason
import tv.roxy.tv.api.DownloadStatus
import tv.roxy.tv.ui.components.RoxyImage

/*
 * Discover on the TV: the phone's Discover (Roxy's own request app) laid out for a remote. A
 * search box at the top, then "Your requests" and the TMDB rows; typing turns the rows into
 * a grid of results. A title already in the library opens its page; anything else opens the
 * TMDB title page with Request.
 */

private val DiscoverPin = 40.dp
private const val ResultColumns = 6

@OptIn(ExperimentalFoundationApi::class)
@Composable
fun TvDiscover(onOpenSeries: (Item) -> Unit, onOpenMovie: (Item) -> Unit, onOpenDiscover: (type: String, tmdbId: Int) -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    // Kept by the activity, not the page: leaving Discover on the rail pops the page, and a
    // page-scoped model fetched every TMDB row again on each visit. Rows stay 30 min; only
    // "Your requests" (their status moves) is asked again when coming back.
    val activity = LocalContext.current as androidx.activity.ComponentActivity
    val vm: DiscoverViewModel = viewModel(viewModelStoreOwner = activity, key = "tv-discover") { DiscoverViewModel(app.api, rowsFreshMs = 30 * 60_000L) }
    val state by vm.state.collectAsStateWithLifecycle()
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { vm.load() }
    val memory = rememberFocusMemory()

    val rows: List<DiscoverRow> = remember(state.rows, state.requests) {
        buildList {
            if (state.requests.isNotEmpty()) add(DiscoverRow("requests", "Your requests", state.requests))
            addAll(state.rows.orEmpty().filter { it.items.isNotEmpty() })
        }
    }
    // Each row remembers its last focused card, so Up / Down land there (as on Home).
    val lastInRow = remember { HashMap<String, String>() }
    // The cards that exist right now (a lazy row or column drops what scrolled away). Every
    // Up / Down aims at one of these: aiming at a card that had scrolled off was a dead end,
    // and focus was lost (Down from the search box once the rows had scrolled, say).
    val composed = remember { HashSet<String>() }
    fun rowTarget(i: Int): String? = rows.getOrNull(i)?.let { r ->
        lastInRow[r.key]?.takeIf { it in composed } ?: r.items.asSequence().map { cardKey(r.key, it) }.firstOrNull { it in composed }
    }
    val listState = rememberLazyListState()
    val focusedRow = remember { mutableStateOf(-1) }
    // Down from the search box: the row last used if it is on screen, else the first row showing.
    fun belowSearch(): String? = rowTarget(focusedRow.value.coerceAtLeast(0))
        ?: rowTarget(listState.firstVisibleItemIndex) ?: rowTarget(listState.firstVisibleItemIndex + 1)
    val searching = state.results != null || state.query.isNotBlank()
    // Back from the rail: the card that had focus if it is still there, else the first card
    // showing in its row (or the row showing), the search box only when there are no rows.
    RegisterPageEntry {
        if (searching) return@RegisterPageEntry memory.restore("discover/search")
        val k = memory.key?.takeIf { it in composed } ?: rowTarget(focusedRow.value.coerceAtLeast(0)) ?: belowSearch()
        if (k != null) runCatching { memory.requester(k).requestFocus() }.getOrDefault(false) else memory.restore("discover/search")
    }

    fun open(c: DiscoverItem) {
        val id = c.libraryItemId
        if (id != null) {
            val stub = Item(id = id, type = if (c.isMovie) "movie" else "series", title = c.title)
            if (stub.isMovie) onOpenMovie(stub) else onOpenSeries(stub)
        } else onOpenDiscover(c.type, c.tmdbId)
    }

    Column(modifier = Modifier.fillMaxSize().padding(start = TvDims.RailWidth)) {
        Spacer(Modifier.height(40.dp))
        TvTextField(
            state.query, vm::search, placeholder = "Search TMDB for a series or movie", icon = Icons.Outlined.Search, imeAction = ImeAction.Search,
            modifier = Modifier.padding(start = 15.dp).width(560.dp).focusKey(memory, "discover/search")
                .focusProperties { if (searching) down = memory.requester("discover/result/first") else belowSearch()?.let { down = memory.requester(it) } },
        )
        Spacer(Modifier.height(16.dp))
        when {
            state.searchError != null -> PageMessage("Search failed: ${state.searchError}", color = RoxyColors.error)
            searching -> {
                val results = state.results
                if (results == null) Unit // typing; results on their way
                else if (results.isEmpty()) PageMessage("Nothing on TMDB matches \"${state.query.trim()}\".")
                else LazyVerticalGrid(
                    columns = GridCells.Fixed(ResultColumns),
                    contentPadding = PaddingValues(start = 15.dp - TvDims.FocusBorder, end = 40.dp, bottom = 32.dp),
                    verticalArrangement = Arrangement.spacedBy(2.dp),
                    modifier = Modifier.fillMaxSize(),
                ) {
                    itemsIndexed(results, key = { _, c -> "${c.type}:${c.tmdbId}" }) { i, c ->
                        DiscoverCard(
                            c, onClick = { open(c) },
                            modifier = Modifier.focusKey(memory, "discover/result/${c.type}:${c.tmdbId}")
                                .then(if (i == 0) Modifier.focusRequester(memory.requester("discover/result/first")) else Modifier)
                                .then(if (i < ResultColumns) Modifier.focusProperties { up = memory.requester("discover/search") } else Modifier),
                        )
                    }
                }
            }
            state.rows == null && state.error != null -> PageMessage(state.error ?: "Could not load Discover", color = RoxyColors.error)
            else -> {
                val pin = with(LocalDensity.current) { DiscoverPin.roundToPx() }
                // The focused row goes to the top with the row above peeking in (so Up has a
                // card to land on); the rows themselves scroll their cards as usual.
                LaunchedEffect(listState) { snapshotFlow { focusedRow.value }.collectLatest { if (it == 0) listState.animateScrollToItem(0) else if (it > 0) listState.animateScrollToItem(it, -pin) } }
                val rowSpec = LocalBringIntoViewSpec.current
                CompositionLocalProvider(LocalBringIntoViewSpec provides NoBringIntoView) {
                    LazyColumn(state = listState, modifier = Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 40.dp)) {
                        itemsIndexed(rows, key = { _, r -> r.key }) { ri, row ->
                            Column(modifier = Modifier.fillMaxWidth().padding(top = 6.dp)) {
                                Text(
                                    row.title, color = RoxyColors.muted, fontSize = 20.sp, lineHeight = 28.sp, fontWeight = FontWeight.Bold, maxLines = 1, overflow = TextOverflow.Ellipsis,
                                    modifier = Modifier.padding(start = 15.dp),
                                )
                                CompositionLocalProvider(LocalBringIntoViewSpec provides rowSpec) {
                                    LazyRow(contentPadding = PaddingValues(start = 15.dp - TvDims.FocusBorder, end = 40.dp)) {
                                        itemsIndexed(row.items, key = { _, c -> cardKey(row.key, c) }) { _, c ->
                                            val key = cardKey(row.key, c)
                                            DisposableEffect(key) { composed += key; onDispose { composed -= key } }
                                            DiscoverCard(
                                                c, onClick = { open(c) },
                                                modifier = Modifier.focusKey(memory, key)
                                                    .onFocusChanged { if (it.isFocused) { lastInRow[row.key] = key; focusedRow.value = ri } }
                                                    .focusProperties {
                                                        up = memory.requester(if (ri == 0) "discover/search" else rowTarget(ri - 1) ?: "discover/search")
                                                        rowTarget(ri + 1)?.let { down = memory.requester(it) }
                                                    },
                                            )
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}

private fun cardKey(rowKey: String, c: DiscoverItem) = "discover/$rowKey/${c.type}:${c.tmdbId}"

/** A TMDB poster with where it stands: in the library, requested, downloading, failed. */
@Composable
private fun DiscoverCard(c: DiscoverItem, onClick: () -> Unit, modifier: Modifier = Modifier) {
    val dl = c.download
    val note = when {
        c.inLibrary -> "In library"
        dl?.state == "downloading" -> "Downloading ${(dl.progress * 100).toInt().coerceIn(0, 100)}%"
        dl?.state == "failed" || dl?.state == "attention" || c.requestStatus == "failed" -> "Request failed"
        dl?.state == "importing" -> "Importing"
        dl?.state == "notFound" -> "Not found yet"
        c.requestStatus != null || dl != null -> "Requested"
        else -> null
    }
    UrlPosterCard(title = c.title, url = c.poster, width = TvDims.PosterCardWidth, captionHeight = TvDims.PosterCaption, onClick = onClick, modifier = modifier, note = note)
}

/**
 * A title from TMDB, its own page rather than a library title's frame: the art at the
 * right, copy and facts at the left, one action (Request, its status, or Open in library)
 * with a line under it saying what will happen or what is happening, then the seasons
 * and what the library holds of each. Only the action takes focus.
 */
@Composable
fun TvDiscoverTitle(type: String, tmdbId: Int, onOpenSeries: (Item) -> Unit, onOpenMovie: (Item) -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    val scope = rememberCoroutineScope()
    val cacheKey = "$type:$tmdbId"
    var detail by remember(type, tmdbId) { mutableStateOf(DetailCache.get(cacheKey)) }
    var error by remember(type, tmdbId) { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    suspend fun load() {
        runCatching { app.api.discoverDetail(type, tmdbId) }
            .onSuccess { detail = it; DetailCache.put(cacheKey, it) }
            .onFailure { if (detail == null) error = it.message ?: "Could not load this title" }
    }
    // Opened again within a few minutes (Back and forth from Discover): shown from memory, no request.
    LaunchedEffect(type, tmdbId) { if (detail == null) load() }
    // A request's download moves while the page is open.
    val watching = detail?.let { it.request != null && !it.inLibrary } == true
    LaunchedEffect(watching) {
        while (watching) { delay(15_000); load() }
    }
    val d = detail
    if (d == null) {
        if (error != null) PageMessage(error!!, color = RoxyColors.error) else DiscoverTitleSkeleton()
        return
    }
    val action = remember { FocusRequester() }
    LaunchedEffect(d.tmdbId) { repeat(6) { withFrameNanos { }; if (runCatching { action.requestFocus() }.getOrDefault(false)) return@LaunchedEffect } }

    val dl = d.download
    val canRequest = !d.inLibrary && d.request == null && d.requestsEnabled && !busy
    val label = when {
        d.inLibrary -> "Open in library"
        busy -> "Requesting…"
        d.request != null -> requestWord(d)
        d.requestsEnabled -> "Request"
        else -> "Requests aren't set up"
    }
    val icon = when {
        d.inLibrary -> Icons.Outlined.PlayArrow
        d.request != null -> Icons.Outlined.Check
        d.requestsEnabled -> Icons.Outlined.Add
        else -> Icons.Outlined.Info
    }
    val missing = d.missingSeasons
    val hint = when {
        error != null -> error
        d.inLibrary -> null
        d.request != null && dl?.message != null && (dl.state == "attention" || dl.state == "failed") -> dl.message
        d.request != null -> "Requested by ${d.request.userName.ifBlank { "someone" }}"
        !d.requestsEnabled && app.prefs.session.value.isAdmin -> "Set up ${if (d.isMovie) "Radarr" else "Sonarr"} on your phone or in the web app (Account, Sonarr & Radarr) to turn requests on."
        !d.requestsEnabled -> "Ask whoever runs this server to connect ${if (d.isMovie) "Radarr" else "Sonarr"}."
        d.isMovie -> "Sends the movie to Radarr."
        missing.isEmpty() || missing.size == d.seasons.count { !it.special } -> "Sends the series to Sonarr."
        else -> "Sends " + missing.joinToString(", ") { "S${it.number}" } + " to Sonarr."
    }

    Box(modifier = Modifier.fillMaxSize()) {
        // The art fills the screen; the copy sits on a near-solid scrim at the left.
        RoxyImage(d.backdrop ?: d.poster, "", modifier = Modifier.fillMaxSize(), placeholderTextSize = 0)
        Box(modifier = Modifier.fillMaxWidth(0.74f).fillMaxHeight().background(Brush.horizontalGradient(0f to RoxyColors.background, 0.68f to RoxyColors.background.copy(alpha = 0.93f), 1f to Color.Transparent)))
        Box(modifier = Modifier.fillMaxWidth().fillMaxHeight(0.45f).align(Alignment.BottomCenter).background(Brush.verticalGradient(0f to Color.Transparent, 1f to RoxyColors.background)))
        Column(modifier = Modifier.padding(start = 56.dp, top = 48.dp).width(520.dp)) {
            Box(modifier = Modifier.height(110.dp).fillMaxWidth(), contentAlignment = Alignment.BottomStart) {
                if (d.logo != null) {
                    AsyncImage(model = d.logo, contentDescription = d.title, contentScale = ContentScale.Fit, alignment = Alignment.BottomStart, modifier = Modifier.width(280.dp).height(110.dp))
                } else {
                    Text(d.title, fontSize = 36.sp, lineHeight = 40.sp, fontWeight = FontWeight.Bold, maxLines = 2, overflow = TextOverflow.Ellipsis, color = RoxyColors.onBackground)
                }
            }
            Spacer(Modifier.height(14.dp))
            DiscoverFacts(d)
            if (!d.tagline.isNullOrBlank()) {
                Spacer(Modifier.height(12.dp))
                Text(d.tagline, color = TvSubtle, fontSize = 16.sp, fontStyle = FontStyle.Italic, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            Spacer(Modifier.height(10.dp))
            Text(d.overview.orEmpty(), color = TvCopy, fontSize = 16.sp, lineHeight = 22.sp, maxLines = 4, overflow = TextOverflow.Ellipsis, style = TvShadowed)
            Spacer(Modifier.height(26.dp))
            TvListRow(
                label = label, icon = icon, selected = true, height = TvDims.ListRowHeight,
                trailing = if (dl != null && !d.inLibrary) dl.percentLabel() else null,
                onClick = {
                    val libraryId = d.libraryItemId
                    when {
                        libraryId != null -> {
                            val lib = Item(id = libraryId, type = if (d.isMovie) "movie" else "series", title = d.title)
                            if (lib.isMovie) onOpenMovie(lib) else onOpenSeries(lib)
                        }
                        canRequest -> scope.launch {
                            busy = true
                            error = null
                            runCatching { app.api.discoverRequest(d.type, d.tmdbId, missing.map { it.number }) }.onFailure { error = it.message ?: "The request failed" }
                            busy = false
                            load()
                        }
                    }
                },
                modifier = Modifier.width(360.dp).focusRequester(action).alpha(if (canRequest || d.inLibrary || d.request != null) 1f else 0.6f),
            )
            if (hint != null) {
                Spacer(Modifier.height(8.dp))
                Text(hint, color = if (error != null) RoxyColors.error else RoxyColors.muted, fontSize = 14.sp, maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
        }
        // Bottom: the seasons (series) and the facts that do not fit the top line.
        Column(modifier = Modifier.align(Alignment.BottomStart).padding(start = 56.dp, end = 56.dp, bottom = 36.dp)) {
            val seasons = d.seasons.filter { !it.special }
            if (seasons.isNotEmpty()) {
                Text("SEASONS", color = RoxyColors.muted, fontSize = 13.sp, letterSpacing = 1.sp, fontWeight = FontWeight.Medium)
                Spacer(Modifier.height(8.dp))
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    seasons.take(10).forEach { SeasonPill(it) }
                    if (seasons.size > 10) Text("+${seasons.size - 10}", color = RoxyColors.muted, fontSize = 14.sp, modifier = Modifier.padding(top = 10.dp))
                }
                Spacer(Modifier.height(18.dp))
            }
            val more = buildList {
                d.status?.takeIf { it.isNotBlank() }?.let { add("Status" to it) }
                d.networks.takeIf { it.isNotEmpty() }?.let { add((if (d.isMovie) "Studio" else "Network") to it.take(2).joinToString(", ")) }
                if (!d.isMovie && d.episodes > 0) add("Episodes" to d.episodes.toString())
                if (d.runtimeMinutes > 0) add("Runtime" to "${d.runtimeMinutes} min")
            }
            if (more.isNotEmpty()) Row(horizontalArrangement = Arrangement.spacedBy(36.dp)) {
                more.forEach { (k, v) ->
                    Column {
                        Text(k.uppercase(), color = RoxyColors.muted, fontSize = 12.sp, letterSpacing = 1.sp, fontWeight = FontWeight.Medium)
                        Text(v, color = RoxyColors.onBackground, fontSize = 16.sp, maxLines = 1)
                    }
                }
            }
        }
    }
}

/** TMDB title pages already fetched this session, for a few minutes each. */
private object DetailCache {
    private const val FRESH_MS = 5 * 60_000L
    private val map = HashMap<String, Pair<Long, DiscoverDetail>>()
    @Synchronized fun get(key: String): DiscoverDetail? = map[key]?.takeIf { System.currentTimeMillis() - it.first < FRESH_MS }?.second
    @Synchronized fun put(key: String, d: DiscoverDetail) {
        if (map.size > 60) map.clear()
        map[key] = System.currentTimeMillis() to d
    }
}

/** "TV-14  2023 · ★ 8.1 · Animation, Action · 3 seasons" over the copy. */
@Composable
private fun DiscoverFacts(d: DiscoverDetail) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        d.officialRating?.takeIf { it.isNotBlank() }?.let { AgePill(it); Spacer(Modifier.width(10.dp)) }
        val parts = listOfNotNull(
            d.year?.toString(),
            d.rating?.takeIf { it > 0 }?.let { "★ " + String.format(java.util.Locale.US, "%.1f", it) },
            d.genres.take(3).joinToString(", ").ifEmpty { null },
            if (d.isMovie) null else d.seasonCount.takeIf { it > 0 }?.let { if (it == 1) "1 season" else "$it seasons" },
        )
        Text(parts.joinToString("  ·  "), color = TvCopy, fontSize = 16.sp, maxLines = 1, overflow = TextOverflow.Ellipsis, style = TvShadowed)
    }
}

/** One season: "S1" over what the library has (accent = all there, outlined text = requested, grey = missing). */
@Composable
private fun SeasonPill(s: DiscoverSeason) {
    val bg = if (s.complete) RoxyColors.accent else RoxyColors.surfaceHigh
    val fg = when {
        s.complete -> OnAccent
        s.requested -> RoxyColors.accent
        s.partial -> RoxyColors.onBackground
        else -> RoxyColors.muted
    }
    val note = when {
        s.complete -> "In library"
        s.requested -> "Requested"
        s.partial -> "${s.have}/${s.episodes}"
        s.episodes > 0 -> "${s.episodes} eps"
        else -> ""
    }
    Column(modifier = Modifier.background(bg, RoundedCornerShape(6.dp)).padding(horizontal = 12.dp, vertical = 6.dp), horizontalAlignment = Alignment.CenterHorizontally) {
        Text("S${s.number}", color = fg, fontSize = 16.sp, fontWeight = FontWeight.Bold)
        if (note.isNotEmpty()) Text(note, color = fg, fontSize = 11.sp, maxLines = 1)
    }
}

/** The request's state in a word or two, for the action. */
private fun requestWord(d: DiscoverDetail): String = when (d.download?.state) {
    "downloading" -> "Downloading"
    "stalled" -> "Download stalled"
    "importing" -> "Adding to library"
    "queued", "waiting" -> "Requested  ·  waiting"
    "searching" -> "Requested  ·  searching"
    "notFound" -> "Requested  ·  not found yet"
    "failed", "attention" -> "Request needs attention"
    "complete" -> "Downloaded"
    else -> "Requested"
}

private fun DownloadStatus.percentLabel(): String? = when (state) {
    "downloading", "stalled" -> "${(progress * 100).toInt().coerceIn(0, 100)}%"
    else -> null
}

/** Blocks where the logo, facts, copy and action will be, while TMDB answers. */
@Composable
private fun DiscoverTitleSkeleton() {
    val pulse = rememberSkeletonPulse()
    Column(modifier = Modifier.padding(start = 56.dp, top = 48.dp)) {
        SkeletonBlock(pulse, Modifier.width(280.dp).height(90.dp))
        Spacer(Modifier.height(22.dp))
        SkeletonBlock(pulse, Modifier.width(340.dp).height(18.dp))
        Spacer(Modifier.height(20.dp))
        repeat(3) { SkeletonBlock(pulse, Modifier.width(480.dp).height(15.dp)); Spacer(Modifier.height(9.dp)) }
        Spacer(Modifier.height(22.dp))
        SkeletonBlock(pulse, Modifier.width(360.dp).height(TvDims.ListRowHeight))
    }
}
