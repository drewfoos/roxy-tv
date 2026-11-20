package tv.roxy.tv.player

import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleOwner
import androidx.lifecycle.compose.LifecycleEventEffect
import timber.log.Timber

/**
 * What leaving the app does to an open player, and what coming back to it finds.
 *
 * The TV's player closes when it is no longer on screen (the TV put to sleep, Home, another
 * input, the screensaver over a paused film): its place is saved, and the TV wakes on the
 * title's page or Home instead of a film paused since yesterday. Jellyfin's TV app does
 * the same.
 *
 * The phone keeps its player for a short time away (a message answered, the floating
 * window): it closes only when the app comes back after [PHONE_LIMIT_MS] off screen, and
 * never while it is the floating window.
 */
object Away {
    const val PHONE_LIMIT_MS = 30 * 60_000L

    /** The TV's player, stopped: closed unless it is only being rebuilt or is closing already. */
    fun closesWhenStopped(tv: Boolean, changingConfigurations: Boolean, finishing: Boolean): Boolean =
        tv && !changingConfigurations && !finishing

    /**
     * The phone's player, back on screen at [nowMs] after being stopped at [stoppedAtMs]
     * (0 = it was not). A clock set back while away reads as no time away: the player stays.
     */
    fun closesOnReturn(stoppedAtMs: Long, nowMs: Long, inPip: Boolean, limitMs: Long = PHONE_LIMIT_MS): Boolean =
        stoppedAtMs > 0 && !inPip && nowMs - stoppedAtMs >= limitMs
}

/**
 * The phone's player page: [onClose] when [owner] (the activity) is back on screen after
 * a long time off it (see [Away.closesOnReturn]). The time it left is kept with the page's
 * saved state, on the wall clock, so it also holds when the system ended the app meanwhile
 * and rebuilt the page: that is the "still on the same video a day later" case.
 * A player stopped is a player paused (the page pauses it), so there is no "still playing"
 * to spare here, only the floating window.
 */
@Composable
fun CloseAfterLongAbsence(owner: LifecycleOwner, inPip: () -> Boolean, now: () -> Long = System::currentTimeMillis, onClose: () -> Unit) {
    var stoppedAt by rememberSaveable { mutableLongStateOf(0L) }
    val close by rememberUpdatedState(onClose)
    LifecycleEventEffect(Lifecycle.Event.ON_STOP, owner) { stoppedAt = now() }
    LifecycleEventEffect(Lifecycle.Event.ON_START, owner) {
        val at = stoppedAt
        stoppedAt = 0L
        if (Away.closesOnReturn(at, now(), inPip())) {
            Timber.i("player closed: back after %d min away", (now() - at) / 60_000)
            close()
        }
    }
}
