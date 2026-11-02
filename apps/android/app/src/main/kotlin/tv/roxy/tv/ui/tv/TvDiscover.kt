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
    val vm: DiscoverViewModel = viewModel { DiscoverViewModel(app.api) }
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
    fun rowTarget(i: Int): String? = rows.getOrNull(i)?.let { r ->
        lastInRow[r.key]?.takeIf { k -> r.items.any { cardKey(r.key, it) == k } } ?: r.items.firstOrNull()?.let { cardKey(r.key, it) }
    }
    val searching = state.results != null || state.query.isNotBlank()
    RegisterPageEntry { memory.restore(if (searching) "discover/search" else rowTarget(0) ?: "discover/search") }

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
            state.query, vm::search, placeholder = "Search for a series or movie to request", icon = Icons.Outlined.Search, imeAction = ImeAction.Search,
            modifier = Modifier.padding(start = 15.dp).width(560.dp).focusKey(memory, "discover/search")
                .focusProperties { down = memory.requester(if (searching) "discover/result/first" else rowTarget(0) ?: "discover/search") },
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
                val listState = rememberLazyListState()
                val pin = with(LocalDensity.current) { DiscoverPin.roundToPx() }
                // The focused row goes to the top with the row above peeking in (so Up has a
                // card to land on); the rows themselves scroll their cards as usual.
                val focusedRow = remember { mutableStateOf(-1) }
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
