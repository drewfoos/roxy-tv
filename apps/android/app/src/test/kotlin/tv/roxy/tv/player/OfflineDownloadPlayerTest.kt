package tv.roxy.tv.player

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithContentDescription
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import tv.roxy.tv.api.ApiException
import tv.roxy.tv.api.Item
import tv.roxy.tv.ui.inPlayOrder
import tv.roxy.tv.ui.phone.markWatchedOffline
import java.io.IOException

/**
 * A download played away from the server (a plane, mobile data without the home network).
 * The player offered 720p / 480p there: "signed in" was read as "connected", and picking
 * one left the player waiting on a server that was not coming.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], qualifiers = "w915dp-h412dp-land-xxhdpi")
class OfflineDownloadPlayerTest {
    @get:Rule val compose = createComposeRule()

    private fun keys(m: QualityMenu) = m.options.map { it.key }
    private val everything = QualityOption.forSource(1080).map { it.key }

    @Test fun downloadAwayFromTheServerOffersItsOwnFileOnly() {
        assertTrue("a 1080p source has lower qualities to hide", everything.size > 1)
        val m = qualityMenu(1080, hasSource = true, signedIn = true, local = true, serverOnline = false)
        assertEquals(listOf(QualityOption.Original.key), keys(m))
        assertTrue("says why", m.note.orEmpty().contains("Not connected"))
    }

    @Test fun downloadWithTheServerThereKeepsTheLowerQualities() {
        val m = qualityMenu(1080, hasSource = true, signedIn = true, local = true, serverOnline = true)
        assertEquals(everything, keys(m))
        assertTrue(m.note.orEmpty().startsWith("Downloaded"))
    }

    // The stream is reconnecting: the choice is for when the server is back.
    @Test fun streamKeepsItsListWhileTheServerIsAway() {
        val m = qualityMenu(1080, hasSource = true, signedIn = true, local = false, serverOnline = false)
        assertEquals(everything, keys(m))
        assertNull(m.note)
    }

    @Test fun signedOutOrUnknownSourceOffersTheFileOnly() {
        assertEquals(1, qualityMenu(1080, hasSource = true, signedIn = false, local = true, serverOnline = true).options.size)
        assertEquals(1, qualityMenu(1080, hasSource = false, signedIn = true, local = true, serverOnline = true).options.size)
    }

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

    private fun castButtons(available: Boolean): Int {
        val state = PlayerUiState(title = "Episode", positionSec = 300.0, durationSec = 1440.0, paused = true, fullscreen = true, castAvailable = available)
        compose.setContent {
            Box(Modifier.fillMaxSize()) { PlayerControls(state = state, visible = true, sheet = Sheet.None, onSheet = {}, onInteraction = {}, actions = actions) }
        }
        return compose.onAllNodesWithContentDescription("Play on another device").fetchSemanticsNodes().size
    }

    @Test fun playOnAnotherDeviceIsOfferedNormally() = assertEquals(1, castButtons(true))

    // It lists the server's devices: with no server it opened an empty sheet.
    @Test fun playOnAnotherDeviceIsHiddenForADownloadAwayFromTheServer() = assertEquals(0, castButtons(false))

    // The downloaded episodes are the "next episode" list offline; the index holds them in no order.
    @Test fun downloadedEpisodesPlayInOrderSpecialsLast() {
        fun ep(id: String, s: Int, e: Int) = Item(id = id, type = "episode", seasonAired = s, indexAired = e)
        val got = inPlayOrder(listOf(ep("sp", 0, 1), ep("s2e1", 2, 1), ep("s1e3", 1, 3), ep("s1e2", 1, 2))).map { it.id }
        assertEquals(listOf("s1e2", "s1e3", "s2e1", "sp"), got)
    }

    @Test fun watchedOnADownloadWaitsForTheServerOnlyWhenItDidNotAnswer() {
        val away = IOException("failed to connect")
        assertTrue(markWatchedOffline(away, on = true, downloaded = true))
        assertEquals("not a download: nothing here to mark", false, markWatchedOffline(away, on = true, downloaded = false))
        assertEquals("unwatched cannot be queued", false, markWatchedOffline(away, on = false, downloaded = true))
        assertEquals("the server refused: not offline", false, markWatchedOffline(ApiException(403, "no"), on = true, downloaded = true))
    }
}
