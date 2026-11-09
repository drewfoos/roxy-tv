package tv.roxy.tv.ui.detail

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.TestCoroutineScheduler
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import tv.roxy.tv.api.Item
import tv.roxy.tv.api.ItemsResponse
import tv.roxy.tv.api.Section

/**
 * The page's opening season (the next episode's) when page reads overlap the first /next
 * lookup: only the latest lookup decides, in the season list as it is then. Virtual time
 * drives the lookup's 3 s window.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class SeriesOpeningTest {
    private val clock = TestCoroutineScheduler()
    private val show = Item(id = "show", type = "series", title = "Show")
    private val s1 = Item(id = "s1", type = "season", title = "Season 1", indexAired = 1)
    private val s2 = Item(id = "s2", type = "season", title = "Season 2", indexAired = 2)
    private fun ep(season: String, n: Int) = Item(id = "$season-e$n", type = "episode", title = "E$n", parentId = season, indexAired = n)
    private fun eps(season: String) = ItemsResponse(listOf(ep(season, 1)), 1)

    @Before fun main() = Dispatchers.setMain(UnconfinedTestDispatcher(clock))
    @After fun reset() = Dispatchers.resetMain()

    private fun page(server: FakeSeriesServer, sections: List<Section> = emptyList()) {
        server.answer("item", show, newest = true)
        server.answer("seasons", listOf(s1, s2), newest = true)
        server.answer("sections", sections, newest = true)
    }

    private fun episodeReads(server: FakeSeriesServer) = server.calls.filter { it.startsWith("episodes:") }

    /** First read done, its /next lookup (the opening's) waiting; then a second read lands. */
    private fun twoLookups(server: FakeSeriesServer, secondSections: List<Section> = emptyList()): SeriesViewModel {
        val vm = SeriesViewModel(server, "show", initial = show)
        vm.load()
        page(server)
        vm.load() // back from the player, a watched mark...
        page(server, secondSections)
        assertEquals(2, server.pending("next"))
        // Nothing opened yet: no season read, the rows say loading.
        assertEquals(emptyList<String>(), episodeReads(server))
        assertTrue(vm.state.value.episodesLoading)
        return vm
    }

    // The first lookup answers (Season 2, from before a watch elsewhere) while the newer one
    // runs: not acted on. The newer answer (Season 1) opens the page, one season read.
    @Test fun staleLookupDoesNotOpen() {
        val server = FakeSeriesServer()
        val vm = twoLookups(server)
        server.answer("next", ep("s2", 4)) // the older one
        assertEquals(emptyList<String>(), episodeReads(server))
        server.answer("next", ep("s1", 7))
        assertEquals("s1", vm.state.value.seasons[vm.state.value.selectedSeason].id)
        assertEquals(listOf("episodes:s1"), episodeReads(server))
        assertEquals("s1-e7", vm.state.value.upNext?.id)
        server.answer("episodes:s1", eps("s1"))
        assertEquals("s1", vm.state.value.episodes.single().parentId)
    }

    // The newer read reordered the list (the sections now put an OVA before Season 2): the
    // season opened is Season 2 by id, not the old position.
    @Test fun openingUsesTheListOnScreen() {
        val server = FakeSeriesServer()
        val vm = twoLookups(server, secondSections = listOf(
            Section(kind = "season", seasonId = "s1"),
            Section(kind = "specials", title = "OVA", seasonId = "s0", episodeIds = listOf("ova1"), episodes = 1),
            Section(kind = "season", seasonId = "s2"),
        ))
        server.answer("next", ep("s2", 4), newest = true)
        assertEquals(2, vm.state.value.selectedSeason)
        assertEquals("s2", vm.state.value.seasons[2].id)
        assertEquals(listOf("episodes:s2"), episodeReads(server))
    }

    // The newer lookup hangs: its own window ends and the first season opens; the older
    // answer arriving meanwhile still changes nothing.
    @Test fun hungNewerLookupOpensTheFirstSeason() {
        val server = FakeSeriesServer()
        val vm = twoLookups(server)
        server.answer("next", ep("s2", 4))
        clock.advanceTimeBy(OPENING_NEXT_WAIT_MS + 1)
        clock.runCurrent()
        assertEquals(0, vm.state.value.selectedSeason)
        assertEquals(listOf("episodes:s1"), episodeReads(server))
    }

    // A season picked before any lookup answers ends the opening: neither lookup moves it.
    @Test fun pickEndsTheOpening() {
        val server = FakeSeriesServer()
        val vm = twoLookups(server)
        vm.selectSeason(1)
        server.answer("next", ep("s1", 7))
        server.answer("next", ep("s1", 7))
        assertEquals(1, vm.state.value.selectedSeason)
        assertEquals(listOf("episodes:s2"), episodeReads(server))
    }

    // One page read only (the usual case): its lookup opens the page as before.
    @Test fun singleLookupOpens() {
        val server = FakeSeriesServer()
        val vm = SeriesViewModel(server, "show", initial = show)
        vm.load()
        page(server)
        server.answer("next", ep("s2", 4))
        assertEquals(1, vm.state.value.selectedSeason)
        assertEquals(listOf("episodes:s2"), episodeReads(server))
    }

    // The first read fails on a page opened from a list (header from its card): the rows
    // said loading for ever with no Try again. Now an error with Try again, which works.
    @Test fun firstReadFailingShowsTryAgain() {
        val server = FakeSeriesServer()
        val vm = SeriesViewModel(server, "show", initial = show)
        vm.load()
        server.fail("item")
        assertEquals(false, vm.state.value.episodesLoading)
        assertEquals("Can't reach Roxy right now.", vm.state.value.episodesError)
        vm.retryEpisodes()
        assertEquals(true, vm.state.value.episodesLoading)
        page(server)
        server.answer("next", ep("s2", 4))
        server.answer("episodes:s2", eps("s2"))
        assertEquals(1, vm.state.value.selectedSeason)
        assertEquals(1, vm.state.value.episodes.size)
        assertEquals(null, vm.state.value.episodesError)
    }

    // The error page (nothing cached) shows loading again while its Try again reads.
    @Test fun errorPageRetryShowsLoading() {
        val server = FakeSeriesServer()
        val vm = SeriesViewModel(server, "show", initial = null)
        vm.load()
        server.fail("item")
        assertEquals("Can't reach Roxy right now.", vm.state.value.error)
        vm.load()
        assertEquals(true, vm.state.value.loading)
        assertEquals(null, vm.state.value.error)
    }

    // Tapping the season already shown while the opening waits is not a pick: the opening
    // still opens the next episode's season.
    @Test fun tappingTheShownSeasonKeepsTheOpening() {
        val server = FakeSeriesServer()
        val vm = SeriesViewModel(server, "show", initial = show)
        vm.load()
        page(server)
        vm.selectSeason(0)
        assertEquals(emptyList<String>(), episodeReads(server))
        server.answer("next", ep("s2", 4))
        assertEquals(1, vm.state.value.selectedSeason)
        assertEquals(listOf("episodes:s2"), episodeReads(server))
    }

    // Next is an OVA, listed in release order in its own entry (its parent, the Specials
    // season, is not an entry): the page opens on the OVA entry, not the first season.
    @Test fun openingOnASpecialFindsItsEntry() {
        val server = FakeSeriesServer()
        val vm = SeriesViewModel(server, "show", initial = show)
        vm.load()
        page(server, listOf(
            Section(kind = "season", seasonId = "s1"),
            Section(kind = "specials", title = "OVA", seasonId = "s0", episodeIds = listOf("ova1"), episodes = 1),
            Section(kind = "season", seasonId = "s2"),
        ))
        server.answer("next", Item(id = "ova1", type = "episode", title = "OVA", parentId = "s0"))
        assertEquals(1, vm.state.value.selectedSeason)
        assertEquals("OVA", vm.state.value.seasons[1].title)
        assertEquals(listOf("episodes:s0"), episodeReads(server))
    }

    // Reloads landing during the opening do not push it back: 3 s from the first lookup,
    // not from the newest (each reload used to start a fresh 3 s).
    @Test fun reloadsDoNotPushTheOpeningBack() {
        val server = FakeSeriesServer()
        val vm = SeriesViewModel(server, "show", initial = show)
        vm.load()
        page(server)
        clock.advanceTimeBy(2_000); clock.runCurrent()
        vm.load() // back from somewhere, 2 s in
        page(server)
        clock.advanceTimeBy(1_001); clock.runCurrent() // 3 s after the first lookup
        assertEquals(listOf("episodes:s1"), episodeReads(server))
        assertEquals(false, vm.state.value.seasons.isEmpty())
    }

    // Entry ids follow what the entry holds: an OVA coming in before a film keeps the film
    // picked across the reload.
    @Test fun pickedFilmSurvivesAnEntryAddedBeforeIt() {
        val server = FakeSeriesServer()
        val film = Item(id = "film1", type = "movie", title = "The Movie")
        val vm = SeriesViewModel(server, "show", initial = show)
        vm.load()
        page(server, listOf(Section(kind = "season", seasonId = "s1"), Section(kind = "movie", item = film), Section(kind = "season", seasonId = "s2")))
        server.answer("next", null)
        vm.selectSeason(1)
        assertEquals("The Movie", vm.state.value.seasons[vm.state.value.selectedSeason].title)
        vm.load()
        page(server, listOf(
            Section(kind = "season", seasonId = "s1"),
            Section(kind = "specials", title = "OVA", seasonId = "s0", episodeIds = listOf("ova1"), episodes = 1),
            Section(kind = "movie", item = film),
            Section(kind = "season", seasonId = "s2"),
        ))
        assertEquals("The Movie", vm.state.value.seasons[vm.state.value.selectedSeason].title)
    }
}
