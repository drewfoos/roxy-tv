package tv.roxy.tv.ui.phone

import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.unit.dp
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.runBlocking
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.data.Session
import tv.roxy.tv.ui.theme.RoxyTheme
import java.util.concurrent.atomic.AtomicInteger

/**
 * The Browse page itself, through a failed request and its recovery: the real app, its
 * real client and a server on this machine that cuts the library's answer short once (what a
 * change of network does to a call in flight) while still answering "I am here".
 *
 * The page showed "Can't reach Roxy" and stayed there until Retry was pressed. It must
 * fill in by itself, with nobody pressing anything. This is the test that notices if the
 * page's loading or error states change in a way the retry signal (ServerStatus.reloads)
 * no longer reaches.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], qualifiers = "w412dp-h915dp-port-xxhdpi")
class BrowseRecoveryTest {
    @get:Rule val compose = createComposeRule()

    private val app get() = ApplicationProvider.getApplicationContext<RoxyApp>()
    private val libraryCalls = AtomicInteger()

    // A server that can cut an answer short, which the JDK's own cannot: 200, the promise
    // of 5000 bytes, a few of them, and the connection closes.
    private val socket = java.net.ServerSocket(0, 50, java.net.InetAddress.getByName("127.0.0.1"))
    private val url = "http://127.0.0.1:${socket.localPort}"
    private val serving = kotlin.concurrent.thread(isDaemon = true) {
        while (!socket.isClosed) {
            val c = runCatching { socket.accept() }.getOrNull() ?: break
            kotlin.concurrent.thread(isDaemon = true) {
                runCatching {
                    c.use {
                        val reader = it.getInputStream().bufferedReader()
                        val path = reader.readLine().orEmpty().split(" ").getOrElse(1) { "" }.substringBefore('?')
                        while (!reader.readLine().isNullOrEmpty()) { /* headers */ }
                        fun send(body: String, promised: Int = body.toByteArray().size) {
                            it.getOutputStream().write("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: $promised\r\nConnection: close\r\n\r\n$body".toByteArray())
                            it.getOutputStream().flush()
                        }
                        when {
                            path == "/v1/system/info" -> send("""{"product":"roxy","id":"srv-1"}""")
                            path == "/v1/items" && libraryCalls.incrementAndGet() == 1 -> send("""{"items":[""", promised = 5000)
                            path == "/v1/items" -> send("""{"items":[{"id":"s1","type":"series","title":"Bocchi the Rock!","year":2022}],"total":1}""")
                            else -> send("{}")
                        }
                    }
                }
            }
        }
    }

    @After fun stop() = socket.close()

    @Test fun browseFillsInByItselfAfterItsRequestFailedWithTheServerThere() {
        runBlocking { app.prefs.saveSession(Session(serverUrl = url, token = "t", profileId = "p1")) }
        val returnsBefore = app.server.backOnline.value
        val reloadsBefore = app.server.reloads.value
        compose.setContent { RoxyTheme { PhoneBrowse(onOpenSeries = {}, onOpenMovie = {}, onSearch = {}, bottomInset = 0.dp) } }

        // The request fails and the page says so...
        compose.waitUntil(15_000) { compose.onAllNodesWithText("Can't reach Roxy right now.").fetchSemanticsNodes().isNotEmpty() }
        assertEquals("the library was asked for once, and that failed", 1, libraryCalls.get())
        // ...and then shows the library, without Retry.
        compose.waitUntil(15_000) { compose.onAllNodesWithText("Bocchi the Rock!").fetchSemanticsNodes().isNotEmpty() }

        assertEquals("the error is gone", 0, compose.onAllNodesWithText("Can't reach Roxy right now.").fetchSemanticsNodes().size)
        assertEquals("one retry, not a loop", 2, libraryCalls.get())
        assertEquals("pages were told once", reloadsBefore + 1, app.server.reloads.value)
        assertTrue("the server never counted as away", app.server.online.value)
        assertEquals(returnsBefore, app.server.backOnline.value)
    }
}
