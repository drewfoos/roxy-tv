package tv.roxy.tv.ui.phone

import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import tv.roxy.tv.api.DiscoverFilm

/**
 * A film's row in a series' season list on Discover: a film that is only listed gets a
 * Request of its own, one TMDB could not be matched to gets none, and one the library
 * has says so.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = android.app.Application::class, qualifiers = "w412dp-h915dp-xxhdpi")
class DiscoverFilmRowTest {
    @get:Rule val compose = createComposeRule()
    private var requested = 0
    private var opened = 0

    private fun show(f: DiscoverFilm, enabled: Boolean = true) = compose.setContent {
        DiscoverFilmRow(f, requestsEnabled = enabled, onOpen = { opened++ }, onRequest = { requested++ })
    }

    @Test fun aListedFilmHasItsOwnRequest() {
        show(DiscoverFilm(anilistId = 1, tmdbId = 13, title = "Aside", year = 2019, note = "Side story"))
        compose.onNodeWithText("Film  ·  2019  ·  Side story").assertExists()
        compose.onNodeWithText("Request").performClick()
        assertEquals(1, requested)
        assertEquals("the chip does not open the film", 0, opened)
    }

    @Test fun aFilmNotMatchedOnTmdbCannotBeRequestedOrOpened() {
        show(DiscoverFilm(anilistId = 1, title = "Unknown", note = "Not found on TMDB"))
        compose.onAllNodesWithText("Request").assertCountEquals(0)
        compose.onNodeWithText("Unknown").performClick()
        assertEquals(0, opened)
    }

    @Test fun noRequestWhileRequestsAreOff() {
        show(DiscoverFilm(anilistId = 1, tmdbId = 13, title = "Aside"), enabled = false)
        compose.onAllNodesWithText("Request").assertCountEquals(0)
    }

    @Test fun aFilmInTheLibrarySaysSoAndOpens() {
        show(DiscoverFilm(anilistId = 1, tmdbId = 13, title = "Between", libraryItemId = "lib-1", withRequest = false, eligible = true))
        compose.onNodeWithText("In library").assertExists()
        compose.onAllNodesWithText("Request").assertCountEquals(0)
        compose.onNodeWithText("Between").performClick()
        assertEquals(1, opened)
    }
}
