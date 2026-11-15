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
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import tv.roxy.tv.api.Item
import tv.roxy.tv.api.ItemsResponse
import tv.roxy.tv.api.Section

/**
 * A series page opened a second time is there at once, as it stood: the season list, the
 * season that was showing, its episodes and Play's words do not arrive a moment after the
 * page again. It is read again behind what is shown.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class SeriesPageMemoryTest {
    private val show = Item(id = "show", type = "series", title = "Show")
    private val s1 = Item(id = "s1", type = "season", title = "Season 1", indexAired = 1)
    private val s2 = Item(id = "s2", type = "season", title = "Season 2", indexAired = 2)
    private val e1 = Item(id = "s1-e1", type = "episode", title = "E1", parentId = "s1", indexAired = 1)
    private val e2 = Item(id = "s2-e1", type = "episode", title = "S2 E1", parentId = "s2", indexAired = 1)
    private val e3 = Item(id = "s2-e2", type = "episode", title = "S2 E2", parentId = "s2", indexAired = 2)

    @Before fun main() = Dispatchers.setMain(UnconfinedTestDispatcher())
    @After fun reset() = Dispatchers.resetMain()

    /** The page read, opened on Season 1, then Season 2 picked and read. */
    private fun visited(memory: PageMemory<SeriesPage>, key: String = "p:show"): SeriesViewModel {
        val server = FakeSeriesServer()
        val vm = SeriesViewModel(server, "show", show, memory, key)
        vm.load()
        server.answer("item", show)
        server.answer("seasons", listOf(s1, s2))
        server.answer("sections", emptyList<Section>())
        server.answer("next", e1)
        server.answer("episodes:s1", ItemsResponse(listOf(e1), 1))
        vm.selectSeason(1)
        server.answer("episodes:s2", ItemsResponse(listOf(e2), 1))
        return vm
    }

    @Test fun aSecondVisitOpensAsTheFirstWasLeft() {
        val memory = PageMemory<SeriesPage>()
        visited(memory)

        val server = FakeSeriesServer()
        val again = SeriesViewModel(server, "show", show, memory, "p:show")
        // Before anything is read: all of it is there.
        val s = again.state.value
        assertFalse(s.loading)
        assertEquals(listOf("s1", "s2"), s.seasons.map { it.id })
        assertEquals(1, s.selectedSeason)
        assertEquals(listOf("s2-e1"), s.episodes.map { it.id })
        assertTrue(s.upNextKnown)
        assertEquals("s1-e1", s.upNext?.id)

        // Read again behind it: the season on screen is the one read (not the opening's
        // choice a second time), and its rows stay up until the new ones land.
        again.load()
        server.answer("item", show)
        server.answer("seasons", listOf(s1, s2))
        server.answer("sections", emptyList<Section>())
        assertEquals(1, server.pending("episodes:s2"))
        assertEquals(0, server.pending("episodes:s1"))
        assertEquals(listOf("s2-e1"), again.state.value.episodes.map { it.id })
        server.answer("episodes:s2", ItemsResponse(listOf(e2, e3), 2))
        server.answer("next", e3)
        assertEquals(listOf("s2-e1", "s2-e2"), again.state.value.episodes.map { it.id })
        assertEquals(1, again.state.value.selectedSeason)
        assertEquals("s2-e2", again.state.value.upNext?.id)
    }

    @Test fun anotherProfileStartsFresh() {
        val memory = PageMemory<SeriesPage>()
        visited(memory, "drew:show")
        val other = SeriesViewModel(FakeSeriesServer(), "show", show, memory, "coffee:show")
        assertTrue(other.state.value.seasons.isEmpty())
        assertTrue(other.state.value.episodes.isEmpty())
    }

    @Test fun aPageThatFailedIsNotRemembered() {
        val memory = PageMemory<SeriesPage>()
        val server = FakeSeriesServer()
        val vm = SeriesViewModel(server, "show", show, memory, "p:show")
        vm.load()
        server.answer("item", show)
        server.answer("seasons", listOf(s1, s2))
        server.answer("sections", emptyList<Section>())
        server.answer("next", e1)
        server.fail("episodes:s1")
        assertNull(memory.get("p:show"))
    }

    @Test fun withoutAMemoryNothingIsKept() {
        // The plain constructor (every other test): each page starts from nothing.
        val vm = SeriesViewModel(FakeSeriesServer(), "show", initial = show)
        assertTrue(vm.state.value.seasons.isEmpty())
    }
}
