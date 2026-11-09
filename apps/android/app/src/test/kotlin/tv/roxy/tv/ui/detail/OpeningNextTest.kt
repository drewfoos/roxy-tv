package tv.roxy.tv.ui.detail

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import tv.roxy.tv.api.Item

class OpeningNextTest {
    private val seasons = listOf(Item(id = "s1", type = "season", title = "S1"), Item(id = "s2", type = "season", title = "S2"))
    private val inS2 = Item(id = "e7", type = "episode", title = "E7", parentId = "s2")

    // /next answers: the page opens on that episode's season.
    @Test fun answerOpensItsSeason() = runBlocking {
        val w = awaitNext(CompletableDeferred(inS2), waitMs = 1_000)
        assertTrue(w.answered)
        assertEquals(1, openingSeason(seasons, w))
    }

    // /next hangs: the wait ends and the first season opens (the episodes no longer wait on it).
    @Test(timeout = 5_000) fun hungNextOpensTheFirstSeason() = runBlocking {
        val w = awaitNext(CompletableDeferred(), waitMs = 50)
        assertFalse(w.answered)
        assertEquals(0, openingSeason(seasons, w))
    }

    // Nothing to play (or an episode of no listed season): the first season.
    @Test fun noNextOpensTheFirstSeason() = runBlocking {
        assertEquals(0, openingSeason(seasons, awaitNext(CompletableDeferred(null), waitMs = 1_000)))
        assertEquals(0, openingSeason(seasons, NextWait(inS2.copy(parentId = "gone"), answered = true)))
    }
}
