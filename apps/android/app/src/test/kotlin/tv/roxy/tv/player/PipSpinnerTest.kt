package tv.roxy.tv.player

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.assertHeightIsEqualTo
import androidx.compose.ui.test.assertWidthIsEqualTo
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.unit.dp
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import java.io.File

/**
 * In the PiP window nothing of the player's own is drawn over the picture, except the
 * spinner. A video still loading there (the next episode on a slow link, a stream that
 * ran dry) was a plain black window with nothing to say it was coming.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], qualifiers = "w400dp-h800dp-mdpi")
class PipSpinnerTest {
    @get:Rule val compose = createComposeRule()

    private var buffering by mutableStateOf(false)
    private var compact by mutableStateOf(true)

    /** The PiP window on a phone: about 225 x 127 dp. */
    private fun show() {
        compose.mainClock.autoAdvance = false
        compose.setContent { Box(Modifier.size(225.dp, 127.dp)) { BufferingSpinner(buffering, compact) } }
    }
    private fun spinner() = compose.onNodeWithTag("buffering")
    /** A change made from the test, told to the composition, and [ms] of the test's clock. */
    private fun pass(ms: Long) {
        androidx.compose.runtime.snapshots.Snapshot.sendApplyNotifications()
        compose.mainClock.advanceTimeByFrame()
        compose.mainClock.advanceTimeBy(ms)
    }

    @Test fun nothingWhileTheVideoPlays() {
        show()
        pass(2000)
        spinner().assertDoesNotExist()
    }

    /** A seek inside what is already loaded, a short hiccup: no spinner flashes up. */
    @Test fun itWaitsAMomentBeforeShowing() {
        show()
        buffering = true
        pass(BUFFERING_GRACE_MS - 100)
        spinner().assertDoesNotExist()
        pass(300)
        spinner().assertExists()
    }

    @Test fun itGoesWhenTheVideoIsBack() {
        show()
        buffering = true
        pass(BUFFERING_GRACE_MS + 200)
        spinner().assertExists()
        buffering = false
        pass(100)
        spinner().assertDoesNotExist()
    }

    /** The small one fits the PiP window (and the box on the upright page); fullscreen has the large one. */
    @Test fun theSmallOneForTheSmallBoxes() {
        show()
        buffering = true
        pass(BUFFERING_GRACE_MS + 200)
        spinner().assertWidthIsEqualTo(36.dp).assertHeightIsEqualTo(36.dp)
        compact = false
        pass(100)
        spinner().assertWidthIsEqualTo(56.dp).assertHeightIsEqualTo(56.dp)
    }

    // ---- the wiring (a PlayerSession needs mpv, which cannot run here: see MpvInitOrderTest) ----

    @Test fun thePipWindowKeepsTheSpinnerAndNothingElse() {
        val src = File("src/main/kotlin/tv/roxy/tv/player/PlayerSurface.kt").readText()
        val spinner = src.indexOf("BufferingSpinner(session.ui.buffering, compact = session.host.inPip || session.ui.fullscreen == false)")
        val pipStop = src.indexOf("if (session.host.inPip) return@Box")
        val controls = src.indexOf("PlayerControls(")
        assertTrue("the spinner is drawn, small in PiP", spinner > 0)
        assertTrue("before the point where the PiP window stops drawing the player's layers", spinner < pipStop)
        assertTrue("and the controls come after that point", pipStop in 1 until controls)
    }
}
