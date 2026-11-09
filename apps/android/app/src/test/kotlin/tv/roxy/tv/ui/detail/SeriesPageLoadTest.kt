package tv.roxy.tv.ui.detail

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Before
import org.junit.Test
import tv.roxy.tv.api.Item
import tv.roxy.tv.api.ItemsResponse
import tv.roxy.tv.api.Section

/**
 * Overlapping page reads (back from the player, a watched mark, Try again): only the latest
 * one's answer is used, whichever lands last. Each case runs twice: with the older read
 * cancelled for real, and with its HTTP call answering anyway.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class SeriesPageLoadTest {
    private val show = Item(id = "show", type = "series", title = "Show")
    private val s1 = Item(id = "s1", type = "season", title = "Season 1", indexAired = 1)
    private val s2 = Item(id = "s2", type = "season", title = "Season 2", indexAired = 2)
    private val s3 = Item(id = "s3", type = "season", title = "Season 3", indexAired = 3)
    private val ep = Item(id = "s1-e1", type = "episode", title = "E1", parentId = "s1", indexAired = 1)

    @Before fun main() = Dispatchers.setMain(UnconfinedTestDispatcher())
    @After fun reset() = Dispatchers.resetMain()

    private fun both(body: (FakeSeriesServer) -> Unit) {
        body(FakeSeriesServer(ignoresCancel = false))
        body(FakeSeriesServer(ignoresCancel = true))
    }

    /** The page open on Season 1 of [s1, s2]. */
    private fun opened(server: FakeSeriesServer): SeriesViewModel {
        val vm = SeriesViewModel(server, "show", initial = show)
        vm.load()
        server.answer("item", show)
        server.answer("seasons", listOf(s1, s2))
        server.answer("sections", emptyList<Section>())
        server.answer("next", null)
        server.answer("episodes:s1", ItemsResponse(listOf(ep), 1))
        return vm
    }

    // Older read A, newer read B; B lands first (Season 3 added, sections now), A after with
    // the old list. B's list stays.
    @Test fun olderPageReadLandingLastIsDropped() = both { server ->
        val vm = opened(server)
        vm.load() // A
        server.answer("item", show)
        vm.load() // B
        server.answer("item", show, newest = true)
        server.answer("seasons", listOf(s1, s2, s3), newest = true)
        server.answer("sections", listOf(Section(kind = "season", seasonId = "s1"), Section(kind = "season", seasonId = "s2"), Section(kind = "season", seasonId = "s3")), newest = true)
        assertEquals(3, vm.state.value.seasons.size)
        // A's answers come now (only when its call ignored the cancel is anything waiting).
        if (server.pending("seasons") > 0) server.answer("seasons", listOf(s1, s2))
        if (server.pending("sections") > 0) server.answer("sections", emptyList<Section>())
        assertEquals(listOf("s1", "s2", "s3"), vm.state.value.seasons.map { it.id })
    }

    // A failing after B worked changes nothing either (no error, no stopped loading).
    @Test fun olderPageReadFailingLastIsDropped() = both { server ->
        val vm = opened(server)
        vm.load() // A
        vm.load() // B
        server.answer("item", show, newest = true)
        server.answer("seasons", listOf(s1, s2, s3), newest = true)
        server.answer("sections", emptyList<Section>(), newest = true)
        if (server.pending("item") > 0) server.fail("item")
        assertEquals(3, vm.state.value.seasons.size)
        assertNull(vm.state.value.error)
        assertFalse(vm.state.value.loading)
    }

    // First open with nothing cached (a link, the remote): two reads overlap, the newer one
    // works, the older one fails after it. The page stays; it used to turn into the error.
    @Test fun olderFirstReadFailingLastKeepsThePage() = both { server ->
        val vm = SeriesViewModel(server, "show", initial = null)
        vm.load() // A
        vm.load() // B
        server.answer("item", show, newest = true)
        server.answer("seasons", listOf(s1, s2), newest = true)
        server.answer("sections", emptyList<Section>(), newest = true)
        if (server.pending("item") > 0) server.fail("item")
        assertEquals("show", vm.state.value.series?.id)
        assertNull(vm.state.value.error)
    }

    // Try again replaced by a newer read (back from the player) that fails: the problem
    // comes back, rather than loading rows for ever.
    @Test fun retryReplacedByAFailingReadBringsTheProblemBack() = both { server ->
        val vm = SeriesViewModel(server, "show", initial = show)
        vm.load()
        server.answer("item", show)
        server.answer("seasons", listOf(s1, s2))
        server.answer("sections", emptyList<Section>())
        server.answer("next", null)
        server.fail("episodes:s1")
        vm.retryEpisodes()
        vm.load()
        server.fail("item", newest = true)
        if (server.pending("item") > 0) server.answer("item", show)
        if (server.pending("seasons") > 0) server.answer("seasons", listOf(s1, s2))
        if (server.pending("sections") > 0) server.answer("sections", emptyList<Section>())
        assertEquals("Can't reach Roxy right now.", vm.state.value.episodesError)
        assertFalse(vm.state.value.episodesLoading)
    }

    // A plain reload in flight, then Try again; the reload fails while the retry still
    // reads: the retry's problem must not come back early (the retry is still loading).
    @Test fun olderReadFailingDuringARetryLeavesItLoading() = both { server ->
        val vm = SeriesViewModel(server, "show", initial = show)
        vm.load()
        server.answer("item", show)
        server.answer("seasons", listOf(s1, s2))
        server.answer("sections", emptyList<Section>())
        server.answer("next", null)
        server.fail("episodes:s1")
        vm.load() // A: back from somewhere
        vm.retryEpisodes() // B
        if (server.pending("item") > 1) server.fail("item") // A's call answers anyway
        assertNull(vm.state.value.episodesError)
        assertEquals(true, vm.state.value.episodesLoading)
        server.answer("item", show, newest = true)
        server.answer("seasons", listOf(s1, s2), newest = true)
        server.answer("sections", emptyList<Section>(), newest = true)
        server.answer("episodes:s1", ItemsResponse(listOf(ep), 1), newest = true)
        assertEquals(1, vm.state.value.episodes.size)
        assertNull(vm.state.value.episodesError)
    }

    // /next of A answering after /next of B: Play keeps B's answer.
    @Test fun olderNextLandingLastIsDropped() = both { server ->
        val vm = opened(server)
        val e5 = ep.copy(id = "s2-e5", parentId = "s2")
        val e6 = ep.copy(id = "s2-e6", parentId = "s2")
        repeat(2) {
            vm.load()
            server.answer("item", show, newest = true)
            server.answer("seasons", listOf(s1, s2), newest = true)
            server.answer("sections", emptyList<Section>(), newest = true)
        }
        server.answer("next", e6, newest = true)
        server.answer("next", e5)
        assertEquals("s2-e6", vm.state.value.upNext?.id)
    }
}
