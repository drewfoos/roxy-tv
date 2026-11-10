package tv.roxy.tv.player

import android.view.KeyEvent
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * A held key repeats its key-down. Buttons and the remote's one-shot keys act on the first
 * press only; the keys that are meant to be held (scrubbing, moving focus) keep repeating.
 */
class KeyPressTest {
    @Test fun theFirstPressOfOkActs() {
        assertEquals(OkPress.Act, okPress(down = true, okKey = true, repeatCount = 0))
    }

    @Test fun aHeldOkDoesNotActAgainAndIsNotPassedOn() {
        // Passing it on would let the activity show the controls again or a button behind act.
        assertEquals(OkPress.Swallow, okPress(down = true, okKey = true, repeatCount = 1))
        assertEquals(OkPress.Swallow, okPress(down = true, okKey = true, repeatCount = 40))
    }

    @Test fun otherKeysAndKeyUpPass() {
        assertEquals(OkPress.Pass, okPress(down = true, okKey = false, repeatCount = 0))
        assertEquals(OkPress.Pass, okPress(down = true, okKey = false, repeatCount = 3))
        assertEquals(OkPress.Pass, okPress(down = false, okKey = true, repeatCount = 0))
    }

    @Test fun oneShotRemoteKeysDropTheirRepeats() {
        for (k in listOf(
            KeyEvent.KEYCODE_MEDIA_PLAY_PAUSE, KeyEvent.KEYCODE_SPACE, KeyEvent.KEYCODE_MEDIA_PLAY, KeyEvent.KEYCODE_MEDIA_PAUSE,
            KeyEvent.KEYCODE_MEDIA_STOP, KeyEvent.KEYCODE_MEDIA_NEXT, KeyEvent.KEYCODE_MEDIA_PREVIOUS, KeyEvent.KEYCODE_MENU,
            KeyEvent.KEYCODE_I, KeyEvent.KEYCODE_CAPTIONS, KeyEvent.KEYCODE_MEDIA_AUDIO_TRACK, KeyEvent.KEYCODE_A, KeyEvent.KEYCODE_BACK,
        )) {
            assertFalse("first press of $k", dropsRepeat(k, 0))
            assertTrue("repeat of $k", dropsRepeat(k, 1))
        }
    }

    @Test fun keysMeantToBeHeldKeepRepeating() {
        for (k in listOf(
            KeyEvent.KEYCODE_MEDIA_REWIND, KeyEvent.KEYCODE_MEDIA_FAST_FORWARD, KeyEvent.KEYCODE_DPAD_LEFT, KeyEvent.KEYCODE_DPAD_RIGHT,
            KeyEvent.KEYCODE_DPAD_UP, KeyEvent.KEYCODE_DPAD_DOWN, KeyEvent.KEYCODE_DPAD_CENTER, KeyEvent.KEYCODE_ENTER,
        )) assertFalse("repeat of $k", dropsRepeat(k, 5))
    }
}
