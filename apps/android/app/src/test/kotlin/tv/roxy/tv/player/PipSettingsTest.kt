package tv.roxy.tv.player

import android.graphics.Rect
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/** What the system is told about the phone player's PiP window, and by which page. */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class PipSettingsTest {
    private val box = Rect(0, 128, 1440, 938)      // the 16:9 box under the status bar
    private val box43 = Rect(0, 128, 1440, 1208)   // a 4:3 picture's
    private val w = 1440
    private val h = 3120

    @Test fun nothingIsSentUntilThePlayerSaysWhetherItPlays() {
        val pip = PipSettings()
        val page = Any()
        pip.claim(page) {}
        assertNull(pip.shape(page, 1440 to 1080))
        assertEquals(PipSettings.Params(1440 to 1080, box43, autoEnter = true), pip.playing(page, true, box43, w, h))
    }

    @Test fun onlyChangesAreSent() {
        val pip = PipSettings()
        val page = Any()
        pip.claim(page) {}
        assertEquals(PipSettings.Params(16 to 9, box, autoEnter = true), pip.playing(page, true, box, w, h))
        assertNull("the same again", pip.playing(page, true, Rect(box), w, h))
        assertNull("the same shape", pip.shape(page, 16 to 9))
        assertEquals("paused: leaving the app no longer shrinks it", PipSettings.Params(16 to 9, box, autoEnter = false), pip.playing(page, false, box, w, h))
        assertEquals("the next episode is 4:3", PipSettings.Params(4 to 3, null, autoEnter = false), pip.shape(page, 4 to 3))
    }

    /**
     * The Next Episode row opens a second episode page over the first, which lives on until
     * the new one has slid in. Its goodbye wiped the new page's settings: a 4:3 episode
     * opened that way had a 16:9 PiP window.
     */
    @Test fun aPageLeftBehindCannotTouchTheNextPagesSettings() {
        val pip = PipSettings()
        val first = Any()
        val second = Any()
        pip.claim(first) {}
        pip.shape(first, 1920 to 1080)
        pip.playing(first, true, box, w, h)

        pip.claim(second) {}
        pip.shape(second, 1440 to 1080)
        assertEquals(PipSettings.Params(1440 to 1080, box43, autoEnter = true), pip.playing(second, true, box43, w, h))

        // The first page's player stops, its page is taken down.
        assertNull(pip.playing(first, false, box, w, h))
        assertNull(pip.shape(first, 16 to 9))
        assertFalse("not its settings to clear", pip.clear(first))
        assertEquals(PipSettings.Params(1440 to 1080, box43, autoEnter = true), pip.params())
        assertTrue(pip.armed)

        assertTrue(pip.clear(second))
        assertFalse(pip.armed)
    }

    @Test fun aNewOwnerStartsFromNothing() {
        val pip = PipSettings()
        val first = Any()
        val second = Any()
        pip.claim(first) {}
        pip.shape(first, 239 to 100)
        pip.playing(first, true, Rect(0, 128, 1440, 731), w, h)
        pip.claim(second) {}
        assertFalse("nothing plays on the new page yet", pip.armed)
        assertEquals(PipSettings.Params(16 to 9, null, autoEnter = false), pip.params())
    }

    /** A place that is not the PiP window's shape gets the system's icon cross-fade: not sent. */
    @Test fun aSourceOfTheWrongShapeIsLeftOut() {
        val pip = PipSettings()
        val page = Any()
        pip.claim(page) {}
        // The whole sideways screen, for a 16:9 picture.
        assertEquals(PipSettings.Params(16 to 9, null, autoEnter = true), pip.playing(page, true, Rect(0, 0, 3120, 1440), 3120, 1440))
        // The picture itself, centred in it.
        val picture = Rect(280, 0, 2840, 1440)
        assertEquals(PipSettings.Params(16 to 9, picture, autoEnter = true), pip.playing(page, true, picture, 3120, 1440))
    }

    /**
     * In the PiP window the page keeps the place it had in the whole window (where the
     * picture grows back to). It stays good there: it is judged against the window it was
     * measured in, not the small one the settings are sent from.
     */
    @Test fun theSourceOutlivesThePipWindow() {
        val pip = PipSettings()
        val page = Any()
        pip.claim(page) {}
        pip.playing(page, true, box, w, h)
        // Paused from the PiP window's own button: the window is 788 x 443 now.
        assertEquals(PipSettings.Params(16 to 9, box, autoEnter = false), pip.playing(page, false, box, 788, 443))
        assertEquals(PipSettings.Params(16 to 9, box, autoEnter = true), pip.playing(page, true, box, 788, 443))
    }

    @Test fun theStoredSourceIsACopy() {
        val pip = PipSettings()
        val page = Any()
        pip.claim(page) {}
        val r = Rect(box)
        pip.playing(page, true, r, w, h)
        r.offset(300, 0) // the caller's rect moves on
        assertEquals(box, pip.params().source)
    }

    /** The window lost focus: the player's controls go, but only when leaving would make a PiP window. */
    @Test fun leavingIsHeardOnlyWhileAVideoPlays() {
        val pip = PipSettings()
        val page = Any()
        var heard = 0
        pip.claim(page) { heard++ }
        pip.leaving()
        assertEquals("nothing playing yet", 0, heard)
        pip.playing(page, true, box, w, h)
        pip.leaving()
        assertEquals(1, heard)
        pip.playing(page, false, box, w, h)
        pip.leaving()
        assertEquals("paused: the app is just left", 1, heard)
        pip.playing(page, true, box, w, h)
        pip.clear(page)
        pip.leaving()
        assertEquals("the page has gone", 1, heard)
    }

    /**
     * The window lost the focus: is the app being left for the PiP window? The page then
     * draws nothing but the picture, so "yes" must not be said of a menu or sheet of the
     * page's own taking the focus, nor of split screen.
     */
    @Test fun aLostFocusMeansLeavingOnlyWhenNothingOfTheAppsOwnHasIt() {
        val pip = PipSettings()
        val page = Any()
        pip.claim(page) {}
        assertFalse("nothing playing", pip.leftByFocus(hasFocus = false, multiWindow = false, ownWindowFocused = false))
        pip.playing(page, true, box, w, h)
        assertTrue("the Home gesture: the launcher has the focus", pip.leftByFocus(hasFocus = false, multiWindow = false, ownWindowFocused = false))
        assertFalse("the page's own menu or settings page has it", pip.leftByFocus(hasFocus = false, multiWindow = false, ownWindowFocused = true))
        assertFalse("who has it cannot be told (Android 9)", pip.leftByFocus(hasFocus = false, multiWindow = false, ownWindowFocused = null))
        assertFalse("split screen: the other app is in use", pip.leftByFocus(hasFocus = false, multiWindow = true, ownWindowFocused = false))
        assertFalse("the focus is back", pip.leftByFocus(hasFocus = true, multiWindow = false, ownWindowFocused = false))
        pip.playing(page, false, box, w, h)
        assertFalse("paused: leaving makes no PiP window", pip.leftByFocus(hasFocus = false, multiWindow = false, ownWindowFocused = false))
    }

    @Test fun thePageThatTookOverIsTheOneThatHearsLeaving() {
        val pip = PipSettings()
        val first = Any()
        val second = Any()
        var a = 0
        var b = 0
        pip.claim(first) { a++ }
        pip.playing(first, true, box, w, h)
        pip.claim(second) { b++ }
        pip.playing(second, true, box, w, h)
        pip.leaving()
        assertEquals(0, a)
        assertEquals(1, b)
    }
}
