package tv.roxy.tv.player

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.isRoot
import androidx.compose.ui.unit.dp
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.getUnclippedBoundsInRoot
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.onAllNodesWithContentDescription
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

    // Upright phone, small player (a 16:9 box at the top of the episode page): the page is a
    // window of its own at full size right under the back arrow, not squeezed into the video. System Back
    // closes them.
    @Test @Config(qualifiers = "w412dp-h915dp-port-xxhdpi")
    fun uprightSmallPlayerPageStartsUnderTheBackArrow() {
        var sheet by mutableStateOf(Sheet.Settings)
        val state = PlayerUiState(title = "An Explosion for This Chunchunmaru!", subtitle = "KonoSuba  S01E02", positionSec = 300.0, durationSec = 1440.0, paused = true, fullscreen = false)
        compose.setContent {
            Box(Modifier.fillMaxWidth().aspectRatio(16f / 9f)) {
                PlayerControls(state = state, visible = true, sheet = sheet, onSheet = { sheet = it }, onInteraction = {}, actions = actions)
            }
        }
        compose.onAllNodes(isRoot()).assertCountEquals(2) // the player and the page's own window
        // Full-size rows, not squeezed into the 232 dp video; and the page under the title.
        val header = compose.onNodeWithText("Playback Settings")
        header.assertIsDisplayed()
        // Right under the player's back arrow (the sideways page's gap): the arrow in view,
        // the page not pushed further down. The bar's arrow is the first "Back" (the page's own is in its window).
        val bar = compose.onAllNodesWithContentDescription("Back")[0].getUnclippedBoundsInRoot()
        val top = header.getUnclippedBoundsInRoot().top
        assertTrue("under the back arrow: $top vs ${bar.bottom}", top >= bar.bottom && top < bar.bottom + 40.dp)
        compose.onNodeWithText("Autoplay").assertIsDisplayed()

        // The phone's Back closes the settings outright, from a list too (Drew's call).
        compose.onNodeWithText("Quality").performClick()
        assertEquals(Sheet.Quality, sheet)
        compose.waitForIdle()
        systemBack()
        compose.waitForIdle()
        assertEquals("Back from a list: closed", Sheet.None, sheet)
    }

    /** System Back, as the page's window gets it. */
    @Suppress("DEPRECATION")
    private fun systemBack() = compose.runOnUiThread { org.robolectric.shadows.ShadowDialog.getLatestDialog().onBackPressed() }

    // Sideways: the phone's Back closes the settings (from a list too), and only once they
    // are closed does it reach the episode page (which leaves full screen).
    @Test fun sidewaysPhoneBackClosesTheSettingsFirst() {
        var sheet by mutableStateOf(Sheet.Settings)
        var leftFullscreen = 0
        var dispatcher: androidx.activity.OnBackPressedDispatcher? = null
        val state = PlayerUiState(title = "T", subtitle = "S  S01E02", positionSec = 300.0, durationSec = 1440.0, paused = true, fullscreen = true)
        compose.setContent {
            dispatcher = androidx.activity.compose.LocalOnBackPressedDispatcherOwner.current!!.onBackPressedDispatcher
            androidx.activity.compose.BackHandler { leftFullscreen++ } // the episode page's
            Box(Modifier.fillMaxSize()) {
                PlayerControls(state = state, visible = true, sheet = sheet, onSheet = { sheet = it }, onInteraction = {}, actions = actions)
            }
        }
        compose.onNodeWithText("Subtitles").performClick()
        assertEquals(Sheet.Subtitles, sheet)
        compose.runOnUiThread { dispatcher!!.onBackPressed() }
        compose.waitForIdle()
        assertEquals("closed from a list", Sheet.None, sheet)
        assertEquals("full screen kept", 0, leftFullscreen)
        compose.runOnUiThread { dispatcher!!.onBackPressed() }
        compose.waitForIdle()
        assertEquals("then Back leaves full screen", 1, leftFullscreen)
    }

    @Test fun backClosesThePage() {
        val sheet = show()
        compose.onNodeWithContentDescription("Back").performClick()
        compose.waitForIdle()
        assertEquals(Sheet.None, sheet())
    }
}
