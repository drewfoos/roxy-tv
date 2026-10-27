package tv.roxy.tv.ui.detail

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.api.Item
import tv.roxy.tv.api.RoxyApi

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
