package tv.roxy.tv.player

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.withTimeout
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import tv.roxy.tv.api.ApiException
import tv.roxy.tv.data.downloads.PendingProgress
import tv.roxy.tv.data.downloads.pendingAfter
import java.io.IOException

/**
 * The reporting path through a fake durable queue (the app's own rule for it, pendingAfter)
 * and a fake server, with clocks the test moves.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class ProgressReporterTest {
    private class Fake : ProgressReporter.Sink {
        /** What the server got, in order. */
        val sent = java.util.Collections.synchronizedList(mutableListOf<String>())
        /** The durable queue (survives the app: what a restart would send). */
        @Volatile var queue = listOf<PendingProgress>()
        var serverUp = true
        var serverError = false
        /** When set, the server holds every request until it completes. */
        var stall: CompletableDeferred<Unit>? = null
        var refreshes = 0
        var answerDelayMs = 0L

        override fun persist(r: ProgressReporter.Report) = synchronized(this) {
            queue = pendingAfter(queue, null, r.itemId, false, PendingProgress(null, r.itemId, r.positionMs, 0, false))
        }
        override fun persistPlayed(itemId: String) = synchronized(this) {
            queue = pendingAfter(queue, null, itemId, false, PendingProgress(null, itemId, 0, 0, true))
        }
        override suspend fun drain(): Throwable? {
            for (p in synchronized(this) { queue.toList() }) {
                stall?.await()
                if (answerDelayMs > 0) delay((0..answerDelayMs).random())
                if (serverError) return ApiException(500, "boom")
                if (!serverUp) return IOException("away")
                sent += if (p.played) "played ${p.itemId}" else "progress ${p.itemId} ${p.positionMs}"
                synchronized(this) { queue = queue - p }
            }
            return null
        }
        override fun userDataChanged() { refreshes++; sent += "refresh" }
        override fun answered(t: Throwable) = t is ApiException
    }

    private var mono = 0L
    private var wall = 1_000_000L
    // An eager queue on the test's clock, in a scope of its own (its consumer never ends).
    private fun TestScope.eager() = ProgressReporter.SendQueue(CoroutineScope(UnconfinedTestDispatcher(testScheduler)))
    private fun TestScope.reporter(f: Fake, q: ProgressReporter.SendQueue = eager()) = ProgressReporter(f, q, monoNow = { mono }, wallNow = { wall })

    @Test fun sendsInCallOrderAndSkipsUnchangedTicks() = runTest {
        val f = Fake(); val r = reporter(f)
        r.report("tick", "e1", 60.0, 1, 2, "s", 1_440_000)
        assertNull("nothing new", r.report("tick", "e1", 60.0, 1, 2, "s", 1_440_000))
        r.report("pause", "e1", 61.0, 1, 2, "s", 1_440_000)
        r.markWatched("e1")
        assertEquals(listOf("progress e1 60000", "progress e1 61000", "played e1", "refresh"), f.sent.toList())
        assertEquals(1, f.refreshes)
        assertTrue(f.queue.isEmpty())
    }

    @Test fun nothingAtPositionZero() = runTest {
        val f = Fake()
        assertNull(reporter(f).report("stop", "e1", 0.0, null, null, null, null))
        assertTrue(f.sent.isEmpty() && f.queue.isEmpty())
    }

    // The age: a player left paused reports, on closing, how long ago its position was reached.
    @Test fun ageCountsFromTheLastMove() = runTest {
        val f = Fake(); val r = reporter(f)
        r.report("pause", "e1", 700.0, 1, 2, null, null)
        mono += 300_000 // paused five minutes
        r.notePosition(700.1) // mpv's jitter on a paused frame is not a move
        assertEquals(300_000, r.report("stop", "e1", 700.0, 1, 2, null, null)!!.ageMs)
        mono += 1_000
        assertEquals("a track picked counts as fresh", 0, r.report("stop", "e1", 700.0, 1, 3, null, null)!!.ageMs)
        r.newItem(); mono += 5_000
        assertEquals("a new item starts afresh", 0, r.report("tick", "e2", 10.0, 1, 2, null, null)!!.ageMs)
    }

    // Found in review (process termination): a report waiting for its turn lived only in
    // memory. It is in the durable queue the moment it is made, sent or not.
    @Test fun aReportIsDurableBeforeItIsSent() = runTest {
        val f = Fake()
        // A sender that never gets to run (the app is killed first).
        val stopped = ProgressReporter.SendQueue(CoroutineScope(StandardTestDispatcher(testScheduler) + Job()))
        val r = reporter(f, stopped)
        r.report("stop", "e1", 900.0, null, null, null, null)
        r.markWatched("e2")
        assertEquals("nothing sent", emptyList<String>(), f.sent.toList())
        assertEquals("both survive the app", listOf("e1" to false, "e2" to true), f.queue.map { it.itemId to it.played })
    }

    // Found in review (extended stalls): reports made while one request hangs do not pile up
    // behind it. The queue keeps the newest per item and a watched mark ahead of what came
    // after it; one drain waits, whatever the number of reports.
    @Test fun aStallDoesNotPileUpReports() = runTest {
        val f = Fake(); val r = reporter(f)
        f.stall = CompletableDeferred()
        r.report("tick", "e1", 10.0, null, null, null, null) // in flight, held
        for (i in 11..60) r.report("tick", "e1", i.toDouble(), null, null, null, null)
        r.markWatched("e1")
        r.report("tick", "e2", 5.0, null, null, null, null)
        assertTrue("a few entries, not 52: ${f.queue.size}", f.queue.size <= 3)
        f.stall!!.complete(Unit)
        // What reaches the server: e1 watched (the mark replaced its positions), then e2.
        assertTrue("e1 marked watched: ${f.sent}", "played e1" in f.sent)
        assertEquals("progress e2 5000", f.sent.last { it != "refresh" })
        assertTrue(f.queue.isEmpty())
        assertTrue("at most the held one plus the newest of each: ${f.sent}", f.sent.count { it != "refresh" } <= 4)
    }

    // Server away: queued, a minute without trying (no timeout on every tick), then what
    // was queued goes first, the newest per item.
    @Test fun serverAwayRestsThenSendsTheQueue() = runTest {
        val f = Fake(); val r = reporter(f)
        f.serverUp = false
        r.report("tick", "e1", 60.0, null, null, null, null)
        assertTrue(r.resting())
        f.serverUp = true
        wall += 30_000
        r.report("tick", "e1", 70.0, null, null, null, null)
        assertEquals("resting: nothing sent", emptyList<String>(), f.sent.toList())
        wall += 31_000
        r.report("tick", "e2", 5.0, null, null, null, null)
        assertEquals(listOf("progress e1 70000", "progress e2 5000"), f.sent.toList())
    }

    // The server answered with an error: it is not away, no rest (the entry stays queued).
    @Test fun anErrorAnswerIsNotTheServerAway() = runTest {
        val f = Fake(); val r = reporter(f)
        f.serverError = true
        r.report("tick", "e1", 60.0, null, null, null, null)
        assertTrue(!r.resting())
        f.serverError = false
        r.report("tick", "e1", 70.0, null, null, null, null)
        assertEquals(listOf("progress e1 70000"), f.sent.toList())
    }

    // Marked watched while the server is away: kept, and sent when it is back.
    @Test fun markWatchedOffline() = runTest {
        val f = Fake(); val r = reporter(f)
        f.serverUp = false
        r.markWatched("e1")
        assertEquals(listOf("e1" to true), f.queue.map { it.itemId to it.played })
        f.serverUp = true; wall += 61_000
        r.report("tick", "e2", 5.0, null, null, null, null)
        assertEquals(listOf("played e1", "progress e2 5000", "refresh"), f.sent.toList())
    }

    // One queue for every player: the player being replaced and its successor keep order.
    @Test fun twoPlayersShareTheOrder() = runTest {
        val f = Fake(); val q = eager()
        val a = reporter(f, q); val b = reporter(f, q)
        a.report("stop", "e1", 100.0, null, null, null, null)
        b.report("tick", "e2", 5.0, null, null, null, null)
        a.markWatched("e1")
        assertEquals(listOf("progress e1 100000", "refresh", "progress e2 5000", "played e1", "refresh"), f.sent.toList())
    }

    // The order is fixed by the caller (the queue), whatever thread sends: 60 interleaved
    // reports and watched marks on a thread pool, the server answering at varying speeds.
    @Test fun callOrderHoldsOnAThreadPool() = runBlocking {
        val f = Fake(); f.answerDelayMs = 3
        val r = ProgressReporter(f, CoroutineScope(Dispatchers.Default), monoNow = { 0L })
        val expected = mutableListOf<String>()
        for (i in 1..60) {
            r.report("pause", "e$i", i.toDouble(), null, null, null, null); expected += "progress e$i ${i * 1000}"
            r.markWatched("w$i"); expected += "played w$i"
        }
        withTimeout(20_000) { while (f.sent.count { it != "refresh" } < expected.size) delay(10) }
        assertEquals(expected, f.sent.filter { it != "refresh" })
    }

    // Cancelled mid-send is not a failure: no rest, and what was queued stays queued.
    @Test fun cancelledInFlightIsNotAFailure() = runTest {
        val f = Fake(); f.stall = CompletableDeferred()
        val job = Job()
        val q = ProgressReporter.SendQueue(CoroutineScope(UnconfinedTestDispatcher(testScheduler) + job))
        val r = reporter(f, q)
        r.report("stop", "e1", 100.0, null, null, null, null)
        job.cancel()
        assertTrue("no minute's rest", !r.resting())
        assertEquals("still queued for the next start", listOf("e1"), f.queue.map { it.itemId })
    }

    // Found in review: with a drain under way (a tick), a stop made meanwhile set the refresh,
    // and that drain, finishing the tick, used it up: lists reloaded before the stop was
    // sent, and not after. The refresh now follows the drain that sends the stop.
    @Test fun listsReloadAfterTheStopIsSent() = runTest {
        val f = Fake(); val r = reporter(f)
        f.stall = CompletableDeferred()
        r.report("tick", "e1", 10.0, null, null, null, null) // in flight, held
        r.report("stop", "e1", 20.0, null, null, null, null)
        f.stall!!.complete(Unit)
        assertEquals(listOf("progress e1 10000", "progress e1 20000", "refresh"), f.sent.toList())
    }
}

