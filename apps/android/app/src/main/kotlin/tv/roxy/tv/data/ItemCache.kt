package tv.roxy.tv.data

import android.util.LruCache
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import tv.roxy.tv.api.Item

/**
 * The last few hundred items any list on screen has shown, by id. A detail page seeds its
 * state from here so the header (poster, title, synopsis, genres) is on screen the moment
 * the page slides in, instead of a skeleton; the real fetch then replaces it.
 */
class ItemCache {
    private val items = LruCache<String, Item>(400)

    /**
     * Bumped when something about the library changed outside a list's own refresh (an
     * admin picked new art). Lists compare it with what they loaded against and refetch;
     * the cache is emptied so detail pages re-fetch too.
     */
    @Volatile var version = 0L; private set

    fun touch() { version++; items.evictAll() }

    private val _changes = MutableStateFlow(0L)
    /** Bumped when watched state or the Watchlist changed; screens on show reload (see OnUserDataChanged). */
    val changes: StateFlow<Long> get() = _changes

    /** Watched / Watchlist changed: lists refetch (version), detail pages too (cache emptied), and screens on show reload now. */
    fun userDataChanged() { touch(); _changes.value++ }

    fun put(item: Item) {
        // A stub (a season's parent series made up from its id and title) would render an
        // empty header, worse than the skeleton; only remember items that carry artwork.
        if (item.images.isEmpty()) return
        items.put(item.id, item)
    }

    fun putAll(list: List<Item>) = list.forEach { put(it) }

    operator fun get(id: String): Item? = items.get(id)
}
