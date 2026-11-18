package tv.roxy.tv.ui.detail

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.api.Item
import tv.roxy.tv.api.RoxyApi

data class MovieUiState(val loading: Boolean = true, val error: String? = null, val movie: Item? = null, val partOf: List<Item> = emptyList(), val extras: List<Item> = emptyList())

/**
 * What a title's page read last time, kept while the app runs (the last 40 titles): the
 * page opens with it in place and reads again behind it, so what was there a minute ago
 * does not arrive a moment after the page a second time.
 */
internal class PageMemory<T : Any>(private val keep: Int = 40) {
    private val map = object : LinkedHashMap<String, T>(32, 0.75f, true) {
        override fun removeEldestEntry(eldest: MutableMap.MutableEntry<String, T>?) = size > keep
    }
    @Synchronized fun get(id: String): T? = map[id]
    @Synchronized fun put(id: String, value: T) { map[id] = value }
    @Synchronized fun remove(id: String) { map.remove(id) }
    @Synchronized fun clear() = map.clear()
}

/** A film's relations and extras as last read ([PageMemory]). */
internal data class MovieSides(val partOf: List<Item> = emptyList(), val extras: List<Item> = emptyList())
internal val movieSides = PageMemory<MovieSides>()

class MovieViewModel(private val api: RoxyApi, private val movieId: String, initial: Item? = null) : ViewModel() {
    private val _state = MutableStateFlow((movieSides.get(movieId) ?: MovieSides()).let { MovieUiState(movie = initial, partOf = it.partOf, extras = it.extras) })
    val state: StateFlow<MovieUiState> = _state
    private var relatedLoaded = false

    fun load() {
        viewModelScope.launch {
            runCatching { api.item(movieId) }
                .onSuccess { _state.value = MovieUiState(loading = false, movie = it, partOf = _state.value.partOf, extras = _state.value.extras) }
                .onFailure { t ->
                    Timber.e(t, "movie load failed")
                    // A failed reload keeps the page; only a first load shows the error.
                    _state.value = if (_state.value.movie != null && !_state.value.loading) _state.value
                    else MovieUiState(loading = false, error = tv.roxy.tv.data.friendlyError(t, "this movie"))
                }
            // The series this movie belongs to (AniList relations), if it is in the library:
            // once per page, not on every return from the player.
            if (relatedLoaded) return@launch
            runCatching { api.related(movieId).partOf }
                .onSuccess { p -> relatedLoaded = true; _state.value = _state.value.copy(partOf = p) }
                .onFailure { Timber.w(it, "related load failed") }
            // Trailers, featurettes and the like (an older server has none to list).
            runCatching { api.extras(movieId) }
                .onSuccess { x -> _state.value = _state.value.copy(extras = x) }
                .onFailure { Timber.w(it, "extras load failed") }
            movieSides.put(movieId, MovieSides(_state.value.partOf, _state.value.extras))
        }
    }
}
