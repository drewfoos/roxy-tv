package tv.roxy.tv.data.downloads

import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.runBlocking
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import tv.roxy.tv.api.Item
import tv.roxy.tv.api.UserData
import tv.roxy.tv.data.Prefs
import tv.roxy.tv.data.Session

/** What the phone offers from its downloads alone: the Play button's episode and Continue Watching. */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = android.app.Application::class)
class OfflineShelfTest {
    private val context = ApplicationProvider.getApplicationContext<android.app.Application>()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined)

    @After fun stop() { scope.cancel() }

    private fun ep(show: String, season: Int, n: Int, pos: Long = 0, played: Boolean = false, at: String? = null, state: DownloadState = DownloadState.Done) = DownloadEntry(
        itemId = "$show-s${season}e$n", sourceId = "src", titleId = show, seasonNumber = season, state = state,
        item = Item(id = "$show-s${season}e$n", type = "episode", title = "Episode $n", seriesId = show, seasonAired = season, indexAired = n,
            userData = UserData(positionMs = pos, played = played, lastPlayedAt = at)),
    )
    private fun film(id: String, pos: Long = 0, played: Boolean = false, at: String? = null) = DownloadEntry(
        itemId = id, sourceId = "src", titleId = id, state = DownloadState.Done,
        item = Item(id = id, type = "movie", title = id, userData = UserData(positionMs = pos, played = played, lastPlayedAt = at)),
    )
    private fun title(id: String, vararg e: DownloadEntry) = DownloadedTitle(id, Item(id = id, type = if (e.first().item.isMovie) "movie" else "series", title = id), e.toList())

    @Test fun nothingStartedPlaysTheFirstEpisode() {
        val next = upNextOf(listOf(ep("a", 1, 2), ep("a", 1, 1)))!!
        assertEquals("a-s1e1", next.entry.itemId)
        assertEquals("Play S1 E1", next.label)
    }

    @Test fun anEpisodeStoppedPartWayIsResumed() {
        val next = upNextOf(listOf(ep("a", 1, 1, played = true), ep("a", 1, 2, pos = 60_000), ep("a", 1, 3)))!!
        assertEquals("a-s1e2", next.entry.itemId)
        assertEquals("Resume S1 E2", next.label)
    }

    @Test fun ofTwoPartWayTheOnePlayedLastIsResumed() {
        val next = upNextOf(listOf(ep("a", 1, 1, pos = 5_000, at = "2026-11-09T10:00:00Z"), ep("a", 1, 4, pos = 9_000, at = "2026-11-10T10:00:00Z")))!!
        assertEquals("a-s1e4", next.entry.itemId)
    }

    @Test fun afterAWatchedEpisodeComesTheNextUnwatchedOne() {
        // Episode 1 was skipped; 2 and 3 are watched: carry on at 4, not back at 1.
        val next = upNextOf(listOf(ep("a", 1, 1), ep("a", 1, 2, played = true), ep("a", 1, 3, played = true), ep("a", 1, 4), ep("a", 2, 1)))!!
        assertEquals("a-s1e4", next.entry.itemId)
        assertEquals("Play S1 E4", next.label)
    }

    @Test fun withTheLastOneWatchedItGoesBackToWhatWasSkipped() {
        val next = upNextOf(listOf(ep("a", 1, 1), ep("a", 1, 2, played = true)))!!
        assertEquals("a-s1e1", next.entry.itemId)
    }

    @Test fun everythingWatchedOffersTheFirstAgain() {
        val next = upNextOf(listOf(ep("a", 1, 1, played = true), ep("a", 1, 2, played = true)))!!
        assertEquals("a-s1e1", next.entry.itemId)
        assertEquals("Watch again", next.label)
    }

    @Test fun specialsComeAfterTheSeasons() {
        val next = upNextOf(listOf(ep("a", 0, 1), ep("a", 1, 1)))!!
        assertEquals("a-s1e1", next.entry.itemId)
    }

    @Test fun aDownloadStillComingInIsNotOffered() {
        assertEquals("a-s1e2", upNextOf(listOf(ep("a", 1, 1, state = DownloadState.Downloading), ep("a", 1, 2)))!!.entry.itemId)
        assertNull(upNextOf(listOf(ep("a", 1, 1, state = DownloadState.Queued))))
    }

    @Test fun aFilmSaysPlayOrResume() {
        assertEquals("Play", upNextOf(listOf(film("m")))!!.label)
        assertEquals("Resume", upNextOf(listOf(film("m", pos = 1_000)))!!.label)
    }

    @Test fun continueWatchingIsTheStartedTitlesLatestFirst() {
        val row = continueFromDownloads(listOf(
            title("untouched", ep("untouched", 1, 1)),
            title("older", ep("older", 1, 1, played = true, at = "2026-11-01T10:00:00Z"), ep("older", 1, 2)),
            title("finished", ep("finished", 1, 1, played = true, at = "2026-11-10T12:00:00Z")),
            title("film", film("film", pos = 30_000, at = "2026-11-10T09:00:00Z")),
            title("newer", ep("newer", 1, 1, pos = 60_000, at = "2026-11-09T10:00:00+00:00")),
        ))
        assertEquals("not started and finished titles are left out; the next episode of each, the latest played first",
            listOf("film", "newer-s1e1", "older-s1e2"), row.map { it.itemId })
    }

    // Offline, Continue Watching can only order by what the phone wrote down itself: a play
    // with the server away has to leave its time on the download, or the row keeps the
    // order of the last time the server was seen.
    @Test fun offlinePlaybackLeavesItsTimeOnTheDownload() {
        val prefs = Prefs(context, scope)
        runBlocking { prefs.saveSession(Session(serverUrl = "http://127.0.0.1:9", token = "t", profileId = "pA")) }
        val wall = 1_800_000_000_000L
        val d = Downloads(context, prefs, scope, clock = DeviceClock({ wall }, { 500_000L }, { 7 }))
        d.enqueue(listOf(Item(id = "e1", type = "episode", title = "e1", seriesId = "show", mediaSources = listOf(Item.Source(id = "s", container = "mkv", size = 10, isPrimary = true)))), Item(id = "show", type = "series", title = "Show"))
        assertNull(d.entry("e1")!!.item.userData?.lastPlayedAt)

        d.recordProgress("e1", 60_000, reportedToServer = false, ageMs = 5_000)

        assertEquals(wall - 5_000, playedAtMs(d.entry("e1")!!.item))
    }
}
