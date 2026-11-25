package tv.roxy.tv.ui.phone

import android.content.res.Configuration
import android.graphics.Rect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import tv.roxy.tv.MainActivity

/**
 * What a page has open over itself (a menu, File info, a sheet) closes when the app is
 * left for the PiP window: the system makes that window by shrinking the app's as it is,
 * and whatever lies over the picture shrinks with it and stays over it.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class ClosedForPipTest {
    @get:Rule val compose = createAndroidComposeRule<MainActivity>()
    private val page = Any()
    private var open by mutableStateOf(true)
    private var closed = 0

    /** A page's "this is open, and this closes it", put beside the app's own content (which the activity has set already). */
    private fun show(playing: Boolean = true) {
        compose.runOnUiThread {
            val activity = compose.activity
            activity.addContentView(
                androidx.compose.ui.platform.ComposeView(activity).apply { setContent { if (open) ClosedForPip { closed++; open = false } } },
                android.view.ViewGroup.LayoutParams(1, 1),
            )
            activity.claimPip(page) { }
            activity.updatePip(page, playing, Rect(0, 128, 1440, 938))
        }
        compose.waitForIdle()
    }

    @Test fun closedWhenTheAppIsBeingLeft() {
        show()
        assertTrue("open while the page is in use", open && closed == 0)
        // The app is left (here by the page's own window losing the focus to the launcher).
        compose.runOnUiThread { compose.activity.onWindowFocusChanged(false) }
        compose.waitForIdle()
        assertTrue(compose.activity.pipLeaving)
        assertFalse("it went with the page", open)
        assertEquals("and was closed once", 1, closed)
    }

    /**
     * The app was not told early (a window with no teller in it, a phone on which the
     * word does not come): what was open is closed once the app is the PiP window.
     */
    @Test fun closedInThePipWindowWhenNothingWasToldBefore() {
        show()
        assertFalse(compose.activity.pipLeaving)
        compose.runOnUiThread { compose.activity.onPictureInPictureModeChanged(true, Configuration(compose.activity.resources.configuration)) }
        compose.waitForIdle()
        assertFalse("in the PiP window: closed", open)
        assertEquals(1, closed)
    }

    /** With no video playing the app is not on its way into a PiP window, and nothing is closed. */
    @Test fun leftWithNothingPlayingNothingIsClosed() {
        show(playing = false)
        compose.runOnUiThread { compose.activity.onWindowFocusChanged(false) }
        compose.waitForIdle()
        assertTrue(open && closed == 0)
    }
}
