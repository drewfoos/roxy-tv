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
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Replay
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
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
fun PhoneMovie(movieId: String, fallbackTitle: String, onBack: () -> Unit, onOpenSeries: (Item) -> Unit, onOpenDetails: (Item) -> Unit = {}) {
    val context = LocalContext.current
    val app = RoxyApp.get(context)
    val vm: MovieViewModel = viewModel(key = "movie:$movieId") { MovieViewModel(app.api, movieId, initial = app.items[movieId]) }
    val state by vm.state.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { vm.load() }

    val movie = state.movie
    if (movie == null) {
        if (state.loading) DetailSkeleton(onBack)
        else Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { Text(state.error ?: "Not found", color = RoxyColors.error) }
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
        onMarkWatched = { on -> scope.launch { runCatching { app.api.setPlayed(movie.id, on) }.onFailure { Timber.w(it, "mark watched") }; vm.load() } },
        markAll = false,
        onArtChanged = { vm.load() },
    ) { bottomInset ->
        // The series page's shape (Crunchyroll's): key art with the logo, facts and synopsis
        // centred, a tab strip, then the film as one row with its download and menu, and the
        // franchise it belongs to under a second tab when there is one.
        val tabs = if (state.partOf.isNotEmpty()) listOf("Movie", "Part of") else listOf("Movie")
        var tab by rememberSaveable(movie.id) { mutableStateOf(0) }
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
            item(key = "tabs") { SectionTabs(tabs, tab.coerceIn(0, tabs.size - 1), modifier = Modifier.padding(top = 22.dp)) { tab = it } }
            if (tab == 1 && state.partOf.isNotEmpty()) {
                items(state.partOf.chunked(3)) { row -> RelatedGridRow(row.map { RelatedEntry(provider = "roxy", providerId = it.id, item = it, title = it.title, inLibrary = true) }, app.api, onOpen = { rel -> rel.item?.let(onOpenSeries) }) }
            } else {
                item(key = "movie-row") {
                    Spacer(Modifier.height(6.dp))
                    PhoneEpisodeRow(
                        movie, app.api,
                        onClick = { if (movie.isPlayable) scope.launch { playItem(context, movie) } },
                        onDownload = { startDownload(listOf(movie), movie, null) },
                        onMarkWatched = { on -> scope.launch { runCatching { app.api.setPlayed(movie.id, on) }.onFailure { Timber.w(it, "mark watched") }; vm.load() } },
                    )
                }
            }
        }
    }
}
