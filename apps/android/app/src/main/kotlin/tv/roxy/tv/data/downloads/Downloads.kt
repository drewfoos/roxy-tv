package tv.roxy.tv.data.downloads

import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import android.os.StatFs
import androidx.core.content.ContextCompat
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.FlowPreview
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.debounce
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import timber.log.Timber
import tv.roxy.tv.api.Item
import tv.roxy.tv.api.RoxyApi
import tv.roxy.tv.api.UserData
import tv.roxy.tv.data.Prefs
import java.io.File

/** Where a download is in its life. Persisted by name, so keep the names stable. */
@Serializable
enum class DownloadState {
    Queued, Downloading, Paused, WaitingForWifi, Done, Failed;

    val isActive: Boolean get() = this == Queued || this == Downloading || this == WaitingForWifi
}

/**
 * One downloaded (or downloading) episode or movie. [item] is the server's item as it was
 * when the download was queued, including its media sources (streams, attachments,
 * segments), so the player and the Downloads page work with the server unreachable.
 */
@Serializable
data class DownloadEntry(
    val itemId: String,
    val sourceId: String,
    val item: Item,
    /** The series for an episode (the movie's own id for a movie); keys [DownloadIndex.titles]. */
    val titleId: String,
    val seasonNumber: Int? = null,
    val seasonName: String? = null,
    val state: DownloadState = DownloadState.Queued,
    val bytesDone: Long = 0,
    val bytesTotal: Long = 0,
    val error: String? = null,
    val addedAt: Long = 0,
    val completedAt: Long? = null,
    /** File extension of the video ("mkv"). */
    val container: String = "mkv",
    /** Image types saved next to the video (thumb for episodes). */
    val images: List<String> = emptyList(),
    /**
     * The server's ETag (or Last-Modified) for the bytes in the part file, sent as If-Range
     * on resume so a file that changed on the server restarts instead of being appended to.
     */
    val validator: String? = null,
) {
    val isDone: Boolean get() = state == DownloadState.Done
    val fraction: Float get() = if (bytesTotal > 0) (bytesDone.toFloat() / bytesTotal).coerceIn(0f, 1f) else 0f
    /** The user's resume position as last seen (server data at queue time, updated by offline playback). */
    val positionMs: Long get() = item.userData?.positionMs ?: 0
}

/** The show (or movie) a download belongs to: its item snapshot and which images were saved. */
@Serializable
data class TitleSnapshot(val item: Item, val images: List<String> = emptyList())

/** A watch position reported while the server was unreachable; replayed by [Downloads.syncProgress]. */
@Serializable
data class PendingProgress(
    val profileId: String? = null,
    val itemId: String,
    val positionMs: Long,
    val at: Long,
    val played: Boolean = false,
    val audioIndex: Int? = null,
    /** -1 = subtitles off, as in POST /v1/me/progress; null = unchanged. */
    val subtitleIndex: Int? = null,
    /** The version played (its length decides "watched"), and that length. */
    val sourceId: String? = null,
    val durationMs: Long? = null,
)

/**
 * The queue after recording [next] for one item: it replaces what was queued for the item,
 * except a "watched" mark not yet sent, which stays (and goes first) when [next] is plain
 * progress. Finishing an episode offline and opening it again for a few seconds used to
 * replace the mark, and the server never learned it was finished. A report the server took
 * ([sent]) is not queued.
 */
fun pendingAfter(queue: List<PendingProgress>, profile: String?, itemId: String, sent: Boolean, next: PendingProgress): List<PendingProgress> {
    val kept = queue.filterNot { it.itemId == itemId && it.profileId == profile && !(it.played && !next.played) }
    return if (sent) kept else kept + next
}

@Serializable
data class DownloadIndex(
    val entries: Map<String, DownloadEntry> = emptyMap(),
    val titles: Map<String, TitleSnapshot> = emptyMap(),
    val pendingProgress: List<PendingProgress> = emptyList(),
)

/** A show on the Downloads page: its snapshot and the entries under it, in season / episode order. */
data class DownloadedTitle(val id: String, val title: Item, val entries: List<DownloadEntry>) {
    val bytes: Long get() = entries.sumOf { if (it.isDone) it.bytesTotal else it.bytesDone }
    val doneCount: Int get() = entries.count { it.isDone }
    val activeCount: Int get() = entries.count { it.state.isActive }
}

/**
 * The downloads system: an index of entries (JSON in the app's files dir) and the files
 * under `Android/data/<app>/files/downloads/<itemId>/`. Netflix's model: downloads belong
 * to the device, play from disk when present, and are deleted from this app only.
 *
 * The index is the single source of truth; [DownloadService] reads it to know what to
 * fetch and writes progress back through [update]. Every change lands in [index] at once
 * and on disk shortly after (debounced), so the UI never waits on I/O.
 */
class Downloads(private val context: Context, private val prefs: Prefs, private val scope: CoroutineScope) {

    private val json = Json { ignoreUnknownKeys = true; encodeDefaults = true; explicitNulls = false }
    private val indexFile = File(context.filesDir, "downloads.json")

    /** Video and image files; the external app dir is large and needs no permission, with the internal one as a fallback. */
    val root: File = (context.getExternalFilesDir("downloads") ?: File(context.filesDir, "downloads")).apply { mkdirs() }

    private val _index = MutableStateFlow(load())
    val index: StateFlow<DownloadIndex> get() = _index

    init {
        // Persist every change, coalesced; progress ticks arrive several times a second.
        @OptIn(FlowPreview::class)
        scope.launch(Dispatchers.IO) { _index.debounce(400).collect { save(it) } }
        watchNetwork()
        if (_index.value.entries.values.any { it.state == DownloadState.Queued }) kick()
    }

    // ---- queries ----

    fun entry(itemId: String): DownloadEntry? = _index.value.entries[itemId]

    /** Ids come from the server; keep them from ever becoming path segments ("../"). */
    private fun safeSegment(s: String): String = s.filter { it.isLetterOrDigit() || it == '-' || it == '_' }.ifEmpty { "x" }

    fun dir(itemId: String): File = File(root, safeSegment(itemId))

    fun videoFile(e: DownloadEntry): File = File(dir(e.itemId), "video.${e.container}")

    private fun partFile(e: DownloadEntry): File = File(dir(e.itemId), "video.${e.container}.part")

    /** The finished local file for an item, or null when it is not downloaded (or the file went missing). */
    fun localFile(itemId: String): File? {
        val e = entry(itemId)?.takeIf { it.isDone } ?: return null
        return videoFile(e).takeIf { it.exists() }
    }

    /**
     * An image for an item: the saved copy when there is one, else the server URL. Local
     * files are `file://` URIs so Coil reads them without the network.
     */
    fun image(api: RoxyApi, item: Item, type: String, widthPx: Int): String? {
        val f = imageFile(item.id, type)
        if (f.exists() && f.length() > 0) return "file://${f.path}"
        val snap = _index.value.titles[item.id]?.item ?: _index.value.entries[item.id]?.item
        return api.imageUrl(snap ?: item, type, widthPx)
    }

    fun imageFile(itemId: String, type: String): File = File(dir(itemId), "${safeSegment(type)}.img")

    /** Downloads grouped by show / movie, newest show first, entries in season and episode order. */
    fun titles(): List<DownloadedTitle> {
        val idx = _index.value
        return idx.entries.values.groupBy { it.titleId }.mapNotNull { (id, entries) ->
            val title = idx.titles[id]?.item ?: entries.first().item.takeIf { it.isMovie } ?: return@mapNotNull null
            DownloadedTitle(id, title, entries.sortedWith(compareBy({ it.seasonNumber ?: 0 }, { it.item.indexAired ?: it.item.indexAbsolute ?: 0 })))
        }.sortedByDescending { t -> t.entries.maxOf { it.addedAt } }
    }

    fun usedBytes(): Long = _index.value.entries.values.sumOf { if (it.isDone) it.bytesTotal else it.bytesDone }

    fun freeBytes(): Long = runCatching { StatFs(root.path).availableBytes }.getOrDefault(0L)

    // ---- commands ----

    /**
     * Queue [items] (episodes of one show, or a movie) for download. [title] is the show
     * for episodes or the movie itself; its poster, backdrop and logo are saved too so the
     * Downloads page looks right offline. Items without a media source are skipped.
     */
    fun enqueue(items: List<Item>, title: Item, seasonName: String? = null) {
        val now = System.currentTimeMillis()
        // A failed entry is retried from its part file only when it still points at the same
        // file; a new source (the server replaced the release) or a new size means those
        // bytes belong to a different file, and resuming would splice the two together.
        // The part goes before the service is kicked, so it can never resume from it.
        val before = _index.value.entries
        for (item in items) {
            val src = item.primarySource ?: continue
            val existing = before[item.id] ?: continue
            if (existing.state == DownloadState.Failed && !sameFile(existing, src)) partFile(existing).delete()
        }
        _index.update { idx ->
            val entries = idx.entries.toMutableMap()
            for (item in items) {
                val src = item.primarySource ?: continue
                if (!item.isPlayable) continue
                val existing = entries[item.id]
                if (existing != null && existing.state != DownloadState.Failed) continue
                val keep = existing != null && sameFile(existing, src)
                entries[item.id] = DownloadEntry(
                    itemId = item.id,
                    sourceId = src.id,
                    item = item,
                    titleId = title.id,
                    seasonNumber = item.seasonAired,
                    seasonName = seasonName,
                    state = DownloadState.Queued,
                    bytesDone = if (keep) existing!!.bytesDone else 0,
                    validator = if (keep) existing!!.validator else null,
                    bytesTotal = src.size,
                    addedAt = existing?.addedAt ?: now,
                    container = (src.container ?: "mkv").lowercase().filter { it.isLetterOrDigit() }.ifEmpty { "mkv" },
                )
            }
            idx.copy(entries = entries, titles = idx.titles + (title.id to TitleSnapshot(title, idx.titles[title.id]?.images.orEmpty())))
        }
        kick()
    }

    /** Whether [e]'s bytes on disk came from [src]: same source id and (when both are known) same size. */
    private fun sameFile(e: DownloadEntry, src: Item.Source): Boolean =
        e.sourceId == src.id && (e.bytesTotal <= 0 || src.size <= 0 || e.bytesTotal == src.size)

    fun pause(itemId: String) {
        update(itemId) { if (it.state.isActive) it.copy(state = DownloadState.Paused) else it }
    }

    fun resume(itemId: String) {
        update(itemId) { if (it.state == DownloadState.Paused || it.state == DownloadState.Failed) it.copy(state = DownloadState.Queued, error = null) else it }
        kick()
    }

    fun pauseAll(itemIds: Collection<String>) = itemIds.forEach { pause(it) }

    /** Remove a download and its files; a running one stops (the service sees the entry vanish). */
    fun delete(itemId: String) {
        _index.update { idx ->
            val entries = idx.entries - itemId
            val remaining = entries.values.map { it.titleId }.toSet()
            idx.copy(entries = entries, titles = idx.titles.filterKeys { it in remaining })
        }
        scope.launch(Dispatchers.IO) {
            dir(itemId).deleteRecursively()
            // The title's images go once its last download is gone.
            val titles = _index.value.titles.keys
            root.listFiles()?.filter { it.isDirectory && it.name !in _index.value.entries && it.name !in titles }?.forEach { it.deleteRecursively() }
        }
    }

    fun deleteAll() {
        val ids = _index.value.entries.keys.toList()
        _index.update { it.copy(entries = emptyMap(), titles = emptyMap()) }
        scope.launch(Dispatchers.IO) {
            ids.forEach { dir(it).deleteRecursively() }
            root.listFiles()?.filter { it.isDirectory }?.forEach { it.deleteRecursively() }
        }
    }

    /** Atomically change one entry (no-op when it was deleted meanwhile). */
    fun update(itemId: String, f: (DownloadEntry) -> DownloadEntry) {
        _index.update { idx ->
            val e = idx.entries[itemId] ?: return@update idx
            val n = f(e)
            if (n === e) idx else idx.copy(entries = idx.entries + (itemId to n))
        }
    }

    fun markTitleImage(titleId: String, type: String) {
        _index.update { idx ->
            val t = idx.titles[titleId] ?: return@update idx
            if (type in t.images) idx else idx.copy(titles = idx.titles + (titleId to t.copy(images = t.images + type)))
        }
    }

    /** Ask the service to work through the queue (it exits on its own when nothing is left). */
    fun kick() {
        if (_index.value.entries.values.none { it.state == DownloadState.Queued || it.state == DownloadState.WaitingForWifi }) return
        runCatching { ContextCompat.startForegroundService(context, DownloadService.startIntent(context)) }
            .onFailure { Timber.w(it, "could not start the download service") }
    }

    // ---- offline watch state ----

    /**
     * Playback position for a downloaded item: shown on the Downloads page straight away
     * and, when [reportedToServer] is false, kept for [syncProgress] once the server answers.
     */
    fun recordProgress(itemId: String, positionMs: Long, reportedToServer: Boolean, played: Boolean = false, audioIndex: Int? = null, subtitleIndex: Int? = null, ageMs: Long = 0, sourceId: String? = null, durationMs: Long? = null) {
        val profile = prefs.session.value.profileId
        _index.update { idx ->
            val e = idx.entries[itemId]
            val entries = if (e == null) idx.entries else {
                val old = e.item.userData ?: UserData()
                val ud = old.copy(
                    positionMs = positionMs,
                    played = played || (old.played && positionMs == 0L),
                    // Kept for offline playback: a saved -1 starts the download with subtitles off.
                    subtitleIndex = subtitleIndex ?: old.subtitleIndex,
                )
                idx.entries + (itemId to e.copy(item = e.item.copy(userData = ud)))
            }
            val pending = pendingAfter(idx.pendingProgress, profile, itemId, reportedToServer,
                PendingProgress(profile, itemId, positionMs, System.currentTimeMillis() - ageMs.coerceAtLeast(0), played, audioIndex, subtitleIndex, sourceId, durationMs))
            idx.copy(entries = entries, pendingProgress = pending)
        }
    }

    /** Whether a position for [itemId] (this profile) is waiting to be sent. */
    /** The newest progress this profile recorded offline for [itemId] and has not sent yet. */
    fun latestPendingProgress(itemId: String): PendingProgress? {
        val profile = prefs.session.value.profileId
        return _index.value.pendingProgress.filter { it.itemId == itemId && it.profileId == profile }.maxByOrNull { it.at }
    }

    fun hasPendingProgress(itemId: String): Boolean {
        val profile = prefs.session.value.profileId
        return _index.value.pendingProgress.any { it.itemId == itemId && it.profileId == profile }
    }

    /** Replay positions (and "watched" marks) recorded offline. Only the active profile's are sent (the header is per request). */
    suspend fun syncProgress(api: RoxyApi) {
        val profile = prefs.session.value.profileId
        val mine = _index.value.pendingProgress.filter { it.profileId == profile }
        for (p in mine) {
            // Sent as an age by this device's clock (both times are this clock's, so a clock
            // set wrong cancels out); at stays for an older server.
            val age = (System.currentTimeMillis() - p.at).coerceAtLeast(0)
            val result = runCatching { if (p.played) api.setPlayed(p.itemId, true, at = p.at, ageMs = age) else api.progress(p.itemId, p.positionMs, p.audioIndex, p.subtitleIndex, at = p.at, ageMs = age, sourceId = p.sourceId, durationMs = p.durationMs) }
            result.exceptionOrNull()?.let { if (it is kotlinx.coroutines.CancellationException) throw it }
            // A 4xx (the title was deleted meanwhile) will never succeed: drop it rather than block the rest.
            val rejected = (result.exceptionOrNull() as? tv.roxy.tv.api.ApiException)?.let { it.code in 400..499 && !it.isUnauthorized } == true
            if (result.isFailure && !rejected) return
            _index.update { idx -> idx.copy(pendingProgress = idx.pendingProgress - p) }
            Timber.i("synced offline progress item=%s pos=%dms", p.itemId, p.positionMs)
        }
    }

    // ---- service helpers ----

    /** The next entry to fetch, honouring the Wi-Fi-only setting; flips waiting entries back when the network allows. */
    fun nextToFetch(): DownloadEntry? {
        val wifiOk = !prefs.downloadsWifiOnly.value || isUnmetered()
        if (wifiOk) {
            _index.update { idx ->
                val fixed = idx.entries.mapValues { (_, e) -> if (e.state == DownloadState.WaitingForWifi) e.copy(state = DownloadState.Queued) else e }
                idx.copy(entries = fixed)
            }
        } else {
            _index.update { idx ->
                val fixed = idx.entries.mapValues { (_, e) -> if (e.state == DownloadState.Queued) e.copy(state = DownloadState.WaitingForWifi) else e }
                idx.copy(entries = fixed)
            }
            return null
        }
        return _index.value.entries.values.filter { it.state == DownloadState.Queued }.minByOrNull { it.addedAt }
    }

    fun isUnmetered(): Boolean {
        val cm = context.getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager
        val caps = cm.getNetworkCapabilities(cm.activeNetwork ?: return false) ?: return false
        return caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_METERED)
    }

    /** Where the service writes while fetching; the length is the resume offset. */
    fun partFileFor(e: DownloadEntry): File = partFile(e)

    // ---- persistence ----

    private fun load(): DownloadIndex {
        if (!indexFile.exists()) return DownloadIndex()
        val idx = runCatching { json.decodeFromString<DownloadIndex>(indexFile.readText()) }
            .onFailure { Timber.w(it, "downloads index unreadable; starting empty") }
            .getOrDefault(DownloadIndex())
        // Reconcile with disk after a process death: a download that was running is queued
        // again from the bytes on disk; a finished one whose file vanished is dropped.
        val entries = idx.entries.mapNotNull { (id, e) ->
            when {
                e.isDone && !videoFile(e).exists() -> null
                e.state == DownloadState.Downloading -> id to e.copy(state = DownloadState.Queued, bytesDone = partFile(e).length())
                else -> id to e.copy(bytesDone = if (e.isDone) e.bytesTotal else partFile(e).length())
            }
        }.toMap()
        return idx.copy(entries = entries)
    }

    private suspend fun save(idx: DownloadIndex) = withContext(Dispatchers.IO) {
        runCatching {
            val tmp = File(indexFile.path + ".tmp")
            tmp.writeText(json.encodeToString(DownloadIndex.serializer(), idx))
            if (!tmp.renameTo(indexFile)) { indexFile.delete(); tmp.renameTo(indexFile) }
        }.onFailure { Timber.w(it, "could not save the downloads index") }
    }

    /** Resume waiting downloads when a usable network appears (Wi-Fi after cellular, or any network after none). */
    private fun watchNetwork() {
        val cm = context.getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager
        val req = NetworkRequest.Builder().addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET).build()
        runCatching {
            cm.registerNetworkCallback(req, object : ConnectivityManager.NetworkCallback() {
                override fun onAvailable(network: Network) { kick() }
                override fun onCapabilitiesChanged(network: Network, caps: NetworkCapabilities) {
                    if (caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_METERED)) kick()
                }
            })
        }.onFailure { Timber.w(it, "network callback") }
    }
}

/** "1.2 GB", "340 MB", "12 KB". */
fun formatBytes(b: Long): String = when {
    b >= 1L shl 30 -> "%.1f GB".format(b / (1L shl 30).toDouble())
    b >= 1L shl 20 -> "%d MB".format(b / (1L shl 20))
    b >= 1L shl 10 -> "%d KB".format(b / (1L shl 10))
    else -> "$b B"
}
