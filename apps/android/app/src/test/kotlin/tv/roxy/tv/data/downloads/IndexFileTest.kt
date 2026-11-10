package tv.roxy.tv.data.downloads

import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import tv.roxy.tv.api.Item
import java.io.File
import java.io.IOException

/** The downloads index on disk: written whole or not at all, and never taken for empty when it cannot be read. */
class IndexFileTest {
    @get:Rule val tmp = TemporaryFolder()
    private val json = Json { ignoreUnknownKeys = true; encodeDefaults = true; explicitNulls = false }
    private fun root() = File(tmp.root, "downloads").apply { mkdirs() }
    private fun put(root: File, path: String, bytes: Int = 16) = File(root, path).apply { parentFile!!.mkdirs(); writeBytes(ByteArray(bytes)) }
    private fun read(index: File, root: File) = readIndexFile(index, json, root) { it }
    private fun episode(id: String) = DownloadEntry(
        itemId = id, sourceId = "s-$id", item = Item(id = id, type = "episode", title = "Episode $id"), titleId = "show",
        state = DownloadState.Done, bytesDone = 16, bytesTotal = 16,
    )

    // The finding: the temporary file was renamed over the index without being flushed, so a
    // power cut soon after could leave an empty or cut-off index under the real name. The
    // flush happens, on the whole text, while the index is still the old one.
    @Test fun theNewIndexIsFlushedBeforeItReplacesTheOld() {
        val index = File(tmp.root, "downloads.json").apply { writeText("old") }
        val atFlush = mutableListOf<String>()
        writeIndexFile(index, "new") { fd -> atFlush += "${File(index.path + ".tmp").readText()}/${index.readText()}"; fd.sync() }
        assertEquals("flushed once: the new text in the temporary file, the old index untouched", listOf("new/old"), atFlush)
        assertEquals("new", index.readText())
        assertFalse(File(index.path + ".tmp").exists())
    }

    // The finding: when the rename failed, the index was deleted first and the rename tried
    // again; dying (or failing) there left no index at all. A write that fails leaves the
    // previous index as it was, and says so.
    @Test fun aFailedWriteLeavesThePreviousIndex() {
        val index = File(tmp.root, "downloads.json").apply { writeText("old") }
        try {
            writeIndexFile(index, "new") { throw IOException("disk refuses") }
            fail("the failure is not hidden")
        } catch (_: IOException) {
        }
        assertEquals("old", index.readText())
        val blocker = File(index.path + ".tmp").apply { delete(); mkdirs() } // the temporary file cannot be made
        try {
            writeIndexFile(index, "new")
            fail("the failure is not hidden")
        } catch (_: IOException) {
        }
        assertEquals("old", index.readText())
        blocker.deleteRecursively()
    }

    // The finding: an index that could not be read became an empty one. The downloads were
    // forgotten with their files still on the disk, and the next delete swept every folder
    // away. The bad file is kept, and each folder with a finished video is listed again.
    @Test fun anUnreadableIndexIsKeptAsideAndTheFoldersAreListedAgain() {
        val root = root()
        put(root, "m1/video.mkv", 4096)
        put(root, "m1/video.en.3.srt") // a subtitle beside it is not the video
        put(root, "m1/poster.img")
        put(root, "m2/video.mp4", 100)
        put(root, "show/poster.img") // a title's images: no video, no entry
        put(root, "p1/video.mkv.part", 500) // a part cannot be resumed without its entry
        put(root, "z0/video.mkv", 0) // an empty file is not a download
        val cut = "{\"entries\":{\"m1\":{\"itemId\":\"m1\",\"sourceId\":\"s1\",\"item\":{\"id\":\"m1\",\"ty"
        val index = File(tmp.root, "downloads.json").apply { writeText(cut) }

        val idx = read(index, root)

        assertEquals(setOf("m1", "m2"), idx.entries.keys)
        val m1 = idx.entries.getValue("m1")
        assertTrue(m1.isDone)
        assertEquals("mkv", m1.container)
        assertEquals(4096L, m1.bytesTotal)
        assertEquals(4096L, m1.bytesDone)
        assertEquals(listOf("poster"), m1.images)
        assertEquals("m1", m1.titleId)
        assertTrue("listed without a title snapshot", m1.item.isMovie)
        assertEquals(RECOVERED_TITLE, m1.item.title)
        assertNotNull("the player needs a source to play it", m1.item.primarySource)
        assertEquals("mp4", idx.entries.getValue("m2").container)
        assertEquals("the unreadable index is kept", cut, File(index.path + ".bad").readText())
        assertFalse(index.exists())
    }

    // An index that is JSON but holds one entry this build cannot decode: the others, the
    // titles and the queued reports are kept as they were; only the bad entry is made anew
    // from its folder.
    @Test fun whatCanStillBeDecodedOfABadIndexIsKept() {
        val root = root()
        put(root, "e1/video.mkv")
        put(root, "e2/video.mkv", 64)
        val good = DownloadIndex(
            entries = mapOf("e1" to episode("e1"), "e2" to episode("e2")),
            titles = mapOf("show" to TitleSnapshot(Item(id = "show", type = "series", title = "The Show"))),
            pendingProgress = listOf(PendingProgress(profileId = "p", itemId = "e1", positionMs = 5_000, at = 1)),
        )
        val text = json.encodeToString(DownloadIndex.serializer(), good).replaceFirst("\"itemId\":\"e2\"", "\"itemId\":7.5e400")
        val index = File(tmp.root, "downloads.json").apply { writeText(text) }
        assertTrue("the index as a whole does not decode", runCatching { json.decodeFromString(DownloadIndex.serializer(), text) }.isFailure)

        val idx = read(index, root)

        assertEquals("kept as it was", episode("e1"), idx.entries["e1"])
        assertEquals("The Show", idx.titles["show"]?.item?.title)
        assertEquals(listOf(5_000L), idx.pendingProgress.map { it.positionMs })
        assertEquals("made again from its folder", RECOVERED_TITLE, idx.entries["e2"]?.item?.title)
        assertEquals(64L, idx.entries["e2"]?.bytesTotal)
        assertTrue(File(index.path + ".bad").exists())
    }

    // A good index is returned as it is: folders without an entry (title images, leftovers)
    // are not turned into downloads.
    @Test fun aGoodIndexIsReadAsItIs() {
        val root = root()
        put(root, "e1/video.mkv")
        put(root, "left/video.mkv")
        val good = DownloadIndex(entries = mapOf("e1" to episode("e1")))
        val index = File(tmp.root, "downloads.json").apply { writeText(json.encodeToString(DownloadIndex.serializer(), good)) }
        assertEquals(good, read(index, root))
        assertFalse(File(index.path + ".bad").exists())
    }
}
