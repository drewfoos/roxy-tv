package tv.roxy.tv.ui.phone

import org.junit.Assert.assertEquals
import org.junit.Test
import tv.roxy.tv.api.Item

class ContinueLineTest {
    // The season in its short form, then the episode: with several seasons "Episode 2" alone does not say which.
    @Test fun theSeasonIsNamedShortBeforeTheEpisode() {
        assertEquals("S3 · Episode 2", continueLine(Item(id = "e", type = "episode", title = "An Explosion", seasonAired = 3, indexAired = 2)))
        assertEquals("S1 · Episode 12", continueLine(Item(id = "e", type = "episode", title = "First season", seasonAired = 1, indexAired = 12)))
        // An episode counted through the whole show (no number within its season) keeps the season it sits in.
        assertEquals("S2 · Episode 30", continueLine(Item(id = "a", type = "episode", title = "Absolute", seasonAired = 2, indexAbsolute = 30)))
    }

    // No title; a special or an episode of no known season has no season to name; nothing for a film.
    @Test fun noSeasonWhereThereIsNoneToName() {
        assertEquals("Episode 3", continueLine(Item(id = "s", type = "episode", title = "OVA", seasonAired = 0, indexAired = 3)))
        assertEquals("Episode 7", continueLine(Item(id = "n", type = "episode", title = "No season", indexAired = 7)))
        assertEquals("", continueLine(Item(id = "x", type = "episode", title = "Odd file")))
        assertEquals("", continueLine(Item(id = "x", type = "episode", title = "Odd file in a season", seasonAired = 2)))
        assertEquals("", continueLine(Item(id = "m", type = "movie", title = "Film", year = 2019)))
    }
}

class ContinueMinutesTest {
    private fun ep(pos: Long, len: Long?) = Item(id = "e", type = "episode", title = "E", runtimeMs = len, userData = tv.roxy.tv.api.UserData(positionMs = pos))

    // The card's tag: the length before the episode is started (it was blank), what is left after.
    @Test fun lengthUntilStarted() {
        assertEquals("24m", tv.roxy.tv.ui.tv.minutesLabel(ep(0, 1_440_000)))
        assertEquals("23m left", tv.roxy.tv.ui.tv.minutesLabel(ep(60_000, 1_440_000)))
        assertEquals(null, tv.roxy.tv.ui.tv.minutesLabel(ep(0, null)))
    }
}
