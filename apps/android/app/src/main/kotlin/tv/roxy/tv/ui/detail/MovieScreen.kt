package tv.roxy.tv.ui.detail

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.LifecycleEventEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.tv.material3.Text
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.Item
import tv.roxy.tv.api.RoxyApi
import tv.roxy.tv.ui.components.CenteredMessage
import tv.roxy.tv.ui.components.PrimaryButton
import tv.roxy.tv.ui.components.ScreenPadding
import tv.roxy.tv.ui.components.SecondaryButton
import tv.roxy.tv.ui.playItem
import tv.roxy.tv.ui.theme.RoxyColors

data class MovieUiState(val loading: Boolean = true, val error: String? = null, val movie: Item? = null, val partOf: List<Item> = emptyList())

class MovieViewModel(private val api: RoxyApi, private val movieId: String, initial: Item? = null) : ViewModel() {
    private val _state = MutableStateFlow(MovieUiState(movie = initial))
    val state: StateFlow<MovieUiState> = _state

    fun load() {
        viewModelScope.launch {
            runCatching { api.item(movieId) }
                .onSuccess { _state.value = MovieUiState(loading = false, movie = it, partOf = _state.value.partOf) }
                .onFailure { t -> Timber.e(t, "movie load failed"); _state.value = MovieUiState(loading = false, error = t.message) }
            // The series this movie belongs to (AniList relations), if it is in the library.
            runCatching { api.related(movieId).partOf }
                .onSuccess { p -> _state.value = _state.value.copy(partOf = p) }
                .onFailure { Timber.w(it, "related load failed") }
        }
    }
}

@Composable
fun MovieScreen(movieId: String, fallbackTitle: String, onOpenSeries: (Item) -> Unit = {}) {
    val context = LocalContext.current
    val app = RoxyApp.get(context)
    val vm: MovieViewModel = viewModel(key = "movie:$movieId") { MovieViewModel(app.api, movieId) }
    val state by vm.state.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()

    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { vm.load() }

    val movie = state.movie
    if (state.loading && movie == null) { CenteredMessage("Loading $fallbackTitle..."); return }
    if (movie == null) { CenteredMessage(state.error ?: "Not found", color = RoxyColors.error); return }

    val resumable = (movie.userData?.positionMs ?: 0) > 0
    Column(modifier = Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
        DetailHeader(item = movie, api = app.api, actions = {
            PrimaryButton(if (resumable) "Resume" else "Play", onClick = { scope.launch { playItem(context, movie).onFailure { Timber.w(it, "play failed") } } })
            if (resumable) SecondaryButton("Play from start", onClick = { scope.launch { playItem(context, movie, fromStart = true) } })
            state.partOf.firstOrNull()?.let { series -> SecondaryButton("Part of: ${series.title}", onClick = { onOpenSeries(series) }) }
        })
        val episode = with(app.api) { movie.toEpisode() }
        Column(modifier = Modifier.padding(horizontal = ScreenPadding, vertical = 16.dp)) {
            val tags = buildString { if (episode.isHi10p) append("Hi10P  "); if (episode.hasAss) append("ASS  ") }
            Text(tags + episode.videoLine, fontSize = 12.sp, fontFamily = FontFamily.Monospace, color = RoxyColors.muted)
            Text(episode.tracksLine, fontSize = 12.sp, fontFamily = FontFamily.Monospace, color = RoxyColors.muted)
            movie.studio?.let { Text("Studio: $it", fontSize = 13.sp, color = RoxyColors.muted, modifier = Modifier.padding(top = 8.dp)) }
        }
    }
}
