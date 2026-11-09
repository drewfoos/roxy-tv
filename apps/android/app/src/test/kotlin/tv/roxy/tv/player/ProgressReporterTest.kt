package tv.roxy.tv.player

import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import tv.roxy.tv.api.ApiException
import java.io.IOException

/** The reporting path through a fake server and offline queue, with clocks the test moves. */
@OptIn(ExperimentalCoroutinesApi::class)
class ProgressReporterTest {
    private class Fake : ProgressReporter.Sink {
        val log = mutableListOf<String>()
        var serverUp = true
        var serverError = false
        val downloads = mutableSetOf<String>()
        val pending = mutableSetOf<String>()
        override suspend fun progress(r: ProgressReporter.Report) {
            if (serverError) throw ApiException(500, "boom")
            if (!serverUp) throw IOException("away")
            log += "progress ${r.reason} ${r.itemId} ${r.positionMs} age=${r.ageMs}"
        }
        override suspend fun markPlayed(itemId: String) {
            if (!serverUp) throw IOException("away")
            log += "played $itemId"
        }
        override fun isDownload(itemId: String) = itemId in downloads
        override fun hasPending(itemId: String) = itemId in pending
        override suspend fun syncPending() { if (pending.isNotEmpty()) { log += "sync ${pending.sorted()}"; pending.clear() } }
        override fun queue(r: ProgressReporter.Report, sent: Boolean) { log += "queue ${r.itemId} ${r.positionMs} sent=$sent"; if (!sent) pending += r.itemId else pending -= r.itemId }
        override fun queuePlayed(itemId: String, sent: Boolean) { log += "queue played $itemId sent=$sent"; if (!sent) pending += itemId }
        override fun userDataChanged() { log += "refresh" }
        override fun answered(t: Throwable) = t is ApiException
    }

    private var mono = 0L
    private var wall = 1_000_000L
    private fun TestScope.reporter(f: Fake) = ProgressReporter(f, this, monoNow = { mono }, wallNow = { wall })

    @Test fun sendsInCallOrderAndSkipsUnchangedTicks() = runTest {
        val f = Fake(); val r = reporter(f)
        r.report("tick", "e1", 60.0, 1, 2, "s", 1_440_000)
        r.report("tick", "e1", 60.0, 1, 2, "s", 1_440_000) // nothing new
        r.report("pause", "e1", 60.0, 1, 2, "s", 1_440_000) // a pause always goes
        r.markWatched("e1")
        advanceUntilIdle()
        assertEquals(listOf("progress tick e1 60000 age=0", "progress pause e1 60000 age=0", "played e1", "refresh"), f.log)
    }

    @Test fun nothingAtPositionZero() = runTest {
        val f = Fake()
        assertNull(reporter(f).report("stop", "e1", 0.0, null, null, null, null))
        advanceUntilIdle()
        assertTrue(f.log.isEmpty())
    }

    // The age: a player left paused reports, on closing, how long ago its position was reached.
    @Test fun ageCountsFromTheLastMove() = runTest {
        val f = Fake(); val r = reporter(f)
        r.report("pause", "e1", 700.0, 1, 2, null, null)
        mono += 300_000 // paused five minutes
        r.notePosition(700.1) // mpv's jitter on a paused frame is not a move
        val stop = r.report("stop", "e1", 700.0, 1, 2, null, null)!!
        assertEquals(300_000, stop.ageMs)
        // A track picked counts as fresh.
        mono += 1_000
        assertEquals(0, r.report("stop", "e1", 700.0, 1, 3, null, null)!!.ageMs)
        // A new item starts afresh.
        r.newItem(); mono += 5_000
        assertEquals(0, r.report("tick", "e2", 10.0, 1, 2, null, null)!!.ageMs)
    }

    // Server away: queued, then a minute without trying (no timeout on every tick); after
    // it the queue goes first, then the live report.
    @Test fun serverAwayQueuesThenRestsThenReplays() = runTest {
        val f = Fake(); val r = reporter(f)
        f.serverUp = false
        r.report("tick", "e1", 60.0, null, null, null, null); advanceUntilIdle()
        assertEquals(listOf("queue e1 60000 sent=false"), f.log)
        f.serverUp = true; f.log.clear()
        wall += 30_000
        r.report("tick", "e1", 70.0, null, null, null, null); advanceUntilIdle()
        assertEquals("resting: not sent, queued", listOf("queue e1 70000 sent=false"), f.log)
        f.log.clear(); wall += 31_000
        r.report("tick", "e1", 80.0, null, null, null, null); advanceUntilIdle()
        assertEquals(listOf("sync [e1]", "progress tick e1 80000 age=0"), f.log)
    }

    // The server answered with an error: it is not away, no rest.
    @Test fun anErrorAnswerIsNotTheServerAway() = runTest {
        val f = Fake(); val r = reporter(f)
        f.serverError = true
        r.report("tick", "e1", 60.0, null, null, null, null); advanceUntilIdle()
        f.serverError = false; f.log.clear(); f.pending.clear()
        r.report("tick", "e1", 70.0, null, null, null, null); advanceUntilIdle()
        assertEquals(listOf("progress tick e1 70000 age=0"), f.log)
    }

    // A download keeps its position locally even when the server took it; an end refreshes lists.
    @Test fun downloadsAreRecordedLocallyToo() = runTest {
        val f = Fake(); f.downloads += "d1"
        reporter(f).report("end", "d1", 1400.0, null, null, null, null); advanceUntilIdle()
        assertEquals(listOf("progress end d1 1400000 age=0", "queue d1 1400000 sent=true", "refresh"), f.log)
    }

    // An offline "watched" waiting for this item goes before the live report (or the server
    // would drop it as older).
    @Test fun pendingMarkGoesFirst() = runTest {
        val f = Fake(); f.pending += "e1"
        reporter(f).report("tick", "e1", 10.0, null, null, null, null); advanceUntilIdle()
        assertEquals(listOf("sync [e1]", "progress tick e1 10000 age=0"), f.log)
    }

    // Marked watched while the server is away: queued as a watched mark.
    @Test fun markWatchedOffline() = runTest {
        val f = Fake(); f.serverUp = false
        reporter(f).markWatched("e1"); advanceUntilIdle()
        assertEquals(listOf("queue played e1 sent=false"), f.log)
    }

    // One lock for every player: a report from the player being replaced and one from its
    // successor go out in call order.
    @Test fun twoPlayersShareTheOrder() = runTest {
        val f = Fake()
        val a = reporter(f); val b = reporter(f)
        a.report("stop", "e1", 100.0, null, null, null, null)
        b.report("tick", "e2", 5.0, null, null, null, null)
        a.markWatched("e1")
        advanceUntilIdle()
        assertEquals(listOf("progress stop e1 100000 age=0", "refresh", "progress tick e2 5000 age=0", "played e1", "refresh"), f.log)
    }
}
