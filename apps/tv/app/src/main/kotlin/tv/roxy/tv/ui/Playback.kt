package tv.roxy.tv.ui

import android.content.Context
import timber.log.Timber
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.Item
import tv.roxy.tv.player.PlayerActivity

/**
 * Starts [PlayerActivity] for an item. Fetches the item when it carries no `mediaSources`
 * (Home rows may omit them). Resumes from `userData.positionMs` unless [fromStart].
 */
suspend fun playItem(context: Context, item: Item, fromStart: Boolean = false): Result<Unit> {
    val app = RoxyApp.get(context)
    val full = if (item.mediaSources.isEmpty()) runCatching { app.api.item(item.id) }.getOrElse { return Result.failure(it) } else item
    val source = full.primarySource ?: return Result.failure(IllegalStateException("No media source for ${full.title}"))
    val position = if (fromStart) 0L else (full.userData ?: item.userData)?.positionMs ?: 0L
    val startSeconds = (position / 1000).toInt()
    val intent = PlayerActivity.intent(
        context = context,
        url = app.api.streamUrl(source.id),
        title = full.title.ifEmpty { full.displayLabel },
        itemId = full.id,
        sourceId = source.id,
        startSeconds = startSeconds,
        settings = app.prefs.settings.value,
        subtitleLine = when {
            full.isEpisode -> listOfNotNull(full.seriesTitle, full.episodeCode.takeIf { it.isNotEmpty() }).joinToString("  ")
            else -> full.year?.toString() ?: ""
        },
    )
    Timber.i("play %s item=%s source=%s start=%ds", full.displayLabel, full.id, source.id, startSeconds)
    context.startActivity(intent)
    return Result.success(Unit)
}
