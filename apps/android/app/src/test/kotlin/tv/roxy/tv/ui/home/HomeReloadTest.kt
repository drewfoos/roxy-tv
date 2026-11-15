package tv.roxy.tv.ui.home

import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import tv.roxy.tv.api.RecordingServer
import tv.roxy.tv.api.RoxyApi
import tv.roxy.tv.data.Prefs
import tv.roxy.tv.data.Session

/**
 * Every return to Home (back from a title page, from the player) read the rows and then
 * worked the carousel out again: a series and its next episode per slide, up to twelve
 * requests, plus a look for the server at each of its addresses. With the same rows as
 * before, one request is enough.
 */
@OptIn(ExperimentalCoroutinesApi::class)
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = android.app.Application::class)
class HomeReloadTest {
    private val context = ApplicationProvider.getApplicationContext<android.app.Application>()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined)
    @Volatile private var position = 1000
    private val server = RecordingServer { seen ->
        when {
            seen.path == "/v1/system/info" -> 200 to """{"product":"roxy","id":"srv1"}"""
            seen.path == "/v1/home" -> 200 to """{"rows":[
                {"key":"continue","title":"Continue Watching","items":[{"id":"e1","type":"episode","title":"One","seriesId":"showA","userData":{"positionMs":$position}}]},
                {"key":"recentEpisodes","title":"New Episodes","items":[{"id":"e9","type":"episode","title":"Nine","seriesId":"showB"}]}]}"""
            seen.path.endsWith("/next") -> 200 to """{"item":{"id":"e8","type":"episode","title":"Eight","seriesId":"showB"}}"""
            seen.path.startsWith("/v1/items/show") -> 200 to """{"id":"${seen.path.substringAfterLast('/')}","type":"series","title":"A show"}"""
            else -> 404 to "{}"
        }
    }
    private val prefs = Prefs(context, scope)
    private var version = 0L
    private val vm = HomeViewModel(RoxyApi(prefs), libraryVersion = { version }, minRefreshMs = 0)

    @Before fun start() {
        Dispatchers.setMain(UnconfinedTestDispatcher())
        runBlocking { prefs.saveSession(Session(serverUrl = server.url, token = "t", profileId = "p", serverId = "srv1")) }
    }

    @After fun stop() { Dispatchers.resetMain(); server.close(); scope.cancel() }

    private fun paths() = server.seen.map { it.path }
    private fun count(path: String) = paths().count { it == path }

    private fun waitFor(what: String, cond: () -> Boolean) {
        val until = System.currentTimeMillis() + 5_000
        while (!cond()) {
            if (System.currentTimeMillis() > until) throw AssertionError("$what; server saw ${paths()}")
            Thread.sleep(10)
        }
    }

    private fun heroes() = (vm.state.value as? HomeUiState.Ready)?.heroes.orEmpty()

    /** Home as first opened: rows read, two slides worked out. */
    private fun opened() {
        vm.load()
        waitFor("the carousel is worked out") { heroes().size == 2 }
        assertEquals(listOf("showA", "showB"), heroes().map { it.title.id })
        assertEquals("the new episode's slide plays the show's next one", "e8", heroes()[1].play.id)
    }

    /** A silent reload, and the time for anything it starts to reach the server. */
    private fun comeBack() {
        val homes = count("/v1/home")
        vm.load(silent = true)
        waitFor("the rows are read again") { count("/v1/home") == homes + 1 }
        Thread.sleep(300)
    }

    @Test fun comingBackToTheSameRowsReadsOnlyTheRows() {
        opened()
        val before = paths()

        comeBack()

        assertEquals("only the rows", listOf("/v1/home"), paths().drop(before.size))
        assertEquals(listOf("showA", "showB"), heroes().map { it.title.id })
    }

    // Something was watched meanwhile: Continue Watching differs, and so may the slides.
    @Test fun changedRowsWorkTheCarouselOutAgain() {
        opened()
        position = 5000
        val before = count("/v1/items/showA")

        comeBack()

        waitFor("the slides are read again") { count("/v1/items/showA") == before + 1 && count("/v1/items/showB/next") == 2 }
        val ready = vm.state.value as HomeUiState.Ready
        assertEquals("and the row itself is the new one", 5000L, ready.rows.first().items.first().userData?.positionMs)
    }

    // A watched mark or new artwork made in the app: the rows may read the same, the slides not.
    @Test fun aChangeMadeInTheAppWorksTheCarouselOutAgain() {
        opened()
        version++
        val before = count("/v1/items/showB/next")

        comeBack()

        waitFor("the slides are read again") { count("/v1/items/showB/next") == before + 1 }
    }

    @Test fun rowsOnScreenAreNotCheckedByALookForTheServer() {
        opened()
        val looks = count("/v1/system/info")
        position = 5000 // the long way round, with the carousel read again

        comeBack()

        assertEquals(looks, count("/v1/system/info"))
        assertTrue(vm.state.value is HomeUiState.Ready)
    }

    // A pull down, or Home tapped while on Home: the rows are read again behind the page,
    // and the page says so until they are in.
    @Test fun aReloadAskedForSaysSoUntilTheRowsAreIn() {
        opened()
        val before = paths()

        vm.refresh()
        assertTrue("reloading is shown at once", vm.refreshing.value)
        assertEquals("a pull has its own circle, not the bar's", false, vm.reloadingByTab.value)
        assertTrue("with the page left as it is", vm.state.value is HomeUiState.Ready)
        vm.refresh() // an impatient second tap asks nothing more

        waitFor("the reload ends") { !vm.refreshing.value }
        Thread.sleep(300)
        assertEquals("the rows, once", listOf("/v1/home"), paths().drop(before.size))
    }

    // Home tapped in the bar while on Home: the bar's Home icon is the circle meanwhile.
    @Test fun aReloadFromTheBarIsShownOnTheBar() {
        opened()

        vm.refresh(byTab = true)
        assertTrue(vm.refreshing.value)
        assertTrue("the bar shows it", vm.reloadingByTab.value)

        waitFor("the reload ends") { !vm.refreshing.value }
        assertEquals("and the icon is back", false, vm.reloadingByTab.value)
    }

    @Test fun aReloadWithNoPageYetIsAnOrdinaryLoad() {
        vm.refresh()
        waitFor("the carousel is worked out") { heroes().size == 2 }
        assertEquals(false, vm.refreshing.value)
    }
}
