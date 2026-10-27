package tv.roxy.tv.tv

import android.content.ContentUris
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Build
import androidx.tvprovider.media.tv.TvContractCompat
import androidx.tvprovider.media.tv.WatchNextProgram
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import okhttp3.Request
import timber.log.Timber
import tv.roxy.tv.BuildConfig
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.Item
import tv.roxy.tv.isTelevision
import java.io.File

/**
 * Android TV's "Watch Next" row on the launcher's Home (Netflix, Prime and Jellyfin
 * write there too): one row per title the profile is part-way through. The player
 * publishes when it pauses or stops, with the position and the clock as the sort key,
 * and removes the row when the title finishes. Thumbnails come from [ImageProvider],
 * since the launcher cannot reach the server; the file is written before the row.
 *
 * Row ids are kept per item in a small preferences file so later reports update the
 * same row instead of adding one. TV only: phones have no launcher row to feed.
 */
object WatchNext {
    private const val PREFS = "watchnext"
    private const val SCHEME = "roxytv"
    private val lock = Mutex()

    fun thumbDir(context: Context): File = File(context.cacheDir, "watchnext")
    fun thumbFile(context: Context, itemId: String): File = File(thumbDir(context), itemId.filter { it.isLetterOrDigit() || it == '-' } + ".jpg")

    private fun authority(): String = BuildConfig.APPLICATION_ID + ".images"

    /** What the launcher fires: `roxytv://play/<itemId>`, handled by MainActivity, which resumes the item. */
    fun resumeUri(itemId: String): Uri = Uri.parse("$SCHEME://play/$itemId")

    fun itemIdFrom(uri: Uri?): String? =
        uri?.takeIf { it.scheme == SCHEME && it.host == "play" }?.pathSegments?.firstOrNull()?.takeIf { it.isNotBlank() }

    private fun supported(context: Context): Boolean = Build.VERSION.SDK_INT >= 26 && isTelevision(context)

    /** Insert or update the row for [item] at [positionMs] of [durationMs]. Call off the main thread. */
    suspend fun publish(app: RoxyApp, item: Item, positionMs: Long, durationMs: Long) {
        if (!supported(app) || positionMs <= 0) return
        if (durationMs > 0 && positionMs >= durationMs * 0.97) { remove(app, item.id); return }
        withContext(Dispatchers.IO) {
            lock.withLock {
                runCatching {
                    val thumb = cacheThumb(app, item)
                    val series = item.isEpisode
                    val builder = WatchNextProgram.Builder()
                        .setType(if (series) TvContractCompat.PreviewPrograms.TYPE_TV_EPISODE else TvContractCompat.PreviewPrograms.TYPE_MOVIE)
                        .setWatchNextType(TvContractCompat.WatchNextPrograms.WATCH_NEXT_TYPE_CONTINUE)
                        .setTitle(if (series) item.seriesTitle ?: item.title else item.title)
                        .setIntentUri(Uri.parse(Intent(Intent.ACTION_VIEW, resumeUri(item.id)).setPackage(BuildConfig.APPLICATION_ID).toUri(Intent.URI_INTENT_SCHEME)))
                        .setInternalProviderId(item.id)
                        .setLastEngagementTimeUtcMillis(System.currentTimeMillis())
                        .setLastPlaybackPositionMillis(positionMs.toInt())
                        .setSearchable(true)
                        .setBrowsable(true)
                    if (durationMs > 0) builder.setDurationMillis(durationMs.toInt())
                    if (series) {
                        builder.setEpisodeTitle(item.title)
                        item.seasonAired?.let { builder.setSeasonNumber(it) }
                        (item.indexAired ?: item.indexAbsolute)?.let { builder.setEpisodeNumber(it) }
                    }
                    if (thumb != null) {
                        val uri = Uri.parse("content://${authority()}/items/${thumbFile(app, item.id).nameWithoutExtension}/thumb")
                        builder.setThumbnailUri(uri).setPosterArtUri(uri)
                    }
                    val values = builder.build().toContentValues()
                    val prefs = app.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
                    val existing = prefs.getLong(item.id, -1L)
                    val updated = existing >= 0 && app.contentResolver.update(TvContractCompat.buildWatchNextProgramUri(existing), values, null, null) > 0
                    if (!updated) {
                        val row = app.contentResolver.insert(TvContractCompat.WatchNextPrograms.CONTENT_URI, values)
                        val id = row?.let { ContentUris.parseId(it) } ?: -1L
                        prefs.edit().putLong(item.id, id).apply()
                        Timber.i("watch next: inserted %s row=%d pos=%dms", item.id, id, positionMs)
                    } else Timber.d("watch next: updated %s row=%d pos=%dms", item.id, existing, positionMs)
                }.onFailure { Timber.w("watch next publish failed: %s", it.message) }
            }
        }
    }

    /** The title finished (or was marked watched): its row goes. */
    suspend fun remove(app: RoxyApp, itemId: String) {
        if (!supported(app)) return
        withContext(Dispatchers.IO) {
            lock.withLock {
                val prefs = app.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
                val id = prefs.getLong(itemId, -1L)
                if (id < 0) return@withLock
                runCatching { app.contentResolver.delete(TvContractCompat.buildWatchNextProgramUri(id), null, null) }
                    .onFailure { Timber.w("watch next delete failed: %s", it.message) }
                prefs.edit().remove(itemId).apply()
                thumbFile(app, itemId).delete()
            }
        }
    }

    /** A 480-wide 16:9 JPEG of the item's still (or its backdrop) in the cache; null when the server has none. */
    private fun cacheThumb(app: RoxyApp, item: Item): File? {
        val url = app.api.imageUrl(item, if (item.isEpisode) "thumb" else "backdrop", 480) ?: app.api.imageUrl(item, "poster", 320) ?: return null
        val file = thumbFile(app, item.id)
        if (file.isFile && file.length() > 0 && System.currentTimeMillis() - file.lastModified() < 7L * 24 * 3600 * 1000) return file
        thumbDir(app).mkdirs()
        return runCatching {
            app.api.http.newCall(Request.Builder().url(url).build()).execute().use { res ->
                if (!res.isSuccessful) return null
                val tmp = File(file.path + ".part")
                tmp.outputStream().use { out -> res.body?.byteStream()?.copyTo(out) }
                if (!tmp.renameTo(file)) { file.delete(); tmp.renameTo(file) }
                file
            }
        }.onFailure { Timber.w("watch next thumb failed: %s", it.message) }.getOrNull()
    }
}
