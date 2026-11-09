package tv.roxy.tv.ui.detail

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import tv.roxy.tv.api.Item
import tv.roxy.tv.api.Section

class SectionEntriesTest {
    private val s1 = Item(id = "s1", type = "season", title = "Season 1", indexAired = 1)
    private val s2 = Item(id = "s2", type = "season", title = "Season 2", indexAired = 2)
    private val specials = Item(id = "s0", type = "season", title = "Specials", indexAired = 0)
    private val film = Item(id = "film", type = "movie", title = "The Movie", year = 2014)

    // Seasons as themselves, a group of specials and a film as entries of their own,
    // in the server's (release) order.
    @Test fun releaseOrder() {
        val got = sectionEntries(
            listOf(
                Section(kind = "season", title = "Season 1", seasonId = "s1", episodes = 12),
                Section(kind = "specials", title = "OVAs", seasonId = "s0", episodeIds = listOf("ova1", "ova2"), episodes = 2, year = 2012),
                Section(kind = "season", title = "Season 2", seasonId = "s2", episodes = 12),
                Section(kind = "movie", title = "The Movie", item = film, episodes = 1, year = 2014),
                Section(kind = "specials", title = "Specials", seasonId = "s0", episodeIds = listOf("odd"), episodes = 1),
            ),
            listOf(s1, s2, specials),
        )!!
        assertEquals(listOf("Season 1", "OVAs", "Season 2", "The Movie", "Specials"), got.seasons.map { it.title })
        assertEquals(listOf("s1", "specials:s0:ova1:OVAs", "s2", "film:film", "specials:s0:odd:Specials"), got.seasons.map { it.id })
        assertEquals(SectionSource.Specials("s0", listOf("ova1", "ova2")), got.sources["specials:s0:ova1:OVAs"])
        assertEquals(SectionSource.Movie(film), got.sources["film:film"])
        assertEquals(2, got.counts["specials:s0:ova1:OVAs"])
        assertEquals("Film · 2014", got.labels["film:film"])
        assertNull(got.labels["specials:s0:ova1:OVAs"])
    }

    // Nothing usable: the plain season list instead.
    @Test fun fallsBackWhenUnusable() {
        assertNull(sectionEntries(emptyList(), listOf(s1)))
        assertNull(sectionEntries(listOf(Section(kind = "season", seasonId = "gone")), listOf(s1)))
    }
}
