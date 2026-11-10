package tv.roxy.tv.ui.phone

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.grid.LazyGridState
import androidx.compose.foundation.lazy.grid.rememberLazyGridState
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.ui.text.style.TextOverflow
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.mapLatest
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.withContext
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Search
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
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelStoreOwner
import androidx.lifecycle.compose.LifecycleEventEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.Item
import tv.roxy.tv.api.RoxyApi
import tv.roxy.tv.ui.matchesSearch
import tv.roxy.tv.ui.playItem
import tv.roxy.tv.ui.theme.RoxyColors

data class LibraryUiState(val items: List<Item>? = null, val error: String? = null, val refreshing: Boolean = false)

/** A way of slicing the library for Browse: a chip label and the rule (sort / filter). */
class BrowseCategory(val key: String, val label: String, val pick: (List<Item>) -> List<Item>)

/** Browse's chips and the grid under the selected one; [items] is null until the library arrives. */
data class BrowseUiState(val categories: List<BrowseCategory> = emptyList(), val selected: String = "new", val items: List<Item>? = null)

/**
 * Browse's categories, as the TV's Browse has them: Newly Added, A-Z, Series, Movies, then
 * every genre in the library, the most common first.
 */
private fun browseCategories(all: List<Item>): List<BrowseCategory> {
    val byTitle = compareBy<Item> { it.sortTitle ?: it.title.lowercase() }
    val fixed = listOf(
        BrowseCategory("new", "Newly Added") { it.sortedByDescending { x -> x.dateCreated.orEmpty() } },
        BrowseCategory("az", "A-Z") { it.sortedWith(byTitle) },
        BrowseCategory("series", "Series") { it.filter { x -> x.isSeries }.sortedWith(byTitle) },
        BrowseCategory("movies", "Movies") { it.filter { x -> x.isMovie }.sortedWith(byTitle) },
    )
    val genres = all.flatMap { it.genres }.groupingBy { it }.eachCount().entries
        .sortedWith(compareByDescending<Map.Entry<String, Int>> { it.value }.thenBy { it.key }).map { it.key }
    return fixed + genres.map { g -> BrowseCategory("genre:$g", g) { it.filter { x -> g in x.genres }.sortedWith(byTitle) } }
}

/**
 * The whole library's series and movies in one list (sorted by title), shared by Browse
 * and Search so they cost one request between them. Activity-scoped, so it outlives a trip
 * to a title page: reloaded on resume only when it is more than [FRESH_MS] old or the
 * library version changed, not on every return.
 */
class LibraryViewModel(private val api: RoxyApi, private val favoritesOnly: Boolean = false) : ViewModel() {
    private val _state = MutableStateFlow(LibraryUiState())
    val state: StateFlow<LibraryUiState> = _state
    private var loadedAt = 0L
    private var seenVersion = -1L

    private val category = MutableStateFlow("new")

    /**
     * Browse's chips and grid, sorted and filtered off the main thread. Kept in the view
     * model so coming back from a title page shows the same category and list at once.
     */
    @OptIn(ExperimentalCoroutinesApi::class)
    val browse: StateFlow<BrowseUiState> = combine(_state.map { it.items }.distinctUntilChanged(), category) { items, key -> items to key }
        .mapLatest { (items, key) ->
            if (items == null) BrowseUiState(selected = key)
            else withContext(Dispatchers.Default) {
                val cats = browseCategories(items)
                val cat = cats.firstOrNull { it.key == key } ?: cats.first()
                BrowseUiState(categories = cats, selected = cat.key, items = cat.pick(items))
            }
        }
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), BrowseUiState())

    fun selectCategory(key: String) { category.value = key }

    /** [version] is the library version the caller sees (ItemCache.version); a change forces a refetch. */
    fun load(force: Boolean = false, version: Long = seenVersion) {
        val now = System.currentTimeMillis()
        val stale = version != seenVersion
        if (!force && !stale && _state.value.items != null && now - loadedAt < FRESH_MS) return
        seenVersion = version
        if (force && _state.value.items != null) _state.value = _state.value.copy(refreshing = true)
        viewModelScope.launch {
            runCatching {
                if (favoritesOnly) api.favorites().items
                else api.allItems(type = "series,movie", recursive = true, sort = "title").items
            }
                .onSuccess { loadedAt = System.currentTimeMillis(); _state.value = LibraryUiState(items = it) }
                .onFailure { t -> Timber.e(t, "library load failed"); _state.value = _state.value.copy(error = tv.roxy.tv.data.friendlyError(t, "the library"), refreshing = false) }
        }
    }

    private companion object { const val FRESH_MS = 5 * 60_000L }
}

/**
 * Browse tab, after Crunchyroll's phone app: a scrolling row of category chips (Newly
 * Added, A-Z, Series, Movies, then the library's genres) over a three-column poster grid
 * with each title and "year · Series/Movie" under its poster. Pull down to refresh.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun PhoneBrowse(onOpenSeries: (Item) -> Unit, onOpenMovie: (Item) -> Unit, onSearch: () -> Unit, bottomInset: Dp) {
    val vm = libraryViewModel()
    val state by vm.state.collectAsStateWithLifecycle()
    val browse by vm.browse.collectAsStateWithLifecycle()
    val items = RoxyApp.get(LocalContext.current).items
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { vm.load(version = items.version) }
    OnUserDataChanged { vm.load(version = items.version) }
    val scope = rememberCoroutineScope()
    val gridState = rememberLazyGridState()
    val chipState = rememberLazyListState()

    Column(modifier = Modifier.fillMaxSize().statusBarsPadding()) {
        TabHeader("Browse", onSearch = onSearch)
        if (state.items == null && state.error != null) {
            tv.roxy.tv.ui.components.LoadError(state.error ?: "", onRetry = { vm.load(force = true) })
            return@Column
        }
        if (browse.categories.isEmpty()) {
            ChipRowSkeleton()
        } else {
            LazyRow(
                state = chipState,
                modifier = Modifier.fillMaxWidth(),
                contentPadding = PaddingValues(horizontal = PhonePad, vertical = 6.dp),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                items(browse.categories, key = { it.key }) { c ->
                    Chip(c.label, selected = c.key == browse.selected, onClick = {
                        if (c.key != browse.selected) {
                            vm.selectCategory(c.key)
                            scope.launch { gridState.scrollToItem(0) }
                        }
                    })
                }
            }
        }
        PullToRefreshBox(
            isRefreshing = state.refreshing,
            onRefresh = { vm.load(force = true, version = items.version) },
            modifier = Modifier.fillMaxSize(),
        ) {
            PosterGrid(
                items = browse.items, error = null, emptyText = "Nothing here yet.", bottomInset = bottomInset, state = gridState,
                onOpen = { if (it.isMovie) onOpenMovie(it) else onOpenSeries(it) },
            )
        }
    }
}

/** Placeholder for the category chips while the library loads. */
@Composable
private fun ChipRowSkeleton() {
    Row(modifier = Modifier.fillMaxWidth().padding(horizontal = PhonePad, vertical = 6.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        listOf(104.dp, 56.dp, 70.dp, 74.dp, 84.dp).forEach { Bone(Modifier.width(it).height(34.dp), shape = RoundedCornerShape(17.dp)) }
    }
}

/**
 * My Lists tab (Crunchyroll's): Watchlist (the profile's saved titles), History (what it
 * played, newest first, as a two-column grid of episodes that play on tap) and Downloads.
 */
@Composable
fun PhoneWatchlist(
    onOpenSeries: (Item) -> Unit, onOpenMovie: (Item) -> Unit, bottomInset: Dp, onSearch: (() -> Unit)? = null,
    onOpenDownloadedTitle: (String) -> Unit = {}, onBrowse: () -> Unit = {},
) {
    var tab by rememberSaveable { mutableStateOf(0) }
    Column(modifier = Modifier.fillMaxSize().statusBarsPadding()) {
        TabHeader("My Lists", onSearch = onSearch)
        SectionTabs(listOf("Watchlist", "History", "Downloads"), tab) { tab = it }
        when (tab) {
            0 -> WatchlistGrid(onOpenSeries, onOpenMovie, bottomInset)
            1 -> HistoryGrid(bottomInset)
            else -> PhoneDownloads(onBack = null, onOpenTitle = onOpenDownloadedTitle, onBrowse = onBrowse, bottomInset = bottomInset)
        }
    }
}

@Composable
private fun WatchlistGrid(onOpenSeries: (Item) -> Unit, onOpenMovie: (Item) -> Unit, bottomInset: Dp) {
    val app = RoxyApp.get(LocalContext.current)
    val vm: LibraryViewModel = viewModel(key = "watchlist") { LibraryViewModel(app.api, favoritesOnly = true) }
    val state by vm.state.collectAsStateWithLifecycle()
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { vm.load(force = true) } // a + on a title page changes it
    OnUserDataChanged { vm.load(force = true) }
    Spacer(Modifier.height(6.dp))
    PosterGrid(
        items = state.items, error = state.error,
        emptyText = "Titles you add with \"My List\" on their page show up here.",
        bottomInset = bottomInset, onOpen = { if (it.isMovie) onOpenMovie(it) else onOpenSeries(it) },
    )
}

/** Everything this profile played, newest first, two episodes across; a tap plays (or resumes) it. */
@Composable
private fun HistoryGrid(bottomInset: Dp) {
    val context = LocalContext.current
    val app = RoxyApp.get(context)
    val scope = rememberCoroutineScope()
    var items by remember { mutableStateOf<List<Item>?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    fun reload() = scope.launch {
        runCatching { app.api.history(200).items }
            .onSuccess { items = it; error = null }
            .onFailure { Timber.w(it, "history"); error = tv.roxy.tv.data.friendlyError(it, "your history") }
    }
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { reload() }
    OnUserDataChanged { reload() }
    val list = items
    val tileWidth = (LocalConfiguration.current.screenWidthDp.dp - PhonePad * 2 - 10.dp) / 2
    when {
        list == null && error != null -> tv.roxy.tv.ui.components.LoadError(error ?: "", onRetry = { reload() })
        list == null -> Column(modifier = Modifier.padding(start = PhonePad, end = PhonePad, top = 14.dp), verticalArrangement = Arrangement.spacedBy(16.dp)) {
            repeat(3) { Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) { repeat(2) { Bone(Modifier.weight(1f).aspectRatio(16f / 9f)) } } }
        }
        list.isEmpty() -> Box(Modifier.fillMaxWidth().padding(32.dp), contentAlignment = Alignment.Center) {
            Text("Episodes and movies you watch show up here, newest first.", color = RoxyColors.muted, textAlign = TextAlign.Center, lineHeight = 20.sp)
        }
        else -> LazyVerticalGrid(
            columns = GridCells.Fixed(2),
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(start = PhonePad, end = PhonePad, top = 14.dp, bottom = bottomInset + 16.dp),
            horizontalArrangement = Arrangement.spacedBy(10.dp),
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            items(list, key = { it.id }) { item ->
                ThumbTile(item, app.api, width = tileWidth, onClick = { scope.launch { playItem(context, item).onFailure { Timber.w(it, "play failed") } } })
            }
        }
    }
}

/** Search: type-to-filter over the library's titles. */
@Composable
fun PhoneSearch(onBack: () -> Unit, onOpenSeries: (Item) -> Unit, onOpenMovie: (Item) -> Unit) {
    val vm = libraryViewModel()
    val state by vm.state.collectAsStateWithLifecycle()
    val items = RoxyApp.get(LocalContext.current).items
    LaunchedEffect(Unit) { vm.load(version = items.version) }
    var query by rememberSaveable { mutableStateOf("") }
    val focus = remember { FocusRequester() }
    LaunchedEffect(Unit) { focus.requestFocus() }

    val q = query.trim()
    val results = state.items?.let { all ->
        if (q.isEmpty()) emptyList() else all.filter { matchesSearch(q, it.title, it.genres) }
    }
    Column(modifier = Modifier.fillMaxSize().statusBarsPadding().imePadding()) {
        Row(modifier = Modifier.fillMaxWidth().padding(horizontal = PhonePad, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
            BackCircle(onBack)
            Box(
                modifier = Modifier.weight(1f).padding(start = 12.dp).clip(RoundedCornerShape(8.dp)).background(ChipGrey).padding(horizontal = 12.dp, vertical = 10.dp),
                contentAlignment = Alignment.CenterStart,
            ) {
                if (query.isEmpty()) Text("Search series, movies, genres", color = RoxyColors.muted, fontSize = 15.sp)
                BasicTextField(
                    value = query, onValueChange = { query = it }, singleLine = true,
                    textStyle = TextStyle(color = Color.White, fontSize = 15.sp), cursorBrush = SolidColor(Color.White),
                    keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
                    modifier = Modifier.fillMaxWidth().focusRequester(focus),
                )
            }
        }
        when {
            state.items == null && state.error == null -> GridSkeleton(modifier = Modifier.padding(top = 8.dp), rows = 2)
            q.isEmpty() -> Text("Start typing to search your library.", color = RoxyColors.muted, modifier = Modifier.padding(PhonePad))
            else -> PosterGrid(items = results, error = state.error, emptyText = "No matches for \"$q\".", bottomInset = 16.dp, onOpen = { if (it.isMovie) onOpenMovie(it) else onOpenSeries(it) })
        }
    }
}

/** Three-column poster grid (title and "year · Series/Movie" under each) with skeleton, error and empty states. */
@Composable
private fun PosterGrid(
    items: List<Item>?, error: String?, emptyText: String, bottomInset: Dp,
    state: LazyGridState = rememberLazyGridState(), onOpen: (Item) -> Unit,
) {
    val app = RoxyApp.get(LocalContext.current)
    when {
        items == null && error != null -> Text(error, color = RoxyColors.error, modifier = Modifier.padding(PhonePad))
        items == null -> CaptionedGridSkeleton(modifier = Modifier.padding(top = 8.dp))
        // Scrollable even when empty, so pull-to-refresh still works on an empty category.
        items.isEmpty() -> Box(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(32.dp), contentAlignment = Alignment.TopCenter) {
            Text(emptyText, color = RoxyColors.muted, textAlign = TextAlign.Center, lineHeight = 20.sp)
        }
        else -> LazyVerticalGrid(
            columns = GridCells.Fixed(3),
            state = state,
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(start = PhonePad, end = PhonePad, top = 8.dp, bottom = bottomInset + 16.dp),
            horizontalArrangement = Arrangement.spacedBy(8.dp),
            verticalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            items(items, key = { it.id }) { CaptionedPoster(it, app.api, onClick = { onOpen(it) }) }
        }
    }
}

/** A grid poster with its title (two lines at most) and "2019 · Series" under it; the whole tile is the tap target. */
@Composable
private fun CaptionedPoster(item: Item, api: RoxyApi, onClick: () -> Unit) {
    Column(modifier = Modifier.fillMaxWidth().clip(RoundedCornerShape(6.dp)).clickable(onClick = onClick)) {
        PosterTile(item, api, modifier = Modifier.fillMaxWidth(), onClick = onClick)
        Text(
            item.title, fontSize = 13.sp, lineHeight = 16.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground,
            maxLines = 2, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 6.dp),
        )
        Text(
            listOfNotNull(item.year?.toString(), if (item.isMovie) "Movie" else "Series").joinToString(" · "),
            fontSize = 11.sp, lineHeight = 14.sp, color = RoxyColors.muted, maxLines = 1, overflow = TextOverflow.Ellipsis,
            modifier = Modifier.padding(top = 2.dp),
        )
    }
}

/** Skeleton for [PosterGrid]: three posters across, each with a title line and a shorter detail line. */
@Composable
private fun CaptionedGridSkeleton(modifier: Modifier = Modifier, rows: Int = 3) {
    Column(modifier = modifier.fillMaxWidth().padding(horizontal = PhonePad), verticalArrangement = Arrangement.spacedBy(14.dp)) {
        repeat(rows) {
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                repeat(3) {
                    Column(modifier = Modifier.weight(1f)) {
                        Bone(Modifier.fillMaxWidth().aspectRatio(2f / 3f))
                        Spacer(Modifier.height(8.dp))
                        Bone(Modifier.fillMaxWidth(0.85f).height(12.dp), shape = RoundedCornerShape(4.dp))
                        Spacer(Modifier.height(6.dp))
                        Bone(Modifier.fillMaxWidth(0.5f).height(10.dp), shape = RoundedCornerShape(4.dp))
                    }
                }
            }
        }
    }
}

/** The library list is one per app (activity-scoped), so Browse and Search share a single request. */
@Composable
private fun libraryViewModel(): LibraryViewModel {
    val context = LocalContext.current
    val app = RoxyApp.get(context)
    val owner = context as ViewModelStoreOwner
    val profile = app.prefs.session.value.profileId ?: "-"
    return viewModel(viewModelStoreOwner = owner, key = "library:$profile") { LibraryViewModel(app.api) }
}
