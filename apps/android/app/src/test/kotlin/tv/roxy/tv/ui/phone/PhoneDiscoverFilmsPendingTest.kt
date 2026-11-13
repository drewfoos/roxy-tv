package tv.roxy.tv.ui.phone

import android.os.Looper
import androidx.compose.ui.test.hasTestTag
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.performClick
import androidx.test.core.app.ApplicationProvider
import com.sun.net.httpserver.HttpExchange
import com.sun.net.httpserver.HttpServer
import kotlinx.coroutines.runBlocking
import org.junit.After
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.data.Session
import java.net.InetSocketAddress
import java.util.Collections
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/**
 * The phone's Discover page of an anime series while its films are still being read (the
 * first look at a series takes the server about ten seconds). The Request button said
 * "Request" and then "Request + 1 film", and a press in between sent the series without
 * its films. Now it shows the loading circle and no words until the films are known, a
 * circle by "Seasons" says they are on their way, and a press meanwhile sends nothing.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], qualifiers = "w412dp-h915dp-port-xxhdpi")
class PhoneDiscoverFilmsPendingTest {
    @get:Rule val compose = createComposeRule()

    private val hits = Collections.synchronizedList(mutableListOf<String>())
    private lateinit var server: HttpServer
    private val app get() = ApplicationProvider.getApplicationContext<RoxyApp>()
    private var filmsGate = CountDownLatch(1)
    private var filmsStatus = 200

    private fun HttpExchange.json(body: String, status: Int = 200) {
        val b = body.toByteArray()
        responseHeaders.add("Content-Type", "application/json")
        sendResponseHeaders(status, b.size.toLong()); responseBody.use { it.write(b) }; close()
    }

    @Before fun start() {
        server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.executor = java.util.concurrent.Executors.newCachedThreadPool()
        server.createContext("/v1/system/info") { it.json("""{"product":"roxy"}""") }
        server.createContext("/v1/discover/tv/7707") {
            it.json("""{"tmdbId":7707,"type":"tv","title":"Bunny","isAnime":true,"requestsEnabled":true,
                "seasons":[{"number":1,"name":"Season 1","episodes":13},{"number":2,"name":"Season 2","episodes":13}]}""")
        }
        server.createContext("/v1/discover/tv/7707/films") {
            hits += "GET films"
            filmsGate.await(20, TimeUnit.SECONDS)
            if (filmsStatus != 200) it.json("""{"error":{"message":"AniList did not answer"}}""", filmsStatus)
            else it.json("""{"requestsEnabled":true,"films":[
                {"anilistId":1,"tmdbId":11,"title":"Between","year":2019,"afterSeason":1,"eligible":true,"withRequest":true}]}""")
        }
        server.createContext("/v1/discover/request") {
            hits += "POST " + it.requestBody.readBytes().decodeToString()
            it.json("""{"id":"r1","status":"sent"}""")
        }
        server.createContext("/") { it.sendResponseHeaders(404, -1); it.close() }
        server.start()
        runBlocking { app.prefs.saveSession(Session(serverUrl = "http://127.0.0.1:${server.address.port}", token = "t", profileId = "p")) }
    }

    @After fun stop() { filmsGate.countDown(); server.stop(0) }

    private fun waitFor(what: String, cond: () -> Boolean) {
        val until = System.currentTimeMillis() + 8_000
        while (true) {
            shadowOf(Looper.getMainLooper()).idle()
            compose.waitForIdle()
            if (runCatching(cond).getOrDefault(false)) return
            if (System.currentTimeMillis() > until) throw AssertionError("$what; server saw $hits")
            Thread.sleep(20)
        }
    }

    /** Each season's row has a "Request" of its own (two here); the page's main button is one more. */
    private fun requestButtons() = compose.onAllNodes(hasText("Request")).fetchSemanticsNodes().size
    private fun shown(text: String) = compose.onAllNodes(hasText(text)).fetchSemanticsNodes().isNotEmpty()
    // Unmerged: a button folds what is inside it into one node.
    private fun tagged(tag: String) = compose.onAllNodes(hasTestTag(tag), useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty()

    private fun open() {
        compose.setContent { PhoneDiscoverDetail("tv", 7707, onBack = {}, onOpenSeries = { _, _ -> }, onOpenMovie = { _, _ -> }) }
        waitFor("the page is up and the films are asked for") { shown("Seasons") && hits.contains("GET films") }
    }

    @Test fun theRequestButtonWaitsForTheFilms() {
        open()
        assertTrue("the circle in the button", tagged("button-loading"))
        assertTrue("the circle by Seasons", tagged("films-pending") && shown("Looking for films"))
        assertTrue("no Request words that would change", requestButtons() == 2 && !shown("Request + 1 film"))
        compose.onNode(hasTestTag("button-loading"), useUnmergedTree = true).performClick()
        compose.waitForIdle()
        assertTrue("nothing was requested: $hits", hits.none { it.startsWith("POST") })

        filmsGate.countDown()
        waitFor("the button says what it sends") { shown("Request + 1 film") }
        assertTrue("the circles are gone", !tagged("button-loading") && !tagged("films-pending"))
        assertTrue("the film is listed", shown("Between"))
    }

    @Test fun aLookThatFailsIsAnAnswer() {
        filmsStatus = 502
        open()
        assertTrue(tagged("button-loading"))
        filmsGate.countDown()
        waitFor("the button is usable without films") { requestButtons() == 3 }
        assertTrue(!tagged("button-loading") && !tagged("films-pending"))
    }
}
