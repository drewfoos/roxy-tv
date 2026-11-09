package tv.roxy.tv.data.downloads

import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.Json
import timber.log.Timber
import java.io.File

/**
 * The watch-progress queue on disk, written the moment it changes (a temporary file renamed
 * over the old: never half written). It used to ride in downloads.json, saved 400 ms after
 * the last change, so a report made just before Android killed the app could still be lost;
 * and every 10 s tick rewrote the whole downloads index. A write that has returned survives
 * the process (the OS keeps it); only a power cut in the next moments could undo it.
 */
class ProgressJournal(private val file: File) {
    private val json = Json { ignoreUnknownKeys = true; encodeDefaults = true; explicitNulls = false }
    private val serializer = ListSerializer(PendingProgress.serializer())

    /** The queue as last written, or null when there is no journal yet (an older install). */
    fun read(): List<PendingProgress>? {
        if (!file.exists()) return null
        return runCatching { json.decodeFromString(serializer, file.readText()) }
            .onFailure { Timber.w(it, "progress journal unreadable") }
            .getOrNull()
    }

    /** Writes [queue] now. Callers serialize (one writer at a time, the latest state). */
    fun write(queue: List<PendingProgress>) {
        runCatching {
            val tmp = File(file.path + ".tmp")
            tmp.writeText(json.encodeToString(serializer, queue))
            if (!tmp.renameTo(file)) { file.delete(); tmp.renameTo(file) }
        }.onFailure { Timber.w(it, "could not write the progress journal") }
    }
}
