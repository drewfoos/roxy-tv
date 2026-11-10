package tv.roxy.tv.player

import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * The next-episode card and its countdown. Once up, the countdown used to stop only on
 * Play now, Watch credits or leaving: seeking back into the episode, or switching autoplay
 * off, left it running and the next episode started anyway.
 */
class NextCardRuleTest {
    private val trigger = 1400.0

    @Test fun seekingBackBeforeTheCardTakesItDown() {
        assertEquals(NextCardStep.Hide, nextCardStep(cardUp = true, countingDown = true, autoplay = true, positionSec = 600.0, triggerAtSec = trigger))
        // Without a countdown too: the card belongs to the end of the episode.
        assertEquals(NextCardStep.Hide, nextCardStep(cardUp = true, countingDown = false, autoplay = false, positionSec = 600.0, triggerAtSec = trigger))
    }

    @Test fun theCardStaysWhilePlayingOnPastItsTrigger() {
        assertEquals(NextCardStep.Keep, nextCardStep(cardUp = true, countingDown = true, autoplay = true, positionSec = 1400.0, triggerAtSec = trigger))
        assertEquals(NextCardStep.Keep, nextCardStep(cardUp = true, countingDown = true, autoplay = true, positionSec = 1409.5, triggerAtSec = trigger))
    }

    @Test fun aPositionAHairBeforeTheTriggerIsNotASeekBack() {
        // A seek within the card's stretch can land a frame early: the card must not blink.
        assertEquals(NextCardStep.Keep, nextCardStep(cardUp = true, countingDown = true, autoplay = true, positionSec = trigger - 0.4, triggerAtSec = trigger))
        assertEquals(NextCardStep.Hide, nextCardStep(cardUp = true, countingDown = true, autoplay = true, positionSec = trigger - NEXT_CARD_BACK_SEC - 0.1, triggerAtSec = trigger))
    }

    @Test fun autoplayTurnedOffStopsTheCountdownAndKeepsTheCard() {
        assertEquals(NextCardStep.StopCountdown, nextCardStep(cardUp = true, countingDown = true, autoplay = false, positionSec = 1405.0, triggerAtSec = trigger))
        // Already waiting for a choice: nothing more to stop.
        assertEquals(NextCardStep.Keep, nextCardStep(cardUp = true, countingDown = false, autoplay = false, positionSec = 1405.0, triggerAtSec = trigger))
        // Autoplay turned on again does not start a countdown halfway: the end of the file moves on.
        assertEquals(NextCardStep.Keep, nextCardStep(cardUp = true, countingDown = false, autoplay = true, positionSec = 1405.0, triggerAtSec = trigger))
    }

    @Test fun noCardNothingToDo() {
        assertEquals(NextCardStep.Keep, nextCardStep(cardUp = false, countingDown = false, autoplay = false, positionSec = 0.0, triggerAtSec = trigger))
    }

    @Test fun theCardAppearsAtTheOutroThatEndsTheFile() {
        assertEquals(1330.0, nextCardTrigger(outroStartSec = 1330.0, outroEndSec = 1418.0, previewStartSec = null, durationSec = 1420.0, autoSkip = false), 0.0)
    }

    @Test fun anAutoSkippedClosingOutroShowsNoCard() {
        // The skip reaches the end of the file by itself.
        assertEquals(Double.MAX_VALUE, nextCardTrigger(1330.0, 1418.0, null, 1420.0, autoSkip = true), 0.0)
    }

    @Test fun withAScenesAfterTheCreditsTheCardWaitsForThePreviewOrTheLastSeconds() {
        assertEquals(1405.0, nextCardTrigger(1200.0, 1290.0, previewStartSec = 1405.0, durationSec = 1420.0, autoSkip = false), 0.0)
        assertEquals(1405.0, nextCardTrigger(1200.0, 1290.0, null, 1420.0, autoSkip = true), 0.0)
    }

    @Test fun withNoMarkersTheCardComesInTheLastFifteenSeconds() {
        assertEquals(1405.0, nextCardTrigger(null, null, null, 1420.0, autoSkip = false), 0.0)
    }
}
