package tv.roxy.tv.ui.phone

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
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
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
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

class DiscoverViewModel(private val api: RoxyApi) : ViewModel() {
    private val _state = MutableStateFlow(DiscoverUiState())
    val state: StateFlow<DiscoverUiState> = _state
    private var searchJob: Job? = null

    fun load() {
        viewModelScope.launch {
            // Both at once and shown together: "Your requests" arriving after the rows would be
            // inserted above the first visible row, i.e. out of sight.
            val reqs = async { runCatching { api.discoverRequests() }.getOrNull() }
            runCatching { api.discover() }
                .onSuccess { d -> _state.value = _state.value.copy(rows = d.rows, requestsEnabled = d.requestsEnabled, requests = reqs.await() ?: _state.value.requests, error = null) }
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
    // Keep the request cards' progress current while something is downloading.
    val anyActive = state.requests.any { it.download?.isActive == true }
    LaunchedEffect(anyActive) {
        while (anyActive) { delay(5_000); vm.refreshRequests() }
    }
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { vm.load() }

    Column(modifier = Modifier.fillMaxSize().statusBarsPadding()) {
        Row(modifier = Modifier.fillMaxWidth().padding(start = PhonePad, end = PhonePad, top = 10.dp, bottom = 6.dp), verticalAlignment = Alignment.CenterVertically) {
            Text("Discover", fontSize = 24.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground)
        }
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
                GreyButton("Try again", onClick = { vm.load() })
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
private fun DiscoverCard(item: DiscoverItem, modifier: Modifier = Modifier, onClick: () -> Unit) {
    Column(modifier = modifier) {
        Box(modifier = Modifier.fillMaxWidth().aspectRatio(2f / 3f).clip(RoundedCornerShape(6.dp)).background(RoxyColors.surfaceHigh).clickable(onClick = onClick)) {
            if (item.poster != null) AsyncImage(model = item.poster, contentDescription = item.title, contentScale = ContentScale.Crop, modifier = Modifier.fillMaxSize())
            else Text(item.title, color = RoxyColors.muted, fontSize = 11.sp, textAlign = TextAlign.Center, modifier = Modifier.align(Alignment.Center).padding(6.dp))
            StatusBadge(item.inLibrary, item.requestStatus, modifier = Modifier.align(Alignment.TopEnd).padding(5.dp), download = item.download)
            val dl = item.download
            if (dl != null && !item.inLibrary && (dl.state == "downloading" || dl.state == "stalled" || dl.state == "importing")) {
                DownloadBar(dl.progress, dl.state, modifier = Modifier.align(Alignment.BottomCenter).fillMaxWidth())
            }
        }
        Text(item.title, fontSize = 12.sp, lineHeight = 15.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground, maxLines = 2, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 6.dp))
        val dl = item.download
        if (dl != null && !item.inLibrary) {
            Text(dl.shortLine(item.isMovie), fontSize = 11.sp, color = stateColor(dl.state), maxLines = 1, overflow = TextOverflow.Ellipsis)
        } else {
            Text(listOfNotNull(item.year?.toString(), if (item.isAnime) "Anime" else if (item.isMovie) "Movie" else null).joinToString("  ·  "), fontSize = 11.sp, color = RoxyColors.muted, maxLines = 1)
        }
    }
}

/** Small round badge: brand check when the library has it, clock while requested. */
@Composable
internal fun StatusBadge(inLibrary: Boolean, requestStatus: String?, modifier: Modifier = Modifier, download: tv.roxy.tv.api.DownloadStatus? = null) {
    val (icon, bg) = when {
        inLibrary -> Icons.Filled.Check to RoxyColors.accent
        requestStatus == "failed" || download?.state == "failed" || download?.state == "attention" -> Icons.Filled.Close to RoxyColors.error
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
fun PhoneDiscoverDetail(type: String, tmdbId: Int, onBack: () -> Unit, onOpenSeries: (String, String) -> Unit, onOpenMovie: (String, String) -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    val scope = rememberCoroutineScope()
    var detail by remember { mutableStateOf<DiscoverDetail?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    var expanded by rememberSaveable { mutableStateOf(false) }
    var setupPrompt by remember { mutableStateOf(false) }

    suspend fun reload() {
        runCatching { app.api.discoverDetail(type, tmdbId) }
            .onSuccess { detail = it; error = null }
            .onFailure { t -> Timber.w(t, "discover detail"); error = t.message }
    }
    LaunchedEffect(type, tmdbId) {
        reload()
        while (true) {
            val dl = detail?.download
            if (dl == null && detail?.request?.status != "sent") break
            delay(if (dl?.isActive == true) 4_000 else 30_000)
            reload()
        }
    }

    fun requestSeasons(seasons: List<Int>) {
        val cur = detail ?: return
        if (!cur.requestsEnabled) { setupPrompt = true; return }
        if (busy) return
        busy = true
        scope.launch {
            runCatching { app.api.discoverRequest(cur.type, cur.tmdbId, seasons) }.onFailure { t -> Timber.w(t, "request failed"); error = t.message }
            reload(); busy = false
        }
    }

    val d = detail
    if (d == null) {
        if (error != null) Column(modifier = Modifier.fillMaxSize().statusBarsPadding().padding(PhonePad)) { BackCircle(onBack); Text(error ?: "", color = RoxyColors.error, modifier = Modifier.padding(top = 16.dp)) }
        else DetailSkeleton(onBack)
        return
    }

    Box(modifier = Modifier.fillMaxSize()) {
        LazyColumn(modifier = Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 120.dp)) {
            item(key = "banner") {
                Box(modifier = Modifier.fillMaxWidth().height(300.dp)) {
                    AsyncImage(model = d.backdrop ?: d.poster, contentDescription = d.title, contentScale = ContentScale.Crop, modifier = Modifier.fillMaxSize().background(RoxyColors.surfaceHigh))
                    Box(modifier = Modifier.fillMaxSize().background(Brush.verticalGradient(0f to Color(0x66000000), 0.3f to Color.Transparent)))
                    Box(modifier = Modifier.fillMaxSize().background(Brush.verticalGradient(0.5f to Color.Transparent, 1f to RoxyColors.background)))
                }
            }
            item(key = "info") {
                Column(modifier = Modifier.fillMaxWidth().padding(horizontal = PhonePad), horizontalAlignment = Alignment.CenterHorizontally) {
                    Text(d.title, fontSize = 24.sp, lineHeight = 28.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, textAlign = TextAlign.Center)
                    d.tagline?.takeIf { it.isNotBlank() }?.let { Text(it, fontSize = 13.sp, color = RoxyColors.muted, textAlign = TextAlign.Center, modifier = Modifier.padding(top = 4.dp)) }
                    val tags = listOf(if (d.isAnime) "Anime" else if (d.isMovie) "Movie" else "Series", d.genres.take(3).joinToString(", ")).filter { it.isNotEmpty() }.joinToString("  •  ")
                    Text(tags, fontSize = 13.sp, color = Color.White.copy(alpha = 0.85f), textAlign = TextAlign.Center, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 10.dp))
                    // One facts line with the rating leading it, so the block tapers from the
                    // title down instead of narrowing and widening again around a lone "8.5".
                    FactsLine(
                        rating = d.rating?.takeIf { it > 0 },
                        facts = listOfNotNull(
                            d.year?.toString(),
                            d.seasonCount.takeIf { it > 0 }?.let { if (it == 1) "1 Season" else "$it Seasons" },
                            d.episodes.takeIf { it > 0 && !d.isMovie }?.let { "$it Episodes" },
                            d.runtimeMinutes.takeIf { it > 0 }?.let { if (d.isMovie) "${it / 60}h ${it % 60}m".removePrefix("0h ") else "${it}m" },
                            d.status?.let { statusWord(it) },
                        ),
                    )
                    d.networks.firstOrNull()?.let { Text(it, fontSize = 11.sp, color = RoxyColors.muted.copy(alpha = 0.8f), textAlign = TextAlign.Center, modifier = Modifier.padding(top = 4.dp)) }
                    d.overview?.takeIf { it.isNotBlank() }?.let {
                        Text(
                            it, fontSize = 14.sp, lineHeight = 20.sp, color = RoxyColors.onBackground.copy(alpha = 0.9f),
                            maxLines = if (expanded) Int.MAX_VALUE else 5, overflow = TextOverflow.Ellipsis,
                            modifier = Modifier.fillMaxWidth().padding(top = 16.dp).clickable { expanded = !expanded },
                        )
                    }
                    if (d.seasons.isNotEmpty()) {
                        Column(modifier = Modifier.fillMaxWidth().padding(top = 22.dp)) {
                            Text("Seasons", fontSize = 17.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground)
                            d.seasons.forEach { sn ->
                                SeasonRow(sn, tmdbId = d.tmdbId, onRequest = { requestSeasons(listOf(sn.number)) }, onRequestEpisode = { code ->
                                    if (!d.requestsEnabled) setupPrompt = true
                                    else if (!busy) { busy = true; scope.launch { runCatching { app.api.discoverRequest(d.type, d.tmdbId, episodes = listOf(code)) }.onFailure { t -> error = t.message }; reload(); busy = false } }
                                })
                            }
                        }
                    }
                    val dl = d.download
                    if (dl != null && d.request?.status == "sent" && !(d.inLibrary && dl.state == "complete")) {
                        DownloadStatusCard(
                            dl, isMovie = d.isMovie, admin = app.prefs.session.value.isAdmin, busy = busy,
                            onSearchAgain = { val id = d.request.id; busy = true; scope.launch { runCatching { app.api.searchAgain(id) }.onFailure { t -> error = t.message }; reload(); busy = false } },
                            onTryAnother = { val id = d.request.id; busy = true; scope.launch { runCatching { app.api.tryAnotherRelease(id) }.onFailure { t -> error = t.message }; reload(); busy = false } },
                            modifier = Modifier.padding(top = 18.dp),
                        )
                    } else d.request?.let { r ->
                        val line = when (r.status) {
                            "sent" -> "Requested by ${r.userName.ifEmpty { "someone" }}  ·  sent to ${if (d.isMovie) "Radarr" else "Sonarr"}"
                            "failed" -> "Request failed  ·  ${r.error ?: "unknown error"}"
                            else -> "Requested by ${r.userName.ifEmpty { "someone" }}  ·  waiting for ${if (d.isMovie) "Radarr" else "Sonarr"}"
                        }
                        Text(line, fontSize = 12.sp, color = if (r.status == "failed") RoxyColors.error else RoxyColors.muted, textAlign = TextAlign.Center, modifier = Modifier.padding(top = 16.dp))
                    }
                    if (!d.requestsEnabled && (!d.inLibrary || d.missingSeasons.isNotEmpty())) {
                        Text(
                            "Requests are off until ${if (d.isMovie) "Radarr" else "Sonarr"} is connected to Roxy Server.",
                            fontSize = 12.sp, lineHeight = 16.sp, color = RoxyColors.muted, textAlign = TextAlign.Center, modifier = Modifier.padding(top = 12.dp),
                        )
                    }
                }
            }
        }
        Box(modifier = Modifier.fillMaxWidth().statusBarsPadding().padding(12.dp)) { BackCircle(onBack) }
        if (setupPrompt) RequestsSetupDialog(if (d.isMovie) "movie" else "tv", onDismiss = { setupPrompt = false })
        Box(
            modifier = Modifier.align(Alignment.BottomCenter).fillMaxWidth()
                .background(Brush.verticalGradient(0f to Color.Transparent, 0.35f to RoxyColors.background.copy(alpha = 0.96f), 1f to RoxyColors.background))
                .navigationBarsPadding().padding(start = PhonePad, end = PhonePad, top = 24.dp, bottom = 12.dp),
        ) {
            val missing = d.missingSeasons
            val openLibrary = { val id = d.libraryItemId; if (id != null) { if (d.isMovie) onOpenMovie(id, d.title) else onOpenSeries(id, d.title) } }
            when {
                d.inLibrary && (d.seasons.isEmpty() || missing.isEmpty()) -> PlayButton("Watch", modifier = Modifier.fillMaxWidth(), height = 44.dp, pill = true, container = RoxyColors.accent, content = OnBrand, onClick = openLibrary)
                d.inLibrary -> Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                    PlayButton("Watch", modifier = Modifier.weight(1f), height = 44.dp, pill = true, container = RoxyColors.accent, content = OnBrand, onClick = openLibrary)
                    GreyButton(if (busy) "…" else "Request ${missing.size} season${if (missing.size == 1) "" else "s"}", modifier = Modifier.height(44.dp), onClick = { requestSeasons(missing.map { it.number }) })
                }
                d.request != null && d.request.status != "failed" && missing.isEmpty() -> Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                    PlayButton("Requested", modifier = Modifier.weight(1f), icon = Icons.Filled.Check, height = 44.dp, pill = true, container = ButtonGrey, content = Color.White, onClick = {})
                    GreyButton("Withdraw", modifier = Modifier.height(44.dp), onClick = {
                        if (busy) return@GreyButton
                        busy = true
                        scope.launch { runCatching { app.api.deleteRequest(d.request.id) }.onFailure { Timber.w(it, "withdraw") }; reload(); busy = false }
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
                        d.request?.status == "failed" -> "Request again"
                        missing.isNotEmpty() && missing.size < d.seasons.size -> "Request ${missing.size} more season${if (missing.size == 1) "" else "s"}"
                        else -> "Request"
                    },
                    modifier = Modifier.fillMaxWidth(), icon = Icons.Outlined.AddCircleOutline, height = 44.dp, pill = true, container = RoxyColors.accent, content = OnBrand,
                    onClick = {
                        if (busy) return@PlayButton
                        busy = true
                        scope.launch {
                            runCatching { app.api.discoverRequest(d.type, d.tmdbId) }.onFailure { t -> Timber.w(t, "request failed"); error = t.message }
                            reload(); busy = false
                        }
                    },
                )
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
private fun SeasonRow(sn: DiscoverSeason, tmdbId: Int, onRequest: () -> Unit, onRequestEpisode: (String) -> Unit = {}) {
    val app = RoxyApp.get(LocalContext.current)
    var open by rememberSaveable(tmdbId, sn.number) { mutableStateOf(false) }
    var episodes by remember(tmdbId, sn.number) { mutableStateOf<List<DiscoverEpisode>?>(null) }
    LaunchedEffect(open) {
        if (open && episodes == null) episodes = runCatching { app.api.discoverEpisodes(tmdbId, sn.number) }.getOrDefault(emptyList())
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
                Text(listOfNotNull(count, sn.airDate?.take(4)).joinToString("  ·  "), fontSize = 12.sp, color = RoxyColors.muted)
            }
            when {
                sn.special -> Text(if (sn.have > 0) "${sn.have} in library" else "Pick episodes", fontSize = 13.sp, color = RoxyColors.muted)
                sn.complete -> Row(verticalAlignment = Alignment.CenterVertically) {
                    Icon(Icons.Filled.Check, contentDescription = null, tint = RoxyColors.accent, modifier = Modifier.size(18.dp))
                    Text("In library", fontSize = 13.sp, color = RoxyColors.accent, modifier = Modifier.padding(start = 4.dp))
                }
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
                else -> eps.forEach { e -> EpisodeRow(e, special = sn.special, onRequest = { onRequestEpisode("S0E${e.number}") }) }
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
private fun EpisodeRow(e: DiscoverEpisode, special: Boolean, onRequest: () -> Unit) {
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
                val sub = listOfNotNull(e.airDate?.takeIf { it.isNotBlank() }, if (hasMore && !open) "tap for details" else null).joinToString("  ·  ")
                if (sub.isNotEmpty()) Text(sub, fontSize = 11.sp, color = RoxyColors.muted.copy(alpha = 0.8f), modifier = Modifier.padding(top = 1.dp))
            }
            when {
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
