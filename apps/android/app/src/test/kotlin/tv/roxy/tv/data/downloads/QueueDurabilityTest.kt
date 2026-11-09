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

    // Found in review: the journal was written on its own thread, after recordProgress had
    // returned, so a kill right after a watched mark could still lose it (the test above
    // waited for that thread). Watched marks, stop and end reports are written before the
    // call returns now. The background writer is paused here, and nothing is waited for: the
    // mark is on disk (with the tick before it, the queue as it was), the tick recorded after
    // it is not (ticks are written moments later, so a kill can cost a few seconds of position).
    @Test fun aWatchedMarkIsOnDiskBeforeTheCallReturns() {
        val first = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined)
        val before = store(first)
        runBlocking { before.awaitQueueWritten() } // the startup write, before pausing
        before.pauseBackgroundWrites = true
        before.recordProgress("e1", 30_000, reportedToServer = false)
        assertTrue(before.recordProgress("e1", 0, reportedToServer = false, played = true, durableNow = true))
        before.recordProgress("e1", 60_000, reportedToServer = false)
        first.cancel() // killed at once: nothing else is written
        assertEquals(listOf("e1:30000:false", "e1:0:true"), queueOf(store(CoroutineScope(SupervisorJob() + Dispatchers.Unconfined))))
    }

    // Writes refused for 30 s: the journal is tried once at once, then once every
    // JOURNAL_RETRY_MS, never in a loop in between (the first version of the retry spun),
    // and the queue goes down on the first try after the disk takes writes again.
    @OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
    @Test fun sustainedFailureRetriesEveryFiveSecondsThenRecovers() {
        val clock = kotlinx.coroutines.test.TestCoroutineScheduler()
        // At most 200 tasks: a retry loop (as the first version had) stops here and shows up
        // as too many attempts, instead of spinning the test for ever.
        val writer = Bounded(kotlinx.coroutines.test.StandardTestDispatcher(clock), 200)
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined)
        val d = Downloads(context, Prefs(context, scope), scope, journalDispatcher = writer)
        clock.runCurrent() // the startup write
        val blocker = File(context.filesDir, "progress-queue.json.tmp").apply { mkdirs() } // the disk refuses
        val start = d.journalWriteAttempts.get()
        d.recordProgress("e1", 0, reportedToServer = false, played = true) // written in the background
        clock.runCurrent()
        assertEquals("tried at once", start + 1, d.journalWriteAttempts.get())
        assertTrue(d.queueWriteFailed)
        for (n in 1..6) { // 30 s of refused writes
            clock.advanceTimeBy(JOURNAL_RETRY_MS - 1); clock.runCurrent()
            assertEquals("no try before $n x 5 s", start + n, d.journalWriteAttempts.get())
            clock.advanceTimeBy(1); clock.runCurrent()
            assertEquals("one try at $n x 5 s", start + n + 1, d.journalWriteAttempts.get())
            assertTrue(d.queueWriteFailed)
        }
        blocker.deleteRecursively() // the disk takes writes again
        clock.advanceTimeBy(JOURNAL_RETRY_MS); clock.runCurrent()
        assertFalse("recovered on the next try", d.queueWriteFailed)
        val settled = d.journalWriteAttempts.get()
        clock.advanceTimeBy(60_000); clock.runCurrent()
        assertEquals("no more tries once it went down", settled, d.journalWriteAttempts.get())
        scope.cancel()
        assertEquals(listOf("e1:0:true"), queueOf(store(CoroutineScope(SupervisorJob() + Dispatchers.Unconfined))))
    }

    /** A dispatcher that runs at most [max] tasks (on [inner]'s virtual clock) and drops the rest. */
    @OptIn(kotlinx.coroutines.InternalCoroutinesApi::class)
    private class Bounded(private val inner: kotlinx.coroutines.CoroutineDispatcher, private val max: Int) : kotlinx.coroutines.CoroutineDispatcher(), kotlinx.coroutines.Delay {
        private val tasks = java.util.concurrent.atomic.AtomicInteger()
        override fun dispatch(context: kotlin.coroutines.CoroutineContext, block: Runnable) {
            if (tasks.incrementAndGet() <= max) inner.dispatch(context, block)
        }
        override fun scheduleResumeAfterDelay(timeMillis: Long, continuation: kotlinx.coroutines.CancellableContinuation<Unit>) =
            (inner as kotlinx.coroutines.Delay).scheduleResumeAfterDelay(timeMillis, continuation)
    }
}

