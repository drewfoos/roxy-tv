package tv.roxy.tv.ui.phone

import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalContext
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import timber.log.Timber
import tv.roxy.tv.RoxyApp

/*
 * Watched state and the Watchlist change from many places (card menus, title pages, the
 * episode list, the bookmark button). Every change goes through these, so every list and
 * title page on screen refreshes: Home, Browse, My Lists, the series / movie / episode pages.
 */

/** Mark watched / unwatched (a show or season: every episode under it), then refresh everywhere. */
suspend fun RoxyApp.markWatched(itemId: String, on: Boolean): Result<Unit> =
    runCatching { api.setPlayed(itemId, on) }
        .recoverCatching { e ->
            // Away from the server, "watched" on a download is kept on this device and sent
            // when the server is back, like the progress of an episode played offline.
            // ("Unwatched" has no such report to queue, and nothing is known of other titles.)
            if (!markWatchedOffline(e, on, downloaded = downloads.entry(itemId)?.isDone == true)) throw e
            if (!downloads.recordProgress(itemId, 0, reportedToServer = false, played = true, durableNow = true)) throw e
        }
        .onSuccess { items.userDataChanged() }
        .onFailure { Timber.w(it, "mark watched failed") }

/** A failed "mark watched" that can wait on this device: the server did not answer (not: it refused), and the item is a download. */
internal fun markWatchedOffline(failure: Throwable, on: Boolean, downloaded: Boolean): Boolean =
    on && downloaded && failure is java.io.IOException && failure !is tv.roxy.tv.api.ApiException

/** Add to / remove from the Watchlist, then refresh everywhere. */
suspend fun RoxyApp.setWatchlist(itemId: String, on: Boolean): Result<Unit> =
    runCatching { api.setFavorite(itemId, on) }
        .onSuccess { items.userDataChanged() }
        .onFailure { Timber.w(it, "watchlist failed") }

/** Runs [action] each time watched state or the Watchlist changes after this screen appeared. */
@Composable
fun OnUserDataChanged(action: () -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    val n by app.items.changes.collectAsStateWithLifecycle()
    val start = remember { n }
    LaunchedEffect(n) { if (n != start) action() }
}
