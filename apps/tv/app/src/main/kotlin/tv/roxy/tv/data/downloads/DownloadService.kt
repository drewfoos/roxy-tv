package tv.roxy.tv.data.downloads

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.IBinder
import androidx.core.app.NotificationCompat
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch
import okhttp3.Request
import timber.log.Timber
import tv.roxy.tv.MainActivity
import tv.roxy.tv.R
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.Item
import java.io.File
import java.io.IOException
import java.io.RandomAccessFile

/**
 * Foreground service that works through the download queue one video at a time (like
 * Netflix), resuming from the bytes already on disk with HTTP ranges. Small things first
 * (poster, backdrop, logo, episode thumb, sidecar subtitles), then the video. It exits
 * when nothing is queued; [Downloads.kick] starts it again when something is.
 *
 * Progress and state go through [Downloads.update]; the service never owns state. A
 * pause or delete made in the UI is noticed between chunks.
 */
class DownloadService : Service() {

    companion object {
        private const val ACTION_START = "tv.roxy.tv.downloads.START"
        private const val CHANNEL = "downloads"
        private const val NOTIFICATION_ID = 41
        private const val NOTIFY_INTERVAL_MS = 1000L
        private const val PROGRESS_INTERVAL_MS = 400L

        fun startIntent(context: Context): Intent = Intent(context, DownloadService::class.java).setAction(ACTION_START)
    }

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private var worker: Job? = null
    private lateinit var app: RoxyApp
    private lateinit var downloads: Downloads

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onCreate() {
        super.onCreate()
        app = RoxyApp.get(this)
        downloads = app.downloads
        createChannel()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        startInForeground(notification("Preparing downloads", null, 0f, indeterminate = true))
        if (worker?.isActive != true) worker = scope.launch { runQueue() }
        return START_NOT_STICKY
    }

    override fun onTimeout(startId: Int, fgsType: Int) {
        // Android 15 caps data-sync foreground time; stop cleanly and let the next kick resume.
        Timber.w("download service timed out; stopping")
        worker?.cancel()
        stopSelf()
    }

    override fun onDestroy() {
        scope.cancel()
        super.onDestroy()
    }

    private suspend fun runQueue() {
        try {
            while (true) {
                val next = downloads.nextToFetch() ?: break
                fetch(next)
            }
        } finally {
            stopForeground(STOP_FOREGROUND_REMOVE)
            stopSelf()
        }
    }

    private fun startInForeground(n: Notification) {
        if (Build.VERSION.SDK_INT >= 29) startForeground(NOTIFICATION_ID, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC)
        else startForeground(NOTIFICATION_ID, n)
    }

    /** Current state of [itemId] in the index; null when it was deleted. */
    private fun live(itemId: String): DownloadEntry? = downloads.entry(itemId)

    private suspend fun fetch(start: DownloadEntry) {
        val itemId = start.itemId
        downloads.update(itemId) { it.copy(state = DownloadState.Downloading, error = null) }
        val dir = downloads.dir(itemId).apply { mkdirs() }
        val label = start.item.let { if (it.isEpisode) listOfNotNull(it.seriesTitle, it.episodeCode).joinToString(" ") else it.title }
        notify(label, start.item, start.fraction)

        // Extras first: they are small and make the Downloads page complete even if the video stops midway.
        fetchImages(start)
        fetchSidecars(start, dir)

        val part = downloads.partFileFor(start)
        val final = downloads.videoFile(start)
        if (final.exists() && start.bytesTotal > 0 && final.length() == start.bytesTotal) {
            downloads.update(itemId) { it.copy(state = DownloadState.Done, bytesDone = it.bytesTotal, completedAt = System.currentTimeMillis()) }
            return
        }
        var offset = part.length()
        val url = app.api.streamUrl(start.sourceId)
        val req = Request.Builder().url(url).apply { if (offset > 0) header("Range", "bytes=$offset-") }.build()
        try {
            app.api.http.newCall(req).execute().use { resp ->
                if (resp.code == 416) { // our offset is past the end: the file changed on the server
                    part.delete(); offset = 0
                    throw IOException("range rejected; restarting")
                }
                if (!resp.isSuccessful) throw IOException("HTTP ${resp.code}")
                if (resp.code == 200 && offset > 0) { part.delete(); offset = 0 } // server ignored the range
                val total = when {
                    resp.code == 206 -> resp.header("Content-Range")?.substringAfter('/')?.toLongOrNull() ?: (offset + (resp.body.contentLength().takeIf { it >= 0 } ?: 0))
                    else -> resp.body.contentLength().takeIf { it >= 0 } ?: start.bytesTotal
                }
                if (total > 0) downloads.update(itemId) { it.copy(bytesTotal = total) }
                if (total > 0 && downloads.freeBytes() < total - offset + (64L shl 20)) throw IOException("Not enough free space")

                RandomAccessFile(part, "rw").use { out ->
                    out.seek(offset)
                    val buf = ByteArray(256 * 1024)
                    var done = offset
                    var lastProgress = 0L
                    var lastNotify = 0L
                    resp.body.byteStream().use { input ->
                        while (true) {
                            val n = input.read(buf)
                            if (n < 0) break
                            out.write(buf, 0, n)
                            done += n
                            val now = System.currentTimeMillis()
                            if (now - lastProgress >= PROGRESS_INTERVAL_MS) {
                                lastProgress = now
                                val cur = live(itemId)
                                if (cur == null) { Timber.i("download %s deleted while running", itemId); return }
                                if (cur.state != DownloadState.Downloading) { Timber.i("download %s %s", itemId, cur.state); return }
                                downloads.update(itemId) { it.copy(bytesDone = done) }
                                if (now - lastNotify >= NOTIFY_INTERVAL_MS) { lastNotify = now; notify(label, start.item, if (total > 0) done.toFloat() / total else 0f) }
                            }
                        }
                    }
                    if (total > 0 && done != total) throw IOException("Stream ended early ($done of $total bytes)")
                }
            }
            if (live(itemId) == null) { dir.deleteRecursively(); return }
            if (!part.renameTo(final)) throw IOException("Could not move the finished file into place")
            downloads.update(itemId) { it.copy(state = DownloadState.Done, bytesDone = final.length(), bytesTotal = final.length(), completedAt = System.currentTimeMillis()) }
            Timber.i("downloaded %s (%s)", label, formatBytes(final.length()))
        } catch (t: Throwable) {
            if (t is kotlinx.coroutines.CancellationException) {
                downloads.update(itemId) { if (it.state == DownloadState.Downloading) it.copy(state = DownloadState.Queued, bytesDone = part.length()) else it }
                throw t
            }
            val cur = live(itemId) ?: run { dir.deleteRecursively(); return }
            if (cur.state != DownloadState.Downloading) return // paused or deleted mid-stream
            val reachable = app.api.reachable()
            if (!reachable) {
                // Lost the server: keep the bytes, put it back in the queue and stop; the
                // network callback or the next app start kicks the queue again.
                Timber.w("download %s interrupted (%s); server unreachable, will resume later", label, t.message)
                downloads.update(itemId) { it.copy(state = DownloadState.Queued, bytesDone = part.length()) }
                throw IOException("server unreachable")
            }
            Timber.w(t, "download %s failed", label)
            downloads.update(itemId) { it.copy(state = DownloadState.Failed, bytesDone = part.length(), error = t.message ?: t::class.simpleName) }
        }
    }

    /** Poster, backdrop and logo of the show (once), and the episode's thumb. Failures are ignored. */
    private suspend fun fetchImages(e: DownloadEntry) {
        val api = app.api
        val title = downloads.index.value.titles[e.titleId]
        if (title != null) {
            for ((type, px) in listOf("poster" to 720, "backdrop" to 1280, "logo" to 640)) {
                if (type in title.images) continue
                val url = api.imageUrl(title.item, type, px) ?: continue
                val f = downloads.imageFile(e.titleId, type)
                if (saveUrl(url, f)) downloads.markTitleImage(e.titleId, type)
            }
        }
        if (e.item.isEpisode && "thumb" !in e.images) {
            val url = api.imageUrl(e.item, "thumb", 640)
            if (url != null && saveUrl(url, downloads.imageFile(e.itemId, "thumb"))) downloads.update(e.itemId) { it.copy(images = it.images + "thumb") }
        }
        if (e.item.isMovie) {
            for ((type, px) in listOf("backdrop" to 1280, "poster" to 720)) {
                if (type in e.images) continue
                val url = api.imageUrl(e.item, type, px) ?: continue
                if (saveUrl(url, downloads.imageFile(e.itemId, type))) downloads.update(e.itemId) { it.copy(images = it.images + type) }
            }
        }
    }

    /**
     * External subtitle files (Bazarr sidecars) saved as `video.<lang>.<ext>` beside the
     * video so mpv's `sub-auto` picks them up; embedded tracks and fonts are inside the MKV.
     */
    private suspend fun fetchSidecars(e: DownloadEntry, dir: File) {
        val src = e.item.primarySource ?: return
        src.streams.filter { it.type == "subtitle" && it.isExternal }.forEach { s ->
            val ext = when (s.codec.lowercase()) { "ass", "ssa" -> "ass"; "subrip", "srt" -> "srt"; "webvtt", "vtt" -> "vtt"; else -> s.codec.lowercase().ifEmpty { "srt" } }
            val lang = (s.detectedLanguage ?: s.language ?: "und").lowercase()
            val f = File(dir, "video.$lang.${s.index}.$ext")
            if (f.exists()) return@forEach
            saveUrl("${app.api.baseUrl}/v1/sources/${src.id}/subtitles/${s.index}", f)
        }
    }

    private suspend fun saveUrl(url: String, target: File): Boolean {
        if (target.exists() && target.length() > 0) return true
        return runCatching {
            target.parentFile?.mkdirs()
            val bytes = app.api.bytes(url)
            if (bytes.isEmpty()) return false
            target.writeBytes(bytes)
            true
        }.onFailure { Timber.d("skip %s: %s", url.substringBefore('?'), it.message) }.getOrDefault(false)
    }

    // ---- notification ----

    private fun createChannel() {
        val nm = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        nm.createNotificationChannel(NotificationChannel(CHANNEL, "Downloads", NotificationManager.IMPORTANCE_LOW).apply {
            description = "Progress of episodes and movies being downloaded"
            setShowBadge(false)
        })
    }

    private fun notify(label: String, item: Item?, fraction: Float) {
        val nm = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        val queued = downloads.index.value.entries.values.count { it.state == DownloadState.Queued }
        val sub = buildString {
            append("${(fraction * 100).toInt()}%")
            if (queued > 0) append("  ·  $queued more queued")
        }
        runCatching { nm.notify(NOTIFICATION_ID, notification(label, sub, fraction, indeterminate = false)) }
    }

    private fun notification(title: String, text: String?, fraction: Float, indeterminate: Boolean): Notification {
        val open = PendingIntent.getActivity(this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
        return NotificationCompat.Builder(this, CHANNEL)
            .setSmallIcon(R.drawable.ic_launcher_foreground)
            .setContentTitle(title)
            .setContentText(text)
            .setProgress(100, (fraction * 100).toInt(), indeterminate)
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setContentIntent(open)
            .setForegroundServiceBehavior(NotificationCompat.FOREGROUND_SERVICE_IMMEDIATE)
            .build()
    }
}
