package tv.roxy.tv.ui.tv

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.itemsIndexed
import androidx.compose.foundation.lazy.grid.rememberLazyGridState
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Bolt
import androidx.compose.material.icons.filled.Movie
import androidx.compose.material.icons.filled.SortByAlpha
import androidx.compose.material.icons.filled.Tv
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.focusProperties
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.foundation.lazy.grid.LazyGridState
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.Item
import tv.roxy.tv.ui.theme.RoxyColors

/*
 * Browse and Search, as Crunchyroll's TV app lays them out: a category list at the left
 * (Newly Added, A-Z, Series, Movies, then every genre in the library) with the accent focus
 * bar, and five posters across at the right. Both pages read one list of the library's
 * titles, sorted and filtered off the main thread.
 */

private val CategoryWidth = 230.dp
private val CategoryStart = TvDims.RailWidth + 15.dp
private const val BrowseColumns = 5
private const val SearchColumns = 6

/** A way of slicing the library: a label, an icon for the fixed ones, and the rule. */
private class Category(val key: String, val label: String, val icon: ImageVector? = null, val pick: (List<Item>) -> List<Item>)

private fun categories(all: List<Item>): List<Category> {
    val byTitle = compareBy<Item> { it.sortTitle ?: it.title.lowercase() }
    val fixed = listOf(
        Category("new", "Newly Added", Icons.Filled.Bolt) { it.sortedByDescending { x -> x.dateCreated.orEmpty() } },
        Category("az", "A-Z", Icons.Filled.SortByAlpha) { it.sortedWith(byTitle) },
        Category("series", "Series", Icons.Filled.Tv) { it.filter { x -> x.isSeries }.sortedWith(byTitle) },
        Category("movies", "Movies", Icons.Filled.Movie) { it.filter { x -> x.isMovie }.sortedWith(byTitle) },
    )
    val genres = all.flatMap { it.genres }.groupingBy { it }.eachCount().entries.sortedWith(compareByDescending<Map.Entry<String, Int>> { it.value }.thenBy { it.key }).map { it.key }
    return fixed + genres.map { g -> Category("genre:$g", g) { it.filter { x -> g in x.genres }.sortedWith(byTitle) } }
}

/** The library list Browse and Search share, kept across visits for a few minutes. */
private object LibraryCache {
    const val FRESH_MS = 5 * 60_000L
    @Volatile var items: List<Item>? = null
    @Volatile var at = 0L
}

/**
 * The library's series and movies. Shown from [LibraryCache] at once; asked again only when
 * that is a few minutes old. It used to be fetched on every resume, so each return from a
 * title page downloaded the whole library again.
 */
@Composable
private fun rememberLibrary(): Pair<List<Item>?, String?> {
    val app = RoxyApp.get(LocalContext.current)
    // The page's own scope: leaving the page cancels the fetch instead of finishing it into nothing.
    val scope = rememberCoroutineScope()
    var items by remember { mutableStateOf(LibraryCache.items) }
    var error by remember { mutableStateOf<String?>(null) }
    fun fetch() = scope.launch {
        runCatching { app.api.items(type = "series,movie", recursive = true, sort = "title", limit = 1000).items }
            .onSuccess { items = it; error = null; LibraryCache.items = it; LibraryCache.at = System.currentTimeMillis() }
            .onFailure { if (items == null) error = tv.roxy.tv.data.friendlyError(it, "the library") }
    }
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) {
        if (LibraryCache.items != null && System.currentTimeMillis() - LibraryCache.at < LibraryCache.FRESH_MS) {
            items = LibraryCache.items
            return@LifecycleEventEffect
        }
        fetch()
    }
    // Failed while the server was away: fill in once it answers again.
    tv.roxy.tv.ui.components.OnServerBack { if (items == null) fetch() }
    return items to error
}

@Composable
fun TvBrowse(onOpenSeries: (Item) -> Unit, onOpenMovie: (Item) -> Unit) {
    val (all, error) = rememberLibrary()
    val memory = rememberFocusMemory()
    var selected by rememberSaveable { mutableStateOf("new") }
    RegisterPageEntry { memory.restore("cat/$selected") }
    val cats = remember(all) { categories(all.orEmpty()) }
    val gridState = rememberLazyGridState()
    val lastCard = remember { arrayOfNulls<String>(1) }
    // Right from a category: the card last focused in the grid when it is still on screen,
    // else the first card on screen. A fixed "first card" was a dead end once the grid had
    // scrolled (the card was no longer composed).
    fun gridTarget(): androidx.compose.ui.focus.FocusRequester {
        val onScreen = gridState.layoutInfo.visibleItemsInfo.map { "browse/${it.key}" }
        val k = lastCard[0]?.takeIf { it in onScreen } ?: onScreen.firstOrNull() ?: "browse/first"
        return memory.requester(k)
    }
    // Sorting and filtering happen off the main thread; the grid keeps the last result meanwhile.
    var shown by remember { mutableStateOf<List<Item>>(emptyList()) }
    LaunchedEffect(all, selected) {
        val source = all ?: return@LaunchedEffect
        val cat = cats.firstOrNull { it.key == selected } ?: cats.first()
        shown = withContext(Dispatchers.Default) { cat.pick(source) }
    }
    Column(modifier = Modifier.fillMaxSize()) {
        PageTitle("Browse")
        if (all == null && error != null) { PageMessage(error, color = RoxyColors.error); return@Column }
        Row(modifier = Modifier.fillMaxSize()) {
            LazyColumn(modifier = Modifier.width(CategoryStart + CategoryWidth).fillMaxHeight(), contentPadding = PaddingValues(start = CategoryStart, bottom = 24.dp)) {
                items(cats, key = { it.key }) { c ->
                    TvListRow(
                        label = c.label, icon = c.icon, selected = c.key == selected, height = if (c.icon != null) 56.dp else 48.dp, startPadding = if (c.icon != null) 22.dp else 22.dp,
                        onClick = { selected = c.key }, onFocused = { selected = c.key },
                        modifier = Modifier.focusKey(memory, "cat/${c.key}").focusProperties { right = gridTarget() },
                    )
                }
            }
            PosterGrid(shown, memory, key = "browse", resetKey = selected, onOpenSeries = onOpenSeries, onOpenMovie = onOpenMovie, state = gridState, lastCard = lastCard,
                // Left from the grid's first column: the selected category (the two lists do
                // not find each other on their own on this TV; focus was simply dropped).
                leftTarget = { memory.requester("cat/$selected") })
        }
    }
}

/** A text field over the grid; typing filters the library's titles as you go. */
@Composable
fun TvSearch(onOpenSeries: (Item) -> Unit, onOpenMovie: (Item) -> Unit) {
    val (all, error) = rememberLibrary()
    val memory = rememberFocusMemory()
    var query by rememberSaveable { mutableStateOf("") }
    val gridState = rememberLazyGridState()
    val lastCard = remember { arrayOfNulls<String>(1) }
    // Down from the field: the card last focused when it is on screen, else the first card
    // showing. A fixed "first result" was a dead end once the grid had scrolled.
    fun gridTarget(): String? {
        val onScreen = gridState.layoutInfo.visibleItemsInfo.map { "search/${it.key}" }
        return lastCard[0]?.takeIf { it in onScreen } ?: onScreen.firstOrNull()
    }
    // Back from the rail: the card that had focus when it is on screen, else the field.
    // Back from the rail: the card last focused when it is on screen, else the field. (Not
    // "whatever had focus": closing the rail drops focus on the field first, so that would
    // always be the field.)
    RegisterPageEntry {
        val k = lastCard[0]?.takeIf { it == gridTarget() }
        if (k != null) runCatching { memory.requester(k).requestFocus() }.getOrDefault(false) else memory.restore("search/field")
    }
    // Lower-cased once per library load, not on every keystroke.
    val index = remember(all) {
        all.orEmpty().sortedByDescending { it.dateCreated.orEmpty() }.map { it to (it.title + "\n" + it.genres.joinToString("\n")).lowercase() }
    }
    val shown by remember(index) {
        derivedStateOf {
            val q = query.trim().lowercase()
            if (q.isEmpty()) index.map { it.first } else index.filter { it.second.contains(q) }.map { it.first }
        }
    }
    Column(modifier = Modifier.fillMaxSize().padding(start = TvDims.RailWidth)) {
        Spacer(Modifier.height(40.dp))
        TvTextField(query, { query = it }, placeholder = "Search series, movies, genres", imeAction = ImeAction.Search, modifier = Modifier.padding(start = 15.dp, end = 85.dp).width(520.dp).focusKey(memory, "search/field").focusProperties { gridTarget()?.let { down = memory.requester(it) } })
        Spacer(Modifier.height(24.dp))
        if (all == null && error != null) { PageMessage(error, color = RoxyColors.error); return@Column }
        if (all != null && shown.isEmpty()) { PageMessage("No matches for \"${query.trim()}\"."); return@Column }
        // Six posters across at full card size: the Browse grid's five narrow posters sit beside
        // its category list; here the cells were wider than the posters and the focus frame
        // stood out past them.
        PosterGrid(
            shown, memory, key = "search", onOpenSeries = onOpenSeries, onOpenMovie = onOpenMovie, startPadding = 15.dp - TvDims.FocusBorder,
            state = gridState, lastCard = lastCard, columns = SearchColumns, cardWidth = TvDims.PosterCardWidth, captionHeight = TvDims.PosterCaption,
            upTarget = { memory.requester("search/field") },
        )
    }
}

/** Five posters across; the grid remembers which card had focus. */
@Composable
private fun PosterGrid(
    items: List<Item>, memory: FocusMemory, key: String, onOpenSeries: (Item) -> Unit, onOpenMovie: (Item) -> Unit,
    startPadding: androidx.compose.ui.unit.Dp = 20.dp, resetKey: Any? = null,
    state: LazyGridState = rememberLazyGridState(), lastCard: Array<String?>? = null,
    leftTarget: (() -> androidx.compose.ui.focus.FocusRequester)? = null,
    columns: Int = BrowseColumns, cardWidth: androidx.compose.ui.unit.Dp = TvDims.BrowseCardWidth, captionHeight: androidx.compose.ui.unit.Dp = TvDims.BrowseCaption,
    upTarget: (() -> androidx.compose.ui.focus.FocusRequester)? = null,
    onCardFocused: () -> Unit = {},
) {
    val app = RoxyApp.get(LocalContext.current)
    // A new category starts at the top (a lazy grid otherwise keeps the old first card in
    // view by key, which can be anywhere in the new order).
    LaunchedEffect(resetKey) { if (resetKey != null) state.scrollToItem(0) }
    LazyVerticalGrid(
        columns = GridCells.Fixed(columns),
        state = state,
        contentPadding = PaddingValues(start = startPadding, end = 40.dp, bottom = 32.dp),
        verticalArrangement = Arrangement.spacedBy(2.dp),
        modifier = Modifier.fillMaxSize(),
    ) {
        itemsIndexed(items, key = { _, it -> it.id }) { i, item ->
            PosterCard(
                item, app.api, width = cardWidth, captionHeight = captionHeight,
                onClick = { if (item.isMovie) onOpenMovie(item) else onOpenSeries(item) },
                modifier = Modifier.focusKey(memory, "$key/${item.id}").then(if (i == 0) Modifier.focusRequester(memory.requester("$key/first")) else Modifier)
                    .then(if (lastCard != null) Modifier.onFocusChanged { if (it.isFocused) { lastCard[0] = "$key/${item.id}"; onCardFocused() } } else Modifier)
                    .then(if (leftTarget != null && i % columns == 0) Modifier.focusProperties { left = leftTarget() } else Modifier)
                    .then(if (upTarget != null && i < columns) Modifier.focusProperties { up = upTarget() } else Modifier),
            )
        }
    }
}
