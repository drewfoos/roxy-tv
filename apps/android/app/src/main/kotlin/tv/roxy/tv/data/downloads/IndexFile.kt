package tv.roxy.tv.data.downloads

import kotlinx.serialization.KSerializer
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import timber.log.Timber
import tv.roxy.tv.api.Item
import java.io.File
import java.io.FileDescriptor
import java.io.FileOutputStream
import java.nio.file.Files
import java.nio.file.StandardCopyOption

/**
 * The downloads index on disk (downloads.json), kept apart from [Downloads] so its rules
 * can be tested on the JVM.
 *
 * Writing: the text goes to a temporary file, is flushed to storage, and is then moved over
 * the index in one atomic step, as [ProgressJournal] does. At every moment the index is the
 * old one or the new one, whole. (It used to be written without the flush, so a power cut
 * soon after the rename could leave an empty or cut-off index; and when the rename failed
 * the index was deleted first, so dying between the two left none.) A failure is thrown and
 * the index is still the previous one. [sync] is the flush (tests watch it).
 */
internal fun writeIndexFile(file: File, text: String, sync: (FileDescriptor) -> Unit = { it.sync() }) {
    val tmp = File(file.path + ".tmp")
    FileOutputStream(tmp).use { out -> out.write(text.toByteArray()); sync(out.fd) }
    Files.move(tmp.toPath(), file.toPath(), StandardCopyOption.ATOMIC_MOVE, StandardCopyOption.REPLACE_EXISTING)
}

/** The title a download found on disk without its index entry is shown under. */
const val RECOVERED_TITLE = "Recovered download"

/** A finished video in a download's folder: `video.<container>`; not a part, not a sidecar (`video.<lang>.<n>.<ext>`). */
private val finishedVideo = Regex("^video\\.[A-Za-z0-9]+$")

/**
 * Reads the index. One that cannot be read (cut off, damaged, an entry this build cannot
 * decode) used to become an empty index: every download was forgotten while its files
 * stayed on the disk, unseen, until the next delete swept them all away. Now:
 *  - the unreadable file is kept beside the index as `downloads.json.bad`;
 *  - every entry, title and queued report in it that can still be decoded is kept;
 *  - a folder under [root] with a finished video and no entry gets one made from what the
 *    folder shows: the video, its size, its images. What only the index knew (the title,
 *    the tracks, the series) is gone, so it is listed as a film called [RECOVERED_TITLE]:
 *    it plays, it counts in the storage used, and it can be deleted. A part without its
 *    entry cannot be resumed (the source is not known) and is left for the next sweep.
 * The same search of the folders runs when there is no index at all.
 * [folderOf] is the folder name of an item id.
 */
internal fun readIndexFile(file: File, json: Json, root: File, folderOf: (String) -> String): DownloadIndex {
    val text = if (file.exists()) runCatching { file.readText() }.onFailure { Timber.w(it, "downloads index unreadable") }.getOrNull() else null
    if (text != null) {
        runCatching { json.decodeFromString(DownloadIndex.serializer(), text) }
            .onSuccess { return it }
            .onFailure { Timber.w(it, "downloads index unreadable; kept as .bad, rebuilding from what is on disk") }
    }
    if (file.exists()) runCatching { Files.move(file.toPath(), File(file.path + ".bad").toPath(), StandardCopyOption.REPLACE_EXISTING) }
        .onFailure { Timber.w(it, "could not set the unreadable downloads index aside") }
    val kept = text?.let { decodable(it, json) } ?: DownloadIndex()
    val known = kept.entries.keys.map(folderOf).toSet()
    val found = root.listFiles().orEmpty().filter { it.isDirectory && it.name !in known }.sortedBy { it.name }.mapNotNull { entryFromFolder(it) }
    if (found.isNotEmpty()) Timber.w("downloads: %d found on disk without an index entry", found.size)
    return kept.copy(entries = kept.entries + found.associateBy { it.itemId })
}

/** What can still be decoded of an index that as a whole cannot: each entry, title and report by itself. */
private fun decodable(text: String, json: Json): DownloadIndex? {
    val obj = runCatching { json.parseToJsonElement(text) as? JsonObject }.getOrNull() ?: return null
    fun <T> each(name: String, s: KSerializer<T>): Map<String, T> = (obj[name] as? JsonObject).orEmpty()
        .mapNotNull { (k, v) -> runCatching { json.decodeFromJsonElement(s, v) }.getOrNull()?.let { k to it } }.toMap()
    val queue = (obj["pendingProgress"] as? JsonArray).orEmpty().mapNotNull { runCatching { json.decodeFromJsonElement(PendingProgress.serializer(), it) }.getOrNull() }
    return DownloadIndex(each("entries", DownloadEntry.serializer()), each("titles", TitleSnapshot.serializer()), queue)
}

/** An entry for a download's folder that holds a finished video, from the folder alone; null when it holds none. */
private fun entryFromFolder(dir: File): DownloadEntry? {
    val files = dir.listFiles().orEmpty().filter { it.isFile }
    val video = files.firstOrNull { finishedVideo.matches(it.name) && it.length() > 0 } ?: return null
    val size = video.length()
    val item = Item(
        id = dir.name, type = "movie", title = RECOVERED_TITLE,
        mediaSources = listOf(Item.Source(id = "", container = video.extension, size = size, isPrimary = true)),
    )
    return DownloadEntry(
        itemId = dir.name, sourceId = "", item = item, titleId = dir.name, state = DownloadState.Done,
        bytesDone = size, bytesTotal = size, addedAt = video.lastModified(), completedAt = video.lastModified(),
        container = video.extension, images = files.filter { it.name.endsWith(".img") && it.length() > 0 }.map { it.name.removeSuffix(".img") }.sorted(),
    )
}
