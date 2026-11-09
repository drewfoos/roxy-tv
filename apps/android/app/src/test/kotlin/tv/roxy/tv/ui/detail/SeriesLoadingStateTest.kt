package tv.roxy.tv.ui.detail

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class SeriesLoadingStateTest {
    // Try again: the error gives way to the loading rows at once (it used to stay up,
    // looking ignored, until the page had been read again).
    @Test fun retryShowsLoadingNotTheOldError() {
        val failed = SeriesUiState(loading = false, episodesError = "No connection")
        val r = failed.retrying()
        assertTrue(r.episodesLoading)
        assertNull(r.episodesError)
        val partial = SeriesUiState(loading = false, episodesIncomplete = true, episodesExpected = 30)
        assertFalse(partial.retrying().episodesIncomplete)
    }

    // The retry failed before reaching the episodes: the problem is shown again, not a
    // spinner for ever or "No episodes in this season".
    @Test fun failedRetryShowsTheProblemAgain() {
        val failed = SeriesUiState(loading = false, episodesError = "No connection")
        val after = failed.retrying().retryFailed(failed)
        assertFalse(after.episodesLoading)
        assertEquals("No connection", after.episodesError)
        val partial = SeriesUiState(loading = false, episodesIncomplete = true, episodesExpected = 30)
        val p = partial.retrying().retryFailed(partial)
        assertTrue(p.episodesIncomplete)
        assertEquals(30, p.episodesExpected)
    }

    // First read of a show with seasons: its episodes wait for the next-episode answer, so
    // they count as loading meanwhile ("No episodes in this season" flashed first).
    @Test fun firstLoadWithSeasonsIsLoading() {
        val s = SeriesUiState(loading = false)
        assertTrue(s.episodesPending(firstLoad = true, hasSeasons = true).episodesLoading)
        assertFalse(s.episodesPending(firstLoad = false, hasSeasons = true).episodesLoading)
        assertFalse(s.episodesPending(firstLoad = true, hasSeasons = false).episodesLoading)
    }
}
