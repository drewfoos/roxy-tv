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
        .onSuccess { items.userDataChanged() }
        .onFailure { Timber.w(it, "mark watched failed") }

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
