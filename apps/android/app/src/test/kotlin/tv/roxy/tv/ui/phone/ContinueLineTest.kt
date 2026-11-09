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
