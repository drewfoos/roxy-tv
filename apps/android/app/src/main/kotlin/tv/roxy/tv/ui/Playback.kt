package tv.roxy.tv.ui

import android.content.Context
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
 */
suspend fun playRequest(context: Context, item: Item, fromStart: Boolean = false): Result<PlayRequest> {
    val app = RoxyApp.get(context)
    val local = app.downloads.localFile(item.id)
    val entry = if (local != null) app.downloads.entry(item.id) else null
    val full = when {
        entry != null -> entry.item
        item.mediaSources.isEmpty() -> runCatching { app.api.item(item.id) }.getOrElse { return Result.failure(it) }
        else -> item
    }
    val source = full.primarySource ?: return Result.failure(IllegalStateException("No media source for ${full.title}"))
    // Freshest position wins: the server's when we have it, else the snapshot's (updated by offline playback).
    val watched = (full.userData?.played ?: item.userData?.played) == true
    val position = if (fromStart) 0L else listOfNotNull(item.userData?.positionMs, full.userData?.positionMs).maxOrNull() ?: 0L
    val start = if (watched && position == 0L) 0 else (position / 1000).toInt()
    val url = local?.path ?: runCatching { app.api.streamUrl(source.id, app.api.mediaToken(source.id)) }.getOrElse { return Result.failure(it) }
    return Result.success(PlayRequest(item = full, url = url, sourceId = source.id, startSeconds = start, local = local != null))
}

/** Starts [PlayerActivity] for an item (see [playRequest]). */
suspend fun playItem(context: Context, item: Item, fromStart: Boolean = false): Result<Unit> {
    val app = RoxyApp.get(context)
    // A target device is set ("Play on"): the play happens there, this device stays a remote.
    app.remote.target.value?.let { t ->
        if (fromStart) runCatching { app.api.progress(item.id, 0) }
        val ok = app.remote.send(RemoteCommand(type = "play", itemId = item.id))
        return if (ok) Result.success(Unit) else Result.failure(IllegalStateException("${t.name} is not connected any more"))
    }
    // The phone has one player, the page's; the TV keeps the full-screen activity.
    app.openPlayerPage?.let { open -> open(item); return Result.success(Unit) }
    val req = playRequest(context, item, fromStart).getOrElse { t ->
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
