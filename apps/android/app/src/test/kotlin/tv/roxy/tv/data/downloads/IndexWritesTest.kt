package tv.roxy.tv.data.downloads

import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import tv.roxy.tv.api.Item
import tv.roxy.tv.data.Prefs
import java.io.File

/**
 * What a running download costs the rest of the app. The finding: every progress tick (one
 * each 400 ms) rewrote the whole downloads index and flushed it to storage, and redrew
 * every page and card that reads the index, for as long as the download ran.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = android.app.Application::class)
class IndexWritesTest {
    private val context = ApplicationProvider.getApplicationContext<android.app.Application>()
    private val json = Json { ignoreUnknownKeys = true; encodeDefaults = true; explicitNulls = false }

    private fun entry(id: String, state: DownloadState, done: Long = 0, total: Long = 1000) = DownloadEntry(
        itemId = id, sourceId = "s", item = Item(id = id, type = "movie", title = id), titleId = id, state = state, bytesDone = done, bytesTotal = total,
    )

    private fun waitFor(what: String, limitMs: Long = 5000, ok: () -> Boolean) {
        val end = System.currentTimeMillis() + limitMs
        while (!ok()) { assertTrue(what, System.currentTimeMillis() < end); Thread.sleep(20) }
    }

    @Test fun progressTicksNeitherWriteTheIndexNorChangeWhatQuietPagesRead() {
        val file = File(context.filesDir, "downloads.json")
        file.writeText(json.encodeToString(DownloadIndex.serializer(), DownloadIndex(entries = mapOf("m1" to entry("m1", DownloadState.Paused), "m2" to entry("m2", DownloadState.Paused)))))
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined)
        val d = Downloads(context, Prefs(context, scope), scope)
        d.update("m1") { it.copy(state = DownloadState.Downloading) }
        waitFor("the change of state is written") { d.indexWrites.get() >= 1 && file.readText().contains("Downloading") }
        Thread.sleep(INDEX_WRITE_DELAY_MS + 200) // nothing more on its way
        val writes = d.indexWrites.get()
        val quiet = d.settled.value

        // The download runs: ticks spread over more than twice the write delay.
        for (i in 1..8) { d.update("m1") { it.copy(bytesDone = i * 100L) }; Thread.sleep(150) }
        Thread.sleep(INDEX_WRITE_DELAY_MS + 300)

        assertEquals("the index was not rewritten for progress", writes, d.indexWrites.get())
        assertSame("a page that shows no progress saw no change", quiet, d.settled.value)
        assertEquals("the progress itself is live", 800L, d.index.value.entries.getValue("m1").bytesDone)
        assertEquals("and so is the card's own entry", 800L, runBlocking { d.entryFlow("m1").first() }?.bytesDone)

        // The end of the download is a change everyone hears of, and it is written.
        d.update("m1") { it.copy(state = DownloadState.Done, bytesDone = 1000) }
        assertNotEquals(quiet, d.settled.value)
        assertEquals(DownloadState.Done, d.settled.value.entries.getValue("m1").state)
        waitFor("the finished download is written") { d.indexWrites.get() == writes + 1 }
        waitFor("as finished") { json.decodeFromString(DownloadIndex.serializer(), file.readText()).entries.getValue("m1").state == DownloadState.Done }
        scope.cancel()
    }

    @Test fun withoutProgressKeepsEverythingButTheMovingNumbers() {
        val done = entry("a", DownloadState.Done, done = 1000)
        val running = entry("b", DownloadState.Downloading, done = 300)
        val waiting = entry("c", DownloadState.Queued)
        val idx = DownloadIndex(entries = mapOf("a" to done, "b" to running, "c" to waiting), pendingProgress = listOf(PendingProgress(itemId = "a", positionMs = 5, at = 1)))

        val quiet = idx.withoutProgress()

        assertSame("a finished download keeps its size", done, quiet.entries["a"])
        assertSame(waiting, quiet.entries["c"])
        assertEquals(running.copy(bytesDone = 0), quiet.entries["b"])
        assertTrue(quiet.pendingProgress.isEmpty())
        assertEquals("a tick later it is the same index", quiet, idx.copy(entries = idx.entries + ("b" to running.copy(bytesDone = 900))).withoutProgress())
        assertNotEquals("a change of state is not", quiet, idx.copy(entries = idx.entries + ("b" to running.copy(state = DownloadState.Paused))).withoutProgress())
        val still = DownloadIndex(entries = mapOf("a" to done, "c" to waiting))
        assertSame("nothing moving: the index itself", still, still.withoutProgress())
    }
}
