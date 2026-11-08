package tv.roxy.tv.ui.detail

import org.junit.Assert.assertEquals
import org.junit.Test
import tv.roxy.tv.api.Item
import tv.roxy.tv.api.ItemsResponse

class EpisodeRowsTest {
    private fun ep(id: String, s: Int, n: Int) = Item(id = id, type = "episode", title = id, seasonAired = s, indexAired = n)

    // A season read whole: its rows in order, complete.
    @Test fun wholeSeason() {
        val r = episodeRows(null, ItemsResponse(listOf(ep("e2", 1, 2), ep("e1", 1, 1)), 2))
        assertEquals(listOf("e1", "e2"), r.items.map { it.id })
        assertEquals(true, r.complete)
        assertEquals(2, r.expected)
    }

    // A season the read could not finish: the rows there are, marked incomplete, of the
    // server's total (what the screens say: "2 of 5").
    @Test fun partialSeason() {
        val r = episodeRows(null, ItemsResponse(listOf(ep("e1", 1, 1), ep("e2", 1, 2)), 5, complete = false))
        assertEquals(false, r.complete)
        assertEquals(5, r.expected)
        assertEquals(2, r.items.size)
    }

    // A group of specials names its episodes; one the read did not bring is missing, and
    // the group says so instead of being one shorter with nothing said.
    @Test fun specialsWithAMissingId() {
        val src = SectionSource.Specials("s0", listOf("sp3", "sp1", "sp9"))
        val r = episodeRows(src, ItemsResponse(listOf(ep("sp1", 0, 1), ep("sp3", 0, 3), ep("sp4", 0, 4)), 3))
        assertEquals(listOf("sp3", "sp1"), r.items.map { it.id }) // the section's order, its own only
        assertEquals(false, r.complete)
        assertEquals(3, r.expected)
    }

    // All named specials came, but the read itself was cut short: still incomplete.
    @Test fun specialsFromAnIncompleteRead() {
        val src = SectionSource.Specials("s0", listOf("sp1"))
        val r = episodeRows(src, ItemsResponse(listOf(ep("sp1", 0, 1)), 40, complete = false))
        assertEquals(false, r.complete)
    }

    @Test fun specialsAllThere() {
        val src = SectionSource.Specials("s0", listOf("sp2", "sp1"))
        val r = episodeRows(src, ItemsResponse(listOf(ep("sp1", 0, 1), ep("sp2", 0, 2)), 2))
        assertEquals(true, r.complete)
        assertEquals(listOf("sp2", "sp1"), r.items.map { it.id })
    }

    // A film entry: its one row.
    @Test fun film() {
        val f = Item(id = "f", type = "movie", title = "Film")
        assertEquals(true, episodeRows(SectionSource.Movie(f), ItemsResponse(listOf(f), 1)).complete)
    }
}
