package tv.roxy.tv.data.downloads

import tv.roxy.tv.api.Item

/*
 * What the phone shows from its downloads alone, with the server out of reach: the episode
 * a show's Play button starts, and a Continue Watching row. Both read the item snapshots in
 * the index, which offline playback keeps current (see Downloads.recordProgress).
 */

/** The download a show's (or film's) Play button starts, and the button's words. */
data class UpNext(val entry: DownloadEntry, val label: String)

/** When the item was last played, in ms; 0 when never or unreadable. */
internal fun playedAtMs(item: Item): Long =
    item.userData?.lastPlayedAt?.let { runCatching { java.time.OffsetDateTime.parse(it).toInstant().toEpochMilli() }.getOrNull() } ?: 0L

/** "S1 E3", "Special 2", or "" for a film. */
internal fun shortEpisode(item: Item): String {
    if (!item.isEpisode) return ""
    val n = item.indexAired ?: item.indexAbsolute
    val season = item.seasonAired ?: 0
    return when {
        season == 0 -> if (n != null) "Special $n" else "Special"
        n != null -> "S$season E$n"
        else -> "Season $season"
    }
}

/** Play order for downloads: by season with the specials last, then by number. */
private fun inOrder(entries: List<DownloadEntry>): List<DownloadEntry> =
    entries.sortedWith(compareBy({ (it.item.seasonAired ?: it.seasonNumber ?: 0).let { s -> if (s == 0) Int.MAX_VALUE else s } }, { it.item.indexAired ?: it.item.indexAbsolute ?: 0 }))

/**
 * Of one title's downloads, the one to play next, among those finished downloading:
 * the episode stopped part-way (the latest played, when there are several), else the first
 * unwatched one after the last watched, else the first unwatched, else (all watched) the
 * first again. Null when nothing has finished downloading.
 */
fun upNextOf(entries: List<DownloadEntry>): UpNext? {
    val done = inOrder(entries.filter { it.isDone })
    if (done.isEmpty()) return null
    fun words(verb: String, e: DownloadEntry) = listOf(verb, shortEpisode(e.item)).filter { it.isNotEmpty() }.joinToString(" ")
    val partWay = done.filter { it.positionMs > 0 }
    if (partWay.isNotEmpty()) {
        // maxBy keeps the first of equals, so with no dates the earliest episode wins.
        val e = partWay.maxBy { playedAtMs(it.item) }
        return UpNext(e, words("Resume", e))
    }
    val lastWatched = done.indexOfLast { it.item.isWatched }
    val next = done.drop(lastWatched + 1).firstOrNull { !it.item.isWatched } ?: done.firstOrNull { !it.item.isWatched }
    if (next != null) return UpNext(next, words("Play", next))
    return UpNext(done.first(), "Watch again")
}

/**
 * Continue Watching from the downloads: for each title that was started and is not
 * finished (something part-way, or something watched with an unwatched download left), its
 * [upNextOf], the title played latest first.
 */
fun continueFromDownloads(titles: List<DownloadedTitle>): List<DownloadEntry> =
    titles.mapNotNull { t ->
        val done = t.entries.filter { it.isDone }
        val started = done.any { it.positionMs > 0 || it.item.isWatched }
        val left = done.any { !it.item.isWatched }
        if (!started || !left) return@mapNotNull null
        val next = upNextOf(done)?.entry ?: return@mapNotNull null
        next to done.maxOf { playedAtMs(it.item) }
    }.sortedByDescending { it.second }.map { it.first }
