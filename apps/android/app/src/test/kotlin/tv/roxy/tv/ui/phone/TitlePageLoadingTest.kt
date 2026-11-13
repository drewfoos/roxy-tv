package tv.roxy.tv.ui.phone

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import tv.roxy.tv.api.Item
import tv.roxy.tv.api.UserData
import tv.roxy.tv.ui.detail.SeriesUiState

/**
 * A title page opened from a card shows its header at once and reads the rest. What is not
 * read yet is a placeholder, never words that change a moment later: the series page said
 * "Episodes" and then "Season 1", "Play" and then "Start watching S01E01", and the film
 * page's "Part of" tab appeared between the other two.
 */
class TitlePageLoadingTest {
    private val show = Item(id = "show", type = "series", title = "Show")
    private val s1 = Item(id = "s1", type = "season", title = "", indexAired = 1)
    private val e1 = Item(id = "e1", type = "episode", title = "One", parentId = "s1", indexAired = 1)

    /** As the page opens: the header from the card, everything else on its way. */
    private val opening = SeriesUiState(series = show, episodesLoading = true)

    @Test fun theSeasonNameIsNotGuessedWhileTheListIsRead() {
        assertNull(seriesSeasonLabel(opening, selectedExtra = null))
        assertEquals("Season 1", seriesSeasonLabel(opening.copy(loading = false, seasons = listOf(s1)), null))
        // A show without season folders: said once that is known.
        assertEquals("Episodes", seriesSeasonLabel(opening.copy(loading = false), null))
    }

    @Test fun downloadAllHoldsItsPlaceUntilTheEpisodesAreIn() {
        assertTrue(downloadAllPending(opening, null))
        assertTrue(downloadAllPending(opening.copy(loading = false, seasons = listOf(s1)), null))
        assertFalse(downloadAllPending(opening.copy(loading = false, seasons = listOf(s1), episodes = listOf(e1), episodesLoading = false), null))
        // Nothing to wait for: a season the library lacks, a list that failed, an empty season.
        assertFalse(downloadAllPending(opening, selectedExtra = 2))
        assertFalse(downloadAllPending(opening.copy(loading = false, episodesLoading = false, episodesError = "no"), null))
        assertFalse(downloadAllPending(opening.copy(loading = false, seasons = listOf(s1), episodesLoading = false), null))
    }

    @Test fun playHasNoWordsUntilTheyAreFinal() {
        assertNull(seriesPlayLabel(opening, next = null))
        // The episodes of the first season are in but the server has not said what comes
        // next: the season's first episode is not announced, then replaced.
        assertNull(seriesPlayLabel(opening.copy(loading = false, seasons = listOf(s1), episodes = listOf(e1)), next = e1))
        val known = opening.copy(loading = false, seasons = listOf(s1), upNext = e1, upNextKnown = true)
        assertEquals("Start watching ${e1.episodeCode}", seriesPlayLabel(known, e1))
        val started = e1.copy(userData = UserData(positionMs = 5_000))
        assertEquals("Resume ${e1.episodeCode}", seriesPlayLabel(known, started))
    }

    @Test fun playSaysSoWhenNothingIsComing() {
        // The page read failed, or the show has nothing on disk: said, not waited for.
        assertEquals("Nothing to play yet", seriesPlayLabel(opening.copy(loading = false, episodesLoading = false), null))
        // /next never answered but the list is in: the list's own pick.
        assertEquals("Start watching ${e1.episodeCode}", seriesPlayLabel(opening.copy(loading = false, episodesLoading = false, episodes = listOf(e1)), e1))
    }

    @Test fun partOfComesAfterTheTabsThatAreAlwaysThere() {
        assertEquals(listOf("Movie", "More like this"), movieTabs(partOf = false))
        assertEquals(movieTabs(false), movieTabs(partOf = true).take(2))
        assertEquals("Part of", movieTabs(true).last())
    }
}
