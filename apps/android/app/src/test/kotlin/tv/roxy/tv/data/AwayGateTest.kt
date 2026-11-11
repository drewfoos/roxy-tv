package tv.roxy.tv.data

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.runBlocking
import androidx.test.core.app.ApplicationProvider
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
import tv.roxy.tv.api.ServerAwayException

/**
 * Away from home on a network that swallows the packets, every page waited out the 10 s
 * connect timeout before saying "Can't reach Roxy". Calls now fail at once while the
 * server is known to be away, and one fresh look lets them through again.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = android.app.Application::class)
class AwayGateTest {
    private var clock = 0L
    private var probes = 0
    private var serverThere = false
    private val gate = AwayGate(now = { clock }, probe = { probes++; serverThere })

    @Test fun nothingIsRefusedWhileTheServerAnswers() {
        assertFalse(gate.failNow(away = false))
        assertEquals("and nobody looks", 0, probes)
    }

    @Test fun awayRefusesAtOnceOnAFreshLook() {
        gate.looked(false)
        clock += 2_000
        assertTrue(gate.failNow(away = true))
        assertTrue(gate.failNow(away = true))
        assertEquals("no new look for either call", 0, probes)
    }

    @Test fun anOldLookIsRepeatedOnceAndSharedByTheCallsAfterIt() {
        gate.looked(false)
        clock += AwayGate.FRESH_MS
        assertTrue(gate.failNow(away = true))
        assertTrue(gate.failNow(away = true))
        assertEquals(1, probes)
    }

    @Test fun aServerThatCameBackIsNoticedByTheNextCall() {
        gate.looked(false)
        clock += AwayGate.FRESH_MS
        serverThere = true
        assertFalse("the call goes out", gate.failNow(away = true))
    }

    // The same through the app's own client: the refused call never leaves the phone, and
    // the probe itself is not refused (it would never see the server come back).
    private val context = ApplicationProvider.getApplicationContext<android.app.Application>()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined)
    private val server = RecordingServer()
    private val prefs = Prefs(context, scope)
    private val api = RoxyApi(prefs)

    @After fun stop() { server.close(); scope.cancel() }

    @Test fun aRefusedCallIsNotSentAndTheProbeStillIs() {
        runBlocking { prefs.saveSession(Session(serverUrl = server.url, token = "t", profileId = "pA")) }
        api.awayGate = { true }

        val failure = runBlocking { runCatching { api.item("e1") }.exceptionOrNull() }

        assertTrue("fails as a connection failure: $failure", failure is ServerAwayException)
        assertEquals("reads like one", "Can't reach Roxy right now.", friendlyError(failure!!))
        assertEquals(emptyList<String>(), server.seen.map { it.path })
        assertTrue("the probe goes out", runBlocking { api.reachable() })
        assertEquals(listOf("/v1/system/info"), server.seen.map { it.path })
    }
}
