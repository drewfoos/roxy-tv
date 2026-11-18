package tv.roxy.tv.data

import tv.roxy.tv.api.MediaSource

/**
 * The file facts a play request has just read from the server with its item (tracks,
 * subtitle languages, skippable segments), kept for the player that opens next. The
 * player asked the server for the same facts again and held the file back until the
 * answer came: one round trip before every start, for what was already in hand.
 *
 * Only for a moment ([MAX_AGE_MS]) and only once: a next episode looked up at the start
 * of this one opens half an hour later, when the server may know more about it (an intro
 * found since), so that player asks as before.
 *
 * Age is counted on a clock that only runs forward ([System.nanoTime]), not the time of
 * day: a clock set while the player opens (network time, a time zone) must not age the
 * facts out, or keep them past their moment.
 */
class FetchedSources(private val now: () -> Long = { System.nanoTime() / 1_000_000 }) {
    private val held = HashMap<String, Pair<MediaSource, Long>>()

    /** [source] as the server gave it just now. */
    @Synchronized fun put(source: MediaSource) {
        val at = now()
        held.values.removeAll { at - it.second > MAX_AGE_MS }
        held[source.id] = source to at
    }

    /** The facts for [sourceId] when they were read a moment ago; null means ask the server. */
    @Synchronized fun take(sourceId: String): MediaSource? {
        val (source, at) = held.remove(sourceId) ?: return null
        val age = now() - at
        return source.takeIf { age in 0..MAX_AGE_MS }
    }

    companion object {
        const val MAX_AGE_MS = 10_000L
    }
}
