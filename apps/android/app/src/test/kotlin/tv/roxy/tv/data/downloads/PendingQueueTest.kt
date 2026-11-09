package tv.roxy.tv.data.downloads

import org.junit.Assert.assertEquals
import org.junit.Test

class PendingQueueTest {
    private fun p(item: String, pos: Long, played: Boolean = false, at: Long = 0, profile: String? = "pr") = PendingProgress(profile, item, pos, at, played)

    // Finished offline, then opened again for a few seconds: the watched mark stays, first.
    @Test fun watchedMarkSurvivesALaterReport() {
        val q = pendingAfter(listOf(p("e1", 0, played = true, at = 1)), "pr", "e1", sent = false, next = p("e1", 15_000, at = 2))
        assertEquals(listOf(true, false), q.map { it.played })
        // Sent live (the server took it): the mark is still owed.
        assertEquals(listOf(true), pendingAfter(listOf(p("e1", 0, played = true)), "pr", "e1", sent = true, next = p("e1", 15_000)).map { it.played })
    }

    // Otherwise the newest replaces what was queued for the item (this profile only).
    @Test fun newestReplacesOlder() {
        val q = pendingAfter(listOf(p("e1", 5_000), p("e2", 7_000), p("e1", 6_000, profile = "other")), "pr", "e1", sent = false, next = p("e1", 9_000))
        assertEquals(listOf("e2:7000", "e1:6000", "e1:9000"), q.map { "${it.itemId}:${it.positionMs}" })
        // A new watched mark replaces everything queued for the item.
        assertEquals(listOf(true), pendingAfter(listOf(p("e1", 0, played = true), p("e1", 5_000)), "pr", "e1", sent = false, next = p("e1", 0, played = true)).map { it.played })
    }
}
