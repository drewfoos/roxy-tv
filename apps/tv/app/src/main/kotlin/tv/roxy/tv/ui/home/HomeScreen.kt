package tv.roxy.tv.ui.home

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.LifecycleEventEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.tv.material3.MaterialTheme
import androidx.tv.material3.Text
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.ApiException
import tv.roxy.tv.api.HomeRow
import tv.roxy.tv.api.Item
import tv.roxy.tv.api.RoxyApi
import tv.roxy.tv.ui.components.CenteredMessage
import tv.roxy.tv.ui.components.MediaRow
import tv.roxy.tv.ui.components.PrimaryButton
import tv.roxy.tv.ui.components.RoxyImage
import tv.roxy.tv.ui.components.ScreenPadding
import tv.roxy.tv.ui.components.SecondaryButton
import tv.roxy.tv.ui.components.metaLine
import tv.roxy.tv.ui.components.widthPx
import tv.roxy.tv.ui.playItem
import tv.roxy.tv.ui.theme.RoxyColors

sealed interface HomeUiState {
    data object Loading : HomeUiState
    data class Error(val message: String, val unauthorized: Boolean = false) : HomeUiState
    data class Ready(val hero: Item?, val rows: List<HomeRow>, val composedLocally: Boolean, val note: String? = null) : HomeUiState
}

class HomeViewModel(private val api: RoxyApi) : ViewModel() {
    private val _state = MutableStateFlow<HomeUiState>(HomeUiState.Loading)
    val state: StateFlow<HomeUiState> = _state

    fun load(silent: Boolean = false) {
        if (!silent) _state.value = HomeUiState.Loading
        viewModelScope.launch {
            val result = runCatching { fromHomeEndpoint() }
                .recoverCatching { t ->
                    if (t is ApiException && t.isNotFound) {
                        Timber.w("GET /v1/home is 404: composing rows from /v1/items instead")
                        composeRows()
                    } else throw t
                }
            result.onSuccess { _state.value = it }
                .onFailure { t ->
                    Timber.e(t, "home failed")
                    val unauthorized = t is ApiException && t.isUnauthorized
                    _state.value = HomeUiState.Error(t.message ?: t::class.simpleName.orEmpty(), unauthorized)
                }
        }
    }

    private suspend fun fromHomeEndpoint(): HomeUiState.Ready {
        val rows = api.home().rows.filter { it.items.isNotEmpty() }
        return HomeUiState.Ready(hero = pickHero(rows), rows = rows, composedLocally = false)
    }

    /** Fallback for a server without `/v1/home`: five parallel-ish list calls. */
    private suspend fun composeRows(): HomeUiState.Ready {
        val notes = mutableListOf<String>()
        val resume = runCatching { api.resume(20).items }.getOrElse { t ->
            if (t is ApiException && t.isNotFound) notes += "/v1/me/resume" else Timber.w(t, "resume failed")
            emptyList()
        }
        val episodes = api.items(type = "episode", recursive = true, sort = "date_created", desc = true, limit = 40, includeSources = true).items
            .distinctBy { it.seriesId ?: it.seriesTitle ?: it.id } // one per series, like the server's row
            .take(20)
        val recentMovies = api.items(type = "movie", recursive = true, sort = "date_created", desc = true, limit = 20).items
        val series = api.items(type = "series", sort = "date_created", desc = true, limit = 50).items
        val movies = api.items(type = "movie", recursive = true, sort = "title", limit = 50).items
        val rows = listOf(
            HomeRow("continue", "Continue Watching", resume),
            HomeRow("recentEpisodes", "New Episodes", episodes),
            HomeRow("recentMovies", "New Movies", recentMovies),
            HomeRow("series", "Series", series),
            HomeRow("movies", "Movies", movies),
        ).filter { it.items.isNotEmpty() }
        val note = "Rows composed from /v1/items (server has no /v1/home" + (if (notes.isNotEmpty()) ", " + notes.joinToString() else "") + " yet)"
        return HomeUiState.Ready(hero = pickHero(rows), rows = rows, composedLocally = true, note = note)
    }

    private fun pickHero(rows: List<HomeRow>): Item? =
        rows.firstOrNull { it.key == "continue" }?.items?.firstOrNull()
            ?: rows.firstOrNull { it.key == "recentEpisodes" }?.items?.firstOrNull()
            ?: rows.firstOrNull()?.items?.firstOrNull()
}

@Composable
fun HomeScreen(
    onOpenSeries: (Item) -> Unit,
    onOpenMovie: (Item) -> Unit,
    onOpenSettings: () -> Unit,
    onUnauthorized: () -> Unit,
) {
    val context = LocalContext.current
    val app = RoxyApp.get(context)
    val vm: HomeViewModel = viewModel { HomeViewModel(app.api) }
    val state by vm.state.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()

    // (Re)load on every return to the foreground so progress made in the player shows up.
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { vm.load(silent = state is HomeUiState.Ready) }

    fun open(item: Item) {
        when {
            item.isSeries -> onOpenSeries(item)
            item.isMovie -> onOpenMovie(item)
            item.isEpisode -> scope.launch { playItem(context, item).onFailure { Timber.w(it, "play failed") } }
            item.isSeason -> item.seriesId?.let { onOpenSeries(Item(id = it, type = "series", title = item.seriesTitle ?: item.title)) }
        }
    }

    when (val s = state) {
        HomeUiState.Loading -> CenteredMessage("Loading your library...")
        is HomeUiState.Error -> Column(modifier = Modifier.fillMaxSize(), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.Center) {
            Text(if (s.unauthorized) "Your session has expired." else "Could not load the library", color = RoxyColors.error)
            Text(s.message, color = RoxyColors.muted, fontSize = 13.sp, modifier = Modifier.padding(top = 6.dp, bottom = 16.dp))
            Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                if (s.unauthorized) PrimaryButton("Sign in again", onClick = onUnauthorized) else PrimaryButton("Retry", onClick = { vm.load() })
                SecondaryButton("Settings", onClick = onOpenSettings)
            }
        }
        is HomeUiState.Ready -> BoxWithConstraints(modifier = Modifier.fillMaxSize()) {
            val heroHeight = maxHeight * 0.58f
            LazyColumn(modifier = Modifier.fillMaxSize()) {
                item(key = "hero") {
                    Hero(
                        item = s.hero,
                        api = app.api,
                        height = heroHeight,
                        onPlay = { hero ->
                            if (hero.isEpisode || hero.isMovie) scope.launch { playItem(context, hero).onFailure { Timber.w(it, "play failed") } }
                            else open(hero)
                        },
                        onInfo = { hero ->
                            when {
                                hero.isSeries || hero.isMovie -> open(hero)
                                hero.isEpisode -> (hero.seriesId ?: hero.parentId)?.let { onOpenSeries(Item(id = it, type = "series", title = hero.seriesTitle ?: "")) }
                            }
                        },
                        onOpenSettings = onOpenSettings,
                    )
                }
                items(s.rows, key = { it.key }) { row ->
                    MediaRow(row.title.ifEmpty { rowTitle(row.key) }, row.items, app.api, onClick = ::open, modifier = Modifier.padding(bottom = 18.dp))
                }
                item(key = "footer") {
                    if (s.rows.isEmpty()) CenteredMessage("The library is empty. Scan a library on the server first.", modifier = Modifier.height(160.dp))
                    s.note?.let { Text(it, color = RoxyColors.muted.copy(alpha = 0.6f), fontSize = 11.sp, modifier = Modifier.padding(horizontal = ScreenPadding, vertical = 12.dp)) }
                    Spacer(Modifier.height(24.dp))
                }
            }
        }
    }
}

private fun rowTitle(key: String) = when (key) {
    "continue" -> "Continue Watching"
    "recentEpisodes" -> "New Episodes"
    "recentMovies" -> "New Movies"
    "series" -> "Series"
    "movies" -> "Movies"
    else -> key
}

@Composable
private fun Hero(
    item: Item?,
    api: RoxyApi,
    height: androidx.compose.ui.unit.Dp,
    onPlay: (Item) -> Unit,
    onInfo: (Item) -> Unit,
    onOpenSettings: () -> Unit,
) {
    Box(modifier = Modifier.fillMaxWidth().height(height)) {
        if (item != null) {
            val type = if (item.isEpisode) "thumb" else "backdrop"
            val url = api.imageUrl(item, type, widthPx(960.dp)) ?: api.imageUrl(item, "backdrop", widthPx(960.dp))
            RoxyImage(url, "", modifier = Modifier.fillMaxSize(), placeholderTextSize = 0)
        }
        // Netflix-style scrims: dark at the bottom into the page background, dark on the left under the text.
        Box(
            modifier = Modifier.fillMaxSize().background(
                Brush.verticalGradient(0f to Color.Transparent, 0.55f to RoxyColors.background.copy(alpha = 0.55f), 1f to RoxyColors.background),
            ),
        )
        Box(modifier = Modifier.fillMaxSize().background(Brush.horizontalGradient(0f to RoxyColors.background.copy(alpha = 0.85f), 0.6f to Color.Transparent)))

        // Top bar
        Row(
            modifier = Modifier.fillMaxWidth().padding(horizontal = ScreenPadding, vertical = 20.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text("Roxy", style = MaterialTheme.typography.headlineSmall, fontWeight = FontWeight.Bold, color = RoxyColors.accent)
            Spacer(Modifier.weight(1f))
            SecondaryButton("Settings", onClick = onOpenSettings)
        }

        if (item != null) {
            Column(
                modifier = Modifier.align(Alignment.BottomStart).fillMaxWidth(0.55f).padding(start = ScreenPadding, bottom = 24.dp),
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                val resumable = (item.userData?.positionMs ?: 0) > 0
                if (item.isEpisode) {
                    Text(item.seriesTitle ?: "", color = RoxyColors.muted, fontSize = 14.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
                Text(
                    if (item.isEpisode) listOf(item.episodeCode, item.title).filter { it.isNotEmpty() }.joinToString("  ") else item.title,
                    style = MaterialTheme.typography.headlineLarge,
                    fontWeight = FontWeight.Bold,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
                val meta = item.metaLine()
                if (meta.isNotEmpty()) Text(meta, color = RoxyColors.muted, fontSize = 13.sp, maxLines = 1)
                item.overview?.takeIf { it.isNotBlank() }?.let {
                    Text(it, fontSize = 14.sp, maxLines = 3, overflow = TextOverflow.Ellipsis, color = RoxyColors.onBackground.copy(alpha = 0.9f))
                }
                Row(horizontalArrangement = Arrangement.spacedBy(12.dp), modifier = Modifier.padding(top = 6.dp)) {
                    PrimaryButton(if (resumable) "Resume" else "Play", onClick = { onPlay(item) })
                    if (item.isEpisode || item.isSeries || item.isMovie) SecondaryButton("Details", onClick = { onInfo(item) })
                }
            }
        } else {
            Column(modifier = Modifier.align(Alignment.BottomStart).padding(start = ScreenPadding, bottom = 24.dp)) {
                Text("Nothing to show yet", style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
                Text("Add media on the server and scan the library.", color = RoxyColors.muted)
            }
        }
    }
}
