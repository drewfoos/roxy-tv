package tv.roxy.tv.ui.tv

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.itemsIndexed
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.width
import androidx.compose.material3.Text
import androidx.compose.ui.Alignment
import androidx.compose.ui.unit.sp
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.focus.focusProperties
import androidx.compose.foundation.lazy.grid.rememberLazyGridState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.Item
import tv.roxy.tv.ui.phone.OnUserDataChanged
import tv.roxy.tv.ui.phone.audioBadge
import tv.roxy.tv.ui.playItem
import tv.roxy.tv.ui.theme.RoxyColors
import java.text.DateFormat
import java.util.Date

/*
 * Watchlist and History, as Crunchyroll's TV app shows them: a centred uppercase title,
 * then five 16:9 cards across, each with a caption box. The list is one request; a card's
 * focus is remembered so Back from a title page lands on it again.
 */

private const val GridColumns = 4

/** The lists' column: four cards and their gaps, plus room for the focus frame at both edges. */
private val ListsWidth = TvDims.GridCardWidth * GridColumns + TvDims.GridGap * (GridColumns - 1) + TvDims.FocusBorder * 2

/**
 * My Lists (Crunchyroll's): the Watchlist and History as two tabs. History left the rail to
 * make room for Discover. Tabs switch as focus lands on them; Down goes to the list.
 */
@Composable
fun TvMyLists(onOpenSeries: (Item) -> Unit, onOpenMovie: (Item) -> Unit) {
    val memory = rememberFocusMemory()
    var tab by rememberSaveable { mutableStateOf(0) }
    // The first card of each tab once it has loaded: where Down from a tab and the rail go.
    val firstCard = remember { mutableStateOf(arrayOfNulls<String>(2)) }
    fun tabKey(i: Int) = "lists/tab/$i"
    RegisterPageEntry { memory.restore(firstCard.value[tab] ?: tabKey(tab)) }
    Column(modifier = Modifier.fillMaxSize()) {
        PageTitle("My Lists")
        // Tabs and cards share one column centred under the title, so their left edges line up.
        Column(modifier = Modifier.fillMaxSize().padding(start = TvDims.RailWidth), horizontalAlignment = Alignment.CenterHorizontally) {
        Column(modifier = Modifier.width(ListsWidth).fillMaxHeight()) {
        Row(modifier = Modifier.padding(start = TvDims.FocusBorder, bottom = 14.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            listOf("Watchlist", "History").forEachIndexed { i, label ->
                TvTab(
                    // Not while the rail is closing: focus drops on the first tab then, and
                    // switching to it threw away the list (and the card) being returned to.
                    label, selected = i == tab, onClick = { tab = i }, onFocused = { if (!FocusMemory.muted()) tab = i },
                    modifier = Modifier.focusKey(memory, tabKey(i)).focusProperties { firstCard.value[i]?.let { down = memory.requester(it) } },
                )
            }
        }
        val onFirst = { i: Int, key: String? -> if (firstCard.value[i] != key) firstCard.value = firstCard.value.copyOf().also { it[i] = key } }
        when (tab) {
            0 -> WatchlistTab(memory, upKey = tabKey(0), onFirst = { onFirst(0, it) }, onOpenSeries = onOpenSeries, onOpenMovie = onOpenMovie)
            else -> HistoryTab(memory, upKey = tabKey(1), onFirst = { onFirst(1, it) })
        }
        }
        }
    }
}

/** An empty or failed list: one line under the tabs. */
@Composable
private fun ListMessage(text: String, color: androidx.compose.ui.graphics.Color = RoxyColors.muted) {
    Text(text, color = color, fontSize = 16.sp, modifier = Modifier.padding(start = TvDims.FocusBorder, top = 10.dp))
}

/** The saved titles (the profile's favourites), newest saved first. */
@Composable
private fun WatchlistTab(memory: FocusMemory, upKey: String, onFirst: (String?) -> Unit, onOpenSeries: (Item) -> Unit, onOpenMovie: (Item) -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    val scope = rememberCoroutineScope()
    var items by remember { mutableStateOf<List<Item>?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(items) { onFirst(items?.firstOrNull()?.let { "watchlist/${it.id}" }) }
    suspend fun load() {
        runCatching { app.api.favorites().items }
            .onSuccess { items = it; error = null }
            .onFailure { if (items == null) error = it.message ?: "Could not load the watchlist" }
    }
    // The page's scope, not the app's: leaving the page cancels the request.
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { scope.launch { load() } }
    OnUserDataChanged { scope.launch { load() } }

    val list = items
    when {
        list == null && error != null -> ListMessage(error!!, color = RoxyColors.error)
        list == null -> Unit
        list.isEmpty() -> ListMessage("Titles you add to your watchlist show up here.")
        else -> CardGrid(list, memory, key = "watchlist", upKey = upKey, secondLine = { audioBadge(it) ?: it.year?.toString() }) { item ->
            if (item.isMovie) onOpenMovie(item) else onOpenSeries(item)
        }
    }
}

/** What the profile played, newest first: episodes and films, OK plays them. */
@Composable
private fun HistoryTab(memory: FocusMemory, upKey: String, onFirst: (String?) -> Unit) {
    val context = LocalContext.current
    val app = RoxyApp.get(context)
    val scope = rememberCoroutineScope()
    var items by remember { mutableStateOf<List<Item>?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(items) { onFirst(items?.firstOrNull()?.let { "history/${it.id}" }) }
    suspend fun load() {
        runCatching { app.api.history(100).items }
            .onSuccess { items = it; error = null }
            .onFailure { if (items == null) error = it.message ?: "Could not load the history" }
    }
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { scope.launch { load() } }
    OnUserDataChanged { scope.launch { load() } }
    val dateFormat = remember { DateFormat.getDateInstance(DateFormat.SHORT) }

    val list = items
    when {
        list == null && error != null -> ListMessage(error!!, color = RoxyColors.error)
        list == null -> Unit
        list.isEmpty() -> ListMessage("Episodes and movies you watch show up here, newest first.")
        else -> CardGrid(list, memory, key = "history", upKey = upKey, secondLine = { historyLine(it, dateFormat) }) { item ->
            scope.launch { playItem(context, item).onFailure { Timber.w(it, "play failed") } }
        }
    }
}

/** "S1 E2  ·  10/27/2026": the season and episode, and the day it was last played. */
private fun historyLine(item: Item, dateFormat: DateFormat): String {
    val code = if (!item.isEpisode) "" else {
        val ep = item.indexAired ?: item.indexAbsolute
        val season = item.seasonAired
        when {
            ep == null -> ""
            season == 0 -> "Special $ep"
            season != null -> "S$season E$ep"
            else -> "E$ep"
        }
    }
    val day = item.userData?.lastPlayedAt?.let { runCatching { dateFormat.format(Date.from(java.time.Instant.parse(it))) }.getOrNull() }
    return listOfNotNull(code.ifEmpty { null }, day).joinToString("  \u00b7  ")
}

/**
 * Five cards across with the accent frame's spacing. The remembered card takes focus again
 * when the list comes back (from a title page); the first row's Up goes to [upKey] (the tabs).
 */
@Composable
private fun CardGrid(items: List<Item>, memory: FocusMemory, key: String, upKey: String, secondLine: (Item) -> String?, onClick: (Item) -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    val gridState = rememberLazyGridState()
    var restored by remember { mutableStateOf(false) }
    LaunchedEffect(items.isNotEmpty()) {
        if (restored || items.isEmpty()) return@LaunchedEffect
        // Only a card of this list: a tab switch must leave focus on the tab.
        val k = memory.key?.takeIf { it.startsWith("$key/") } ?: return@LaunchedEffect
        items.indexOfFirst { "$key/${it.id}" == k }.takeIf { it >= 0 }?.let { gridState.scrollToItem(it) } ?: return@LaunchedEffect
        repeat(6) { withFrameNanos { }; if (memory.restore()) { restored = true; return@LaunchedEffect } }
    }
    LazyVerticalGrid(
        columns = GridCells.Fixed(GridColumns),
        state = gridState,
        contentPadding = PaddingValues(start = TvDims.FocusBorder, end = TvDims.FocusBorder, top = TvDims.FocusBorder, bottom = 32.dp),
        horizontalArrangement = Arrangement.spacedBy(TvDims.GridGap),
        verticalArrangement = Arrangement.spacedBy(TvDims.GridGap),
        modifier = Modifier.fillMaxSize(),
    ) {
        itemsIndexed(items, key = { _, it -> it.id }) { i, item ->
            ThumbCard(
                item, app.api, width = TvDims.GridCardWidth, captionHeight = TvDims.GridCaption, titleLines = 1, secondLine = secondLine(item),
                onClick = { onClick(item) },
                modifier = Modifier.focusKey(memory, "$key/${item.id}").then(if (i < GridColumns) Modifier.focusProperties { up = memory.requester(upKey) } else Modifier),
                badge = if (item.isEpisode || item.isMovie) minutesLabel(item) else null,
                progress = if (item.isEpisode || item.isMovie) item.progressFraction else null,
            )
        }
    }
}
