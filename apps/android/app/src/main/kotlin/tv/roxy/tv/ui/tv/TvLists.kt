package tv.roxy.tv.ui.tv

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
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

private const val GridColumns = 5

/** The saved titles (the profile's favourites), newest saved first. */
@Composable
fun TvWatchlist(onOpenSeries: (Item) -> Unit, onOpenMovie: (Item) -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    val memory = rememberFocusMemory()
    var items by remember { mutableStateOf<List<Item>?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    RegisterPageEntry { memory.restore(items?.firstOrNull()?.let { "watchlist/${it.id}" }) }
    suspend fun load() {
        runCatching { app.api.favorites().items }
            .onSuccess { items = it; error = null }
            .onFailure { if (items == null) error = it.message ?: "Could not load the watchlist" }
    }
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { app.scope.launch { load() } }
    OnUserDataChanged { app.scope.launch { load() } }

    Column(modifier = Modifier.fillMaxSize()) {
        PageTitle("Watchlist")
        val list = items
        when {
            list == null && error != null -> PageMessage(error!!, color = RoxyColors.error)
            list == null -> Unit
            list.isEmpty() -> PageMessage("Titles you add to your watchlist show up here.")
            else -> CardGrid(list, memory, key = "watchlist", secondLine = { audioBadge(it) ?: it.year?.toString() }) { item ->
                if (item.isMovie) onOpenMovie(item) else onOpenSeries(item)
            }
        }
    }
}

/** What the profile played, newest first: episodes and films, OK plays them. */
@Composable
fun TvHistory() {
    val context = LocalContext.current
    val app = RoxyApp.get(context)
    val scope = rememberCoroutineScope()
    val memory = rememberFocusMemory()
    var items by remember { mutableStateOf<List<Item>?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    RegisterPageEntry { memory.restore(items?.firstOrNull()?.let { "history/${it.id}" }) }
    suspend fun load() {
        runCatching { app.api.history(100).items }
            .onSuccess { items = it; error = null }
            .onFailure { if (items == null) error = it.message ?: "Could not load the history" }
    }
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { app.scope.launch { load() } }
    OnUserDataChanged { app.scope.launch { load() } }
    val dateFormat = remember { DateFormat.getDateInstance(DateFormat.SHORT) }

    Column(modifier = Modifier.fillMaxSize()) {
        PageTitle("History")
        val list = items
        when {
            list == null && error != null -> PageMessage(error!!, color = RoxyColors.error)
            list == null -> Unit
            list.isEmpty() -> PageMessage("Episodes and movies you watch show up here, newest first.")
            else -> CardGrid(list, memory, key = "history", secondLine = { historyLine(it, dateFormat) }) { item ->
                scope.launch { playItem(context, item).onFailure { Timber.w(it, "play failed") } }
            }
        }
    }
}

/** "E2 - 10/27/2026": the episode code and the day it was last played. */
private fun historyLine(item: Item, dateFormat: DateFormat): String {
    val code = if (item.isEpisode) episodeShort(item) else ""
    val day = item.userData?.lastPlayedAt?.let { runCatching { dateFormat.format(Date.from(java.time.Instant.parse(it))) }.getOrNull() }
    return listOfNotNull(code.ifEmpty { null }, day).joinToString(" - ")
}

/**
 * Five cards across with the accent frame's spacing. The first card takes focus when the
 * page opens; later the remembered card does.
 */
@Composable
private fun CardGrid(items: List<Item>, memory: FocusMemory, key: String, secondLine: (Item) -> String?, onClick: (Item) -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    val gridState = rememberLazyGridState()
    var restored by remember { mutableStateOf(false) }
    LaunchedEffect(items.isNotEmpty()) {
        if (restored || items.isEmpty()) return@LaunchedEffect
        val first = "$key/${items.first().id}"
        memory.key?.let { k -> items.indexOfFirst { "$key/${it.id}" == k }.takeIf { it >= 0 }?.let { gridState.scrollToItem(it) } }
        repeat(6) { withFrameNanos { }; if (memory.restore(first)) { restored = true; return@LaunchedEffect } }
    }
    LazyVerticalGrid(
        columns = GridCells.Fixed(GridColumns),
        state = gridState,
        contentPadding = PaddingValues(start = TvDims.CardStart, end = TvDims.ContentStart, bottom = 32.dp),
        horizontalArrangement = Arrangement.spacedBy(0.dp),
        verticalArrangement = Arrangement.spacedBy(2.dp),
        modifier = Modifier.fillMaxSize(),
    ) {
        items(items, key = { it.id }) { item ->
            ThumbCard(
                item, app.api, width = TvDims.GridCardWidth, captionHeight = TvDims.GridCaption, titleLines = 2, secondLine = secondLine(item),
                onClick = { onClick(item) }, modifier = Modifier.focusKey(memory, "$key/${item.id}"),
                badge = if (item.isEpisode || item.isMovie) minutesLabel(item) else null,
                progress = if (item.isEpisode || item.isMovie) item.progressFraction else null,
            )
        }
    }
}
