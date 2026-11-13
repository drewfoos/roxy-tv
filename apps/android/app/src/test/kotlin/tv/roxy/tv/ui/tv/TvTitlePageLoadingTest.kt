package tv.roxy.tv.ui.tv

import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import tv.roxy.tv.api.Item
import tv.roxy.tv.api.UserData
import tv.roxy.tv.ui.detail.SeriesUiState

/**
 * The TV's title pages while they load: what is not read yet is a placeholder in its
 * final place, never words that change a moment later. The series page said "Play" (or
 * the open season's first episode) and then "CONTINUE: S2 E5"; the Extras tab appeared
 * between Details and More like this and pushed that one along.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], qualifiers = "w960dp-h540dp-xhdpi")
class TvTitlePageLoadingTest {
    @get:Rule val compose = createComposeRule()

    private val show = Item(id = "show", type = "series", title = "Show")
    private val s1 = Item(id = "s1", type = "season", title = "", indexAired = 1)
    private val e1 = Item(id = "e1", type = "episode", title = "One", parentId = "s1", indexAired = 1)
    private val opening = SeriesUiState(series = show, episodesLoading = true)

    @Test fun playHasNoWordsUntilTheyAreFinal() {
        assertNull(tvSeriesPlayLabel(opening, next = null))
        // The first season's episodes are in, the answer for the whole series is not.
        assertNull(tvSeriesPlayLabel(opening.copy(loading = false, seasons = listOf(s1), episodes = listOf(e1)), next = e1))
        val known = opening.copy(loading = false, seasons = listOf(s1), upNext = e1, upNextKnown = true)
        assertEquals(continueLabel(e1), tvSeriesPlayLabel(known, e1))
        val started = e1.copy(userData = UserData(positionMs = 5_000))
        assertEquals(continueLabel(started), tvSeriesPlayLabel(known, started))
    }

    @Test fun playSaysSoWhenNothingIsComing() {
        assertEquals("Nothing to play yet", tvSeriesPlayLabel(opening.copy(loading = false, episodesLoading = false), null))
        // /next never answered but the list is in: the list's own pick.
        assertEquals(continueLabel(e1), tvSeriesPlayLabel(opening.copy(loading = false, episodesLoading = false, episodes = listOf(e1)), e1))
    }

    @Test fun extrasComesLastSoTheOtherTabsStayPut() {
        val before = titleTabs("Episodes", extras = false)
        val after = titleTabs("Episodes", extras = true)
        assertEquals(listOf("Episodes", "Details", "More like this"), before)
        assertEquals(before, after.take(before.size))
        assertEquals("Extras", after.last())
        assertEquals(listOf("Movie", "Details", "More like this", "Extras"), titleTabs("Movie", extras = true))
    }

    @Test fun aRowWithoutWordsHoldsALineAndARowThatWaitsShowsTheCircle() {
        compose.setContent {
            androidx.compose.foundation.layout.Column {
                TvListRow(label = null, onClick = {})
            }
        }
        compose.onNodeWithTag("row-words-pending").assertExists()
        compose.onNodeWithTag("row-loading").assertDoesNotExist()
    }

    @Test fun aRowThatWaitsShowsTheCircleInTheIconsPlace() {
        compose.setContent { TvListRow(label = null, onClick = {}, icon = Icons.Filled.Check, loading = true) }
        compose.onNodeWithTag("row-loading").assertExists()
        compose.onNodeWithTag("row-words-pending").assertExists()
    }

    @Test fun aRowWithWordsShowsThem() {
        compose.setContent { TvListRow(label = "Request + 1 film", onClick = {}) }
        compose.onNodeWithText("Request + 1 film").assertExists()
        compose.onNodeWithTag("row-words-pending").assertDoesNotExist()
    }
}
