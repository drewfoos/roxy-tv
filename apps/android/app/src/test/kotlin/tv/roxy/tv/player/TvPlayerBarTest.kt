package tv.roxy.tv.player

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.graphics.toPixelMap
import org.robolectric.annotation.GraphicsMode
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.test.getUnclippedBoundsInRoot
import androidx.compose.ui.unit.dp
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import org.junit.Assert.assertEquals
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

    // The bar sat 108 dp up (a fifth of the screen under it), so Skip had to sit a third of
    // the way up to clear it and dropped to the corner when the controls hid. Now the bar is
    // low, and Skip has one place: just above the bar's end, controls showing or not.
    @Test fun skipHasOnePlaceAboveALowBar() {
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
        val left = compose.onNodeWithText("22:06").getUnclippedBoundsInRoot()
        val shown = compose.onNodeWithTag("tv-skip").getUnclippedBoundsInRoot()
        assertTrue("the bar is low: ${screen - time.bottom} from the bottom", screen - time.bottom < 70.dp)
        assertTrue("above the bar: ${shown.bottom} vs ${time.top}", shown.bottom < time.top)
        assertTrue("right above it, not far up: ${time.top - shown.bottom}", time.top - shown.bottom < 30.dp)
        assertEquals("ends where the bar's time ends", left.right.value, shown.right.value, 0.5f)
        visible = false
        compose.waitForIdle()
        val hidden = compose.onNodeWithTag("tv-skip").getUnclippedBoundsInRoot()
        assertEquals("does not move when the controls hide", shown, hidden)
    }

    // Seen in a drawn shot: with the controls showing, the focused (white) Skip button came
    // out grey. The controls' dark layer was drawn over it, so it looked behind the bar.
    @Test @GraphicsMode(GraphicsMode.Mode.NATIVE)
    fun skipIsDrawnOverTheControlsShade() {
        val state = PlayerUiState(title = "T", subtitle = "S  S01E14", positionSec = 95.0, durationSec = 1421.0, fullscreen = null,
            segments = listOf(tv.roxy.tv.api.MediaSegment("intro", 90_000, 180_000)))
        compose.setContent {
            Box(Modifier.fillMaxSize()) {
                PlayerControls(state = state, visible = true, sheet = Sheet.None, onSheet = {}, onInteraction = {}, actions = actions)
            }
        }
        compose.waitForIdle()
        val b = compose.onNodeWithTag("tv-skip").getUnclippedBoundsInRoot()
        val px = compose.onRoot().captureToImage().toPixelMap()
        val scale = px.width / compose.onRoot().getUnclippedBoundsInRoot().right.value
        // Inside the button, clear of its border and its words.
        val c = px[((b.left.value + 8) * scale).toInt(), ((b.bottom.value - 6) * scale).toInt()]
        assertEquals("the focused button is white, not shaded: $c", androidx.compose.ui.graphics.Color.White, c)
    }

    // The next-episode card moved between two heights as the controls came and went, and
    // sat further right than Skip. It has Skip's place, whatever the controls do.
    @Test fun nextCardHasSkipsPlace() {
        var visible by mutableStateOf(true)
        val state = PlayerUiState(title = "T", subtitle = "S  S01E14", positionSec = 95.0, durationSec = 1421.0, fullscreen = null,
            segments = listOf(tv.roxy.tv.api.MediaSegment("intro", 90_000, 180_000)))
        var card by mutableStateOf(false)
        compose.setContent {
            Box(Modifier.fillMaxSize()) {
                PlayerControls(state = state.copy(nextCardShown = card), visible = visible, sheet = Sheet.None, onSheet = {}, onInteraction = {}, actions = actions)
                if (card) NextEpisodeCard(NextCard("S01E15  Next", null, 8), onPlayNow = {}, onDismiss = {})
            }
        }
        val skip = compose.onNodeWithTag("tv-skip").getUnclippedBoundsInRoot()
        card = true
        compose.waitForIdle()
        val shown = compose.onNodeWithTag("next-card").getUnclippedBoundsInRoot()
        assertEquals("ends where Skip ends", skip.right.value, shown.right.value, 0.5f)
        assertEquals("stands where Skip stands", skip.bottom.value, shown.bottom.value, 0.5f)
        visible = false
        compose.waitForIdle()
        assertEquals("does not move when the controls hide", shown, compose.onNodeWithTag("next-card").getUnclippedBoundsInRoot())
    }
}
