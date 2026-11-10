package tv.roxy.tv.player

import android.view.KeyEvent
import androidx.compose.ui.Modifier
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.onKeyEvent
import androidx.compose.ui.input.key.type

/** What a key event means to a focused button (see [okPress]). */
internal enum class OkPress { Act, Swallow, Pass }

/**
 * OK / Enter on a focused button. A held key repeats its KeyDown many times a second, and
 * acting on each one flipped a switch back and forth, opened a list and picked its first
 * row, or skipped two episodes. Only the first press acts ([repeatCount] 0); the repeats
 * are still taken, so nothing behind the button gets them. Anything else passes.
 */
internal fun okPress(down: Boolean, okKey: Boolean, repeatCount: Int): OkPress = when {
    !down || !okKey -> OkPress.Pass
    repeatCount == 0 -> OkPress.Act
    else -> OkPress.Swallow
}

/** [onClick] on OK / Enter, once per press however long the key is held (see [okPress]). */
internal fun Modifier.onOkPress(onClick: () -> Unit): Modifier = onKeyEvent { ev ->
    when (okPress(ev.type == KeyEventType.KeyDown, ev.key == Key.DirectionCenter || ev.key == Key.Enter, ev.nativeKeyEvent.repeatCount)) {
        OkPress.Act -> { onClick(); true }
        OkPress.Swallow -> true
        OkPress.Pass -> false
    }
}

/**
 * The remote's keys the player acts on once per press: held, their repeats are dropped
 * (Next skipped an episode per repeat, Play/Pause flickered, Back left the player after
 * closing the controls). Rewind, fast-forward and the D-pad are not here: holding them
 * scrubs faster, and the D-pad walks the buttons.
 */
internal fun actsOncePerPress(keyCode: Int): Boolean = when (keyCode) {
    KeyEvent.KEYCODE_MEDIA_PLAY_PAUSE, KeyEvent.KEYCODE_SPACE, KeyEvent.KEYCODE_MEDIA_PLAY, KeyEvent.KEYCODE_MEDIA_PAUSE,
    KeyEvent.KEYCODE_MEDIA_STOP, KeyEvent.KEYCODE_MEDIA_NEXT, KeyEvent.KEYCODE_MEDIA_PREVIOUS,
    KeyEvent.KEYCODE_MENU, KeyEvent.KEYCODE_I, KeyEvent.KEYCODE_CAPTIONS,
    KeyEvent.KEYCODE_MEDIA_AUDIO_TRACK, KeyEvent.KEYCODE_A, KeyEvent.KEYCODE_BACK -> true
    else -> false
}

/** True when the activity should drop this key-down: a repeat of a key that acts once per press. */
internal fun dropsRepeat(keyCode: Int, repeatCount: Int): Boolean = repeatCount > 0 && actsOncePerPress(keyCode)
