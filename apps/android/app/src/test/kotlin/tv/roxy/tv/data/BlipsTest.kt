package tv.roxy.tv.data

import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeoutOrNull
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import tv.roxy.tv.api.RecordingServer
import tv.roxy.tv.api.RoxyApi

/**
 * A call that fails although the server is there (the phone changed network or address
 * under it, a relayed link dropped it) left its page on "Can't reach Roxy" until someone
 * pressed Retry: the server never counted as away, so it never "came back". Pages are now
 * told to try again, once per failure and never in a loop.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = android.app.Application::class)
class BlipsTest {
    private var clock = 0L
    private val blips = Blips(now = { clock })

    @Test fun aFailedCallWithTheServerThereIsRetriedOnce() {
        assertFalse("nothing failed", blips.retry())
        blips.failed()
        assertTrue(blips.retry())
        assertFalse("one failure, one retry", blips.retry())
    }

    @Test fun aCallThatKeepsFailingIsNotAskedForInALoop() {
        blips.failed(); assertTrue(blips.retry())
        clock += Blips.GAP_MS * 10
        blips.failed()
        assertFalse("the retry failed too and nothing was answered since", blips.retry())
        blips.answered()
        blips.failed()
        assertTrue("answers again, and a new failure", blips.retry())
    }

    @Test fun retriesKeepTheirDistance() {
        blips.failed(); assertTrue(blips.retry())
        blips.answered(); blips.failed()
        clock += Blips.GAP_MS - 1
        assertFalse(blips.retry())
        blips.failed()
        clock += 1
        assertTrue(blips.retry())
    }

    @Test fun aServerThatWentAwayAndCameBackNeedsNoSecondReload() {
        blips.failed()
        blips.forget()
        assertFalse(blips.retry())
    }

    // The whole path: the app's own client reports a failed call, the look finds the
    // server, and the pages' counter moves without the server ever counting as away.
    private val context = ApplicationProvider.getApplicationContext<android.app.Application>()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined)
    private val server = RecordingServer { 200 to """{"product":"roxy","id":"srv-1"}""" }
    @After fun stop() { server.close(); scope.cancel() }

    @Test fun aPageIsToldToTryAgainWhenItsCallFailedButTheServerIsThere() {
        val prefs = Prefs(context, scope)
        runBlocking { prefs.saveSession(Session(serverUrl = server.url, token = "t")) }
        val api = RoxyApi(prefs)
        val status = ServerStatus(context, api, prefs, scope)
        // The first look of a test run pays for the client's start-up: not what is measured.
        assertTrue(runBlocking { api.reachable() })
        assertEquals(0, status.reloads.value)
        api.onReachability!!.invoke(false)
        val n = runBlocking { withTimeoutOrNull(20_000) { status.reloads.first { it > 0 } } }
        assertEquals("pages retry", 1, n)
        assertTrue("the server never counted as away", status.online.value)
        assertEquals("so nothing says \"back online\"", 0, status.backOnline.value)
    }
}
