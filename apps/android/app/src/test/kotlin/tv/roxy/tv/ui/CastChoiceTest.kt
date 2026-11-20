package tv.roxy.tv.ui

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import tv.roxy.tv.api.CastGroup
import tv.roxy.tv.api.CastMember
import tv.roxy.tv.api.Item
import tv.roxy.tv.api.ItemDetails
import tv.roxy.tv.api.TmdbExtras

/**
 * Which cast a page shows. An anime's TMDB cast is the Japanese one, which says nothing
 * to someone who watches the dub: the server sends the voice casts and names the one to
 * show first, and the page's switch picks another.
 */
class CastChoiceTest {
    private val item = Item(id = "a", type = "series", title = "Show")
    private val tmdb = TmdbExtras(cast = listOf(CastMember("Kaito Ishikawa", "Sakuta")))
    private val jpn = CastGroup("jpn", "Japanese", original = true, members = listOf(CastMember("Kaito Ishikawa", "Sakuta", tmdbId = 7)))
    private val eng = CastGroup("eng", "English", members = (1..10).map { CastMember("Voice $it", "Role $it") })

    @Test fun theServersDefaultIsShownFirst() {
        val shown = shownCast(ItemDetails(item, tmdb = tmdb, casts = listOf(jpn, eng), castLanguage = "eng"))
        assertEquals("eng", shown.language)
        assertEquals("Voice 1", shown.members.first().name)
        assertEquals(listOf("jpn", "eng"), shown.groups.map { it.language })
        assertEquals("English cast", castHeading(shown))
    }

    @Test fun theSwitchPicksAnother() {
        val d = ItemDetails(item, tmdb = tmdb, casts = listOf(jpn, eng), castLanguage = "eng")
        assertEquals("Kaito Ishikawa", shownCast(d, "jpn").members.single().name)
        assertEquals("a language the title does not have falls back to the default", "eng", shownCast(d, "deu").language)
    }

    @Test fun withoutADefaultItIsTheFirst() {
        assertEquals("jpn", shownCast(ItemDetails(item, casts = listOf(jpn, eng))).language)
    }

    @Test fun oneCastIsTheOldCastWithNoSwitch() {
        for (casts in listOf(emptyList(), listOf(jpn), listOf(jpn, CastGroup("eng", "English")))) {
            val shown = shownCast(ItemDetails(item, tmdb = tmdb, casts = casts, castLanguage = "eng"))
            assertTrue(shown.groups.isEmpty())
            assertEquals(tmdb.cast, shown.members)
            assertEquals("Cast", castHeading(shown))
        }
        assertTrue(shownCast(null).members.isEmpty())
    }

    @Test fun theTvLineNamesTheLanguageItShows() {
        val line = castLine(ItemDetails(item, tmdb = tmdb, casts = listOf(jpn, eng), castLanguage = "eng"))
        assertEquals("English cast", line?.first)
        assertEquals((1..8).joinToString(", ") { "Voice $it" }, line?.second)
        assertEquals("Cast" to "Kaito Ishikawa", castLine(ItemDetails(item, tmdb = tmdb)))
        assertNull(castLine(ItemDetails(item)))
    }
}
