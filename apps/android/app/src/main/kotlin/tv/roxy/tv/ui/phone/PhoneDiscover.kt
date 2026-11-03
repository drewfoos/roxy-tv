package tv.roxy.tv.ui.phone

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.ExperimentalFoundationApi
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
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.lazy.grid.rememberLazyGridState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.filled.SwapHoriz
import androidx.compose.material.icons.filled.Tune
import androidx.compose.material3.BottomSheetDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.derivedStateOf
import tv.roxy.tv.api.BrowseFilters
import tv.roxy.tv.api.Genre
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import tv.roxy.tv.api.DiscoverEpisode
import androidx.compose.material.icons.outlined.Circle
import androidx.compose.material.icons.filled.ExpandMore
import androidx.compose.material.icons.filled.ExpandLess
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Download
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.outlined.AddCircleOutline
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material.icons.outlined.Schedule
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.animation.animateColorAsState
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.LifecycleEventEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import coil3.compose.AsyncImage
import kotlinx.coroutines.Job
import kotlinx.coroutines.async
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.DiscoverDetail
import tv.roxy.tv.api.DiscoverItem
import tv.roxy.tv.api.DiscoverRow
import tv.roxy.tv.api.DiscoverSeason
import tv.roxy.tv.api.RoxyApi
import tv.roxy.tv.ui.theme.RoxyColors

/*
 * Discover: Roxy's own request app. TMDB rows (trending, new anime this season, popular and
 * top-rated anime, anime movies, popular and upcoming movies, popular series) and a
 * search box, every poster marked when the library already has it or someone asked for
 * it; a title page with Watch (in the library) or Request. Requests go to Sonarr /
 * Radarr when the server has them configured and are kept as pending until then.
 */

data class DiscoverUiState(
    val rows: List<DiscoverRow>? = null,
    val requests: List<DiscoverItem> = emptyList(),
    val requestsEnabled: Map<String, Boolean> = emptyMap(),
    val error: String? = null,
    val query: String = "",
    val results: List<DiscoverItem>? = null, // null = not searching
    val searching: Boolean = false,
    /** The last search failed (TMDB unreachable); shown with a retry instead of falling back to the rows. */
    val searchError: String? = null,
    // Filters: when active, a paged grid from TMDB discover replaces the rows.
    val filters: BrowseFilters = BrowseFilters(),
    val browseItems: List<DiscoverItem>? = null,
    val browsePage: Int = 0,
    val browseTotalPages: Int = 1,
    val browseLoading: Boolean = false,
    val genres: Map<String, List<Genre>> = emptyMap(),
)

class DiscoverViewModel(private val api: RoxyApi, private val rowsFreshMs: Long = 120_000L) : ViewModel() {
    private val _state = MutableStateFlow(DiscoverUiState())
    val state: StateFlow<DiscoverUiState> = _state
    private var searchJob: Job? = null
    private var loadedAt = 0L

    /**
     * The rows and "Your requests". Coming back to the tab within a couple of minutes only
     * refreshes the requests (their status moves); the TMDB rows (~100 KB) are kept.
     */
    fun load(force: Boolean = false) {
        val fresh = System.currentTimeMillis() - loadedAt < rowsFreshMs && !_state.value.rows.isNullOrEmpty()
        if (fresh && !force) {
            viewModelScope.launch { refreshRequests() }
            return
        }
        viewModelScope.launch {
            // Both at once and shown together: "Your requests" arriving after the rows would be
            // inserted above the first visible row, i.e. out of sight.
            val reqs = async { runCatching { api.discoverRequests() }.getOrNull() }
            runCatching { api.discover() }
                .onSuccess { d ->
                    loadedAt = System.currentTimeMillis()
                    _state.value = _state.value.copy(rows = d.rows, requestsEnabled = d.requestsEnabled, requests = reqs.await() ?: _state.value.requests, error = null)
                }
                .onFailure { t -> Timber.w(t, "discover failed"); _state.value = _state.value.copy(error = t.message) }
        }
    }

    /** "Your requests" again: their download status moves while the tab is open. */
    suspend fun refreshRequests() {
        runCatching { api.discoverRequests() }.onSuccess { r -> _state.value = _state.value.copy(requests = r) }
    }

    /** New filters: the grid starts over from page one (the type alone keeps the rows). */
    fun setFilters(f: BrowseFilters) {
        _state.value = _state.value.copy(filters = f, browseItems = null, browsePage = 0, browseTotalPages = 1)
        if (f.isActive) loadMore()
    }

    /** Next page of the filtered grid; a no-op while loading or past the last page. */
    fun loadMore() {
        val s = _state.value
        if (s.browseLoading || !s.filters.isActive || (s.browsePage > 0 && s.browsePage >= s.browseTotalPages)) return
        val f = s.filters
        val page = s.browsePage + 1
        _state.value = s.copy(browseLoading = true)
        viewModelScope.launch {
            runCatching { api.discoverBrowse(f, page) }
                .onSuccess { r ->
                    val cur = _state.value
                    if (cur.filters != f) return@onSuccess
                    _state.value = cur.copy(browseItems = cur.browseItems.orEmpty() + r.items, browsePage = r.page, browseTotalPages = r.totalPages, browseLoading = false)
                }
                .onFailure { t -> Timber.w(t, "browse"); _state.value = _state.value.copy(browseLoading = false, browseItems = _state.value.browseItems ?: emptyList()) }
        }
    }

    fun loadGenres(type: String) {
        if (_state.value.genres.containsKey(type)) return
        viewModelScope.launch {
            runCatching { api.discoverGenres(type) }.onSuccess { g -> _state.value = _state.value.copy(genres = _state.value.genres + (type to g)) }
        }
    }

    /** Type-to-search with a short debounce; an empty query returns to the rows. */
    fun search(q: String) {
        _state.value = _state.value.copy(query = q, searchError = null)
        searchJob?.cancel()
        if (q.isBlank()) { _state.value = _state.value.copy(results = null, searching = false); return }
        searchJob = viewModelScope.launch {
            delay(350)
            _state.value = _state.value.copy(searching = true)
            runCatching { api.discoverSearch(q) }
                .onSuccess { r -> if (_state.value.query == q) _state.value = _state.value.copy(results = r, searching = false) }
                .onFailure { t -> Timber.w(t, "discover search"); if (_state.value.query == q) _state.value = _state.value.copy(searching = false, results = null, searchError = t.message ?: "search failed") }
        }
    }
}

/** The Discover tab: search box, then "Your requests" and the TMDB rows (or search results as a grid). */
@Composable
fun PhoneDiscover(onOpen: (DiscoverItem) -> Unit, bottomInset: Dp) {
    val app = RoxyApp.get(LocalContext.current)
    val vm: DiscoverViewModel = viewModel { DiscoverViewModel(app.api) }
    val state by vm.state.collectAsStateWithLifecycle()
    // Keep the request cards current while the tab shows: fast while something downloads.
    val anyActive = state.requests.any { it.download?.isActive == true }
    PollWhileResumed(if (anyActive) PollFastMs else PollSlowMs) { vm.refreshRequests() }
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { vm.load() }
    tv.roxy.tv.ui.components.OnServerBack { vm.load(force = true) }

    Column(modifier = Modifier.fillMaxSize().statusBarsPadding()) {
        TabHeader("Discover")
        SearchBox(query = state.query, onChange = vm::search, modifier = Modifier.padding(horizontal = PhonePad, vertical = 6.dp))
        var sheet by rememberSaveable { mutableStateOf(false) }
        if (state.results == null && !state.searching && state.searchError == null) {
            FilterBar(filters = state.filters, onChange = vm::setFilters, onOpenSheet = { vm.loadGenres(state.filters.type); sheet = true })
        }
        if (sheet) {
            FilterSheet(filters = state.filters, genres = state.genres[state.filters.type].orEmpty(), onDismiss = { sheet = false }, onApply = { f -> sheet = false; vm.setFilters(f) })
        }
        when {
            state.results == null && !state.searching && state.filters.isActive -> {
                val items = state.browseItems
                val gridState = rememberLazyGridState()
                // Infinite scroll: ask for the next page when the last row comes into view.
                val nearEnd by remember { derivedStateOf { val info = gridState.layoutInfo; info.visibleItemsInfo.lastOrNull()?.index ?: 0 >= info.totalItemsCount - 6 } }
                LaunchedEffect(nearEnd, items?.size) { if (nearEnd && !items.isNullOrEmpty()) vm.loadMore() }
                when {
                    items == null -> GridSkeleton(modifier = Modifier.padding(top = 8.dp))
                    items.isEmpty() -> Box(Modifier.fillMaxWidth().padding(32.dp), contentAlignment = Alignment.Center) { Text("Nothing matches these filters.", color = RoxyColors.muted, textAlign = TextAlign.Center) }
                    else -> LazyVerticalGrid(
                        columns = GridCells.Fixed(3), state = gridState, modifier = Modifier.fillMaxSize(),
                        contentPadding = PaddingValues(start = PhonePad, end = PhonePad, top = 8.dp, bottom = bottomInset + 16.dp),
                        horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(12.dp),
                    ) {
                        items(items, key = { "${it.type}:${it.tmdbId}" }) { DiscoverCard(it, modifier = Modifier.fillMaxWidth(), onClick = { onOpen(it) }) }
                        if (state.browseLoading) item(span = { GridItemSpan(3) }) { Box(Modifier.fillMaxWidth().padding(16.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator(color = RoxyColors.accent, modifier = Modifier.size(24.dp), strokeWidth = 2.dp) } }
                    }
                }
            }
            state.searchError != null -> Column(modifier = Modifier.fillMaxWidth().padding(32.dp), horizontalAlignment = Alignment.CenterHorizontally) {
                Text("Couldn't reach TMDB", fontSize = 16.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground)
                Text(state.searchError ?: "", fontSize = 12.sp, color = RoxyColors.muted, textAlign = TextAlign.Center, modifier = Modifier.padding(top = 6.dp, bottom = 14.dp))
                GreyButton("Try again", onClick = { vm.search(state.query) })
            }
            state.results != null || state.searching -> {
                val results = state.results
                when {
                    results == null -> GridSkeleton(modifier = Modifier.padding(top = 8.dp))
                    results.isEmpty() -> Box(Modifier.fillMaxWidth().padding(32.dp), contentAlignment = Alignment.Center) { Text("Nothing on TMDB for \"${state.query}\".", color = RoxyColors.muted, textAlign = TextAlign.Center) }
                    else -> LazyVerticalGrid(
                        columns = GridCells.Fixed(3), modifier = Modifier.fillMaxSize(),
                        contentPadding = PaddingValues(start = PhonePad, end = PhonePad, top = 8.dp, bottom = bottomInset + 16.dp),
                        horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(12.dp),
                    ) {
                        items(results, key = { "${it.type}:${it.tmdbId}" }) { DiscoverCard(it, modifier = Modifier.fillMaxWidth(), onClick = { onOpen(it) }) }
                    }
                }
            }
            state.rows == null && state.error != null -> Column(modifier = Modifier.fillMaxWidth().padding(32.dp), horizontalAlignment = Alignment.CenterHorizontally) {
                Text("Discover is unavailable", fontSize = 18.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground)
                Text(state.error ?: "", color = RoxyColors.muted, fontSize = 13.sp, textAlign = TextAlign.Center, modifier = Modifier.padding(top = 8.dp, bottom = 16.dp))
                GreyButton("Try again", onClick = { vm.load(force = true) })
            }
            state.rows == null -> Column(modifier = Modifier.fillMaxSize().padding(bottom = bottomInset)) {
                RowSkeleton(modifier = Modifier.padding(top = 16.dp)); RowSkeleton(modifier = Modifier.padding(top = 22.dp)); RowSkeleton(modifier = Modifier.padding(top = 22.dp))
            }
            else -> LazyColumn(modifier = Modifier.fillMaxSize(), state = rememberLazyListState(), contentPadding = PaddingValues(top = 8.dp, bottom = bottomInset + 16.dp)) {
                if (state.requests.isNotEmpty()) {
                    item(key = "requests") { DiscoverRowView("Your requests", state.requests, onOpen = onOpen, modifier = Modifier.padding(top = 10.dp)) }
                }
                items(state.rows.orEmpty(), key = { it.key }) { row ->
                    DiscoverRowView(row.title, row.items, onOpen = onOpen, modifier = Modifier.padding(top = 22.dp))
                }
            }
        }
    }
}

private val sortChoices = listOf("popular" to "Popular", "top" to "Top rated", "newest" to "Newest", "oldest" to "Oldest", "votes" to "Most voted")
private val languageChoices = listOf("" to "Any language", "ja" to "Japanese", "ko" to "Korean", "en" to "English", "zh" to "Chinese", "es" to "Spanish", "fr" to "French", "de" to "German", "hi" to "Hindi")
private val ratingChoices = listOf(0.0 to "Any rating", 6.0 to "6+", 7.0 to "7+", 8.0 to "8+", 8.5 to "8.5+")
private val yearChoices: List<Pair<Pair<Int, Int>, String>> = listOf((0 to 0) to "Any year", (2020 to 0) to "2020s", (2010 to 2019) to "2010s", (2000 to 2009) to "2000s", (1990 to 1999) to "1990s", (0 to 1989) to "Before 1990")

/**
 * Chips under the search box: Series / Movies, Anime, one chip for the sheet (named after
 * the sort, with a count of the other filters), and Clear when anything is set.
 */
@Composable
private fun FilterBar(filters: BrowseFilters, onChange: (BrowseFilters) -> Unit, onOpenSheet: () -> Unit) {
    Row(modifier = Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(horizontal = PhonePad, vertical = 6.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        Chip(if (filters.type == "movie") "Movies" else "Series", selected = filters.browsing, trailing = Icons.Filled.SwapHoriz, onClick = { onChange(filters.copy(type = if (filters.type == "movie") "tv" else "movie", genres = emptyList(), status = "", browsing = true)) })
        Chip("Anime", selected = filters.anime, onClick = { onChange(filters.copy(anime = !filters.anime, language = "", browsing = true)) })
        // One chip for the sheet; it names the sort and counts the other filters, e.g. "Top rated · 2".
        val n = filters.count - (if (filters.anime) 1 else 0)
        val sortLabel = sortChoices.firstOrNull { it.first == filters.sort }?.second ?: "Popular"
        Chip(if (n > 0) "$sortLabel · $n" else sortLabel, selected = n > 0 || filters.sort != "popular", trailing = Icons.Filled.Tune, onClick = onOpenSheet)
        if (filters.isActive) Chip("Clear", selected = false, trailing = Icons.Filled.Close, onClick = { onChange(BrowseFilters(type = filters.type)) })
    }
}

/** The full filter form in a bottom sheet: sort, genres, language, years, rating, status. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun FilterSheet(filters: BrowseFilters, genres: List<Genre>, onDismiss: () -> Unit, onApply: (BrowseFilters) -> Unit) {
    var f by remember(filters) { mutableStateOf(filters) }
    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true), containerColor = RoxyColors.surface, dragHandle = { BottomSheetDefaults.DragHandle(color = RoxyColors.muted) }) {
        Column(modifier = Modifier.fillMaxWidth().fillMaxHeight(0.92f)) {
            Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.padding(horizontal = PhonePad)) {
                Text("Filters", fontSize = 20.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.weight(1f))
                Text("Reset", fontSize = 14.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.accent, modifier = Modifier.clickable { f = BrowseFilters(type = f.type) }.padding(8.dp))
            }
            // The sections scroll; Show results stays pinned underneath.
            Column(modifier = Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(horizontal = PhonePad).padding(top = 12.dp, bottom = 16.dp), verticalArrangement = Arrangement.spacedBy(16.dp)) {
            FilterGroup("Show") {
                Chip("Series", selected = f.type == "tv", onClick = { f = f.copy(type = "tv", genres = emptyList()) })
                Chip("Movies", selected = f.type == "movie", onClick = { f = f.copy(type = "movie", genres = emptyList(), status = "") })
                Chip("Anime only", selected = f.anime, onClick = { f = f.copy(anime = !f.anime, language = "") })
            }
            FilterGroup("Sort by") { sortChoices.forEach { (v, l) -> Chip(l, selected = f.sort == v, onClick = { f = f.copy(sort = v) }) } }
            FilterGroup("Genres") {
                if (genres.isEmpty()) Text("Loading genres…", fontSize = 13.sp, color = RoxyColors.muted)
                genres.forEach { g -> val on = g.id in f.genres; Chip(g.name, selected = on, onClick = { f = f.copy(genres = if (on) f.genres - g.id else f.genres + g.id) }) }
            }
            if (!f.anime) FilterGroup("Original language") { languageChoices.forEach { (v, l) -> Chip(l, selected = f.language == v, onClick = { f = f.copy(language = v) }) } }
            FilterGroup("Years") { yearChoices.forEach { (range, l) -> val on = f.yearFrom == range.first && f.yearTo == range.second; Chip(l, selected = on, onClick = { f = f.copy(yearFrom = range.first, yearTo = range.second) }) } }
            FilterGroup("Rating") { ratingChoices.forEach { (v, l) -> Chip(l, selected = f.minRating == v, onClick = { f = f.copy(minRating = v) }) } }
            if (f.type == "tv") FilterGroup("Status") {
                listOf("" to "Any", "airing" to "Airing", "ended" to "Ended").forEach { (v, l) -> Chip(l, selected = f.status == v, onClick = { f = f.copy(status = v) }) }
            }
            }
            Box(modifier = Modifier.fillMaxWidth().background(RoxyColors.surface).navigationBarsPadding().padding(horizontal = PhonePad, vertical = 12.dp)) {
                PlayButton("Show results", pill = true, container = RoxyColors.accent, content = OnBrand, modifier = Modifier.fillMaxWidth(), icon = Icons.Filled.Check, onClick = { onApply(f.copy(browsing = true)) })
            }
        }
    }
}

@Composable
private fun FilterGroup(title: String, content: @Composable () -> Unit) {
    Column {
        Text(title, fontSize = 13.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.muted, modifier = Modifier.padding(bottom = 8.dp))
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) { content() }
    }
}

/** Rounded dark search field with a clear button; TMDB search as you type. */
@Composable
private fun SearchBox(query: String, onChange: (String) -> Unit, modifier: Modifier = Modifier) {
    Row(
        modifier = modifier.fillMaxWidth().height(42.dp).clip(RoundedCornerShape(21.dp)).background(RoxyColors.surfaceHigh).padding(horizontal = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(Icons.Filled.Search, contentDescription = null, tint = RoxyColors.muted, modifier = Modifier.size(20.dp))
        Box(modifier = Modifier.weight(1f).padding(start = 10.dp)) {
            if (query.isEmpty()) Text("Search shows and movies on TMDB", color = RoxyColors.muted, fontSize = 14.sp)
            BasicTextField(
                value = query, onValueChange = onChange, singleLine = true,
                textStyle = TextStyle(color = RoxyColors.onBackground, fontSize = 14.sp),
                cursorBrush = SolidColor(RoxyColors.accent),
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
                modifier = Modifier.fillMaxWidth(),
            )
        }
        if (query.isNotEmpty()) Icon(Icons.Filled.Close, contentDescription = "Clear", tint = RoxyColors.muted, modifier = Modifier.size(20.dp).clickable { onChange("") })
    }
}

/** A titled row of Discover posters, three and a half across. */
@Composable
private fun DiscoverRowView(title: String, items: List<DiscoverItem>, onOpen: (DiscoverItem) -> Unit, modifier: Modifier = Modifier) {
    val screen = LocalConfiguration.current.screenWidthDp.dp
    val cardWidth = (screen - PhonePad - 8.dp * 3) / 3.5f
    Column(modifier = modifier.fillMaxWidth()) {
        Text(title, fontSize = 18.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(start = PhonePad, bottom = 10.dp))
        LazyRow(contentPadding = PaddingValues(horizontal = PhonePad), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            items(items, key = { "${it.type}:${it.tmdbId}" }) { DiscoverCard(it, modifier = Modifier.width(cardWidth), onClick = { onOpen(it) }) }
        }
    }
}

/** Poster with a corner badge (check = in your library, clock = requested), name and year beneath. */
@Composable
@OptIn(ExperimentalFoundationApi::class)
private fun DiscoverCard(item: DiscoverItem, modifier: Modifier = Modifier, onClick: () -> Unit) {
    val dl = item.download
    // A download in progress wins over the library tick: a show half in the library can
    // be fetching its next episodes.
    val moving = dl != null && dl.isMoving
    var showDownloads by remember { mutableStateOf(false) }
    Column(modifier = modifier) {
        Box(
            modifier = Modifier.fillMaxWidth().aspectRatio(2f / 3f).clip(RoundedCornerShape(6.dp)).background(RoxyColors.surfaceHigh)
                // Long-press (the phone's hover) lists what is downloading, episode by episode.
                .combinedClickable(onClick = onClick, onLongClick = if (moving) ({ showDownloads = true }) else null),
        ) {
            if (item.poster != null) AsyncImage(model = item.poster, contentDescription = item.title, contentScale = ContentScale.Crop, modifier = Modifier.fillMaxSize())
            else Text(item.title, color = RoxyColors.muted, fontSize = 11.sp, textAlign = TextAlign.Center, modifier = Modifier.align(Alignment.Center).padding(6.dp))
            // The corner says it all: a downloading title its episodes and progress (tap for
            // the list), a requested one its state ("Searching", "Not out yet", "Failed"),
            // a title in the library the tick.
            when {
                moving -> ProgressBadge(dl!!, item.isMovie, modifier = Modifier.align(Alignment.TopEnd).padding(5.dp).clip(RoundedCornerShape(4.dp)).clickable { showDownloads = true })
                dl != null && !item.inLibrary -> StateBox(dl, modifier = Modifier.align(Alignment.TopEnd).padding(5.dp))
                else -> StatusBadge(item.inLibrary, item.requestStatus, modifier = Modifier.align(Alignment.TopEnd).padding(5.dp), download = dl)
            }
            if (moving) DownloadBar(dl!!.progress, dl.state, modifier = Modifier.align(Alignment.BottomCenter).fillMaxWidth())
            androidx.compose.material3.DropdownMenu(expanded = showDownloads, onDismissRequest = { showDownloads = false }, containerColor = RoxyColors.surface) {
                if (dl != null) ActiveDownloadsList(dl, item.title, item.isMovie)
            }
        }
        Text(item.title, fontSize = 12.sp, lineHeight = 15.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground, maxLines = 2, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 6.dp))
        // Under the title only the year and type; the state lives in the corner box.
        Text(listOfNotNull(item.year?.toString(), if (item.isAnime) "Anime" else if (item.isMovie) "Movie" else null).joinToString("  ·  "), fontSize = 11.sp, color = RoxyColors.muted, maxLines = 1)
    }
}

/** Small round badge: brand check when the library has it, clock while requested. */
@Composable
internal fun StatusBadge(inLibrary: Boolean, requestStatus: String?, modifier: Modifier = Modifier, download: tv.roxy.tv.api.DownloadStatus? = null) {
    val (icon, bg) = when {
        inLibrary -> Icons.Filled.Check to RoxyColors.accent
        requestStatus == "failed" || requestStatus == "declined" || download?.state == "failed" || download?.state == "attention" -> Icons.Filled.Close to RoxyColors.error
        download?.state == "downloading" || download?.state == "importing" -> Icons.Filled.Download to RoxyColors.accent
        requestStatus != null -> Icons.Outlined.Schedule to Color(0xFF4B4B57)
        else -> return
    }
    Box(modifier = modifier.size(22.dp).clip(CircleShape).background(bg).border(1.5.dp, Color(0x66000000), CircleShape), contentAlignment = Alignment.Center) {
        Icon(icon, contentDescription = null, tint = if (inLibrary) OnBrand else Color.White, modifier = Modifier.size(14.dp))
    }
}

/**
 * The Discover title page: backdrop, name and facts from TMDB, then Watch when the
 * library has it, Request otherwise. A request shows its state (pending until Sonarr /
 * Radarr are connected, then sent, or failed with the reason) and can be withdrawn.
 */
@Composable
fun PhoneDiscoverDetail(
    type: String, tmdbId: Int, onBack: () -> Unit, onOpenSeries: (String, String) -> Unit, onOpenMovie: (String, String) -> Unit,
    onMoreDetails: (String) -> Unit = {}, onOpenPerson: (tv.roxy.tv.api.CastMember) -> Unit = {},
) {
    val app = RoxyApp.get(LocalContext.current)
    val scope = rememberCoroutineScope()
    var detail by remember { mutableStateOf<DiscoverDetail?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    var setupPrompt by remember { mutableStateOf(false) }
    // An action the server refused ("already on it", "searched a moment ago"): shown for a few seconds.
    var notice by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(notice) { if (notice != null) { delay(6_000); notice = null } }
    // A request / withdraw / approve the server refused (409 "already requested", network):
    // shown over the action button. Not touched by reload(); cleared by the next action.
    var requestError by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(requestError) { if (requestError != null) { delay(12_000); requestError = null } }
    fun failed(t: Throwable) { Timber.w(t, "discover action"); requestError = t.message ?: "Something went wrong" }

    suspend fun reload() {
        runCatching { app.api.discoverDetail(type, tmdbId) }
            .onSuccess { detail = it; error = null }
            .onFailure { t -> Timber.w(t, "discover detail"); error = t.message }
    }
    // A movie has no seasons: its page carries what "More details" would (cast, studio,
    // dates, keywords, links) instead of a link to it.
    var movieDetails by remember { mutableStateOf<tv.roxy.tv.api.ItemDetails?>(null) }
    LaunchedEffect(type, tmdbId) {
        if (type == "movie") movieDetails = runCatching { app.api.discoverMoreDetails(type, tmdbId) }.onFailure { Timber.w(it, "movie details") }.getOrNull()
    }
    LaunchedEffect(type, tmdbId) { reload() }
    // Poll while a request is in flight. Keyed on the request, so one sent from this page
    // starts it: fast while something moves, 10 s until the tracker first reports, slow
    // while it waits (approval, pending, not found, not aired). Stops once it is downloaded.
    val dlNow = detail?.download
    val reqNow = detail?.request
    val pollMs = when {
        reqNow == null && dlNow == null -> 0L
        dlNow?.isActive == true -> PollFastMs
        reqNow?.status == "sent" && dlNow == null -> 10_000L
        reqNow?.status == "approval" || reqNow?.status == "pending" -> PollSlowMs
        dlNow != null && !(dlNow.state == "complete" && dlNow.have > 0) -> PollSlowMs
        else -> 0L
    }
    PollWhileResumed(pollMs, key = "${reqNow?.id}/${reqNow?.status}") { reload() }

    fun requestSeasons(seasons: List<Int>) {
        val cur = detail ?: return
        if (!cur.requestsEnabled) { setupPrompt = true; return }
        if (busy) return
        busy = true
        requestError = null
        scope.launch {
            runCatching { app.api.discoverRequest(cur.type, cur.tmdbId, seasons) }.onFailure(::failed)
            reload(); busy = false
        }
    }

    val d = detail
    if (d == null) {
        if (error != null) Column(modifier = Modifier.fillMaxSize().statusBarsPadding().padding(PhonePad)) { BackCircle(onBack); Text(error ?: "", color = RoxyColors.error, modifier = Modifier.padding(top = 16.dp)) }
        else DetailSkeleton(onBack)
        return
    }

    // The download card (with its own notice line) shows for a sent request still on its way.
    val cardShown = d.download.let { dl -> dl != null && d.request?.status == "sent" && !(d.inLibrary && dl.state == "complete") }
    val listState = rememberLazyListState()
    // The library title page's look: tall key art under a black status bar, the logo on
    // its faded foot. The header strip goes solid as the art fades, and the name takes it
    // over once the logo has scrolled under it.
    val density = LocalDensity.current
    val titleFromPx = with(density) { (BannerHeight - LogoOverlap + 30.dp).toPx() }
    val stripPx = with(density) { 70.dp.toPx() }
    val stripFromPx = titleFromPx - stripPx
    val headerAlpha by remember { derivedStateOf { if (listState.firstVisibleItemIndex > 0) 1f else ((listState.firstVisibleItemScrollOffset - stripFromPx) / stripPx).coerceIn(0f, 1f) } }
    val pinned by remember { derivedStateOf { listState.firstVisibleItemIndex > 0 || listState.firstVisibleItemScrollOffset > titleFromPx } }
    Box(modifier = Modifier.fillMaxSize()) {
        LazyColumn(modifier = Modifier.fillMaxSize(), state = listState, contentPadding = PaddingValues(bottom = 120.dp)) {
            // TMDB's art straight from TMDB: nothing is saved, and there is no art picker here.
            item(key = "banner") { TitleBannerArt(d.backdrop ?: d.poster, d.title, listState) }
            item(key = "info") {
                TitleLogoColumn(logo = d.logo, title = d.title) {
                    FactsLine(
                        rating = d.rating?.takeIf { it > 0 },
                        age = d.officialRating?.takeIf { it.isNotBlank() }?.let { ageLabel(it) },
                        facts = listOfNotNull(
                            d.year?.toString(),
                            d.seasonCount.takeIf { it > 0 }?.let { if (it == 1) "1 Season" else "$it Seasons" },
                            d.runtimeMinutes.takeIf { it > 0 && d.isMovie }?.let { "${it / 60}h ${it % 60}m".removePrefix("0h ") },
                        ),
                    )
                    val genres = d.genres.take(3).joinToString(", ")
                    if (genres.isNotEmpty()) Text(genres, fontSize = 13.sp, color = FactsGrey, textAlign = TextAlign.Center, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 8.dp))
                    if (!d.isMovie) {
                        d.overview?.takeIf { it.isNotBlank() }?.let {
                            Text(
                                it, fontSize = 14.sp, lineHeight = 20.sp, color = RoxyColors.onBackground.copy(alpha = 0.9f),
                                maxLines = 4, overflow = TextOverflow.Ellipsis, // the whole text is on "More details"
                                modifier = Modifier.fillMaxWidth().padding(top = 16.dp),
                            )
                        }
                        // Crunchyroll's "More details": cast, studio, dates, keywords, links.
                        Text(
                            "More details", fontSize = 13.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.accent,
                            modifier = Modifier.padding(top = 10.dp).clickable { onMoreDetails(d.title) }.padding(vertical = 4.dp, horizontal = 8.dp),
                        )
                    }
                    if (d.seasons.isNotEmpty()) {
                        Column(modifier = Modifier.fillMaxWidth().padding(top = 22.dp)) {
                            Text("Seasons", fontSize = 17.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground)
                            d.seasons.forEach { sn ->
                                val reqId = d.request?.takeIf { it.status == "sent" }?.id
                                SeasonRow(
                                    sn, tmdbId = d.tmdbId, refreshKey = d.download?.updatedAt,
                                    canSearchAgain = d.download?.canSearch == true,
                                    onRequest = { requestSeasons(listOf(sn.number)) },
                                    onRequestEpisode = { code ->
                                        if (!d.requestsEnabled) setupPrompt = true
                                        else if (!busy) { busy = true; requestError = null; scope.launch { runCatching { app.api.discoverRequest(d.type, d.tmdbId, episodes = listOf(code)) }.onFailure(::failed); reload(); busy = false } }
                                    },
                                    // One episode: search for it again, or (admin) replace its failed / stalled download.
                                    episodeAction = if (reqId == null) null else { code, retry ->
                                        if (!busy) {
                                            busy = true
                                            scope.launch {
                                                runCatching { if (retry) app.api.tryAnotherRelease(reqId, listOf(code)) else app.api.searchAgain(reqId, listOf(code)) }.onFailure { t -> notice = t.message }
                                                reload(); busy = false
                                            }
                                        }
                                    },
                                    admin = app.prefs.session.value.isAdmin,
                                )
                            }
                        }
                    }
                    val dl = d.download
                    if (cardShown && dl != null && d.request != null) {
                        DownloadStatusCard(
                            dl, isMovie = d.isMovie, admin = app.prefs.session.value.isAdmin, busy = busy,
                            onSearchAgain = { val id = d.request.id; busy = true; scope.launch { runCatching { app.api.searchAgain(id) }.onFailure { t -> notice = t.message }; reload(); busy = false } },
                            onTryAnother = { val id = d.request.id; busy = true; scope.launch { runCatching { app.api.tryAnotherRelease(id) }.onFailure { t -> notice = t.message }; reload(); busy = false } },
                            modifier = Modifier.padding(top = 18.dp),
                        )
                        notice?.let { Text(it.replaceFirstChar { c -> c.uppercase() }, fontSize = 13.sp, lineHeight = 18.sp, color = RoxyColors.muted, textAlign = TextAlign.Center, modifier = Modifier.padding(top = 8.dp)) }
                    } else d.request?.let { r ->
                        val line = when (r.status) {
                            "sent" -> "Requested by ${r.userName.ifEmpty { "someone" }}  ·  sent to ${if (d.isMovie) "Radarr" else "Sonarr"}"
                            "failed" -> "Request failed  ·  ${r.error ?: "unknown error"}"
                            "approval" -> "Requested by ${r.userName.ifEmpty { "someone" }}  ·  waiting for an admin to approve"
                            "declined" -> "Request declined" + (r.error?.takeIf { it.isNotBlank() }?.let { "  ·  $it" } ?: "")
                            else -> "Requested by ${r.userName.ifEmpty { "someone" }}  ·  waiting for ${if (d.isMovie) "Radarr" else "Sonarr"}"
                        }
                        Text(line, fontSize = 12.sp, color = if (r.status == "failed" || r.status == "declined") RoxyColors.error else RoxyColors.muted, textAlign = TextAlign.Center, modifier = Modifier.padding(top = 16.dp))
                        // Admin: approve or decline a basic user's request right here.
                        if (r.status == "approval" && app.prefs.session.value.isAdmin) {
                            Row(horizontalArrangement = Arrangement.spacedBy(12.dp), modifier = Modifier.padding(top = 10.dp)) {
                                GreyButton("Decline", modifier = Modifier.height(40.dp), onClick = {
                                    if (!busy) { busy = true; scope.launch { runCatching { app.api.declineRequest(r.id) }.onFailure(::failed); reload(); busy = false } }
                                })
                                PlayButton("Approve", icon = Icons.Filled.Check, height = 40.dp, pill = true, container = RoxyColors.accent, content = OnBrand, onClick = {
                                    if (!busy) { busy = true; scope.launch { runCatching { app.api.approveRequest(r.id) }.onFailure(::failed); reload(); busy = false } }
                                })
                            }
                        }
                    }
                    if (!d.requestsEnabled && (!d.inLibrary || d.missingSeasons.isNotEmpty())) {
                        Text(
                            "Requests are off until ${if (d.isMovie) "Radarr" else "Sonarr"} is connected to Roxy Server.",
                            fontSize = 12.sp, lineHeight = 16.sp, color = RoxyColors.muted, textAlign = TextAlign.Center, modifier = Modifier.padding(top = 12.dp),
                        )
                    }
                }
            }
            if (d.isMovie) item(key = "details") {
                val m = movieDetails
                if (m != null) DetailsSections(m, onOpenPerson = onOpenPerson, showTagline = false) // the tagline is under the title already
                else Column(modifier = Modifier.padding(horizontal = PhonePad, vertical = 16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    BoneLine(width = 120.dp, height = 16.dp); repeat(4) { BoneLine(width = 300.dp, height = 12.dp) }
                }
            }
        }
        Box(modifier = Modifier.fillMaxWidth().statusBarsPadding().padding(top = HeaderRow).height(18.dp).alpha(headerAlpha)
            .background(Brush.verticalGradient(0f to RoxyColors.background, 1f to Color.Transparent)))
        Row(modifier = Modifier.fillMaxWidth().background(RoxyColors.background.copy(alpha = headerAlpha)).statusBarsPadding().padding(horizontal = 12.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
            BackCircle(onBack)
            if (pinned) Text(d.title, fontSize = 17.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(start = 10.dp))
        }
        if (setupPrompt) RequestsSetupDialog(if (d.isMovie) "movie" else "tv", onDismiss = { setupPrompt = false })
        Box(
            modifier = Modifier.align(Alignment.BottomCenter).fillMaxWidth()
                .background(Brush.verticalGradient(0f to Color.Transparent, 0.35f to RoxyColors.background.copy(alpha = 0.96f), 1f to RoxyColors.background))
                .navigationBarsPadding().padding(start = PhonePad, end = PhonePad, top = 24.dp, bottom = 12.dp),
        ) {
            val missing = d.missingSeasons
            // Refusals show right over the button that caused them (notice too when the card is not there).
            val message = requestError ?: notice?.takeIf { !cardShown }
            Column {
                if (message != null) Text(
                    message.replaceFirstChar { c -> c.uppercase() }, fontSize = 13.sp, lineHeight = 18.sp,
                    color = if (requestError != null) RoxyColors.error else RoxyColors.muted, textAlign = TextAlign.Center,
                    modifier = Modifier.fillMaxWidth().padding(bottom = 10.dp),
                )
                val openLibrary = { val id = d.libraryItemId; if (id != null) { if (d.isMovie) onOpenMovie(id, d.title) else onOpenSeries(id, d.title) } }
                when {
                    d.inLibrary && (d.seasons.isEmpty() || missing.isEmpty()) -> PlayButton("Watch", modifier = Modifier.fillMaxWidth(), height = 44.dp, pill = true, container = RoxyColors.accent, content = OnBrand, onClick = openLibrary)
                    d.inLibrary -> Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                        PlayButton("Watch", modifier = Modifier.weight(1f), height = 44.dp, pill = true, container = RoxyColors.accent, content = OnBrand, onClick = openLibrary)
                        GreyButton(if (busy) "…" else "Request ${missing.size} season${if (missing.size == 1) "" else "s"}", modifier = Modifier.height(44.dp), onClick = { requestSeasons(missing.map { it.number }) })
                    }
                    d.request != null && d.request.status != "failed" && d.request.status != "declined" && missing.isEmpty() -> Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                        PlayButton(if (d.request.status == "approval") "Waiting for approval" else "Requested", modifier = Modifier.weight(1f), icon = Icons.Filled.Check, height = 44.dp, pill = true, container = ButtonGrey, content = Color.White, onClick = {})
                        GreyButton("Withdraw", modifier = Modifier.height(44.dp), onClick = {
                            if (busy) return@GreyButton
                            busy = true
                            requestError = null
                            scope.launch { runCatching { app.api.deleteRequest(d.request.id) }.onFailure(::failed); reload(); busy = false }
                        })
                    }
                    !d.requestsEnabled -> PlayButton(
                        if (app.prefs.session.value.isAdmin) "Set up requests" else "Requests aren't set up",
                        modifier = Modifier.fillMaxWidth(), icon = Icons.Outlined.Settings, height = 44.dp, pill = true, container = ButtonGrey, content = Color.White,
                        onClick = { setupPrompt = true },
                    )
                    else -> PlayButton(
                        when {
                            busy -> "Requesting…"
                            d.request?.status == "failed" || d.request?.status == "declined" -> "Request again"
                            missing.isNotEmpty() && missing.size < d.seasons.count { !it.special } -> "Request ${missing.size} more season${if (missing.size == 1) "" else "s"}"
                            else -> "Request"
                        },
                        modifier = Modifier.fillMaxWidth(), icon = Icons.Outlined.AddCircleOutline, height = 44.dp, pill = true, container = RoxyColors.accent, content = OnBrand,
                        onClick = {
                            if (busy) return@PlayButton
                            busy = true
                            requestError = null
                            scope.launch {
                                runCatching { app.api.discoverRequest(d.type, d.tmdbId) }.onFailure(::failed)
                                reload(); busy = false
                            }
                        },
                    )
                }
            }
        }
    }
}

/**
 * One season on the Discover title page: name and size, its state (In library, N of M,
 * Requested) or a Request chip, and a chevron that unfolds the episode list with a tick
 * on each one the library holds, so a half-complete season shows which half.
 */
@Composable
private fun SeasonRow(
    sn: DiscoverSeason, tmdbId: Int, onRequest: () -> Unit, onRequestEpisode: (String) -> Unit = {},
    refreshKey: Any? = null, episodeAction: ((code: String, retry: Boolean) -> Unit)? = null, admin: Boolean = false,
    canSearchAgain: Boolean = false,
) {
    val app = RoxyApp.get(LocalContext.current)
    var open by rememberSaveable(tmdbId, sn.number) { mutableStateOf(false) }
    var episodes by remember(tmdbId, sn.number) { mutableStateOf<List<DiscoverEpisode>?>(null) }
    // Open: load, and reload as the request's download status moves (per-episode progress).
    LaunchedEffect(open, refreshKey) {
        if (open) runCatching { app.api.discoverEpisodes(tmdbId, sn.number) }.onSuccess { episodes = it }.onFailure { if (episodes == null) episodes = emptyList() }
    }
    Column(modifier = Modifier.fillMaxWidth()) {
        Row(modifier = Modifier.fillMaxWidth().clickable { open = !open }.padding(vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
            Icon(if (open) Icons.Filled.ExpandLess else Icons.Filled.ExpandMore, contentDescription = null, tint = RoxyColors.muted, modifier = Modifier.size(22.dp))
            Column(modifier = Modifier.weight(1f).padding(start = 6.dp)) {
                Text(if (sn.special) "Specials" else sn.name.ifEmpty { "Season ${sn.number}" }, fontSize = 15.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground)
                val count = when {
                    sn.episodes <= 0 -> null
                    sn.have in 1 until sn.episodes -> "${sn.have} of ${sn.episodes} episodes"
                    else -> "${sn.episodes} episode${if (sn.episodes == 1) "" else "s"}"
                }
                Text(listOfNotNull(count, sn.airDate?.take(4), if (sn.notOut && !sn.special) "Not out yet" else null).joinToString("  ·  "), fontSize = 12.sp, color = RoxyColors.muted)
            }
            when {
                sn.special && !sn.requested -> Chip(if (sn.have > 0) "Request the rest" else "Request all", selected = false, onClick = onRequest)
                sn.complete -> Row(verticalAlignment = Alignment.CenterVertically) {
                    Icon(Icons.Filled.Check, contentDescription = null, tint = RoxyColors.accent, modifier = Modifier.size(18.dp))
                    Text("In library", fontSize = 13.sp, color = RoxyColors.accent, modifier = Modifier.padding(start = 4.dp))
                }
                // Requested but not all there yet: asking again searches for what is missing.
                sn.requested && episodeAction != null && !sn.special && canSearchAgain -> Chip("Search again", selected = false, onClick = onRequest)
                sn.requested -> Row(verticalAlignment = Alignment.CenterVertically) {
                    Icon(Icons.Outlined.Schedule, contentDescription = null, tint = RoxyColors.muted, modifier = Modifier.size(18.dp))
                    Text("Requested", fontSize = 13.sp, color = RoxyColors.muted, modifier = Modifier.padding(start = 4.dp))
                }
                else -> Chip(if (sn.partial) "Request rest" else "Request", selected = false, onClick = onRequest)
            }
        }
        if (open) {
            val eps = episodes
            when {
                eps == null -> Column(modifier = Modifier.padding(start = 28.dp, bottom = 8.dp)) { repeat(3) { BoneLine(width = 240.dp, height = 12.dp, modifier = Modifier.padding(vertical = 6.dp)) } }
                eps.isEmpty() -> Text("No episode list on TMDB yet.", fontSize = 12.sp, color = RoxyColors.muted, modifier = Modifier.padding(start = 28.dp, bottom = 8.dp))
                else -> eps.forEach { e ->
                    EpisodeRow(
                        e, special = sn.special, onRequest = { onRequestEpisode("S0E${e.number}") }, admin = admin,
                        onAction = episodeAction?.let { act -> { retry: Boolean -> act("S${sn.number}E${e.number}", retry) } },
                    )
                }
            }
        }
    }
}

/**
 * One episode under a season: tick or ring, the full title (wrapping) with the air date
 * under it, and Request / Requested for specials. Tapping the row unfolds TMDB's still
 * and synopsis, so a cut-off title or an unfamiliar special can be identified.
 */
@Composable
private fun EpisodeRow(e: DiscoverEpisode, special: Boolean, onRequest: () -> Unit, admin: Boolean = false, onAction: ((retry: Boolean) -> Unit)? = null) {
    var open by rememberSaveable(e.number) { mutableStateOf(false) }
    val hasMore = !e.overview.isNullOrBlank() || !e.still.isNullOrBlank()
    Column(modifier = Modifier.fillMaxWidth().clickable(enabled = hasMore) { open = !open }.padding(start = 28.dp, top = 6.dp, bottom = 6.dp)) {
        Row(verticalAlignment = Alignment.Top) {
            Icon(
                if (e.have) Icons.Filled.CheckCircle else Icons.Outlined.Circle, contentDescription = null,
                tint = if (e.have) RoxyColors.accent else RoxyColors.muted.copy(alpha = 0.5f), modifier = Modifier.padding(top = 2.dp).size(16.dp),
            )
            Column(modifier = Modifier.weight(1f).padding(start = 8.dp)) {
                Text("${e.number}.  ${e.title.ifEmpty { "Episode ${e.number}" }}", fontSize = 13.sp, lineHeight = 17.sp, color = if (e.have) RoxyColors.onBackground else RoxyColors.muted)
                val dl = e.download?.takeIf { !e.have }
                if (dl != null) {
                    // This episode's own download: state in its colour, a bar while it moves.
                    Text(episodeLine(dl), fontSize = 11.sp, color = stateColor(dl.state), maxLines = 2, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 1.dp))
                    if (dl.state == "downloading" || dl.state == "stalled") DownloadBar(dl.progress, dl.state, modifier = Modifier.padding(top = 4.dp, end = 8.dp).fillMaxWidth().clip(RoundedCornerShape(2.dp)))
                } else {
                    val sub = listOfNotNull(e.airDate?.takeIf { it.isNotBlank() }, if (hasMore && !open) "tap for details" else null).joinToString("  ·  ")
                    if (sub.isNotEmpty()) Text(sub, fontSize = 11.sp, color = RoxyColors.muted.copy(alpha = 0.8f), modifier = Modifier.padding(top = 1.dp))
                }
            }
            val dl = e.download?.takeIf { !e.have }
            when {
                onAction != null && dl != null && dl.canRetry && admin -> SmallAction("Retry") { onAction(true) }
                onAction != null && dl != null && dl.canSearch -> SmallAction("Search") { onAction(false) }
                special && !e.have && e.requested -> Text("Requested", fontSize = 12.sp, color = RoxyColors.muted, modifier = Modifier.padding(start = 8.dp, top = 2.dp))
                special && !e.have -> Text("Request", fontSize = 12.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.accent, modifier = Modifier.padding(start = 8.dp).clip(RoundedCornerShape(12.dp)).clickable(onClick = onRequest).padding(horizontal = 8.dp, vertical = 4.dp))
            }
        }
        if (open) {
            Row(modifier = Modifier.fillMaxWidth().padding(start = 24.dp, top = 8.dp, end = 4.dp), verticalAlignment = Alignment.Top) {
                e.still?.takeIf { it.isNotBlank() }?.let {
                    AsyncImage(model = it, contentDescription = null, contentScale = ContentScale.Crop, modifier = Modifier.width(120.dp).aspectRatio(16f / 9f).clip(RoundedCornerShape(4.dp)).background(RoxyColors.surfaceHigh))
                }
                e.overview?.takeIf { it.isNotBlank() }?.let {
                    Text(it, fontSize = 12.sp, lineHeight = 16.sp, color = RoxyColors.onBackground.copy(alpha = 0.85f), modifier = Modifier.weight(1f).padding(start = if (e.still.isNullOrBlank()) 0.dp else 10.dp))
                }
            }
        }
    }
}

/** A small text button at the end of an episode row. */
@Composable
private fun SmallAction(label: String, onClick: () -> Unit) {
    Text(label, fontSize = 12.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.accent, modifier = Modifier.padding(start = 8.dp).clip(RoundedCornerShape(12.dp)).clickable(onClick = onClick).padding(horizontal = 8.dp, vertical = 4.dp))
}

/** TMDB's status phrases, shortened so the facts line stays on one line. */
private fun statusWord(status: String): String? = when (status.trim().lowercase()) {
    "returning series" -> "Airing"
    "ended" -> "Ended"
    "canceled", "cancelled" -> "Cancelled"
    "in production", "planned", "post production" -> "Upcoming"
    "released" -> null
    "" -> null
    else -> status
}

@Suppress("unused")
private val playIcon = Icons.Filled.PlayArrow

/**
 * Shown instead of sending a request while Sonarr (series) or Radarr (movies) is not
 * connected. Admins get a button to the settings page; everyone else is told who can fix it.
 */
@Composable
internal fun RequestsSetupDialog(type: String, onDismiss: () -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    val admin = app.prefs.session.value.isAdmin
    val arr = if (type == "movie") "Radarr" else "Sonarr"
    androidx.compose.material3.AlertDialog(
        onDismissRequest = onDismiss,
        containerColor = RoxyColors.surface,
        title = { Text("Connect $arr to request", color = RoxyColors.onBackground, fontWeight = FontWeight.Bold) },
        text = {
            Text(
                if (admin) "Requests go straight to $arr, which downloads the ${if (type == "movie") "movie" else "episodes"}. Connect it to Roxy Server first."
                else "Requests go to $arr, which isn't connected to Roxy Server yet. Ask whoever runs the server to set it up under Account → Server.",
                color = RoxyColors.muted, fontSize = 14.sp, lineHeight = 20.sp,
            )
        },
        confirmButton = {
            if (admin) Text(
                "Set up", color = RoxyColors.accent, fontWeight = FontWeight.Bold,
                modifier = Modifier.clip(RoundedCornerShape(8.dp)).clickable { onDismiss(); app.openArrSettings?.invoke() }.padding(horizontal = 12.dp, vertical = 8.dp),
            )
        },
        dismissButton = {
            Text(
                if (admin) "Not now" else "OK", color = RoxyColors.muted, fontWeight = FontWeight.SemiBold,
                modifier = Modifier.clip(RoundedCornerShape(8.dp)).clickable(onClick = onDismiss).padding(horizontal = 12.dp, vertical = 8.dp),
            )
        },
    )
}
