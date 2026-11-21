package tv.roxy.tv.ui.profiles

import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.runBlocking
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.data.Session
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger

/**
 * "Who's watching?" on a link that is there but slow: the server says "I am here" and then
 * takes its time over the profile list. The page drew two empty circles until that answer
 * came (up to the connect or read timeout), with nothing to pick, though it had the list
 * from its last visit. It must show that list at once, let one be picked, and take the
 * server's list when it does come.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], qualifiers = "w412dp-h915dp-port-xxhdpi")
class ProfilePickerSlowLinkTest {
    @get:Rule val compose = createComposeRule()

    private val app get() = ApplicationProvider.getApplicationContext<RoxyApp>()
    private val listAsked = AtomicInteger()
    private val answerNow = CountDownLatch(1)
    @Volatile private var status = 200
    @Volatile private var list = """{"profiles":[{"id":"p1","name":"Drew"},{"id":"p2","name":"Coffee"},{"id":"p3","name":"Guest"}]}"""

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
                        fun send(body: String, code: Int = 200) {
                            it.getOutputStream().write("HTTP/1.1 $code X\r\nContent-Type: application/json\r\nContent-Length: ${body.toByteArray().size}\r\nConnection: close\r\n\r\n$body".toByteArray())
                            it.getOutputStream().flush()
                        }
                        when (path) {
                            "/v1/system/info" -> send("""{"product":"roxy","id":"srv-1"}""")
                            "/v1/me/profiles" -> { listAsked.incrementAndGet(); answerNow.await(20, TimeUnit.SECONDS); send(list, status) }
                            else -> send("{}")
                        }
                    }
                }
            }
        }
    }

    @After fun stop() { answerNow.countDown(); socket.close() }

    private fun shown(text: String) = compose.onAllNodesWithText(text).fetchSemanticsNodes().isNotEmpty()

    private fun open() {
        runBlocking {
            app.prefs.saveSession(Session(serverUrl = url, token = "t", profileId = null, lastProfileId = "p1"))
            app.prefs.cacheProfiles("""[{"id":"p1","name":"Drew"},{"id":"p2","name":"Coffee"}]""")
        }
        compose.setContent { ProfilePicker() }
    }

    @Test fun theSavedListShowsAndPicksWhileTheServerIsStillAnswering() {
        open()
        compose.waitUntil(5_000) { listAsked.get() == 1 }
        compose.waitUntil(2_000) { shown("Coffee") }
        assertEquals("the server has not answered yet", 1L, answerNow.count)

        compose.onNodeWithText("Coffee").performClick()
        compose.waitUntil(2_000) { app.prefs.session.value.profileId == "p2" }
        assertEquals("Coffee", app.prefs.session.value.profileName)
    }

    @Test fun theServersListReplacesTheSavedOne() {
        open()
        compose.waitUntil(2_000) { shown("Coffee") }
        assertEquals(0, compose.onAllNodesWithText("Guest").fetchSemanticsNodes().size)
        answerNow.countDown()
        compose.waitUntil(5_000) { shown("Guest") }
    }

    @Test fun aRefusalFromTheServerIsNotHiddenBehindTheSavedList() {
        status = 500; list = """{"error":"boom"}"""
        open()
        compose.waitUntil(2_000) { shown("Coffee") }
        answerNow.countDown()
        compose.waitUntil(5_000) { shown("Try again") }
        assertEquals(0, compose.onAllNodesWithText("Coffee").fetchSemanticsNodes().size)
        assertNull(app.prefs.session.value.profileId)
    }
}
