package tv.roxy.tv.ui.detail

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.api.Item
import tv.roxy.tv.api.RelatedEntry
import tv.roxy.tv.api.RoxyApi

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
                // A reload (after marking watched, say) keeps the episodes and related rows on
                // screen until the fresh ones land, so the list doesn't empty and jump to the top.
                _state.value = _state.value.copy(loading = false, error = null, series = series, seasons = seasons, selectedSeason = keep)
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
