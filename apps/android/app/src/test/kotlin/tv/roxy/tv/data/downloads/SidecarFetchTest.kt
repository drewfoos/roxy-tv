package tv.roxy.tv.data.downloads

import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.runBlocking
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeFalse
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import tv.roxy.tv.api.Item
import tv.roxy.tv.api.MediaStream
import tv.roxy.tv.api.RecordingServer
import tv.roxy.tv.api.RoxyApi
import tv.roxy.tv.data.Prefs
import tv.roxy.tv.data.Session
import java.io.File

/**
 * The subtitle files of a download and what is left of a deleted one, with the app's own
 * store and client against a server on this machine.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = android.app.Application::class)
class SidecarFetchTest {
    private val context = ApplicationProvider.getApplicationContext<android.app.Application>()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined)
    /** What the server says to a request for a subtitle, by its stream number. */
    @Volatile private var answer: (Int) -> Pair<Int, String> = { 200 to "1\n00:00:01,000 --> 00:00:02,000\nline $it\n" }
    private val server = RecordingServer { answer(it.path.substringAfterLast('/').toInt()) }
    private val prefs = Prefs(context, scope)
    private val downloads = Downloads(context, prefs, scope)
    private val api = RoxyApi(prefs)

    init {
        runBlocking {
            prefs.saveSession(Session(serverUrl = server.url, token = "account-token", profileId = "pA"))
            prefs.setDownloadsWifiOnly(false)
        }
    }

    @After fun stop() { server.close(); scope.cancel() }

    private val show = Item(id = "show", type = "series", title = "Show")

    private fun episode(id: String) = Item(id = id, type = "episode", title = id, seriesId = "show", mediaSources = listOf(Item.Source(
        id = "src-$id", container = "mkv", size = 5000, isPrimary = true,
        streams = listOf(
            MediaStream(index = 2, type = "subtitle", codec = "ass", language = "eng"),
            MediaStream(index = 1000, type = "subtitle", codec = "subrip", language = "eng", isExternal = true),
            MediaStream(index = 1001, type = "subtitle", codec = "ass", language = "jpn", isExternal = true),
        ),
    )))

    private fun queue(vararg ids: String) = downloads.enqueue(ids.map { episode(it) }, show)
    private fun asked(): List<String> = server.seen.map { it.path }
    private fun files(id: String): Set<String> = downloads.dir(id).list().orEmpty().toSet()

    // Found in review: a subtitle file the server did not give (an error, a restart) was
    // only written to the debug log. The download ended Done without it and nothing ever
    // asked again: that episode had no subtitles offline, for good.
    @Test fun aSubtitleTheServerDidNotGiveIsNotedAndFetchedLater() {
        queue("e1")
        answer = { if (it == 1000) 500 to "" else 200 to "the ass file" }

        assertTrue("the server answered", runBlocking { downloads.fetchSidecars(api, "e1") })

        assertEquals(listOf("/v1/sources/src-e1/subtitles/1000", "/v1/sources/src-e1/subtitles/1001"), asked())
        assertEquals("the one that came is saved", setOf("video.jpn.1001.ass"), files("e1"))
        assertEquals("the other is noted", listOf(1000), downloads.entry("e1")!!.missingSidecars)

        // The video finishes; the server is well again; the app signs in or the queue runs dry.
        downloads.update("e1") { it.copy(state = DownloadState.Done) }
        answer = { 200 to "the srt file" }
        runBlocking { downloads.retryMissingSidecars(api) }

        assertEquals("only the missing one is asked for", "/v1/sources/src-e1/subtitles/1000", asked().last())
        assertEquals(3, asked().size)
        assertEquals("the srt file", File(downloads.dir("e1"), "video.eng.1000.srt").readText())
        assertEquals(emptyList<Int>(), downloads.entry("e1")!!.missingSidecars)

        runBlocking { downloads.retryMissingSidecars(api) }
        assertEquals("nothing is missing: nothing is asked", 3, asked().size)
    }

    @Test fun aSubtitleThatIsGoneIsNotAskedForAgain() {
        queue("e1")
        answer = { if (it == 1000) 404 to "" else 200 to "the ass file" }

        runBlocking { downloads.fetchSidecars(api, "e1") }
        assertEquals(emptyList<Int>(), downloads.entry("e1")!!.missingSidecars)
        runBlocking { downloads.retryMissingSidecars(api) }

        assertEquals(2, asked().size)
        assertEquals(setOf("video.jpn.1001.ass"), files("e1"))
    }

    @Test fun withTheServerAwayAllAreNotedAndTheRestIsNotTried() {
        queue("e1", "e2")
        server.close()

        assertFalse("no answer", runBlocking { downloads.fetchSidecars(api, "e1") })
        assertEquals(listOf(1000, 1001), downloads.entry("e1")!!.missingSidecars)
        assertEquals(emptySet<String>(), files("e1"))
    }

    @Test fun onAMeteredNetworkWifiOnlyKeepsTheRetryBack() {
        assumeFalse("this test needs a metered network", downloads.isUnmetered())
        queue("e1")
        downloads.update("e1") { it.copy(state = DownloadState.Done, missingSidecars = listOf(1000)) }
        runBlocking { prefs.setDownloadsWifiOnly(true) }

        runBlocking { downloads.retryMissingSidecars(api) }
        assertEquals("nothing asked", emptyList<String>(), asked())

        runBlocking { prefs.setDownloadsWifiOnly(false) }
        runBlocking { downloads.retryMissingSidecars(api) }
        assertEquals(2, asked().size)
    }

    // Found in review: a download deleted while its images and subtitles were being fetched.
    // The delete clears the folders at once; the fetch then wrote into them again (and went
    // on to the video), and nothing ever removed those files: space taken by a download the
    // app no longer lists.
    @Test fun whatAFetchWroteAfterTheDeleteIsRemoved() {
        queue("e1", "e2")
        val e1 = downloads.entry("e1")!!
        val e2 = downloads.entry("e2")!!
        fun write(id: String, name: String) = File(downloads.dir(id), name).apply { parentFile!!.mkdirs(); writeBytes(ByteArray(64)) }
        write("e2", "video.mkv.part")
        write("show", "poster.img")

        downloads.delete("e1")
        // The service, still on e1: a subtitle, the first bytes of the video.
        write("e1", "video.eng.1000.srt")
        write("e1", "video.mkv.part")
        downloads.removeLeftovers(e1)

        assertFalse("e1's folder is gone", downloads.dir("e1").exists())
        assertEquals("e2 is still listed: untouched", setOf("video.mkv.part"), files("e2"))
        assertEquals("so is its show", setOf("poster.img"), files("show"))

        // The last download of the show goes while its poster is being fetched.
        downloads.delete("e2")
        write("e2", "thumb.img")
        write("show", "poster.img")
        downloads.removeLeftovers(e2)

        assertFalse(downloads.dir("e2").exists())
        assertFalse("the show's images go with its last download", downloads.dir("show").exists())
    }

    @Test fun aDownloadQueuedAgainKeepsItsFiles() {
        queue("e1")
        val e1 = downloads.entry("e1")!!
        downloads.delete("e1")
        queue("e1")
        File(downloads.dir("e1"), "video.mkv.part").apply { parentFile!!.mkdirs(); writeBytes(ByteArray(64)) }

        downloads.removeLeftovers(e1)

        assertEquals(setOf("video.mkv.part"), files("e1"))
    }

    // The same, where it happens: the entry is deleted while its subtitle is on the way.
    @Test fun aDownloadDeletedWhileItsSubtitlesCameLeavesNothing() {
        queue("e1")
        answer = { downloads.delete("e1"); 200 to "a subtitle" }

        runBlocking { downloads.fetchSidecars(api, "e1") }

        assertFalse("no folder for a download that is not listed", downloads.dir("e1").exists())
        assertFalse(downloads.dir("show").exists())
    }
}
