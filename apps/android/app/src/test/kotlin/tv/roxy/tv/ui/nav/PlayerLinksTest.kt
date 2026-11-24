package tv.roxy.tv.ui.nav

import androidx.compose.ui.test.junit4.createComposeRule
import androidx.navigation.NavBackStackEntry
import androidx.navigation.NavDestination.Companion.hasRoute
import androidx.navigation.NavHostController
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.rememberNavController
import androidx.navigation.toRoute
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import tv.roxy.tv.api.Item
import tv.roxy.tv.ui.phone.playerTitle
import java.io.File

/**
 * "All Episodes" on the player's page was the Back button under another name: with the
 * player opened from Home (a Continue Watching card) it went to Home, not to the show.
 * It leads to the title's own page from wherever the player was opened.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class PlayerLinksTest {
    @get:Rule val compose = createComposeRule()
    private lateinit var nav: NavHostController

    private val show = Item(id = "show", type = "series", title = "Show")
    private val other = Item(id = "other", type = "series", title = "Other")
    private val film = Item(id = "film", type = "movie", title = "Film")

    @Before fun setUp() {
        compose.setContent {
            nav = rememberNavController()
            NavHost(nav, startDestination = Route.Home) {
                composable<Route.Home> { }
                composable<Route.Downloads> { }
                composable<Route.DownloadedTitle> { }
                composable<Route.Series> { }
                composable<Route.Movie> { }
                composable<Route.Episode> { }
            }
        }
        compose.waitForIdle()
    }

    private fun go(vararg routes: Route) {
        compose.runOnUiThread { routes.forEach { nav.navigate(it) } }
        compose.waitForIdle()
    }

    private fun link(title: Item, online: Boolean = true, downloaded: Boolean = false) {
        compose.runOnUiThread { nav.openTitleFromPlayer(title, online, downloaded) }
        compose.waitForIdle()
    }

    private fun name(e: NavBackStackEntry): String = when {
        e.destination.hasRoute<Route.Home>() -> "home"
        e.destination.hasRoute<Route.Downloads>() -> "downloads"
        e.destination.hasRoute<Route.DownloadedTitle>() -> "downloaded:" + e.toRoute<Route.DownloadedTitle>().id
        e.destination.hasRoute<Route.Series>() -> "series:" + e.toRoute<Route.Series>().id
        e.destination.hasRoute<Route.Movie>() -> "movie:" + e.toRoute<Route.Movie>().id
        e.destination.hasRoute<Route.Episode>() -> "player:" + e.toRoute<Route.Episode>().id
        else -> "?"
    }

    /** The pages from the one in front down to Home, read by going back through them. */
    private fun pages(): List<String> {
        val out = mutableListOf<String>()
        compose.runOnUiThread {
            while (true) {
                out += name(nav.currentBackStackEntry ?: break)
                if (nav.previousBackStackEntry == null) break
                nav.popBackStack()
            }
        }
        return out
    }

    private fun player(id: String) = Route.Episode(id, "")

    @Test fun fromHomeItOpensTheShowAndBackFromThereIsHome() {
        go(player("e1")) // a Continue Watching card
        link(show)
        assertEquals("the show's page in the player's place", listOf("series:show", "home"), pages())
    }

    @Test fun openedFromTheShowsPageItGoesBackToIt() {
        go(Route.Series("show", "Show"), player("e1"))
        link(show)
        assertEquals("not a second page of the same show on top of the first", listOf("series:show", "home"), pages())
    }

    @Test fun anotherShowsPageUnderThePlayerIsNotIt() {
        go(Route.Series("other", "Other"), player("e1")) // e1 is of "show": opened from a suggestion
        link(show)
        assertEquals(listOf("series:show", "series:other", "home"), pages())
    }

    @Test fun playersOpenedOneFromAnotherAreLeftTogether() {
        go(player("e1"), player("e2"), player("e3")) // "Next Episode", twice
        link(show)
        assertEquals("no player under the show's page to fall back into", listOf("series:show", "home"), pages())
    }

    @Test fun aChainOfPlayersOverTheShowsPageGoesBackToIt() {
        go(Route.Series("show", "Show"), player("e1"), player("e2"))
        link(show)
        assertEquals(listOf("series:show", "home"), pages())
    }

    @Test fun aFilmsLinkLeadsToTheFilmsPage() {
        go(player("film"))
        link(film)
        assertEquals(listOf("movie:film", "home"), pages())
    }

    @Test fun aFilmOpenedFromItsPageGoesBackToIt() {
        go(Route.Movie("film", "Film"), player("film"))
        link(film)
        assertEquals(listOf("movie:film", "home"), pages())
    }

    /** A show and a film may share nothing but an id's text: the kind is part of "that very page". */
    @Test fun aSeriesPageIsNotAFilmsPage() {
        go(Route.Series("film", "A show whose id reads the same"), player("film"))
        link(film)
        assertEquals(listOf("movie:film", "series:film", "home"), pages())
    }

    @Test fun fromTheDownloadedShowsPageItGoesBackToThat() {
        go(Route.Downloads, Route.DownloadedTitle("show"), player("e1"))
        link(show, online = true, downloaded = true)
        assertEquals("the list the player was opened from", listOf("downloaded:show", "downloads", "home"), pages())
    }

    @Test fun withTheServerAwayItIsTheDownloadedShowsPage() {
        go(player("e1")) // from Home's offline shelf
        link(show, online = false, downloaded = true)
        assertEquals(listOf("downloaded:show", "home"), pages())
    }

    @Test fun withTheServerAwayAndNothingDownloadedItIsBack() {
        go(player("e1"))
        link(show, online = false, downloaded = false)
        assertEquals("no page there to show", listOf("home"), pages())
    }

    @Test fun theTitleOfAPlayerPage() {
        val episode = Item(id = "e1", type = "episode", title = "One", seriesId = "show", seriesTitle = "Show")
        assertEquals("the series as read", show.copy(overview = "Read."), playerTitle(episode, show.copy(overview = "Read.")))
        assertEquals("not yet read: what the episode says of it", Item(id = "show", type = "series", title = "Show"), playerTitle(episode, null))
        assertEquals("another show's page left in memory is not this one's", "show", playerTitle(episode, other)?.id)
        assertEquals("a film is its own title", film, playerTitle(film, null))
        assertEquals(null, playerTitle(Item(id = "x", type = "episode", title = "Loose file"), null))
    }

    /** The page's link asks for the title's page, and the phone's navigation answers with [openTitleFromPlayer]. */
    @Test fun theLinkIsWiredToIt() {
        val src = generateSequence(File("").absoluteFile) { it.parentFile }.map { File(it, "src/main/kotlin/tv/roxy/tv/ui/phone") }.first { it.isDirectory }
        val page = File(src, "PhoneEpisode.kt").readText()
        assertTrue("\"All Episodes\" opens the title", ".testTag(\"to-title\").clickable { if (title != null) onOpenTitle(title) else onBack() }" in page)
        val root = File(src, "PhoneRoot.kt").readText()
        assertTrue("onOpenTitle = { t -> app.items.put(t); nav.openTitleFromPlayer(t, online = app.server.online.value, downloaded = app.downloads.title(t.id) != null) }" in root)
    }
}
