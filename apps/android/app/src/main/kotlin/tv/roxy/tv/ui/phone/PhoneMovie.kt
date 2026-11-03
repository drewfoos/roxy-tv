package tv.roxy.tv.ui.phone

import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.asPaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Replay
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.foundation.background
import androidx.compose.runtime.remember
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import tv.roxy.tv.api.DiscoverItem
import tv.roxy.tv.api.RelatedEntry
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.Spacer
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.Item
import tv.roxy.tv.ui.detail.MovieViewModel
import tv.roxy.tv.ui.playItem
import tv.roxy.tv.ui.theme.RoxyColors

/** Netflix's phone movie page: backdrop, title and meta, Play, synopsis, the franchise it belongs to. */
@Composable
fun PhoneMovie(movieId: String, fallbackTitle: String, onBack: () -> Unit, onOpenSeries: (Item) -> Unit, onOpenDetails: (Item) -> Unit = {}, onOpenMovie: (Item) -> Unit = {}, onOpenDiscover: (DiscoverItem) -> Unit = {}) {
    val context = LocalContext.current
    val app = RoxyApp.get(context)
    val vm: MovieViewModel = viewModel(key = "movie:$movieId") { MovieViewModel(app.api, movieId, initial = app.items[movieId]) }
    val state by vm.state.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { vm.load() }
    OnUserDataChanged { vm.load() }

    val movie = state.movie
    if (movie == null) {
        if (state.loading) DetailSkeleton(onBack)
        else Box(Modifier.fillMaxSize().statusBarsPadding()) {
            tv.roxy.tv.ui.components.LoadError(state.error ?: "Not found", onRetry = { vm.load() })
            BackCircle(onBack, modifier = Modifier.padding(12.dp))
        }
        return
    }

    val resumable = (movie.userData?.positionMs ?: 0) > 0
    val listState = rememberLazyListState()
    val startDownload = rememberDownloadStarter()

    TitlePageScaffold(
        item = movie, api = app.api, listState = listState, onBack = onBack,
        playLabel = if (resumable) "Resume" else "Start watching",
        onPlay = { scope.launch { playItem(context, movie).onFailure { Timber.w(it, "play failed") } } },
        onSaved = { vm.load() },
        onMarkWatched = { on -> scope.launch { app.markWatched(movie.id, on) } },
        markAll = false,
        onArtChanged = { vm.load() },
    ) { bottomInset ->
        // The series page's shape (Crunchyroll's): key art with the logo, facts and synopsis
        // centred, a tab strip, then the film as one row with its download and menu, and the
        // franchise it belongs to under a second tab when there is one.
        val tabs = listOf("Movie") + (if (state.partOf.isNotEmpty()) listOf("Part of") else emptyList()) + "More like this"
        var tab by rememberSaveable(movie.id) { mutableStateOf(0) }
        val moreLike = tab == tabs.lastIndex
        var similar by remember(movie.id) { mutableStateOf<List<DiscoverItem>?>(null) }
        LaunchedEffect(moreLike, movie.id) { if (moreLike && similar == null) similar = runCatching { app.api.similar(movie.id) }.getOrDefault(emptyList()) }
        LazyColumn(state = listState, modifier = Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = bottomInset)) {
            item(key = "banner") { TitleBanner(movie, app.api, listState) }
            item(key = "info") {
                TitleInfo(
                    item = movie, api = app.api,
                    badge = audioBadge(movie),
                    extra = movie.runtimeMinutes?.let { "${it / 60}h ${it % 60}m".removePrefix("0h ") },
                    onMoreDetails = { onOpenDetails(movie) },
                )
            }
            item(key = "tabs") { SectionTabs(tabs, tab.coerceIn(0, tabs.size - 1), modifier = Modifier.background(RoxyColors.background).padding(top = 22.dp)) { tab = it } }
            if (moreLike) {
                val list = similar
                when {
                    list == null -> item(key = "similar-wait") { Row(modifier = Modifier.fillMaxWidth().padding(start = PhonePad, end = PhonePad, top = 18.dp, bottom = 8.dp), horizontalArrangement = Arrangement.spacedBy(12.dp)) { repeat(2) { Bone(Modifier.weight(1f).aspectRatio(2f / 3f)) } } }
                    list.isEmpty() -> item(key = "similar-none") { Text("Nothing to suggest for this title yet.", color = RoxyColors.muted, fontSize = 13.sp, modifier = Modifier.padding(PhonePad)) }
                    else -> { item(key = "similar-top") { Spacer(Modifier.height(10.dp)) }; items(list.chunked(2), key = { row -> "sim:" + row.joinToString("|") { it.tmdbId.toString() } }) { row ->
                        DiscoverGridRow(row) { c ->
                            val id = c.libraryItemId
                            if (id != null) { if (c.isMovie) onOpenMovie(Item(id = id, type = "movie", title = c.title)) else onOpenSeries(Item(id = id, type = "series", title = c.title)) }
                            else onOpenDiscover(c)
                        }
                    } }
                }
            } else if (tab == 1 && state.partOf.isNotEmpty()) {
                item(key = "partof-top") { Spacer(Modifier.height(10.dp)) }
                items(state.partOf.chunked(2)) { row -> RelatedGridRow(row.map { RelatedEntry(provider = "roxy", providerId = it.id, item = it, title = it.title, inLibrary = true) }, app.api, onOpen = { rel -> rel.item?.let(onOpenSeries) }) }
            } else {
                item(key = "movie-row") {
                    Spacer(Modifier.height(6.dp))
                    PhoneEpisodeRow(
                        movie, app.api,
                        onClick = { if (movie.isPlayable) scope.launch { playItem(context, movie) } },
                        onDownload = { startDownload(listOf(movie), movie, null) },
                        onMarkWatched = { on -> scope.launch { app.markWatched(movie.id, on) } },
                    )
                }
            }
        }
    }
}
