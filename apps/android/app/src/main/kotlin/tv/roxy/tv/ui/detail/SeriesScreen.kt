package tv.roxy.tv.ui.detail

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.api.Item
import tv.roxy.tv.api.RelatedEntry
import tv.roxy.tv.api.RoxyApi
import tv.roxy.tv.api.Section

data class SeriesUiState(
    val loading: Boolean = true,
    val error: String? = null,
    val series: Item? = null,
    val seasons: List<Item> = emptyList(),
    val selectedSeason: Int = 0,
    val episodes: List<Item> = emptyList(),
    val episodesLoading: Boolean = false,
    /** Movies / OVAs / side stories of this franchise the provider links (More like this). */
    val related: List<RelatedEntry> = emptyList(),
    /**
     * Episodes per entry of [seasons] that is not a season of its own (a group of
     * specials, a film), by its id: the season list shows them as counts.
     */
    val sectionCounts: Map<String, Int> = emptyMap(),
    /** What the season list says under such an entry instead of a count ("Film · 2014"). */
    val sectionLabels: Map<String, String> = emptyMap(),
    /** The show's trailers, featurettes and other extras. */
    val extras: List<Item> = emptyList(),
    /** The "Extras" entry is selected instead of a season. */
    val showExtras: Boolean = false,
    /** What Play plays, across all seasons (null until the episode list is in). */
    val upNext: Item? = null,
)

/**
 * The episode the title page's Play button means, across every season: one in progress
 * (the latest in order, rewatches included), else the first unwatched one after the last
 * watched, else the first unwatched, else the first (watch again). Specials only count
 * when the show has nothing else.
 */
fun upNextOf(episodes: List<Item>): Item? {
    val playable = episodes.filter { it.isPlayable }
    val regular = playable.filter { (it.seasonAired ?: 1) != 0 }.ifEmpty { playable }
    val ordered = regular.sortedWith(compareBy({ it.seasonAired ?: 1 }, { it.indexAired ?: it.indexAbsolute ?: 0 }))
    ordered.lastOrNull { (it.userData?.positionMs ?: 0) > 0 }?.let { return it }
    val lastWatched = ordered.indexOfLast { it.userData?.played == true }
    ordered.drop(lastWatched + 1).firstOrNull { it.userData?.played != true }?.let { return it }
    return ordered.firstOrNull { it.userData?.played != true } ?: ordered.firstOrNull()
}

/** Where an entry of the season list that is not a season gets its rows. */
sealed interface SectionSource {
    /** Some of the Specials season's episodes, in this order. */
    data class Specials(val seasonId: String, val episodeIds: List<String>) : SectionSource
    /** A film of the franchise: its one row. */
    data class Movie(val item: Item) : SectionSource
}

/**
 * The season list from the server's sections, in release order (Crunchyroll's): each
 * season as itself; a group of specials and a film each as an entry of their own, with an
 * id of its own ("section:N") and where its rows come from. Null when the sections cannot
 * be used (a season the server names is not among [seasons]): the plain list then.
 */
data class SectionList(
    val seasons: List<Item>,
    val sources: Map<String, SectionSource>,
    val counts: Map<String, Int>,
    val labels: Map<String, String>,
)

fun sectionEntries(sections: List<Section>, seasons: List<Item>): SectionList? {
    if (sections.isEmpty()) return null
    val byId = seasons.associateBy { it.id }
    val list = mutableListOf<Item>()
    val sources = mutableMapOf<String, SectionSource>()
    val counts = mutableMapOf<String, Int>()
    val labels = mutableMapOf<String, String>()
    sections.forEachIndexed { i, sec ->
        when (sec.kind) {
            "season" -> list += byId[sec.seasonId] ?: return null
            "specials" -> {
                val seasonId = sec.seasonId ?: return null
                val id = "section:$i"
                list += Item(id = id, type = "season", title = sec.title)
                sources[id] = SectionSource.Specials(seasonId, sec.episodeIds)
                counts[id] = sec.episodes
            }
            "movie" -> {
                val film = sec.item ?: return@forEachIndexed
                val id = "section:$i"
                list += Item(id = id, type = "season", title = sec.title.ifEmpty { film.title })
                sources[id] = SectionSource.Movie(film)
                counts[id] = 1
                labels[id] = listOfNotNull("Film", (sec.year ?: film.year)?.toString()).joinToString(" · ")
            }
        }
    }
    return SectionList(list, sources, counts, labels)
}

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
                val plain = api.items(type = "season", parent = seriesId, sort = "index").items
                    .sortedBy { val n = it.indexAired ?: Int.MAX_VALUE; if (n == 0) Int.MAX_VALUE - 1 else n }
                // In release order with the specials between the seasons and the franchise's
                // films as entries of their own; the plain list from an older server.
                val ordered = runCatching { sectionEntries(api.sections(seriesId), plain) }
                    .onFailure { Timber.w(it, "sections") }.getOrNull()
                Triple(series, plain, ordered)
            }.onSuccess { (series, plain, ordered) ->
                val seasons = ordered?.seasons ?: plain
                sources = ordered?.sources ?: emptyMap()
                val keep = _state.value.selectedSeason.coerceIn(0, (seasons.size - 1).coerceAtLeast(0))
                // A reload (after marking watched, say) keeps the episodes and related rows on
                // screen until the fresh ones land, so the list doesn't empty and jump to the top.
                _state.value = _state.value.copy(loading = false, error = null, series = series, seasons = seasons, selectedSeason = keep, sectionCounts = ordered?.counts ?: emptyMap(), sectionLabels = ordered?.labels ?: emptyMap())
                if (seasons.isNotEmpty() && !firstLoad) loadEpisodes(seasons[keep].id)
                else if (seasons.isEmpty()) loadEpisodes(seriesId, recursive = true) // flat series without season folders
                loadUpNext(seasons)
                // Related titles do not change while you watch: once per page, not on every return.
                if (!relatedLoaded) loadRelated()
            }.onFailure { t ->
                Timber.e(t, "series load failed")
                // A reload that fails (back from the player during a Wi-Fi drop) keeps the
                // page that is showing; only a first load turns into the error.
                if (_state.value.series != null) _state.value = _state.value.copy(loading = false)
                else _state.value = SeriesUiState(loading = false, error = tv.roxy.tv.data.friendlyError(t, "this series"))
            }
        }
    }

    /** Entries of the season list that are not seasons: where their rows come from. */
    private var sources: Map<String, SectionSource> = emptyMap()

    private var firstLoad = true
    private var relatedLoaded = false

    /**
     * Every episode of the show, for [SeriesUiState.upNext]. On the first load the page then
     * opens on that episode's season (Crunchyroll), so Play and the list agree.
     */
    private fun loadUpNext(seasons: List<Item>) {
        val opening = firstLoad
        firstLoad = false
        viewModelScope.launch {
            val all = runCatching { api.items(type = "episode", parent = seriesId, recursive = true, sort = "index").items }
                .onFailure { Timber.w(it, "episodes for up next") }.getOrNull()
            val next = all?.let { upNextOf(it) }
            _state.value = _state.value.copy(upNext = next)
            // Not if a season was picked meanwhile (the all-episodes fetch is slow on long shows).
            if (opening && seasons.isNotEmpty() && !userPicked) {
                val at = next?.parentId?.let { pid -> seasons.indexOfFirst { it.id == pid } }?.takeIf { it >= 0 } ?: 0
                _state.value = _state.value.copy(selectedSeason = at)
                loadEpisodes(seasons[at].id)
            }
        }
    }

    private fun loadRelated() {
        viewModelScope.launch {
            runCatching { api.related(seriesId).related }
                .onSuccess { rel -> relatedLoaded = true; _state.value = _state.value.copy(related = rel) }
                .onFailure { Timber.w(it, "related load failed") } // older server: no row, nothing else breaks
            runCatching { api.extras(seriesId) }
                .onSuccess { x -> _state.value = _state.value.copy(extras = x) }
                .onFailure { Timber.w(it, "extras load failed") }
        }
    }

    fun selectSeason(index: Int) {
        val s = _state.value
        if (index !in s.seasons.indices) return
        if (index == s.selectedSeason && s.episodes.isNotEmpty() && !s.showExtras) return
        userPicked = true
        // A different season empties the list until its episodes land, so the old season's
        // episodes are never shown (or played) under the new season's name.
        val other = index != s.selectedSeason
        _state.value = s.copy(selectedSeason = index, showExtras = false, episodes = if (other) emptyList() else s.episodes)
        if (other || s.episodes.isEmpty()) loadEpisodes(s.seasons[index].id)
    }

    private var userPicked = false
    private var episodesJob: Job? = null

    fun selectExtras() {
        _state.value = _state.value.copy(showExtras = true)
    }

    private fun loadEpisodes(parentId: String, recursive: Boolean = false) {
        _state.value = _state.value.copy(episodesLoading = true)
        // Only the latest request counts: two quick season picks cannot land out of order.
        episodesJob?.cancel()
        val source = sources[parentId]
        episodesJob = viewModelScope.launch {
            runCatching {
                when (source) {
                    // A film of the franchise: its one row (with its file, to play and download).
                    is SectionSource.Movie -> listOf(api.item(source.item.id))
                    // Some of the Specials season, in the order the section lists them.
                    is SectionSource.Specials -> {
                        val all = api.items(type = "episode", parent = source.seasonId, includeSources = true, sort = "index").items.associateBy { it.id }
                        source.episodeIds.mapNotNull { all[it] }
                    }
                    null -> api.items(type = "episode", parent = parentId, recursive = recursive, includeSources = true, sort = "index").items
                        .sortedWith(compareBy({ it.seasonAired ?: 0 }, { it.indexAired ?: it.indexAbsolute ?: 0 }))
                }
            }
                .onSuccess { eps ->
                    _state.value = _state.value.copy(episodes = eps, episodesLoading = false)
                }
                .onFailure { t ->
                    if (t is kotlinx.coroutines.CancellationException) throw t
                    Timber.e(t, "episodes load failed")
                    _state.value = _state.value.copy(episodesLoading = false)
                }
        }
    }
}
