package tv.roxy.tv.ui.nav

import androidx.navigation.NavBackStackEntry
import androidx.navigation.NavController
import androidx.navigation.NavDestination.Companion.hasRoute
import androidx.navigation.toRoute
import tv.roxy.tv.api.Item

/**
 * The player page's link to its title: "All Episodes" under an episode, "Back to the
 * movie" under a film.
 *
 * It used to be the Back button under another name, which is the title's page only when
 * the player was opened from there. Opened from Home (a Continue Watching card), from a
 * search or from another device, "All Episodes" went back to Home.
 *
 * Now it leads to the title's page whatever lies under the player:
 *  - that very page (the show's, the film's, or the downloaded show's): back to it;
 *  - anything else: the title's page takes the player's place, so Back from it goes
 *    where Back from the player would have gone, and not into a player that was left.
 *
 * Player pages opened one from another ("Next Episode") count as one: none is left
 * under the title's page.
 *
 * With the server away the title's page is the downloaded one, when the title is
 * [downloaded]; when it is not there is no page to show, and the link is Back again.
 */
fun NavController.openTitleFromPlayer(title: Item, online: Boolean, downloaded: Boolean) {
    while (previousBackStackEntry?.destination?.hasRoute<Route.Episode>() == true) popBackStack()
    val page: Route? = when {
        previousBackStackEntry?.isPageOf(title) == true -> null
        online -> if (title.isMovie) Route.Movie(title.id, title.title) else Route.Series(title.id, title.title)
        downloaded -> Route.DownloadedTitle(title.id)
        else -> null
    }
    if (page == null) popBackStack()
    else navigate(page) { popUpTo<Route.Episode> { inclusive = true } }
}

/** Whether this entry is [title]'s own page: the library's or the downloaded one. */
private fun NavBackStackEntry.isPageOf(title: Item): Boolean = when {
    destination.hasRoute<Route.Series>() -> !title.isMovie && toRoute<Route.Series>().id == title.id
    destination.hasRoute<Route.Movie>() -> title.isMovie && toRoute<Route.Movie>().id == title.id
    destination.hasRoute<Route.DownloadedTitle>() -> toRoute<Route.DownloadedTitle>().id == title.id
    else -> false
}
