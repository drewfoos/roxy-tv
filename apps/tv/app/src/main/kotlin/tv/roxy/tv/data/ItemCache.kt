package tv.roxy.tv.data

import android.util.LruCache
import tv.roxy.tv.api.Item

/**
 * The last few hundred items any list on screen has shown, by id. A detail page seeds its
 * state from here so the header (poster, title, synopsis, genres) is on screen the moment
 * the page slides in, instead of a skeleton; the real fetch then replaces it.
 */
class ItemCache {
    private val items = LruCache<String, Item>(400)

    fun put(item: Item) {
        // A stub (a season's parent series made up from its id and title) would render an
        // empty header, worse than the skeleton; only remember items that carry artwork.
        if (item.images.isEmpty()) return
        items.put(item.id, item)
    }

    fun putAll(list: List<Item>) = list.forEach { put(it) }

    operator fun get(id: String): Item? = items.get(id)
}
