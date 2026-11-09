package tv.roxy.tv.data.downloads

import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import tv.roxy.tv.data.Prefs
import java.io.File

/**
 * The progress queue survives the app: its journal is written on its own thread without the
 * downloads index's 400 ms wait, and a write the disk refuses is known and tried again.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = android.app.Application::class)
class QueueDurabilityTest {
    private val context = ApplicationProvider.getApplicationContext<android.app.Application>()
    private fun store(scope: CoroutineScope) = Downloads(context, Prefs(context, scope), scope)
    private fun queueOf(d: Downloads) = d.index.value.pendingProgress.map { "${it.itemId}:${it.positionMs}:${it.played}" }

    // Found in review: the queue rode in downloads.json, saved 400 ms after a change; a watched
    // mark recorded just before Android killed the app could be lost. The store that recorded
    // it is dropped as soon as its journal write is done (the index's delayed save never runs),
    // and a new one, as the restarted app makes, reads the mark back.
    @Test fun aWatchedMarkSurvivesAnImmediateKill() {
        val first = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined)
        val before = store(first)
        before.recordProgress("e1", 1_400_000, reportedToServer = false)
        before.recordProgress("e1", 0, reportedToServer = false, played = true)
        runBlocking { before.awaitQueueWritten() }
        first.cancel() // killed: the debounced index save never happens
        assertEquals(listOf("e1:1400000:false", "e1:0:true"), queueOf(store(CoroutineScope(SupervisorJob() + Dispatchers.Unconfined))))
    }

    // Found in review: a write the disk refused was logged and forgotten. It is known
    // (queueWriteFailed), the entries stay in memory, and the next write puts them all down.
    @Test fun aRefusedWriteIsKnownAndRecovers() {
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined)
        val d = store(scope)
        runBlocking { d.awaitQueueWritten() }
        val blocker = File(context.filesDir, "progress-queue.json.tmp").apply { mkdirs() } // the disk refuses
        d.recordProgress("e1", 0, reportedToServer = false, played = true)
        runBlocking { d.awaitQueueWritten() }
        assertTrue("the failure is known", d.queueWriteFailed)
        assertEquals("kept in memory", listOf("e1:0:true"), queueOf(d))
        blocker.deleteRecursively() // the disk takes writes again
        d.recordProgress("e2", 30_000, reportedToServer = false)
        runBlocking { d.awaitQueueWritten() }
        assertFalse(d.queueWriteFailed)
        scope.cancel()
        assertEquals("both on disk", listOf("e1:0:true", "e2:30000:false"), queueOf(store(CoroutineScope(SupervisorJob() + Dispatchers.Unconfined))))
    }
}
