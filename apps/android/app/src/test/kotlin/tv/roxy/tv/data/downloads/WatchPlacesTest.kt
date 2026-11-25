package tv.roxy.tv.data.downloads

import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.serialization.json.Json
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import tv.roxy.tv.api.Item
import tv.roxy.tv.api.UserData
import tv.roxy.tv.data.Prefs
import tv.roxy.tv.ui.inPlayOrder
import java.io.File

/**
 * What plays next follows the season list. The finding: a show's season list has its OVAs
 * between the seasons they came out between, but "next episode" went by the seasons'
 * numbers with every special last, so after season 1's last episode came season 2's
 * first, past the OVAs. The server gives each episode its place in the order of watching
 * ([Item.watchOrder]); the app sorts by it, and downloads learn theirs.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = android.app.Application::class)
class WatchPlacesTest {
    private val context = ApplicationProvider.getApplicationContext<android.app.Application>()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined)
    private val json = Json { ignoreUnknownKeys = true; encodeDefaults = true; explicitNulls = false }

    @After fun stop() { scope.cancel() }

    private fun ep(id: String, season: Int, n: Int, place: Long?, played: Boolean = false) =
        Item(id = id, type = "episode", title = id, seriesId = "show", seasonAired = season, indexAired = n, watchOrder = place, userData = UserData(played = played))
    private fun entry(item: Item) = DownloadEntry(itemId = item.id, sourceId = "src", item = item, titleId = "show", seasonNumber = item.seasonAired, state = DownloadState.Done)

    // A show as the server lists it: two OVAs placed between its seasons, a short at the foot.
    private fun show(places: Boolean = true) = listOf(
        ep("short", 0, 2, 900L.takeIf { places }), ep("s2e1", 2, 1, 400L.takeIf { places }), ep("ova2", 0, 9, 320L.takeIf { places }),
        ep("s1e12", 1, 12, 212L.takeIf { places }), ep("ova1", 0, 8, 310L.takeIf { places }), ep("s1e11", 1, 11, 211L.takeIf { places }),
    )

    @Test fun episodesPlayInTheOrderTheServerPlacesThem() {
        assertEquals(listOf("s1e11", "s1e12", "ova1", "ova2", "s2e1", "short"), inPlayOrder(show()).map { it.id })
    }

    // An older server, or a download from before places were given: one episode without a
    // place and the seasons' numbers decide for all of them, specials last, as they did.
    @Test fun withoutAPlaceForEveryEpisodeTheSeasonsNumbersDecide() {
        assertEquals(listOf("s1e11", "s1e12", "s2e1", "short", "ova1", "ova2"), inPlayOrder(show(places = false)).map { it.id })
        val mixed = show().map { if (it.id == "s2e1") it.copy(watchOrder = null) else it }
        assertEquals(listOf("s1e11", "s1e12", "s2e1", "short", "ova1", "ova2"), inPlayOrder(mixed).map { it.id })
        assertEquals(emptyList<String>(), inPlayOrder(emptyList()).map { it.id })
    }

    // Away from the server the downloads are the list: season 1 watched, what the phone
    // offers to play is the OVA, not season 2.
    @Test fun downloadsOfferTheOvaAfterTheSeasonBeforeIt() {
        val watched = setOf("s1e11", "s1e12")
        val placed = show().map { entry(it.copy(userData = UserData(played = it.id in watched))) }
        assertEquals("ova1", upNextOf(placed)!!.entry.itemId)
        assertEquals(listOf("s1e11", "s1e12", "ova1", "ova2", "s2e1", "short"), inOrder(placed).map { it.itemId })
        val unplaced = show(places = false).map { entry(it.copy(userData = UserData(played = it.id in watched))) }
        assertEquals("s2e1", upNextOf(unplaced)!!.entry.itemId)
    }

    // A download keeps the item as it was when it was made. It learns its place when the
    // show's list is read from the server; nothing is rewritten when nothing differs.
    @Test fun downloadsLearnTheirPlacesFromTheShowsList() {
        val file = File(context.filesDir, "downloads.json")
        // (Not finished yet: a finished download without its file is dropped when the index is read.)
        val old = (show(places = false).filter { it.id in setOf("s1e12", "ova1") }.map { entry(it) } + entry(ep("s2e1", 2, 1, 400L))).map { it.copy(state = DownloadState.Paused) }
        file.writeText(json.encodeToString(DownloadIndex.serializer(), DownloadIndex(entries = old.associateBy { it.itemId })))
        val d = Downloads(context, Prefs(context, scope), scope)
        assertNull(d.entry("ova1")!!.item.watchOrder)

        d.learnPlaces(show())
        assertEquals(310L, d.entry("ova1")!!.item.watchOrder)
        assertEquals(212L, d.entry("s1e12")!!.item.watchOrder)
        assertEquals(400L, d.entry("s2e1")!!.item.watchOrder)
        assertNull("an episode that is not downloaded gets no entry", d.entry("ova2"))
        assertEquals(listOf("s1e12", "ova1", "s2e1"), inPlayOrder(d.index.value.entries.values.map { it.item }).map { it.id })

        val before = d.index.value
        d.learnPlaces(show())
        assertSame("the same places again change nothing", before, d.index.value)
        // A list from a server that gives no places takes none away.
        d.learnPlaces(show(places = false))
        assertSame(before, d.index.value)
        // The library changed: a place that moved is taken.
        d.learnPlaces(listOf(ep("ova1", 0, 8, 305L)))
        assertEquals(305L, d.entry("ova1")!!.item.watchOrder)
    }
}
