package tv.roxy.tv.ui.tv

import androidx.compose.foundation.layout.Box
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import tv.roxy.tv.api.DiscoverFilm
import tv.roxy.tv.ui.FilmAction
import tv.roxy.tv.ui.filmAction

/**
 * A film among a series' seasons on the TV's Discover title page: OK requests a film that
 * can be requested, opens one the library has, opens the film's own page otherwise; a
 * film TMDB could not be matched to does nothing.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = android.app.Application::class, qualifiers = "w960dp-h540dp-xhdpi")
class TvFilmPillTest {
    @get:Rule val compose = createComposeRule()
    private var pressed = 0
    private val focus = FocusRequester()

    /** The entry inside a box whose requester hands focus to it, as the page's row does. */
    @Composable private fun Pill(f: DiscoverFilm) = Box(Modifier.focusRequester(focus)) { TvFilmPill(f, requestsEnabled = true) { pressed++ } }
    private fun giveFocus() { compose.runOnIdle { runCatching { focus.requestFocus() } }; compose.waitForIdle() }

    @Test fun okDoesWhatTheServersFieldsAllow() {
        val listed = DiscoverFilm(anilistId = 1, tmdbId = 13, title = "Aside", note = "Side story")
        assertEquals(FilmAction.Request, filmAction(listed, requestsEnabled = true))
        assertEquals("requests off: the film's page says why", FilmAction.OpenPage, filmAction(listed, requestsEnabled = false))
        assertEquals(FilmAction.OpenLibrary, filmAction(listed.copy(libraryItemId = "lib-1"), true))
        assertEquals(FilmAction.OpenPage, filmAction(listed.copy(requestStatus = "sent"), true))
        assertEquals(FilmAction.OpenPage, filmAction(listed.copy(inRadarr = true), true))
        assertEquals(FilmAction.None, filmAction(DiscoverFilm(anilistId = 2, title = "Unknown"), true))
    }

    @Test fun aListedFilmSaysWhyAndIsPressed() {
        compose.setContent { TvFilmPill(DiscoverFilm(anilistId = 1, tmdbId = 13, title = "Aside", year = 2019, note = "Side story"), requestsEnabled = true) { pressed++ } }
        compose.onNodeWithText("Film  ·  2019  ·  Side story").assertExists()
        compose.onNodeWithText("Aside").performClick()
        assertEquals(1, pressed)
    }

    @Test fun focusedItSaysWhatOkDoes() {
        compose.setContent { Pill(DiscoverFilm(anilistId = 1, tmdbId = 13, title = "Aside", note = "Side story")) }
        compose.onNodeWithText("Film  ·  Side story").assertExists()
        giveFocus()
        compose.onNodeWithText("Request").assertExists()
        compose.onNodeWithText("Film  ·  Side story").assertDoesNotExist()
    }

    @Test fun aFilmNotMatchedOnTmdbTakesNoFocus() {
        compose.setContent { Pill(DiscoverFilm(anilistId = 2, title = "Unknown", note = "Not found on TMDB")) }
        compose.onNodeWithText("Film  ·  Not found on TMDB").assertExists()
        giveFocus()
        compose.onNodeWithText("Film  ·  Not found on TMDB").assertExists() // still its reason, not an action
    }
}
