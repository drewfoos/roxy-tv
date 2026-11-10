package tv.roxy.tv.player

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.test.getUnclippedBoundsInRoot
import androidx.compose.ui.unit.dp
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/** The TV player's top bar. */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], qualifiers = "w960dp-h540dp-land-television-xhdpi")
class TvPlayerBarTest {
    @get:Rule val compose = createComposeRule()

    private val actions = object : PlayerActions {
        override fun togglePause() {}
        override fun seekTo(seconds: Double) {}
        override fun seekBy(seconds: Int) {}
        override fun selectSubtitle(id: Int) {}
        override fun selectAudio(id: Int) {}
        override fun setSpeed(speed: Double) {}
        override fun toggleMute() {}
        override fun toggleCaptions() {}
        override fun toggleAutoSkip() {}
        override fun toggleFill() {}
        override fun enterPip() {}
        override fun toggleFullscreen() {}
        override fun next() {}
        override fun back() {}
    }

    // Found on the TV: "Go to Details" showed only once the title box had focus, so nothing
    // said the title could be selected. It is there with the controls, focused or not.
    @Test fun goToDetailsShowsWithoutFocus() {
        val state = PlayerUiState(title = "The End of the World", subtitle = "Sword Art Online  S01E14", positionSec = 60.0, durationSec = 1421.0, paused = true, fullscreen = null)
        compose.setContent {
            Box(Modifier.fillMaxSize()) {
                PlayerControls(state = state, visible = true, sheet = Sheet.None, onSheet = {}, onInteraction = {}, actions = actions)
            }
        }
        compose.onNodeWithText("Go to Details").assertIsDisplayed()
    }

    // Found on the TV: the Skip button sat 180 dp up whether or not the controls showed. Now
    // just above the bar while they show, low in the corner while they are hidden.
    @Test fun skipSitsLow() {
        var visible by mutableStateOf(true)
        val state = PlayerUiState(title = "T", subtitle = "S  S01E14", positionSec = 95.0, durationSec = 1421.0, fullscreen = null,
            segments = listOf(tv.roxy.tv.api.MediaSegment("intro", 90_000, 180_000)))
        compose.setContent {
            Box(Modifier.fillMaxSize()) {
                PlayerControls(state = state, visible = visible, sheet = Sheet.None, onSheet = {}, onInteraction = {}, actions = actions)
            }
        }
        val screen = compose.onRoot().getUnclippedBoundsInRoot().bottom
        val time = compose.onNodeWithText("1:35").getUnclippedBoundsInRoot()
        val shown = compose.onNodeWithText("Skip intro").getUnclippedBoundsInRoot()
        assertTrue("above the bar: ${shown.bottom} vs ${time.top}", shown.bottom < time.top)
        assertTrue("right above it, not far up: ${time.top - shown.bottom}", time.top - shown.bottom < 40.dp)
        visible = false
        compose.waitForIdle()
        val hidden = compose.onNodeWithText("Skip intro").getUnclippedBoundsInRoot()
        assertTrue("low in the corner: ${screen - hidden.bottom} from the bottom", screen - hidden.bottom < 70.dp)
    }
}
