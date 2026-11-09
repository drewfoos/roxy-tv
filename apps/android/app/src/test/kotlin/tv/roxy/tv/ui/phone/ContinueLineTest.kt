package tv.roxy.tv.ui.phone

import org.junit.Assert.assertEquals
import org.junit.Test
import tv.roxy.tv.api.Item

class ContinueLineTest {
    // "Episode #" and nothing else: no season, no title, specials alike; nothing for a film.
    @Test fun episodeNumberOnly() {
        assertEquals("Episode 2", continueLine(Item(id = "e", type = "episode", title = "An Explosion", seasonAired = 3, indexAired = 2)))
        assertEquals("Episode 3", continueLine(Item(id = "s", type = "episode", title = "OVA", seasonAired = 0, indexAired = 3)))
        assertEquals("", continueLine(Item(id = "x", type = "episode", title = "Odd file")))
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
