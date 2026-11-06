package tv.roxy.tv.ui.components

import android.os.SystemClock
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.focus.FocusRequester
import timber.log.Timber

/** How long a page gets to put its first control on screen before focus gives up on it. */
const val FOCUS_BUDGET_MS = 600L

/**
 * Tries [attempt] once per frame until it succeeds or [budgetMs] has passed. On a TV, focus
 * can only go to something already laid out, and the target often is not yet (a row still
 * arriving, a title page drawn over two frames, a dialog's new window). The budget is time,
 * not a frame count, so a slow TV, slow artwork or a big library update get the same chance
 * as a quick one; giving up is logged with [what], so a page left with nothing focused shows
 * up in the logs instead of only on screen.
 */
suspend fun retryEachFrame(what: String, budgetMs: Long = FOCUS_BUDGET_MS, attempt: () -> Boolean): Boolean {
    val start = SystemClock.uptimeMillis()
    while (true) {
        withFrameNanos { }
        if (runCatching(attempt).getOrDefault(false)) return true
        val spent = SystemClock.uptimeMillis() - start
        if (spent >= budgetMs) {
            Timber.w("focus: gave up on %s after %d ms", what, spent)
            return false
        }
    }
}

/** requestFocus that reports "not there yet" as false instead of throwing. */
fun FocusRequester.tryFocus(): Boolean = runCatching { requestFocus() }.getOrDefault(false)
