package tv.roxy.tv.ui.tv

import android.os.Looper
import androidx.compose.ui.test.getUnclippedBoundsInRoot
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.test.core.app.ApplicationProvider
import com.sun.net.httpserver.HttpExchange
import com.sun.net.httpserver.HttpServer
import kotlinx.coroutines.runBlocking
import org.junit.After
import org.junit.Assert.assertEquals
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

/**
 * The TV's Discover page of an anime series with films: each film sits where the server
 * put it among the seasons and can be pressed. A film that may be requested is requested
 * by itself (a film request, never the series' with films); one the library has opens
 * there; one already requested opens its own page.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], qualifiers = "w960dp-h540dp-xhdpi")
class TvDiscoverFilmsPageTest {
    @get:Rule val compose = createComposeRule()

    private val hits = Collections.synchronizedList(mutableListOf<String>())
    private lateinit var server: HttpServer
    private val app get() = ApplicationProvider.getApplicationContext<RoxyApp>()
    private val opened = mutableListOf<String>()

    private fun HttpExchange.json(body: String) {
        val b = body.toByteArray()
        responseHeaders.add("Content-Type", "application/json")
        sendResponseHeaders(200, b.size.toLong()); responseBody.use { it.write(b) }; close()
    }

    @Before fun start() {
        server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.createContext("/v1/system/info") { it.json("""{"product":"roxy"}""") }
        server.createContext("/v1/discover/tv/7707") {
            it.json("""{"tmdbId":7707,"type":"tv","title":"Bunny","isAnime":true,"requestsEnabled":true,
                "seasons":[{"number":1,"name":"Season 1","episodes":13},{"number":2,"name":"Season 2","episodes":13}]}""")
        }
        server.createContext("/v1/discover/tv/7707/films") {
            hits += "GET films"
            it.json("""{"requestsEnabled":true,"films":[
                {"anilistId":1,"tmdbId":11,"title":"Between","year":2019,"afterSeason":1,"eligible":true,"withRequest":true},
                {"anilistId":2,"tmdbId":12,"title":"Held","afterSeason":1,"eligible":true,"libraryItemId":"lib-12"},
                {"anilistId":3,"tmdbId":13,"title":"Aside","afterSeason":-1,"note":"Side story"},
                {"anilistId":4,"tmdbId":14,"title":"Asked","afterSeason":-1,"note":"Side story","requestStatus":"sent"}]}""")
        }
        server.createContext("/v1/discover/request") {
            hits += "POST " + it.requestBody.readBytes().decodeToString()
            it.json("""{"id":"r1","status":"sent"}""")
        }
        server.createContext("/") { it.sendResponseHeaders(404, -1); it.close() }
        server.start()
        runBlocking { app.prefs.saveSession(Session(serverUrl = "http://127.0.0.1:${server.address.port}", token = "t", profileId = "p")) }
    }

    @After fun stop() = server.stop(0)

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

    private fun shown(text: String) = compose.onAllNodes(androidx.compose.ui.test.hasText(text)).fetchSemanticsNodes().isNotEmpty()
    private fun left(text: String) = compose.onNodeWithText(text).getUnclippedBoundsInRoot().left

    private fun open() {
        compose.setContent {
            TvDiscoverTitle("tv", 7707, onOpenSeries = {}, onOpenMovie = { opened += "library ${it.id}" }, onOpenDiscover = { t, id -> opened += "page $t $id" })
        }
        waitFor("the films are listed") { shown("Between") }
    }

    @Test fun aFilmSitsAfterItsSeasonAndTheRestAtTheEnd() {
        open()
        assertTrue(shown("SEASONS AND FILMS"))
        assertTrue("after Season 1", left("Between") > left("S1"))
        assertTrue("before Season 2", left("Between") < left("S2"))
        assertTrue("held film too", left("Held") < left("S2"))
        assertTrue("the others after the last season", left("MORE FILMS") > left("S2"))
    }

    @Test fun okOnAFilmRequestsThatFilmAlone() {
        open()
        compose.onNodeWithText("S2").assertExists()
        waitFor("the side story is on the page") { shown("Aside") }
        val looks = hits.count { it == "GET films" }
        compose.onNodeWithText("Aside").performClick()
        waitFor("the request was sent") { hits.any { it.startsWith("POST") } }
        val body = hits.single { it.startsWith("POST") }
        assertTrue(body, body.contains("\"type\":\"movie\"") && body.contains("\"tmdbId\":13"))
        assertTrue("no films ride along: $body", !body.contains("\"films\":[1") && !body.contains("11"))
        waitFor("the films are read again for the new state") { hits.count { it == "GET films" } > looks }
    }

    @Test fun okOpensAFilmTheLibraryHasAndTheOwnPageOfOneAlreadyRequested() {
        open()
        compose.onNodeWithText("Held").performClick()
        waitFor("the side stories are on the page") { shown("Asked") }
        compose.onNodeWithText("Asked").performClick()
        compose.waitForIdle()
        assertEquals(listOf("library lib-12", "page movie 14"), opened)
        assertTrue("nothing was requested: $hits", hits.none { it.startsWith("POST") })
    }
}
