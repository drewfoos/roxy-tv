package tv.roxy.tv.player

import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * Back goes up one level at a time. The Back key (onKeyDown) and the system's back callback
 * (Android 16 sends no key) both act on this one answer.
 */
class BackStepTest {
    @Test fun aListGoesBackToSettings() {
        for (s in listOf(Sheet.Subtitles, Sheet.Audio, Sheet.Quality, Sheet.Speed)) {
            assertEquals(s.name, BackStep.SheetUp, backStep(s, controlsVisible = true))
            // A sheet counts even if the controls behind it have gone.
            assertEquals(s.name, BackStep.SheetUp, backStep(s, controlsVisible = false))
        }
    }

    @Test fun settingsCloses() {
        assertEquals(BackStep.CloseSheet, backStep(Sheet.Settings, controlsVisible = true))
        assertEquals(BackStep.CloseSheet, backStep(Sheet.Settings, controlsVisible = false))
    }

    @Test fun theControlsGoAwayBeforeThePlayerIsLeft() {
        assertEquals(BackStep.HideControls, backStep(Sheet.None, controlsVisible = true))
    }

    @Test fun withNothingOpenBackLeavesThePlayer() {
        // The callback is disabled in this state, so the system's own Back runs.
        assertEquals(BackStep.Leave, backStep(Sheet.None, controlsVisible = false))
    }
}
