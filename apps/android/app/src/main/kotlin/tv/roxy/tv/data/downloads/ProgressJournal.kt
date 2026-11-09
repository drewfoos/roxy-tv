package tv.roxy.tv.data.downloads

import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.Json
import timber.log.Timber
import java.io.File
import java.io.FileOutputStream
import java.nio.file.Files
import java.nio.file.StandardCopyOption

/**
 * The watch-progress queue on disk. It used to ride in downloads.json, saved 400 ms after the
 * last change, so a report made just before Android killed the app could be lost.
 *
 * A write goes to a temporary file, flushed to storage, then moved over the journal in one
 * atomic step: at every moment the journal is either the old queue or the new one, whole
 * (the previous "rename, else delete and rename" left no journal at all if the app died
 * between the delete and the rename).
 */
class ProgressJournal(private val file: File) {
    private val json = Json { ignoreUnknownKeys = true; encodeDefaults = true; explicitNulls = false }
    private val serializer = ListSerializer(PendingProgress.serializer())
    private val tmp get() = File(file.path + ".tmp")

    /** The queue as last written, or null when there is none (an older install) or it cannot be read. */
    fun read(): List<PendingProgress>? {
        if (!file.exists()) return null
        return runCatching { json.decodeFromString(serializer, file.readText()) }
            .onFailure { Timber.w(it, "progress journal unreadable") }
            .getOrNull()
    }

    /**
     * Writes [queue]. A failure (a full or read-only disk) is returned, not swallowed: the
     * journal then still holds the previous queue, whole, and the caller knows the newest
     * entries are only in memory. Callers write one at a time.
     */
    fun write(queue: List<PendingProgress>): Result<Unit> = runCatching {
        val bytes = json.encodeToString(serializer, queue).toByteArray()
        FileOutputStream(tmp).use { out -> out.write(bytes); out.fd.sync() }
        Files.move(tmp.toPath(), file.toPath(), StandardCopyOption.ATOMIC_MOVE, StandardCopyOption.REPLACE_EXISTING)
        Unit
    }.onFailure { Timber.w(it, "could not write the progress journal") }
}
