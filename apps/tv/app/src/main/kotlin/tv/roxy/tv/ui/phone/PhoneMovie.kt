package tv.roxy.tv.ui.phone

import androidx.compose.foundation.clickable
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
    var expanded by rememberSaveable { mutableStateOf(false) }
    val bottom = WindowInsets.navigationBars.asPaddingValues().calculateBottomPadding() + 24.dp

    Column(modifier = Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(bottom = bottom)) {
        Backdrop(movie, app.api, onBack)
        Column(modifier = Modifier.padding(horizontal = PhonePad)) {
            Text(movie.title, fontSize = 22.sp, lineHeight = 26.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground)
            val meta = listOfNotNull(
                (movie.year ?: movie.premiered?.take(4)?.toIntOrNull())?.toString(),
                movie.runtimeMinutes?.let { "${it / 60}h ${it % 60}m".removePrefix("0h ") },
                movie.officialRating,
                movie.communityRating?.let { "★ %.1f".format(it) },
            ).joinToString("   ")
            if (meta.isNotEmpty()) Text(meta, fontSize = 13.sp, color = RoxyColors.muted, modifier = Modifier.padding(top = 6.dp))

            PlayButton(if (resumable) "Resume" else "Play", modifier = Modifier.fillMaxWidth().padding(top = 14.dp), onClick = {
                scope.launch { playItem(context, movie).onFailure { Timber.w(it, "play failed") } }
            })
            if (resumable) {
                GreyButton("Play from start", modifier = Modifier.fillMaxWidth().padding(top = 8.dp), icon = Icons.Filled.Replay, onClick = {
                    scope.launch { playItem(context, movie, fromStart = true) }
                })
            }
            MyListButton(movie, app.api, modifier = Modifier.fillMaxWidth().padding(top = 8.dp), onChanged = { vm.load() })
            movie.tagline?.takeIf { it.isNotBlank() }?.let { Text(it, fontSize = 14.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground, modifier = Modifier.padding(top = 14.dp)) }
            movie.overview?.takeIf { it.isNotBlank() }?.let {
                Text(
                    it, fontSize = 14.sp, lineHeight = 20.sp, color = RoxyColors.onBackground.copy(alpha = 0.9f),
                    maxLines = if (expanded) Int.MAX_VALUE else 4, overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.fillMaxWidth().padding(top = 10.dp).clickable { expanded = !expanded },
                )
            }
            val genres = movie.genres.joinToString(", ")
            if (genres.isNotEmpty()) Text("Genres: $genres", fontSize = 12.sp, color = RoxyColors.muted, modifier = Modifier.padding(top = 8.dp))
            movie.studio?.let { Text("Studio: $it", fontSize = 12.sp, color = RoxyColors.muted, modifier = Modifier.padding(top = 2.dp)) }
            val episode = with(app.api) { movie.toEpisode() }
            val tags = buildString { if (episode.isHi10p) append("Hi10P  "); if (episode.hasAss) append("ASS  ") }
            Text(tags + episode.videoLine, fontSize = 11.sp, fontFamily = FontFamily.Monospace, color = RoxyColors.muted, modifier = Modifier.padding(top = 10.dp))
            Text(episode.tracksLine, fontSize = 11.sp, fontFamily = FontFamily.Monospace, color = RoxyColors.muted)
        }
        if (state.partOf.isNotEmpty()) {
            PhoneRow("Part of", state.partOf, app.api, onClick = onOpenSeries, modifier = Modifier.padding(top = 22.dp))
        }
    }
}
