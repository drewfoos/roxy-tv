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
import androidx.compose.foundation.lazy.grid.items
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

/** The library's series and movies, loaded once per page and kept while the page lives. */
@Composable
private fun rememberLibrary(): Pair<List<Item>?, String?> {
    val app = RoxyApp.get(LocalContext.current)
    var items by remember { mutableStateOf<List<Item>?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) {
        app.scope.launch {
            runCatching { app.api.items(type = "series,movie", recursive = true, sort = "title", limit = 1000).items }
                .onSuccess { items = it; error = null }
                .onFailure { if (items == null) error = it.message ?: "Could not load the library" }
        }
    }
    return items to error
}

@Composable
fun TvBrowse(onOpenSeries: (Item) -> Unit, onOpenMovie: (Item) -> Unit) {
    val (all, error) = rememberLibrary()
    val memory = rememberFocusMemory()
    var selected by rememberSaveable { mutableStateOf("new") }
    val cats = remember(all) { categories(all.orEmpty()) }
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
                        modifier = Modifier.focusKey(memory, "cat/${c.key}"),
                    )
                }
            }
            PosterGrid(shown, memory, key = "browse", onOpenSeries = onOpenSeries, onOpenMovie = onOpenMovie)
        }
    }
}

/** A text field over the grid; typing filters the library's titles as you go. */
@Composable
fun TvSearch(onOpenSeries: (Item) -> Unit, onOpenMovie: (Item) -> Unit) {
    val (all, error) = rememberLibrary()
    val memory = rememberFocusMemory()
    var query by rememberSaveable { mutableStateOf("") }
    val shown by remember(all) {
        derivedStateOf {
            val q = query.trim().lowercase()
            val source = all.orEmpty()
            if (q.isEmpty()) source.sortedByDescending { it.dateCreated.orEmpty() }
            else source.filter { it.title.lowercase().contains(q) || it.genres.any { g -> g.lowercase().contains(q) } }
        }
    }
    Column(modifier = Modifier.fillMaxSize().padding(start = TvDims.RailWidth)) {
        Spacer(Modifier.height(40.dp))
        TvTextField(query, { query = it }, placeholder = "Search series, movies, genres", imeAction = ImeAction.Search, modifier = Modifier.padding(start = 15.dp, end = 85.dp).width(520.dp))
        Spacer(Modifier.height(24.dp))
        if (all == null && error != null) { PageMessage(error, color = RoxyColors.error); return@Column }
        if (all != null && shown.isEmpty()) { PageMessage("No matches for \"${query.trim()}\"."); return@Column }
        PosterGrid(shown, memory, key = "search", onOpenSeries = onOpenSeries, onOpenMovie = onOpenMovie, startPadding = 15.dp - TvDims.FocusBorder)
    }
}

/** Five posters across; the grid remembers which card had focus. */
@Composable
private fun PosterGrid(items: List<Item>, memory: FocusMemory, key: String, onOpenSeries: (Item) -> Unit, onOpenMovie: (Item) -> Unit, startPadding: androidx.compose.ui.unit.Dp = 20.dp) {
    val app = RoxyApp.get(LocalContext.current)
    val state = rememberLazyGridState()
    LazyVerticalGrid(
        columns = GridCells.Fixed(BrowseColumns),
        state = state,
        contentPadding = PaddingValues(start = startPadding, end = 40.dp, bottom = 32.dp),
        verticalArrangement = Arrangement.spacedBy(2.dp),
        modifier = Modifier.fillMaxSize(),
    ) {
        items(items, key = { it.id }) { item ->
            PosterCard(
                item, app.api, width = TvDims.BrowseCardWidth, captionHeight = TvDims.BrowseCaption,
                onClick = { if (item.isMovie) onOpenMovie(item) else onOpenSeries(item) },
                modifier = Modifier.focusKey(memory, "$key/${item.id}"),
            )
        }
    }
}
