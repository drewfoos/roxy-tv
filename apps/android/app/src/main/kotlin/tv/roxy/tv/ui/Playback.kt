package tv.roxy.tv.ui

import android.content.Context
import kotlinx.coroutines.async
import tv.roxy.tv.api.RemoteCommand
import android.content.Intent
import android.app.Activity
import timber.log.Timber
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.Item
import tv.roxy.tv.player.PlayerActivity

/** Everything the player needs to start one item: the item, where its bytes are, and where to begin. */
data class PlayRequest(
    val item: Item,
    val url: String,
    val sourceId: String,
    val startSeconds: Int,
    /**
     * The requester chose [startSeconds] (Play from start, a "Play on" handover), so a
     * player that already has this item open jumps there. Otherwise the position is only
     * the saved one, and an open player keeps its own (fresher) place.
     */
    val explicitStart: Boolean = false,
    /** `url` is a downloaded file; segments and sniffed subtitle languages come from the download snapshot. */
    val local: Boolean,
) {
    val title: String get() = item.title.ifEmpty { item.displayLabel }
    val subtitleLine: String
        get() = when {
            item.isEpisode -> listOfNotNull(item.seriesTitle, item.episodeCode.takeIf { it.isNotEmpty() }).joinToString("  ")
            else -> item.year?.toString() ?: ""
        }
}

/**
 * Resolves an item into a [PlayRequest]. A finished download plays from disk (Netflix
 * behaviour: the local copy wins even on the home network), with the snapshot taken at
 * download time standing in for the server. Otherwise the item is fetched when it carries
 * no `mediaSources` (Home rows may omit them). Resumes from `userData.positionMs` unless
 * [fromStart] or the item already counts as watched.
 *
 * The position comes from the server whenever it answers: the [item] handed in may be a
 * cached copy (a row loaded an hour ago, another device watched since), so its userData is
 * only a fallback. A download asks briefly and plays from its snapshot when the server is
 * away; a position recorded offline and not yet sent is fresher than the server's.
 */
suspend fun playRequest(context: Context, item: Item, fromStart: Boolean = false, startMs: Long? = null): Result<PlayRequest> {
    val app = RoxyApp.get(context)
    val local = app.downloads.localFile(item.id)
    val entry = if (local != null) app.downloads.entry(item.id) else null
    val explicit = fromStart || startMs != null
    // The server's current copy, when it is needed and reachable. A download never waits long
    // for it: offline playback must start at once.
    val fresh: Item? = when {
        entry != null -> if (explicit || app.downloads.hasPendingProgress(item.id) || !app.prefs.session.value.isLoggedIn) null
            else {
                // The HTTP call blocks its thread and ignores cancellation, so it runs apart
                // and only the wait for it is cut short.
                val call = app.scope.async(kotlinx.coroutines.Dispatchers.IO) { runCatching { app.api.item(item.id) }.getOrNull() }
                kotlinx.coroutines.withTimeoutOrNull(3_000) { call.await() }
            }
        item.mediaSources.isEmpty() -> runCatching { app.api.item(item.id) }.getOrElse { return Result.failure(it) }
        else -> runCatching { app.api.item(item.id) }.onFailure { if (it is kotlinx.coroutines.CancellationException) throw it }.getOrNull()
    }
    val full = when {
        // The snapshot's sources play from disk; the server's watch state (position,
        // subtitle choice) replaces the snapshot's when it answered.
        entry != null -> fresh?.userData?.let { entry.item.copy(userData = it) } ?: entry.item
        else -> fresh ?: item
    }
    val source = full.primarySource ?: return Result.failure(IllegalStateException("No media source for ${full.title}"))
    // Where to start, from the newest information at hand, never the largest number: a
    // rewind or a restart is a smaller position that is still the latest. In order: the
    // server's watch state when it answered; for a download without it, the newest report
    // recorded offline and not yet sent, then the download's snapshot (updated by every
    // local playback); the passed item last, as it may be an old cached copy. Taking the
    // maximum of item and snapshot resumed at 45 min after a restart watched to 5 min.
    val pending = if (entry != null && fresh == null) app.downloads.latestPendingProgress(item.id) else null
    val snapshot = if (entry != null) entry.item.userData else null
    val watched: Boolean
    val position: Long
    when {
        fresh != null -> { watched = fresh.userData?.played == true; position = fresh.userData?.positionMs ?: 0L }
        pending != null -> { watched = pending.played; position = if (pending.played) 0L else pending.positionMs }
        snapshot != null -> { watched = snapshot.played; position = snapshot.positionMs }
        else -> { watched = item.userData?.played == true; position = item.userData?.positionMs ?: 0L }
    }
    val startAt = when {
        fromStart -> 0L
        startMs != null -> startMs
        else -> position
    }
    val start = if (watched && startAt == 0L) 0 else (startAt / 1000).toInt()
    val url = local?.path ?: runCatching { app.api.streamUrl(source.id, app.api.mediaToken(source.id)) }.getOrElse { return Result.failure(it) }
    return Result.success(PlayRequest(item = full, url = url, sourceId = source.id, startSeconds = start, explicitStart = fromStart || startMs != null, local = local != null))
}

/** Starts [PlayerActivity] for an item (see [playRequest]). */
suspend fun playItem(context: Context, item: Item, fromStart: Boolean = false, startMs: Long? = null): Result<Unit> {
    val app = RoxyApp.get(context)
    // A target device is set ("Play on"): the play happens there, this device stays a remote.
    app.remote.target.value?.let { t ->
        if (fromStart) runCatching { app.api.progress(item.id, 0) }
        val ok = app.remote.send(RemoteCommand(type = "play", itemId = item.id, positionMs = startMs ?: 0, fromStart = fromStart))
        return if (ok) Result.success(Unit) else Result.failure(IllegalStateException("${t.name} is not connected any more"))
    }
    // The phone has one player, the page's; the TV keeps the full-screen activity.
    // The explicit start goes along: a "Play on" handover is acknowledged only once the
    // player is at the position the sender asked for.
    app.openPlayerPage?.let { open -> open(item, fromStart, startMs); return Result.success(Unit) }
    val req = playRequest(context, item, fromStart, startMs).getOrElse { t ->
        // Server away: say so, rather than a Play button that does nothing.
        if (t !is kotlinx.coroutines.CancellationException) kotlinx.coroutines.withContext(kotlinx.coroutines.Dispatchers.Main) {
            android.widget.Toast.makeText(context.applicationContext, tv.roxy.tv.data.friendlyError(t), android.widget.Toast.LENGTH_LONG).show()
        }
        return Result.failure(t)
    }
    Timber.i("play %s item=%s source=%s start=%ds local=%s", req.item.displayLabel, req.item.id, req.sourceId, req.startSeconds, req.local)
    val intent = PlayerActivity.intent(context, req, app.prefs.settings.value)
    if (context !is Activity) intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK) // started from the command poll, not a screen
    context.startActivity(intent)
    return Result.success(Unit)
}

/**
 * The episode after [itemId] in its series (season order, specials last), as a
 * [PlayRequest], or null for movies, the last episode, or when nothing playable follows.
 * Online it asks the server for the series' episodes; away from it, the downloads of the
 * same show are the list, and only downloaded episodes count.
 */
suspend fun nextEpisode(context: Context, itemId: String): PlayRequest? {
    val app = RoxyApp.get(context)
    val current = app.downloads.entry(itemId)?.item ?: runCatching { app.api.item(itemId) }.getOrNull() ?: return null
    if (!current.isEpisode) return null
    val seriesId = current.seriesId ?: return null
    val online = runCatching { app.api.items(type = "episode", parent = seriesId, recursive = true, includeSources = true, sort = "index").items }.getOrNull()
    val episodes = online ?: app.downloads.index.value.entries.values.filter { it.titleId == seriesId && it.isDone }.map { it.item }
    fun seasonKey(e: Item) = (e.seasonAired ?: 0).let { if (it == 0) Int.MAX_VALUE else it }
    val ordered = episodes.sortedWith(compareBy({ seasonKey(it) }, { it.indexAired ?: it.indexAbsolute ?: 0 }))
    val at = ordered.indexOfFirst { it.id == current.id }
    if (at < 0) return null
    val next = ordered.drop(at + 1).firstOrNull { it.isPlayable && (online != null || app.downloads.localFile(it.id) != null) } ?: return null
    return playRequest(context, next).getOrNull()
}
