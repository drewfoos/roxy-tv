package tv.roxy.tv.player

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.test.ExperimentalTestApi
import androidx.compose.ui.Modifier
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.test.performKeyInput
import androidx.compose.ui.test.pressKey
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/** Where the D-pad takes the focus around the TV player's Skip button and next-episode card. */
@OptIn(ExperimentalTestApi::class)
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], qualifiers = "w960dp-h540dp-land-television-xhdpi")
class TvPlayerFocusTest {
    @get:Rule val compose = createComposeRule()

    private val did = mutableListOf<String>()
    private val actions = object : PlayerActions {
        override fun togglePause() { did += "pause" }
        override fun seekTo(seconds: Double) { did += "seek" }
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
        override fun next() { did += "next" }
        override fun back() {}
    }
    private val intro = PlayerUiState(title = "T", subtitle = "S  S01E14", positionSec = 95.0, durationSec = 1421.0, fullscreen = null, hasNext = true,
        segments = listOf(tv.roxy.tv.api.MediaSegment("intro", 90_000, 180_000)))

    private fun press(keys: List<Key>) { for (k in keys) { compose.onRoot().performKeyInput { pressKey(k) }; compose.waitForIdle() } }

    // Found in review of 0.6.44: Skip moved in from the screen's edge, and from then a second
    // Right press went to the top bar's Next episode icon. OK there ended the episode.
    @Test fun rightFromSkipStaysOnSkip() {
        compose.setContent {
            Box(Modifier.fillMaxSize()) { PlayerControls(state = intro, visible = true, sheet = Sheet.None, onSheet = {}, onInteraction = {}, actions = actions) }
        }
        compose.waitForIdle()
        press(listOf(Key.DirectionRight, Key.DirectionRight, Key.DirectionCenter))
        assertEquals("OK after Right, Right is still Skip", listOf("seek"), did)
    }

    // The same from the card's last button.
    @Test fun rightFromWatchCreditsStaysOnTheCard() {
        compose.setContent {
            Box(Modifier.fillMaxSize()) {
                PlayerControls(state = intro.copy(nextCardShown = true), visible = true, sheet = Sheet.None, onSheet = {}, onInteraction = {}, actions = actions)
                NextEpisodeCard(NextCard("S01E15  Next", null, 8), onPlayNow = { did += "play now" }, onDismiss = { did += "credits" })
            }
        }
        compose.waitForIdle()
        press(listOf(Key.DirectionRight, Key.DirectionRight, Key.DirectionRight, Key.DirectionCenter))
        assertEquals("OK after going right is Watch credits", listOf("credits"), did)
    }

    // Skip took the focus away with it (pressed, or the intro ended): OK did nothing until
    // the controls hid and came back. The seek bar has it now: OK there is play/pause.
    @Test fun theSeekBarHasTheFocusWhenSkipLeaves() {
        var state by mutableStateOf(intro)
        compose.setContent {
            Box(Modifier.fillMaxSize()) { PlayerControls(state = state, visible = true, sheet = Sheet.None, onSheet = {}, onInteraction = {}, actions = actions) }
        }
        compose.waitForIdle()
        state = intro.copy(positionSec = 181.0)
        compose.waitForIdle()
        press(listOf(Key.DirectionCenter))
        assertEquals(listOf("pause"), did)
    }
}
