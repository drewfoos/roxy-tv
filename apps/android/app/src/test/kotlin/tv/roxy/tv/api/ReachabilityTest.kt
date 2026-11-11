package tv.roxy.tv.api

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
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import tv.roxy.tv.data.Prefs
import tv.roxy.tv.data.Session

/**
 * "The server is reachable" means Roxy answered, not that something did. With a network
 * (even with the internet) the saved address may be answered by something else: a Wi-Fi
 * sign-in page, another device on a foreign network, a proxy whose server is down.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = android.app.Application::class)
class ReachabilityTest {
    private val context = ApplicationProvider.getApplicationContext<android.app.Application>()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined)
    private var info = 200 to """{"id":"x","name":"Roxy on pookiebox","product":"roxy","version":"0.1.0-dev"}"""
    private val server = RecordingServer { r -> if (r.path == "/v1/system/info") info else 200 to "{}" }
    private val prefs = Prefs(context, scope)
    private val api = RoxyApi(prefs)

    @After fun stop() { server.close(); scope.cancel() }

    private fun probe(): Boolean {
        runBlocking { prefs.saveSession(Session(serverUrl = server.url, token = "t", profileId = "p", profileName = "P")) }
        return api.reachableNow()
    }

    @Test fun roxyAnsweringIsReachable() = assertTrue(probe())

    @Test fun somethingElseAnsweringTheAddressIsNot() {
        info = 200 to """<html><body>Sign in to this Wi-Fi</body></html>"""
        assertFalse("a page that is not Roxy's", probe())
        info = 200 to """{"product":"another"}"""
        assertFalse("another product", probe())
        info = 503 to """{"error":"down"}"""
        assertFalse("an error", probe())
    }

    @Test fun theProbeItselfDoesNotReportAnAnswer() {
        val told = ArrayList<Boolean>()
        api.onReachability = { told += it }
        info = 200 to "<html>not Roxy</html>"
        assertFalse(probe())
        assertEquals("its result decides, not the fact that something answered", emptyList<Boolean>(), told)
        runBlocking { runCatching { api.profiles() } }
        assertEquals("an ordinary call still reports", listOf(true), told)
    }

    @Test fun aProxysGatewayErrorIsNotRoxyAnswering() {
        assertTrue(answeredByRoxy(200, "application/json"))
        assertTrue("an image", answeredByRoxy(200, "image/webp"))
        assertTrue("Roxy's own errors are answers", answeredByRoxy(404, "application/json") && answeredByRoxy(500, "text/plain"))
        assertTrue("Roxy's own 503 (transcodes full) is JSON", answeredByRoxy(503, "application/json; charset=utf-8"))
        for (code in 502..504) {
            assertFalse("a proxy's page", answeredByRoxy(code, "text/html"))
            assertFalse(answeredByRoxy(code, null))
        }
    }
}
