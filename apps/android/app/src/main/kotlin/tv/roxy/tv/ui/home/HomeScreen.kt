package tv.roxy.tv.ui.home

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.cancel
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.api.ApiException
import tv.roxy.tv.api.HomeRow
import tv.roxy.tv.api.Item
import tv.roxy.tv.api.RoxyApi
import tv.roxy.tv.ui.detail.upNextOf

/** One slide of the phone's hero carousel: the series/movie to show, and what Play starts. */
data class HeroPick(val title: Item, val play: Item)

sealed interface HomeUiState {
    data object Loading : HomeUiState
    /** [offline]: the server did not answer at all (not on the home network / Tailscale), as opposed to an API error. */
    data class Error(val message: String, val unauthorized: Boolean = false, val offline: Boolean = false) : HomeUiState
    data class Ready(
        val hero: Item?,
        val rows: List<HomeRow>,
        val composedLocally: Boolean,
        val note: String? = null,
        /** Resolved carousel slides (phone); empty until [HomeViewModel.resolveHeroes] finishes. */
        val heroes: List<HeroPick> = emptyList(),
    ) : HomeUiState
}

class HomeViewModel(private val api: RoxyApi) : ViewModel() {
    private val _state = MutableStateFlow<HomeUiState>(HomeUiState.Loading)
    val state: StateFlow<HomeUiState> = _state

    private var loadJob: Job? = null

    fun load(silent: Boolean = false) {
        if (!silent) _state.value = HomeUiState.Loading
        loadJob?.cancel()
        loadJob = viewModelScope.launch {
            // A 3 s probe runs beside the real request so the offline state (and the way to
            // the downloads) shows up fast: a firewall that drops packets would otherwise
            // make the request sit out its full connect timeout.
            val probe = launch {
                if (!api.reachable() && _state.value !is HomeUiState.Ready) {
                    Timber.w("server unreachable; showing the offline state")
                    _state.value = HomeUiState.Error("Server unreachable", offline = true)
                    this@launch.cancel()
                }
            }
            val result = runCatching { fromHomeEndpoint() }
                .recoverCatching { t ->
                    if (t is ApiException && t.isNotFound) {
                        Timber.w("GET /v1/home is 404: composing rows from /v1/items instead")
                        composeRows()
                    } else throw t
                }
            probe.cancel()
            result.onSuccess { ready ->
                // Keep the previous carousel while the new one resolves, so a silent reload does not blank it.
                val previous = (_state.value as? HomeUiState.Ready)?.heroes.orEmpty()
                _state.value = ready.copy(heroes = previous)
                _state.value = ready.copy(heroes = resolveHeroes(ready.rows))
            }.onFailure { t ->
                if (t is CancellationException) return@onFailure // the probe already decided
                Timber.e(t, "home failed")
                val unauthorized = t is ApiException && t.isUnauthorized
                // A silent reload (back from the player) that fails keeps the rows on screen.
                if (silent && !unauthorized && _state.value is HomeUiState.Ready) return@onFailure
                val offline = t !is ApiException && !api.reachable()
                _state.value = HomeUiState.Error(t.message ?: t::class.simpleName.orEmpty(), unauthorized, offline)
            }
        }
    }

    private suspend fun fromHomeEndpoint(): HomeUiState.Ready {
        val rows = api.home().rows.filter { it.items.isNotEmpty() }
        return HomeUiState.Ready(hero = pickHero(rows), rows = rows, composedLocally = false)
    }

    /**
     * Carousel selection: what you are in the middle of first (one slide per show), then
     * the newest arrivals (one per series, then movies), then the library's series, up to
     * [MAX_HEROES]. Episodes are resolved to their series so the slide can show the poster
     * and logo; the episode stays as the thing Play starts.
     */
    private suspend fun resolveHeroes(rows: List<HomeRow>): List<HeroPick> {
        val picks = LinkedHashMap<String, Pair<Item, Boolean>>() // show key -> (item to play, from Continue Watching)
        fun consider(items: List<Item>, continuing: Boolean) {
            for (it in items) {
                if (picks.size >= MAX_HEROES) return
                val key = if (it.isEpisode) (it.seriesId ?: it.id) else it.id
                picks.putIfAbsent(key, it to continuing)
            }
        }
        listOf("continue", "recentEpisodes", "recentMovies", "series").forEach { key ->
            consider(rows.firstOrNull { it.key == key }?.items.orEmpty(), continuing = key == "continue")
        }
        return coroutineScope {
            picks.values.map { (picked, continuing) ->
                async {
                    val title = if (picked.isEpisode) {
                        picked.seriesId?.let { id -> runCatching { api.item(id) }.getOrNull() } ?: return@async null
                    } else picked
                    // A newly added episode is just what arrived last (a season's E12): Play
                    // starts where the title page's Play would, the show's next episode.
                    val play = if (picked.isEpisode && !continuing) {
                        runCatching { api.items(type = "episode", parent = title.id, recursive = true, sort = "index").items }
                            .onFailure { Timber.w(it, "episodes for hero up next") }.getOrNull()
                            ?.let { upNextOf(it) } ?: picked
                    } else picked
                    HeroPick(title, play)
                }
            }.awaitAll().filterNotNull()
        }
    }

    private companion object { const val MAX_HEROES = 6 }

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
