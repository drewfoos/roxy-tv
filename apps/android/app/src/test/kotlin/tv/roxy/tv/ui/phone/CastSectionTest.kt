package tv.roxy.tv.ui.phone

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.width
import androidx.compose.material3.Text
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.assertIsNotSelected
import androidx.compose.ui.test.assertIsSelected
import androidx.compose.ui.test.assertTextEquals
import androidx.compose.ui.test.getBoundsInRoot
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import tv.roxy.tv.api.CastGroup
import tv.roxy.tv.api.CastMember
import tv.roxy.tv.api.Item
import tv.roxy.tv.api.ItemDetails
import tv.roxy.tv.api.TmdbExtras

/**
 * The cast on a phone page. Someone who watches the dub sees the dub's voices first, and
 * a list beside the heading offers every other language's; a title with one cast has none.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = android.app.Application::class, qualifiers = "w412dp-h915dp-xxhdpi")
class CastSectionTest {
    @get:Rule val compose = createComposeRule()

    private val jpn = CastGroup("jpn", "Japanese", original = true, members = listOf(CastMember("Kaito Ishikawa", "Sakuta", tmdbId = 7), CastMember("Asami Seto", "Mai")))
    private val eng = CastGroup("eng", "English", members = listOf(CastMember("Stephen Fu", "Sakuta", tmdbId = 9), CastMember("Erica Mendez", "Mai")))
    private val fra = CastGroup("fra", "French", members = listOf(CastMember("Un Deux", "Sakuta")))
    private fun details(id: String = "a", casts: List<CastGroup> = listOf(jpn, eng), language: String = "eng") =
        ItemDetails(Item(id = id, type = "series", title = "Show"), tmdb = TmdbExtras(cast = listOf(CastMember("From TMDB", "Sakuta"))), casts = casts, castLanguage = language)

    private val pill get() = compose.onNodeWithTag("cast-languages")
    private fun pick(language: String) {
        pill.performClick()
        compose.onNodeWithTag("cast-language-$language").performClick()
        compose.waitForIdle()
    }

    @Test fun theDubsCastShowsFirstAndTheListShowsTheOthers() {
        val opened = mutableListOf<CastMember>()
        compose.setContent { Box(Modifier.width(412.dp)) { Column { CastSection(details(casts = listOf(jpn, eng, fra)), onOpenPerson = { opened += it }) { Text("Cast") } } } }
        compose.onNodeWithText("Stephen Fu").assertExists()
        compose.onNodeWithText("Kaito Ishikawa").assertDoesNotExist()
        compose.onNodeWithText("From TMDB").assertDoesNotExist()
        // Closed, the control names the cast shown and nothing else.
        pill.assertTextEquals("English")
        compose.onNodeWithText("French").assertDoesNotExist()

        // Open, it lists every language the title has a cast in, the one shown marked.
        pill.performClick()
        compose.onNodeWithTag("cast-language-jpn").assertTextEquals("Japanese (original)").assertIsNotSelected()
        compose.onNodeWithTag("cast-language-eng").assertTextEquals("English").assertIsSelected()
        compose.onNodeWithTag("cast-language-fra").assertTextEquals("French").performClick()
        compose.waitForIdle()
        compose.onNodeWithText("Un Deux").assertExists()
        compose.onNodeWithText("Stephen Fu").assertDoesNotExist()
        pill.assertTextEquals("French")
        compose.onNodeWithTag("cast-language-eng").assertDoesNotExist() // the list closed

        pick("jpn")
        compose.onNodeWithText("Kaito Ishikawa").assertExists()
        pill.assertTextEquals("Japanese")
        compose.onNodeWithText("Kaito Ishikawa").performClick()
        assertEquals(listOf(7), opened.map { it.tmdbId })
        // Someone the server could not link to a page shows but does not open.
        compose.onNodeWithText("Asami Seto").performClick()
        assertEquals(1, opened.size)
    }

    @Test fun theControlFitsBesideTheHeading() {
        compose.setContent { Box(Modifier.width(412.dp)) { Column { CastSection(details()) { Text("Cast") } } } }
        val heading = compose.onNodeWithText("Cast").getBoundsInRoot()
        val switch = pill.getBoundsInRoot()
        assertTrue("the control is right of the heading", switch.left >= heading.right)
        assertTrue("and on the screen (ends at ${switch.right})", switch.right <= 412.dp)
    }

    @Test fun oneCastHasNoSwitch() {
        compose.setContent { Box(Modifier.width(412.dp)) { Column { CastSection(details(casts = listOf(jpn))) { Text("Cast") } } } }
        compose.onNodeWithText("From TMDB").assertExists()
        compose.onNodeWithTag("cast-languages").assertDoesNotExist()
    }

    @Test fun noCastShowsNothing() {
        compose.setContent { Column { CastSection(ItemDetails(Item(id = "a", type = "movie", title = "Film"))) { Text("Cast") } } }
        compose.onNodeWithText("Cast").assertDoesNotExist()
    }

    @Test fun anotherTitleStartsOnItsOwnDefault() {
        var d by mutableStateOf(details())
        compose.setContent { Box(Modifier.width(412.dp)) { Column { CastSection(d) { Text("Cast") } } } }
        pick("jpn")
        compose.onNodeWithText("Kaito Ishikawa").assertExists()
        d = details(id = "b")
        compose.waitForIdle()
        compose.onNodeWithText("Stephen Fu").assertExists()
        pill.assertTextEquals("English")
    }
}
