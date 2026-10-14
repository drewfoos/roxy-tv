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
fun PhoneMovie(movieId: String, fallbackTitle: String, onBack: () -> Unit, onOpenSeries: (Item) -> Unit) {
    val context = LocalContext.current
    val app = RoxyApp.get(context)
    val vm: MovieViewModel = viewModel(key = "movie:$movieId") { MovieViewModel(app.api, movieId) }
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

    TitlePageScaffold(
        item = movie, api = app.api, listState = listState, onBack = onBack,
        playLabel = if (resumable) "Resume" else "Start watching",
        onPlay = { scope.launch { playItem(context, movie).onFailure { Timber.w(it, "play failed") } } },
        onSaved = { vm.load() },
    ) { bottomInset ->
        LazyColumn(state = listState, modifier = Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = bottomInset)) {
            item(key = "banner") { TitleBanner(movie, app.api, listState) }
            item(key = "info") {
                TitleInfo(
                    item = movie,
                    badge = audioBadge(movie),
                    extra = movie.runtimeMinutes?.let { "${it / 60}h ${it % 60}m".removePrefix("0h ") },
                )
            }
            item(key = "more") {
                Column(modifier = Modifier.padding(horizontal = PhonePad)) {
                    if (resumable) {
                        GreyButton("Play from start", modifier = Modifier.fillMaxWidth().padding(top = 16.dp), icon = Icons.Filled.Replay, onClick = {
                            scope.launch { playItem(context, movie, fromStart = true) }
                        })
                    }
                    movie.studio?.let { Text("Studio: $it", fontSize = 12.sp, color = RoxyColors.muted, modifier = Modifier.padding(top = 14.dp)) }
                    val episode = with(app.api) { movie.toEpisode() }
                    val tags = buildString { if (episode.isHi10p) append("Hi10P  "); if (episode.hasAss) append("ASS  ") }
                    Text(tags + episode.videoLine, fontSize = 11.sp, fontFamily = FontFamily.Monospace, color = RoxyColors.muted, modifier = Modifier.padding(top = 10.dp))
                    Text(episode.tracksLine, fontSize = 11.sp, fontFamily = FontFamily.Monospace, color = RoxyColors.muted)
                }
            }
            if (state.partOf.isNotEmpty()) {
                item(key = "partOf") { PhoneRow("Part of", state.partOf, app.api, onClick = onOpenSeries, modifier = Modifier.padding(top = 22.dp)) }
            }
        }
    }
}
