package tv.roxy.tv.data.downloads

import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import tv.roxy.tv.api.ApiException
import java.io.IOException

class PendingQueueTest {
    private fun p(item: String, pos: Long, played: Boolean = false, at: Long = 0, profile: String? = "pr") = PendingProgress(profile, item, pos, at, played)

    // Finished offline, then opened again for a few seconds: the watched mark stays, first.
    @Test fun watchedMarkSurvivesALaterReport() {
        val q = pendingAfter(listOf(p("e1", 0, played = true, at = 1)), "pr", "e1", sent = false, next = p("e1", 15_000, at = 2))
        assertEquals(listOf(true, false), q.map { it.played })
    }

    // Found in review: a watched mark replaced the "end" report queued just before it (its
    // subtitle choice and version were lost). One entry per item and kind: both stay, in order.
    @Test fun watchedMarkKeepsTheReportBeforeIt() {
        val q = pendingAfter(listOf(p("e1", 1_400_000)), "pr", "e1", sent = false, next = p("e1", 0, played = true))
        assertEquals(listOf("1400000:false", "0:true"), q.map { "${it.positionMs}:${it.played}" })
    }

    // A newer position replaces the item's queued position (this profile only); a newer mark
    // replaces its mark.
    @Test fun newestOfEachKindReplacesOlder() {
        val q = pendingAfter(listOf(p("e1", 5_000), p("e2", 7_000), p("e1", 6_000, profile = "other")), "pr", "e1", sent = false, next = p("e1", 9_000))
        assertEquals(listOf("e2:7000", "e1:6000", "e1:9000"), q.map { "${it.itemId}:${it.positionMs}" })
        // Two marks in a row are one.
        assertEquals(listOf(true), pendingAfter(listOf(p("e1", 0, played = true)), "pr", "e1", sent = false, next = p("e1", 0, played = true)).map { it.played })
    }

    // Found in review: one entry per kind collapsed watched / rewatched part-way / watched
    // again into one mark (the server counted one play, not two). Every change of kind stays.
    @Test fun watchedRewatchedWatchedAgainKeepsAllThree() {
        var q = listOf<PendingProgress>()
        q = pendingAfter(q, "pr", "e1", false, p("e1", 1_400_000))
        q = pendingAfter(q, "pr", "e1", false, p("e1", 0, played = true))
        q = pendingAfter(q, "pr", "e1", false, p("e1", 300_000))
        q = pendingAfter(q, "pr", "e1", false, p("e1", 600_000)) // a newer position: one entry
        q = pendingAfter(q, "pr", "e1", false, p("e1", 0, played = true))
        assertEquals(listOf("1400000:false", "0:true", "600000:false", "0:true"), q.map { "${it.positionMs}:${it.played}" })
    }

    private fun drain(entries: List<PendingProgress>, now: Long = 0, answers: Map<String, Throwable>): Triple<Throwable?, List<String>, List<String>> = runBlocking {
        val sent = mutableListOf<String>(); val removed = mutableListOf<String>()
        val r = drainPending(entries, now, send = { e -> answers[e.itemId]?.let { throw it }; sent += e.itemId }, remove = { removed += it.itemId })
        Triple(r, sent, removed)
    }

    // Found in review: every report goes through this queue, so one entry the server keeps
    // refusing (a 500) stopped all watch progress. A refusal skips that entry; the rest goes.
    @Test fun aRefusedEntryDoesNotHoldUpTheRest() {
        val (r, sent, removed) = drain(listOf(p("bad", 1), p("e2", 2), p("e3", 3)), answers = mapOf("bad" to ApiException(500, "boom")))
        assertEquals(listOf("e2", "e3"), sent)
        assertEquals("the refused one is kept for later", listOf("e2", "e3"), removed)
        assertTrue(r is ApiException)
    }

    // The server away (no answer) stops the drain: nothing after it is tried now.
    @Test fun serverAwayStops() {
        val (r, sent, _) = drain(listOf(p("e1", 1), p("e2", 2)), answers = mapOf("e1" to IOException("away")))
        assertEquals(emptyList<String>(), sent)
        assertTrue(r is IOException)
    }

    // A 4xx will never succeed: dropped. A refusal older than a day: let go. Signed out: stop.
    @Test fun dropsAndStops() {
        val (r1, _, removed1) = drain(listOf(p("gone", 1), p("e2", 2)), answers = mapOf("gone" to ApiException(404, "no")))
        assertEquals(listOf("gone", "e2"), removed1); assertNull(r1)
        val (_, _, removed2) = drain(listOf(p("old", 1, at = 0)), now = PENDING_REFUSED_KEEP_MS + 1, answers = mapOf("old" to ApiException(500, "boom")))
        assertEquals(listOf("old"), removed2)
        val (r3, sent3, _) = drain(listOf(p("e1", 1), p("e2", 2)), answers = mapOf("e1" to ApiException(401, "signed out")))
        assertEquals(emptyList<String>(), sent3); assertTrue(r3 is ApiException)
    }

    // Found in review: the queue rode in downloads.json, saved 400 ms after a change, so a mark
    // recorded just before Android killed the app could be lost. Its own journal is written at
    // once: what a new owner (the restarted app) reads is what was last written.
    @Test fun journalSurvivesARestartImmediately() {
        val dir = java.nio.file.Files.createTempDirectory("journal").toFile()
        try {
            val file = java.io.File(dir, "progress-queue.json")
            val before = ProgressJournal(file)
            assertNull("no journal yet: an older install's queue is used", before.read())
            val q = listOf(p("e1", 1_400_000), p("e1", 0, played = true))
            before.write(q)
            assertEquals("read back by a new owner, no wait", q, ProgressJournal(file).read())
            before.write(emptyList())
            assertEquals(emptyList<PendingProgress>(), ProgressJournal(file).read())
            file.writeText("{ not json")
            assertNull("unreadable: treated as none", ProgressJournal(file).read())
        } finally { dir.deleteRecursively() }
    }

    // Found in review: a failed write was swallowed, and the replace (rename, else delete and
    // rename) could leave no journal if the app died in between. A write that fails now says
    // so, and the journal still holds the previous queue, whole.
    @Test fun aFailedWriteKeepsThePreviousJournal() {
        val dir = java.nio.file.Files.createTempDirectory("journal").toFile()
        try {
            val file = java.io.File(dir, "progress-queue.json")
            val j = ProgressJournal(file)
            val q1 = listOf(p("e1", 60_000))
            assertTrue(j.write(q1).isSuccess)
            java.io.File(file.path + ".tmp").mkdirs() // the disk refuses the temporary file
            assertTrue("the failure is returned", j.write(listOf(p("e1", 0, played = true))).isFailure)
            assertEquals("the previous queue, whole", q1, ProgressJournal(file).read())
        } finally { dir.deleteRecursively() }
    }
}

