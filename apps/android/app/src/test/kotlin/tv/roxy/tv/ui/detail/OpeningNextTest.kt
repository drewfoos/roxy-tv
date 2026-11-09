package tv.roxy.tv.ui.detail

import org.junit.Assert.assertEquals
import org.junit.Test
import tv.roxy.tv.api.Item

/** openingSeason: the entry of the next episode in the list on screen (timing: SeriesOpeningTest). */
class OpeningNextTest {
    private val seasons = listOf(Item(id = "s1", type = "season", title = "S1"), Item(id = "s2", type = "season", title = "S2"))
    private val inS2 = Item(id = "e7", type = "episode", title = "E7", parentId = "s2")

    @Test fun answerOpensItsSeason() = assertEquals(1, openingSeason(seasons, NextWait(inS2, answered = true)))

    // No answer in time, nothing to play, or an episode of no listed season: the first.
    @Test fun otherwiseTheFirstSeason() {
        assertEquals(0, openingSeason(seasons, NextWait(null, answered = false)))
        assertEquals(0, openingSeason(seasons, NextWait(null, answered = true)))
        assertEquals(0, openingSeason(seasons, NextWait(inS2.copy(parentId = "gone"), answered = true)))
    }
}
