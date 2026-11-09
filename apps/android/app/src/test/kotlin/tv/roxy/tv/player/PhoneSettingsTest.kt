package tv.roxy.tv.player

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.getUnclippedBoundsInRoot
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.test.performClick
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * The phone's settings page (Crunchyroll's): Autoplay is a switch that really toggles,
 * it is the last row, the page leaves the episode title in view, and Back closes it.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], qualifiers = "w915dp-h412dp-land-xxhdpi")
class PhoneSettingsTest {
    @get:Rule val compose = createComposeRule()

    private var toggles = 0
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
        override fun toggleAutoplay() { toggles++ }
    }

    private fun show(): () -> Sheet {
        var sheet by mutableStateOf(Sheet.Settings)
        val state = PlayerUiState(title = "An Explosion for This Chunchunmaru!", subtitle = "KonoSuba  S01E02", positionSec = 300.0, durationSec = 1440.0, paused = true, fullscreen = true)
        compose.setContent {
            Box(Modifier.fillMaxSize()) {
                PlayerControls(state = state, visible = true, sheet = sheet, onSheet = { sheet = it }, onInteraction = {}, actions = actions)
            }
        }
        return { sheet }
    }

    // Found while building it: under the title bar, 56 dp rows pushed Autoplay below the
    // bottom of a sideways phone (a tap there closed the page instead).
    @Test fun autoplayRowToggles() {
        show()
        val root = compose.onRoot().getUnclippedBoundsInRoot()
        assertTrue("Autoplay wholly on screen", compose.onNodeWithText("Autoplay").getUnclippedBoundsInRoot().bottom <= root.bottom)
        compose.onNodeWithText("Autoplay").performClick()
        assertEquals(1, toggles)
    }

    @Test fun autoplayIsTheLastRow() {
        show()
        val autoplay = compose.onNodeWithText("Autoplay").getUnclippedBoundsInRoot().top
        for (other in listOf("Audio", "Subtitles", "Quality", "Playback speed", "Auto-skip intro & outro"))
            assertTrue("$other above Autoplay", compose.onNodeWithText(other).getUnclippedBoundsInRoot().top < autoplay)
    }

    @Test fun theEpisodeTitleStaysInView() {
        show()
        val title = compose.onNodeWithText("E2 — An Explosion for This Chunchunmaru!", substring = true)
        title.assertIsDisplayed()
        val page = compose.onNodeWithText("Playback Settings").getUnclippedBoundsInRoot().top
        assertTrue("the page starts below the title", title.getUnclippedBoundsInRoot().bottom <= page)
    }

    @Test fun backClosesThePage() {
        val sheet = show()
        compose.onNodeWithContentDescription("Back").performClick()
        compose.waitForIdle()
        assertEquals(Sheet.None, sheet())
    }
}
