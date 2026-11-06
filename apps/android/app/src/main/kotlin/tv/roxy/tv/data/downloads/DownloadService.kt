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
import kotlinx.coroutines.delay
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
        /** How long the service waits for a server that went away before it stops (the queue stays). */
        private const val MAX_SERVER_WAIT_MS = 30 * 60_000L
        /** Failures in a row, with the server answering, before an item is marked Failed. */
        private const val MAX_TRANSIENT_FAILURES = 5
        /** Shown on an entry that is queued because the server is away. */
        const val WAITING_FOR_SERVER = "Waiting for Roxy"

        fun startIntent(context: Context): Intent = Intent(context, DownloadService::class.java).setAction(ACTION_START)
    }

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO + kotlinx.coroutines.CoroutineExceptionHandler { _, t -> Timber.e(t, "download service") })
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

    /** What became of one attempt at an item. */
    private enum class Outcome { Done, Stopped, Retry, Failed }

    private suspend fun runQueue() {
        // Failures in a row per item, so one that keeps failing on a working server gives up.
        val failures = HashMap<String, Int>()
        try {
            while (true) {
                val next = downloads.nextToFetch() ?: break
                val outcome = try { fetch(next) } catch (t: Throwable) {
                    if (t is kotlinx.coroutines.CancellationException) throw t
                    Timber.e(t, "download %s crashed", next.itemId) // never take the app down with it
                    downloads.update(next.itemId) { if (it.state == DownloadState.Downloading) it.copy(state = DownloadState.Queued, error = t.message) else it }
                    Outcome.Retry
                }
                if (outcome != Outcome.Retry) { failures.remove(next.itemId); continue }
                val n = (failures[next.itemId] ?: 0) + 1
                failures[next.itemId] = n
                if (app.api.reachable()) {
                    if (n >= MAX_TRANSIENT_FAILURES) {
                        downloads.update(next.itemId) { if (it.state == DownloadState.Queued) it.copy(state = DownloadState.Failed, error = it.error ?: "Download kept failing") else it }
                        continue
                    }
                    delay(2_000L * n) // a hiccup on a working server: a short pause, then again
                    continue
                }
                // The server went away (restart, Wi-Fi drop): keep the bytes and wait for it.
                if (!waitForServer(next)) break
            }
        } finally {
            stopForeground(STOP_FOREGROUND_REMOVE)
            stopSelf()
        }
    }

    /**
     * Wait for the server to answer again, checking every 5 s growing to a minute, with a
     * "waiting" notification; true when it is back. After [MAX_SERVER_WAIT_MS] it stops and
     * the queue waits for the next kick (network change, the server seen again, app start).
     */
    private suspend fun waitForServer(e: DownloadEntry): Boolean {
        Timber.w("download %s waiting for the server", e.itemId)
        downloads.update(e.itemId) { if (it.state == DownloadState.Queued) it.copy(error = WAITING_FOR_SERVER) else it }
        notifyText("Waiting for Roxy", "Downloads continue when the server is back")
        val started = System.currentTimeMillis()
        var wait = 5_000L
        while (System.currentTimeMillis() - started < MAX_SERVER_WAIT_MS) {
            delay(wait)
            wait = (wait * 2).coerceAtMost(60_000L)
            // Paused or deleted meanwhile: nothing to wait for unless something else is queued.
            if (downloads.index.value.entries.values.none { it.state == DownloadState.Queued || it.state == DownloadState.WaitingForWifi }) return false
            if (app.api.reachable()) { Timber.i("server back; downloads resume"); return true }
        }
        Timber.w("server still away after %d min; downloads stay queued", MAX_SERVER_WAIT_MS / 60_000)
        return false
    }

    private fun startInForeground(n: Notification) {
        if (Build.VERSION.SDK_INT >= 29) startForeground(NOTIFICATION_ID, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC)
        else startForeground(NOTIFICATION_ID, n)
    }

    /** Current state of [itemId] in the index; null when it was deleted. */
    private fun live(itemId: String): DownloadEntry? = downloads.entry(itemId)

    private suspend fun fetch(start: DownloadEntry): Outcome {
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
            return Outcome.Done
        }
        var offset = part.length()
        val url = app.api.streamUrlForHttp(start.sourceId) // the http client adds the token as a header
        // If-Range makes the server send the whole file (200) rather than the rest of a
        // different one when the file changed since the part was started.
        val validator = live(itemId)?.validator
        val req = Request.Builder().url(url).apply {
            if (offset > 0) {
                header("Range", "bytes=$offset-")
                if (validator != null) header("If-Range", validator)
            }
        }.build()
        try {
            app.api.http.newCall(req).execute().use { resp ->
                if (resp.code == 416) { // our offset is past the end: the file changed on the server
                    part.delete(); offset = 0
                    throw IOException("range rejected; restarting")
                }
                // 4xx (gone, no access) will not get better by retrying; 5xx and the rest may.
                if (!resp.isSuccessful) throw if (resp.code in 400..499 && resp.code != 408 && resp.code != 429) PermanentFailure("HTTP ${resp.code}") else IOException("HTTP ${resp.code}")
                // A 200 to a ranged request is the whole file: the range was ignored or the
                // If-Range validator no longer matched. Start over from byte 0 (the part file
                // is truncated below).
                if (resp.code == 200) offset = 0
                // Remember what these bytes are, for the If-Range of a later resume. Only a
                // strong ETag may be used there; Last-Modified is the fallback.
                if (offset == 0L) {
                    val etag = resp.header("ETag")?.takeIf { !it.startsWith("W/") }
                    val v = etag ?: resp.header("Last-Modified")
                    downloads.update(itemId) { if (it.validator == v) it else it.copy(validator = v) }
                }
                val total = when {
                    resp.code == 206 -> resp.header("Content-Range")?.substringAfter('/')?.toLongOrNull() ?: (offset + (resp.body.contentLength().takeIf { it >= 0 } ?: 0))
                    else -> resp.body.contentLength().takeIf { it >= 0 } ?: start.bytesTotal
                }
                if (total > 0) downloads.update(itemId) { it.copy(bytesTotal = total) }
                if (total > 0 && downloads.freeBytes() < total - offset + (64L shl 20)) throw PermanentFailure("Not enough free space")

                RandomAccessFile(part, "rw").use { out ->
                    // Drop anything past the offset (all of it on a restart from 0), so old bytes
                    // can never survive behind the new ones.
                    out.setLength(offset)
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
                                if (cur == null) { Timber.i("download %s deleted while running", itemId); return Outcome.Stopped }
                                if (cur.state != DownloadState.Downloading) { Timber.i("download %s %s", itemId, cur.state); return Outcome.Stopped }
                                downloads.update(itemId) { it.copy(bytesDone = done) }
                                if (now - lastNotify >= NOTIFY_INTERVAL_MS) { lastNotify = now; notify(label, start.item, if (total > 0) done.toFloat() / total else 0f) }
                            }
                        }
                    }
                    if (total > 0 && done != total) throw IOException("Stream ended early ($done of $total bytes)")
                }
            }
            if (live(itemId) == null) { dir.deleteRecursively(); return Outcome.Stopped }
            if (!part.renameTo(final)) throw PermanentFailure("Could not move the finished file into place")
            downloads.update(itemId) { it.copy(state = DownloadState.Done, bytesDone = final.length(), bytesTotal = final.length(), completedAt = System.currentTimeMillis()) }
            Timber.i("downloaded %s (%s)", label, formatBytes(final.length()))
            return Outcome.Done
        } catch (t: Throwable) {
            if (t is kotlinx.coroutines.CancellationException) {
                downloads.update(itemId) { if (it.state == DownloadState.Downloading) it.copy(state = DownloadState.Queued, bytesDone = part.length()) else it }
                throw t
            }
            val cur = live(itemId) ?: run { dir.deleteRecursively(); return Outcome.Stopped }
            if (cur.state != DownloadState.Downloading) return Outcome.Stopped // paused or deleted mid-stream
            if (t is PermanentFailure) {
                Timber.w(t, "download %s failed", label)
                downloads.update(itemId) { it.copy(state = DownloadState.Failed, bytesDone = part.length(), error = t.message) }
                return Outcome.Failed
            }
            // Connection lost, server restarting, stream cut short: keep the bytes (the .part
            // file is only renamed once complete) and queue it again; runQueue decides when.
            Timber.w("download %s interrupted at %s: %s", label, formatBytes(part.length()), t.message)
            downloads.update(itemId) { it.copy(state = DownloadState.Queued, bytesDone = part.length(), error = t.message ?: t::class.simpleName) }
            return Outcome.Retry
        }
    }

    /** A failure that retrying will not fix (not found, no space). */
    private class PermanentFailure(message: String) : IOException(message)

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
            val lang = (s.detectedLanguage ?: s.language ?: "und").lowercase().filter { it.isLetterOrDigit() }.ifEmpty { "und" }
            val f = File(dir, "video.$lang.${s.index}.${ext.filter { it.isLetterOrDigit() }.ifEmpty { "srt" }}")
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

    private fun notifyText(title: String, text: String) {
        val nm = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        runCatching { nm.notify(NOTIFICATION_ID, notification(title, text, 0f, indeterminate = true)) }
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
