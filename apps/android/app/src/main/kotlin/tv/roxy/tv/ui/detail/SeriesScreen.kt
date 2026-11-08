package tv.roxy.tv.ui.detail

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.api.Item
import tv.roxy.tv.api.ItemsResponse
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
    /**
     * The shown entry's list could not be read whole (episodeRows): the screens say "Some
     * episodes could not be loaded (N of M)" with a retry, instead of a partial list passing
     * for the season. [episodesExpected] is M.
     */
    val episodesIncomplete: Boolean = false,
    val episodesExpected: Int = 0,
    /**
     * The list could not be read at all (a failed request): the screens say "Couldn't load
     * the episodes" with a retry. Three states: loaded, part loaded ([episodesIncomplete]),
     * failed (this); see withEpisodes.
     */
    val episodesError: String? = null,
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
    /** Rows that do not show their own episode number (see [rowNumbersFor]): id -> number shown, null for none. */
    val rowNumbers: Map<String, Int?> = emptyMap(),
)

/** The number a row shows: [SeriesUiState.rowNumbers] when it says, else the episode's own. */
fun SeriesUiState.rowNumber(ep: Item): Int? = if (rowNumbers.containsKey(ep.id)) rowNumbers[ep.id] else ep.indexAired ?: ep.indexAbsolute

/**
 * Rows numbered other than by their own episode number: a group of specials by place (1,
 * 2, 3: the TheTVDB numbers files carry, KonoSuba's 3 to 7, mean nothing on screen), a
 * film's row not at all. A season's rows keep their own numbers (empty map).
 */
fun rowNumbersFor(source: SectionSource?, rows: List<Item>): Map<String, Int?> = when (source) {
    is SectionSource.Specials -> rows.mapIndexed { i, ep -> ep.id to (i + 1) }.toMap()
    is SectionSource.Movie -> rows.associate { it.id to null }
    null -> emptyMap()
}

/** An entry's rows, how many it should have, and whether they are all there. */
data class EpisodeRows(val items: List<Item>, val expected: Int, val complete: Boolean)

/**
 * The rows of an entry from the episodes read for it: a season's (or a flat show's) in
 * order, as complete as the read was; a group of specials, the ones the section names, in
 * its order, complete only when the read was and every named one came (an id missing from
 * the read would otherwise just not be there, the group shorter with nothing said).
 */
fun episodeRows(source: SectionSource?, read: ItemsResponse): EpisodeRows = when (source) {
    is SectionSource.Specials -> {
        val byId = read.items.associateBy { it.id }
        val rows = source.episodeIds.mapNotNull { byId[it] }
        EpisodeRows(rows, source.episodeIds.size, read.complete && rows.size == source.episodeIds.size)
    }
    is SectionSource.Movie -> EpisodeRows(read.items, 1, read.complete && read.items.isNotEmpty())
    null -> EpisodeRows(
        read.items.sortedWith(compareBy({ it.seasonAired ?: 0 }, { it.indexAired ?: it.indexAbsolute ?: 0 })),
        maxOf(read.total, read.items.size), read.complete,
    )
}

/**
 * The page with an entry's list as it came: loaded (the rows), part loaded (the rows there
 * are, incomplete, of how many), or failed (no rows: the last entry's must not stay under
 * this one's name; an error to retry). A failed read used to leave the screen as it was,
 * with nothing to say it had failed.
 */
fun SeriesUiState.withEpisodes(result: Result<EpisodeRows>, numbers: (List<Item>) -> Map<String, Int?>): SeriesUiState =
    result.fold(
        onSuccess = { rows ->
            copy(
                episodes = rows.items, episodesLoading = false, rowNumbers = numbers(rows.items),
                episodesIncomplete = !rows.complete, episodesExpected = rows.expected, episodesError = null,
            )
        },
        onFailure = { t ->
            copy(
                episodes = emptyList(), episodesLoading = false, rowNumbers = emptyMap(),
                episodesIncomplete = false, episodesExpected = 0,
                episodesError = tv.roxy.tv.data.friendlyError(t, "the episodes"),
            )
        },
    )

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
     * The show's next episode (the server's /next), for [SeriesUiState.upNext]. On the first load the page then
     * opens on that episode's season (Crunchyroll), so Play and the list agree.
     */
    private fun loadUpNext(seasons: List<Item>) {
        val opening = firstLoad
        firstLoad = false
        viewModelScope.launch {
            // The server works it out: the page used to load every episode of the show for it.
            val next = runCatching { api.next(seriesId) }.onFailure { Timber.w(it, "up next") }.getOrNull()
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

    /** The incomplete-list message's Retry: the whole page again (sections too, which name the specials). */
    fun retryEpisodes() = load()

    private fun loadEpisodes(parentId: String, recursive: Boolean = false) {
        _state.value = _state.value.copy(episodesLoading = true, episodesIncomplete = false, episodesError = null)
        // Only the latest request counts: two quick season picks cannot land out of order.
        episodesJob?.cancel()
        val source = sources[parentId]
        episodesJob = viewModelScope.launch {
            val started = android.os.SystemClock.elapsedRealtime()
            runCatching {
                // The read as it came (complete or not): episodeRows makes the rows of it.
                val read = when (source) {
                    // A film of the franchise: its one row (with its file, to play and download).
                    is SectionSource.Movie -> api.item(source.item.id).let { ItemsResponse(listOf(it), 1) }
                    // Some of the Specials season: episodeRows picks the section's, in its order.
                    is SectionSource.Specials -> api.allItems(type = "episode", parent = source.seasonId, includeSources = true, sort = "index")
                    null -> api.allItems(type = "episode", parent = parentId, recursive = recursive, includeSources = true, sort = "index")
                }
                episodeRows(source, read)
            }
                .onSuccess { rows ->
                    // Read by ops' tv-measure.sh (load time of a long list on the device).
                    Timber.i("episodes: %d loaded in %d ms", rows.items.size, android.os.SystemClock.elapsedRealtime() - started)
                    if (!rows.complete) Timber.w("episodes: %d of %d loaded; the list is incomplete", rows.items.size, rows.expected)
                }
                .onFailure { t ->
                    if (t is kotlinx.coroutines.CancellationException) throw t
                    Timber.e(t, "episodes load failed")
                }
                .let { result -> _state.value = _state.value.withEpisodes(result) { eps -> rowNumbersFor(source, eps) } }
        }
    }
}
