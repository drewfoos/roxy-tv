package tv.roxy.tv.ui.detail

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
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
import androidx.tv.material3.Border
import androidx.tv.material3.Card
import androidx.tv.material3.CardDefaults
import androidx.tv.material3.MaterialTheme
import androidx.tv.material3.Text
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.Item
import tv.roxy.tv.api.RelatedEntry
import tv.roxy.tv.api.RoxyApi
import tv.roxy.tv.ui.components.CenteredMessage
import tv.roxy.tv.ui.components.MediaCard
import tv.roxy.tv.ui.components.PrimaryButton
import tv.roxy.tv.ui.components.ProgressBar
import tv.roxy.tv.ui.components.RoxyImage
import tv.roxy.tv.ui.components.ScreenPadding
import tv.roxy.tv.ui.components.SecondaryButton
import tv.roxy.tv.ui.components.metaLine
import tv.roxy.tv.ui.components.tapClick
import tv.roxy.tv.ui.components.widthPx
import tv.roxy.tv.ui.playItem
import tv.roxy.tv.ui.theme.RoxyColors

data class SeriesUiState(
    val loading: Boolean = true,
    val error: String? = null,
    val series: Item? = null,
    val seasons: List<Item> = emptyList(),
    val selectedSeason: Int = 0,
    val episodes: List<Item> = emptyList(),
    val episodesLoading: Boolean = false,
    /** Movies / OVAs / side stories of this franchise (Crunchyroll-style, seasons and movies together). */
    val related: List<RelatedEntry> = emptyList(),
    /** The "Movies & OVAs" tab is selected instead of a season. */
    val showRelated: Boolean = false,
)

class SeriesViewModel(private val api: RoxyApi, private val seriesId: String, initial: Item? = null) : ViewModel() {
    // Seeded from the list that opened the page, so the header paints at once and only the
    // episode rows wait on the network.
    private val _state = MutableStateFlow(SeriesUiState(series = initial, episodesLoading = initial != null))
    val state: StateFlow<SeriesUiState> = _state

    fun load() {
        viewModelScope.launch {
            runCatching {
                val series = api.item(seriesId)
                // Specials (season 0) go last, like Jellyfin; regular seasons in order.
                val seasons = api.items(type = "season", parent = seriesId, sort = "index").items
                    .sortedBy { val n = it.indexAired ?: Int.MAX_VALUE; if (n == 0) Int.MAX_VALUE - 1 else n }
                series to seasons
            }.onSuccess { (series, seasons) ->
                val keep = _state.value.selectedSeason.coerceIn(0, (seasons.size - 1).coerceAtLeast(0))
                _state.value = SeriesUiState(loading = false, series = series, seasons = seasons, selectedSeason = keep)
                if (seasons.isNotEmpty()) loadEpisodes(seasons[keep].id)
                else loadEpisodes(seriesId, recursive = true) // flat series without season folders
                loadRelated()
            }.onFailure { t ->
                Timber.e(t, "series load failed")
                _state.value = SeriesUiState(loading = false, error = t.message)
            }
        }
    }

    private fun loadRelated() {
        viewModelScope.launch {
            runCatching { api.related(seriesId).related }
                .onSuccess { rel -> _state.value = _state.value.copy(related = rel) }
                .onFailure { Timber.w(it, "related load failed") } // older server: no row, nothing else breaks
        }
    }

    fun selectSeason(index: Int) {
        val s = _state.value
        if (index !in s.seasons.indices) return
        if (index == s.selectedSeason && s.episodes.isNotEmpty() && !s.showRelated) return
        _state.value = s.copy(selectedSeason = index, showRelated = false)
        if (index != s.selectedSeason || s.episodes.isEmpty()) loadEpisodes(s.seasons[index].id)
    }

    fun selectRelated() {
        _state.value = _state.value.copy(showRelated = true)
    }

    private fun loadEpisodes(parentId: String, recursive: Boolean = false) {
        _state.value = _state.value.copy(episodesLoading = true)
        viewModelScope.launch {
            runCatching { api.items(type = "episode", parent = parentId, recursive = recursive, includeSources = true, sort = "index").items }
                .onSuccess { eps ->
                    val sorted = eps.sortedWith(compareBy({ it.seasonAired ?: 0 }, { it.indexAired ?: it.indexAbsolute ?: 0 }))
                    _state.value = _state.value.copy(episodes = sorted, episodesLoading = false)
                }
                .onFailure { t ->
                    Timber.e(t, "episodes load failed")
                    _state.value = _state.value.copy(episodesLoading = false, error = t.message)
                }
        }
    }
}

@Composable
fun SeriesScreen(seriesId: String, fallbackTitle: String, onOpenMovie: (Item) -> Unit = {}, onOpenSeries: (Item) -> Unit = {}) {
    val context = LocalContext.current
    val app = RoxyApp.get(context)
    val vm: SeriesViewModel = viewModel(key = "series:$seriesId") { SeriesViewModel(app.api, seriesId) }
    val state by vm.state.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()

    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { vm.load() }

    val series = state.series
    if (state.loading && series == null) { CenteredMessage("Loading $fallbackTitle..."); return }
    if (series == null) { CenteredMessage(state.error ?: "Not found", color = RoxyColors.error); return }

    fun play(item: Item) = scope.launch { playItem(context, item).onFailure { Timber.w(it, "play failed") } }

    LazyColumn(modifier = Modifier.fillMaxSize()) {
        item(key = "header") {
            DetailHeader(item = series, api = app.api, actions = {
                val playable = state.episodes.filter { it.isPlayable }
                val next = playable.firstOrNull { (it.userData?.positionMs ?: 0) > 0 && it.userData?.played != true }
                    ?: playable.firstOrNull { it.userData?.played != true }
                    ?: playable.firstOrNull()
                if (next != null) {
                    val resumable = (next.userData?.positionMs ?: 0) > 0
                    PrimaryButton(if (resumable) "Resume ${next.episodeCode}" else "Play ${next.episodeCode}".trim(), onClick = { play(next) })
                }
            })
        }
        // Season tabs, with "Movies & OVAs" as the last tab (Crunchyroll-style) so the
        // franchise's films and specials sit next to the seasons, not under the list.
        if (state.seasons.isNotEmpty() || state.related.isNotEmpty()) {
            item(key = "seasons") {
                LazyRow(
                    contentPadding = PaddingValues(horizontal = ScreenPadding),
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                    modifier = Modifier.padding(bottom = 12.dp),
                ) {
                    itemsIndexed(state.seasons, key = { _, s -> s.id }) { i, season ->
                        SecondaryButton(season.title.ifEmpty { "Season ${season.indexAired ?: i + 1}" }, onClick = { vm.selectSeason(i) }, selected = !state.showRelated && i == state.selectedSeason)
                    }
                    if (state.related.isNotEmpty()) {
                        item(key = "related-tab") {
                            SecondaryButton("Movies & OVAs", onClick = { vm.selectRelated() }, selected = state.showRelated)
                        }
                    }
                }
            }
        }
        if (state.showRelated) {
            item(key = "related") {
                RelatedRow(state.related, app.api, onOpen = { it ->
                    val item = it.item ?: return@RelatedRow
                    if (item.isMovie) onOpenMovie(item) else onOpenSeries(item)
                })
            }
        } else {
            if (state.episodesLoading && state.episodes.isEmpty()) {
                item(key = "eps-loading") { Text("Loading episodes...", color = RoxyColors.muted, modifier = Modifier.padding(horizontal = ScreenPadding, vertical = 12.dp)) }
            } else if (state.episodes.isEmpty()) {
                item(key = "eps-empty") { Text("No episodes in this season.", color = RoxyColors.muted, modifier = Modifier.padding(horizontal = ScreenPadding, vertical = 12.dp)) }
            }
            items(state.episodes, key = { it.id }) { ep ->
                EpisodeRow(ep, app.api, onClick = { if (ep.isPlayable) play(ep) })
            }
        }
        item(key = "bottom") { Spacer(Modifier.height(32.dp)) }
    }
}

/** Backdrop hero with poster, title, meta, overview and the action buttons. Shared by series and movie. */
@Composable
fun DetailHeader(item: Item, api: RoxyApi, actions: @Composable () -> Unit) {
    // 340dp on a TV (540dp tall); capped at 60% of the window so a phone still shows the list below.
    val config = LocalConfiguration.current
    val headerHeight = minOf(340.dp, config.screenHeightDp.dp * 0.6f)
    val small = headerHeight < 300.dp
    val posterWidth = if (small) 110.dp else 150.dp
    // heightIn(min) rather than a fixed height: a long title + overview grows the header instead of clipping the buttons.
    Box(modifier = Modifier.fillMaxWidth().heightIn(min = headerHeight)) {
        RoxyImage(api.imageUrl(item, "backdrop", widthPx(960.dp)), "", modifier = Modifier.matchParentSize(), placeholderTextSize = 0)
        Box(modifier = Modifier.matchParentSize().background(Brush.verticalGradient(0f to RoxyColors.background.copy(alpha = 0.2f), 1f to RoxyColors.background)))
        Box(modifier = Modifier.matchParentSize().background(Brush.horizontalGradient(0f to RoxyColors.background.copy(alpha = 0.9f), 0.7f to Color.Transparent)))
        Row(
            modifier = Modifier.align(Alignment.BottomStart).padding(start = ScreenPadding, end = ScreenPadding, top = if (small) 36.dp else 72.dp, bottom = 20.dp),
            verticalAlignment = Alignment.Bottom,
            horizontalArrangement = Arrangement.spacedBy(24.dp),
        ) {
            Box(modifier = Modifier.width(posterWidth).aspectRatio(2f / 3f).clip(RoundedCornerShape(6.dp))) {
                RoxyImage(api.imageUrl(item, "poster", widthPx(posterWidth) * 2), item.title, modifier = Modifier.fillMaxSize())
            }
            Column(verticalArrangement = Arrangement.spacedBy(6.dp), modifier = Modifier.weight(1f)) {
                Text(item.title, style = MaterialTheme.typography.headlineLarge, fontWeight = FontWeight.Bold, maxLines = 2, overflow = TextOverflow.Ellipsis)
                val meta = item.metaLine()
                if (meta.isNotEmpty()) Text(meta, color = RoxyColors.muted, fontSize = 13.sp, maxLines = 1)
                item.tagline?.takeIf { it.isNotBlank() }?.let { Text(it, fontSize = 14.sp, color = RoxyColors.muted, maxLines = 1, overflow = TextOverflow.Ellipsis) }
                item.overview?.takeIf { it.isNotBlank() }?.let { Text(it, fontSize = 14.sp, maxLines = if (small) 3 else 4, overflow = TextOverflow.Ellipsis) }
                Row(horizontalArrangement = Arrangement.spacedBy(12.dp), modifier = Modifier.padding(top = 6.dp)) { actions() }
            }
        }
    }
}

/**
 * "Movies & OVAs" row: the franchise's related titles. Entries in the library are normal
 * poster cards; the rest are dimmed tiles with the provider's title and a "Not in
 * library" note (no provider-hosted images: the image loader sends the Roxy token).
 */
@Composable
private fun RelatedRow(related: List<RelatedEntry>, api: RoxyApi, onOpen: (RelatedEntry) -> Unit) {
    Column(modifier = Modifier.fillMaxWidth()) {
        LazyRow(contentPadding = PaddingValues(horizontal = ScreenPadding, vertical = 10.dp), horizontalArrangement = Arrangement.spacedBy(14.dp)) {
            items(related, key = { it.key }) { rel ->
                val item = rel.item
                if (rel.inLibrary && item != null) {
                    Column(modifier = Modifier.width(RelatedCardWidth)) {
                        MediaCard(item, api, onClick = { onOpen(rel) })
                        Text(rel.label + (rel.year?.let { " · $it" } ?: ""), fontSize = 11.sp, color = RoxyColors.muted, maxLines = 1)
                    }
                } else {
                    Column(modifier = Modifier.width(RelatedCardWidth).alpha(0.55f)) {
                        Card(
                            onClick = {},
                            shape = CardDefaults.shape(RoundedCornerShape(6.dp)),
                            scale = CardDefaults.scale(focusedScale = 1.05f),
                            border = CardDefaults.border(focusedBorder = Border(BorderStroke(2.dp, RoxyColors.focusBorder), shape = RoundedCornerShape(6.dp))),
                            colors = CardDefaults.colors(containerColor = RoxyColors.surface),
                            modifier = Modifier.width(RelatedCardWidth).aspectRatio(2f / 3f),
                        ) {
                            Box(modifier = Modifier.fillMaxSize().padding(10.dp), contentAlignment = Alignment.Center) {
                                Text(rel.title, fontSize = 12.sp, color = RoxyColors.muted, maxLines = 5, overflow = TextOverflow.Ellipsis)
                            }
                        }
                        Text(rel.title, fontSize = 13.sp, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 6.dp))
                        Text(rel.label + (rel.year?.let { " · $it" } ?: "") + " · Not in library", fontSize = 11.sp, color = RoxyColors.muted, maxLines = 1)
                    }
                }
            }
        }
    }
}

private val RelatedCardWidth = 150.dp

/** One episode: thumb with progress, number + title, runtime and the stream summary. */
@Composable
private fun EpisodeRow(ep: Item, api: RoxyApi, onClick: () -> Unit) {
    val thumbWidth = 176.dp
    val episode = with(api) { ep.toEpisode() }
    // Missing / unaired placeholders (Jellyfin-style): greyed out, badged, not playable.
    val placeholder = !ep.isPlayable
    val badge = when (ep.availability) {
        "missing" -> "Missing"
        "unaired" -> ep.airDate?.takeIf { it.isNotBlank() }?.let { "Airs $it" } ?: "Unaired"
        else -> null
    }
    val dim = if (placeholder) 0.45f else 1f
    Card(
        onClick = onClick,
        shape = CardDefaults.shape(RoundedCornerShape(8.dp)),
        scale = CardDefaults.scale(focusedScale = 1.02f),
        border = CardDefaults.border(focusedBorder = Border(BorderStroke(2.dp, RoxyColors.focusBorder), shape = RoundedCornerShape(8.dp))),
        colors = CardDefaults.colors(containerColor = Color.Transparent, focusedContainerColor = RoxyColors.surface),
        modifier = Modifier.fillMaxWidth().padding(horizontal = ScreenPadding - 8.dp, vertical = 2.dp).tapClick(onClick),
    ) {
        Row(modifier = Modifier.fillMaxWidth().padding(8.dp).alpha(dim), horizontalArrangement = Arrangement.spacedBy(16.dp), verticalAlignment = Alignment.CenterVertically) {
            Box(modifier = Modifier.width(thumbWidth).aspectRatio(16f / 9f).clip(RoundedCornerShape(4.dp))) {
                RoxyImage(api.imageUrl(ep, "thumb", widthPx(thumbWidth) * 2), ep.episodeCode, modifier = Modifier.fillMaxSize())
                if (!placeholder) ProgressBar(ep.progressFraction, modifier = Modifier.align(Alignment.BottomCenter))
            }
            Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                    Text((ep.indexAired ?: ep.indexAbsolute)?.let { "$it." } ?: "", color = RoxyColors.muted, fontSize = 15.sp)
                    Text(ep.title, fontSize = 16.sp, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
                    if (badge != null) {
                        Text(
                            badge, fontSize = 11.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground,
                            modifier = Modifier.background(RoxyColors.muted.copy(alpha = 0.35f), RoundedCornerShape(4.dp)).padding(horizontal = 6.dp, vertical = 2.dp),
                        )
                    }
                    ep.runtimeMinutes?.let { Text("$it min", color = RoxyColors.muted, fontSize = 13.sp) }
                    if (ep.userData?.played == true) Text("Watched", color = RoxyColors.muted, fontSize = 12.sp)
                }
                ep.overview?.takeIf { it.isNotBlank() }?.let { Text(it, fontSize = 13.sp, color = RoxyColors.onBackground.copy(alpha = 0.8f), maxLines = 2, overflow = TextOverflow.Ellipsis) }
                if (!placeholder) {
                    val tags = buildString { if (episode.isHi10p) append("Hi10P  "); if (episode.hasAss) append("ASS  ") }
                    Text(tags + episode.videoLine, fontSize = 11.sp, fontFamily = FontFamily.Monospace, color = RoxyColors.muted, maxLines = 1, overflow = TextOverflow.Ellipsis)
                } else if (ep.airDate?.isNotBlank() == true && ep.availability == "missing") {
                    Text("Aired ${ep.airDate}", fontSize = 11.sp, color = RoxyColors.muted)
                }
            }
        }
    }
}
