package tv.roxy.tv.ui.detail

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import tv.roxy.tv.api.Item

class RowNumberTest {
    private fun ep(id: String, n: Int) = Item(id = id, type = "episode", title = id, indexAired = n, seasonAired = 0)

    // KonoSuba's specials carry TheTVDB's numbers 3 to 7: a group of specials numbers its
    // rows by place instead; a film's row shows none; a season keeps its own numbers.
    @Test fun specialsByPlaceFilmNoneSeasonOwn() {
        val choker = ep("choker", 3)
        val art = ep("art", 4)
        val specials = SeriesUiState(rowNumbers = rowNumbersFor(SectionSource.Specials("s0", listOf("choker", "art")), listOf(choker, art)))
        assertEquals(listOf(1, 2), listOf(choker, art).map { specials.rowNumber(it) })

        val crimson = ep("crimson", 5)
        val film = SeriesUiState(rowNumbers = rowNumbersFor(SectionSource.Movie(crimson), listOf(crimson)))
        assertNull(film.rowNumber(crimson))

        val season = SeriesUiState(rowNumbers = rowNumbersFor(null, listOf(ep("e7", 7))))
        assertEquals(7, season.rowNumber(ep("e7", 7)))
    }
}
