package tv.roxy.tv.ui.tv

import android.os.Looper
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.test.core.app.ApplicationProvider
import com.sun.net.httpserver.HttpServer
import kotlinx.coroutines.runBlocking
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.Item
import tv.roxy.tv.data.Session
import java.net.InetSocketAddress
import java.util.Collections

/**
 * Found on the TV (Overgeared, November 2026): "Mark Season 1 as watched" reached the server
 * but the season list stayed unwatched and Home's hero kept "Continue: E6". The menu closes
 * as it acts, and the request ran on the menu's own scope: closing it cancelled the reload
 * that follows the answer. A server that takes a moment to answer, as a real one does.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class TvMenuMarkTest {
    @get:Rule val compose = createComposeRule()

    private val hits = Collections.synchronizedList(mutableListOf<String>())
    private lateinit var server: HttpServer
    private val app get() = ApplicationProvider.getApplicationContext<RoxyApp>()

    @Before fun start() {
        server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.createContext("/") { ex ->
            hits += "${ex.requestMethod} ${ex.requestURI.path}"
            Thread.sleep(300) // answers after the menu has closed
            ex.sendResponseHeaders(204, -1); ex.close()
        }
        server.start()
        runBlocking { app.prefs.saveSession(Session(serverUrl = "http://127.0.0.1:${server.address.port}", token = "t", profileId = "p")) }
    }

    @After fun stop() = server.stop(0)

    private fun waitFor(what: String, cond: () -> Boolean) {
        val until = System.currentTimeMillis() + 5_000
        while (!cond()) {
            shadowOf(Looper.getMainLooper()).idle()
            if (System.currentTimeMillis() > until) throw AssertionError(what)
            Thread.sleep(20)
        }
    }

    @Test fun markingASeasonReloadsTheListsAfterTheMenuCloses() {
        val season = Item(id = "season1", type = "season", title = "Season 1")
        val episode = Item(id = "ep6", type = "episode", title = "First Boss Raid")
        var shown by mutableStateOf(true)
        compose.setContent { if (shown) TvItemMenu(item = episode, onDismiss = { shown = false }, season = season) }
        val before = app.items.changes.value
        compose.onNodeWithText("Mark Season 1 as watched").performClick()
        compose.waitForIdle()
        assertFalse("the menu closed", shown)
        waitFor("the server got the mark") { hits.isNotEmpty() }
        assertEquals("POST /v1/me/played/season1", hits.single())
        waitFor("the lists reload once the server answered (changes still $before)") { app.items.changes.value != before }
        assertTrue(app.items.changes.value > before)
    }
}
