package tv.roxy.tv.ui.detail

import kotlinx.coroutines.CompletableDeferred
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
import tv.roxy.tv.api.RelatedEntry
import tv.roxy.tv.api.Section
import java.io.IOException

/**
 * The series page's Try again against a server the test answers by hand: every call waits
 * until the test responds to it, so the order in which answers land is the test's choice.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class SeriesRetryTest {
    private class Server : SeriesSource {
        val calls = mutableListOf<String>()
        private val waiting = mutableMapOf<String, ArrayDeque<CompletableDeferred<Result<Any?>>>>()

        @Suppress("UNCHECKED_CAST")
        private suspend fun <T> call(key: String): T {
            calls += key
            val d = CompletableDeferred<Result<Any?>>()
            waiting.getOrPut(key) { ArrayDeque() }.addLast(d)
            return d.await().getOrThrow() as T
        }

        fun pending(key: String) = waiting[key]?.size ?: 0
        fun answer(key: String, value: Any?) = waiting.getValue(key).removeFirst().complete(Result.success(value))
        fun fail(key: String) = waiting.getValue(key).removeFirst().complete(Result.failure(IOException("network down")))

        override suspend fun item(id: String): Item = call("item")
        override suspend fun seasons(seriesId: String): List<Item> = call("seasons")
        override suspend fun sections(seriesId: String): List<Section> = call("sections")
        override suspend fun next(seriesId: String): Item? = call("next")
        override suspend fun related(seriesId: String): List<RelatedEntry> = call("related")
        override suspend fun extras(seriesId: String): List<Item> = call("extras")
        override suspend fun episodes(parentId: String, recursive: Boolean): ItemsResponse = call("episodes:$parentId")
    }

    private val show = Item(id = "show", type = "series", title = "Show")
    private val s1 = Item(id = "s1", type = "season", title = "Season 1", indexAired = 1)
    private val s2 = Item(id = "s2", type = "season", title = "Season 2", indexAired = 2)
    private val s3 = Item(id = "s3", type = "season", title = "Season 3", indexAired = 3)
    private fun eps(season: String, n: Int) = ItemsResponse((1..n).map { Item(id = "$season-e$it", type = "episode", title = "E$it", parentId = season, indexAired = it) }, n)

    @Before fun main() = Dispatchers.setMain(UnconfinedTestDispatcher())
    @After fun reset() = Dispatchers.resetMain()

    /** The page open on Season 1 whose episode read failed: the error with Try again is up. */
    private fun failedOnSeason1(server: Server): SeriesViewModel {
        val vm = SeriesViewModel(server, "show", initial = show)
        vm.load()
        server.answer("item", show)
        server.answer("seasons", listOf(s1, s2, s3))
        server.answer("sections", emptyList<Section>()) // older server: the plain list
        server.answer("next", null)
        server.fail("episodes:s1")
        assertEquals("Can't reach Roxy right now.", vm.state.value.episodesError)
        return vm
    }

    // Episodes fail -> Try again -> Season 2 picked before the retry answers -> the retry's
    // page read lands: Season 2 stays picked and shown, read once, with no error.
    @Test fun retryLandingAfterASeasonPickLeavesThePick() {
        val server = Server()
        val vm = failedOnSeason1(server)
        vm.retryEpisodes()
        vm.selectSeason(1)
        server.answer("episodes:s2", eps("s2", 12))
        server.answer("item", show)
        server.answer("seasons", listOf(s1, s2, s3))
        server.answer("sections", emptyList<Section>())
        val st = vm.state.value
        assertEquals(1, st.selectedSeason)
        assertEquals(12, st.episodes.size)
        assertEquals("s2", st.episodes.first().parentId)
        assertNull(st.episodesError)
        assertFalse(st.episodesLoading)
        // Not read again for the retry, and Season 1 not read after the pick.
        assertEquals(1, server.calls.count { it == "episodes:s2" })
        assertEquals(0, server.pending("episodes:s1"))
    }

    // Same, with Season 2's episodes still on their way when the retry's page read lands.
    @Test fun retryLandingBeforeThePickedSeasonsEpisodes() {
        val server = Server()
        val vm = failedOnSeason1(server)
        vm.retryEpisodes()
        vm.selectSeason(1)
        server.answer("item", show)
        server.answer("seasons", listOf(s1, s2, s3))
        server.answer("sections", emptyList<Section>())
        assertEquals(1, server.pending("episodes:s2")) // the pick's read, not a second one
        server.answer("episodes:s2", eps("s2", 12))
        val st = vm.state.value
        assertEquals(1, st.selectedSeason)
        assertEquals(12, st.episodes.size)
        assertNull(st.episodesError)
    }

    // The retry's page read fails after the pick: Season 1's old error must not come back
    // over Season 2 (it did: the retry restored the problem it was retrying).
    @Test fun failedRetryAfterASeasonPickDoesNotBringBackTheOldError() {
        val server = Server()
        val vm = failedOnSeason1(server)
        vm.retryEpisodes()
        vm.selectSeason(1)
        server.answer("episodes:s2", eps("s2", 12))
        server.fail("item")
        val st = vm.state.value
        assertEquals(1, st.selectedSeason)
        assertEquals(12, st.episodes.size)
        assertNull(st.episodesError)
        assertFalse(st.episodesLoading)
    }

    // ... and while Season 2 is still loading: it keeps loading (no error, no early stop).
    @Test fun failedRetryWhileThePickLoadsKeepsLoading() {
        val server = Server()
        val vm = failedOnSeason1(server)
        vm.retryEpisodes()
        vm.selectSeason(1)
        server.fail("item")
        assertNull(vm.state.value.episodesError)
        assertEquals(true, vm.state.value.episodesLoading)
        server.answer("episodes:s2", eps("s2", 12))
        assertEquals(12, vm.state.value.episodes.size)
    }

    // The retry alone (no pick) still works: a failed page read brings the error back, a
    // good one reads the episodes again.
    @Test fun retryWithoutAPick() {
        val server = Server()
        val vm = failedOnSeason1(server)
        vm.retryEpisodes()
        server.fail("item")
        assertEquals("Can't reach Roxy right now.", vm.state.value.episodesError)
        vm.retryEpisodes()
        server.answer("item", show)
        server.answer("seasons", listOf(s1, s2, s3))
        server.answer("sections", emptyList<Section>())
        server.answer("episodes:s1", eps("s1", 10))
        assertEquals(10, vm.state.value.episodes.size)
        assertNull(vm.state.value.episodesError)
    }

    // The page read again answers with a different list (the release-order sections work
    // this time and put an OVA before Season 2): the season on screen stays the same one.
    @Test fun reorderedListKeepsTheSeasonOnScreen() {
        val server = Server()
        val vm = failedOnSeason1(server)
        vm.selectSeason(1)
        server.answer("episodes:s2", eps("s2", 12))
        vm.load()
        server.answer("item", show)
        server.answer("seasons", listOf(s1, s2, s3))
        server.answer("sections", listOf(
            Section(kind = "season", seasonId = "s1"),
            Section(kind = "specials", title = "OVA", seasonId = "s0", episodeIds = listOf("ova1"), episodes = 1),
            Section(kind = "season", seasonId = "s2"),
            Section(kind = "season", seasonId = "s3"),
        ))
        val st = vm.state.value
        assertEquals("s2", st.seasons[st.selectedSeason].id)
        assertEquals(1, server.pending("episodes:s2"))
    }
}
