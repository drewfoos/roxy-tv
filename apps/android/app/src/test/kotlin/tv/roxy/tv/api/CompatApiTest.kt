package tv.roxy.tv.api

import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.runBlocking
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import tv.roxy.tv.data.Compat
import tv.roxy.tv.data.Compat.Verdict
import tv.roxy.tv.data.Prefs
import tv.roxy.tv.data.ServerStatus
import tv.roxy.tv.data.Session

/**
 * The app learns whether it and its server can work together from the server itself: the
 * look it takes before its first call (`/v1/system/info`), and a coded 426 to any call
 * once the server has been updated past this app. The requests are made by the app's own
 * client and status keeper against a server on this machine.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = android.app.Application::class)
class CompatApiTest {
    private val context = ApplicationProvider.getApplicationContext<android.app.Application>()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined)

    /** What the stand-in server states about itself; null = a server from before the rule. */
    @Volatile private var levels: Pair<Int, Int>? = Compat.CLIENT_LEVEL to Compat.CLIENT_LEVEL
    /** The server refuses below this level whatever its info says: a call's refusal is then the app's only word of it. */
    @Volatile private var refuseBelow: Int? = null
    /** A plain 426 from something in between, not Roxy's refusal. */
    @Volatile private var proxy426 = false

    private val server = RecordingServer { r ->
        val l = levels
        val api = l?.let { ""","api":{"level":${it.first},"minClient":${it.second}}""" }.orEmpty()
        val sent = r.header(Compat.HEADER)?.toIntOrNull()
        when {
            r.path == "/v1/system/info" -> 200 to """{"product":"roxy","id":"srv","name":"Home","version":"9"$api}"""
            proxy426 -> 426 to """{"error":"upgrade the connection"}"""
            // The real server's rule (compat.go): below its minimum, refused, except on the way to an update.
            sent != null && sent < (refuseBelow ?: l?.second ?: 0) && !r.path.startsWith("/v1/apps/") ->
                426 to """{"error":"This app is too old for this server. Update the app.","code":"client_too_old"$api}"""
            r.path == "/v1/apps/tv/latest" -> 200 to """{"versionCode":999999,"versionName":"9.9.9","url":"/v1/apps/tv/download"}"""
            else -> 200 to "{}"
        }
    }
    private val prefs = Prefs(context, scope)
    private val api = RoxyApi(prefs)
    private val status = ServerStatus(context, api, prefs, scope)

    @After fun stop() { server.close(); scope.cancel() }

    private fun signIn() = runBlocking { prefs.saveSession(Session(serverUrl = server.url, token = "account-token", serverId = "srv")) }
    private fun codeOf(call: suspend () -> Unit): Int? = runBlocking { runCatching { call() }.exceptionOrNull() }.let { (it as? ApiException)?.code }

    @Test fun everyCallSaysWhichAppThisIs() {
        signIn()
        runBlocking { api.history() }
        val call = server.seen.last { it.path == "/v1/me/history" }
        assertEquals(Compat.CLIENT_LEVEL.toString(), call.header(Compat.HEADER))
    }

    @Test fun aMatchingServerLeavesTheAppAlone() {
        signIn()
        assertTrue(api.reachableNow())
        assertEquals(Verdict.Ok, status.compat.value)
        assertEquals(null, codeOf { api.history() })
        assertEquals(Verdict.Ok, status.compat.value)
    }

    @Test fun aServerFromBeforeTheRuleLeavesTheAppAlone() {
        levels = null
        signIn()
        assertTrue(api.reachableNow())
        assertEquals(Verdict.Ok, status.compat.value)
    }

    @Test fun theFirstLookFindsAServerThatLeftThisAppBehind() {
        levels = Compat.CLIENT_LEVEL + 2 to Compat.CLIENT_LEVEL + 1
        signIn()
        assertTrue("the server is there", api.reachableNow())
        assertEquals(Verdict.AppTooOld, status.compat.value)
    }

    // The server was updated while the app was open: no look is due, the next call is refused.
    // Its info here still states the old numbers, so only the refusal itself can tell the app.
    @Test fun aRefusedCallSaysTheAppIsTooOld() {
        signIn()
        assertTrue(api.reachableNow())
        refuseBelow = Compat.CLIENT_LEVEL + 1
        assertEquals(426, codeOf { api.history() })
        assertEquals(Verdict.AppTooOld, status.compat.value)
        // Its own update is still to be had.
        assertEquals(999999, runBlocking { api.latestAppBuild("tv.roxy.tv") }.versionCode)
    }

    @Test fun someoneElses426SaysNothing() {
        signIn()
        proxy426 = true
        assertEquals(426, codeOf { api.history() })
        assertEquals(Verdict.Ok, status.compat.value)
    }

    @Test fun theNextLookThatMatchesLiftsIt() {
        levels = Compat.CLIENT_LEVEL + 2 to Compat.CLIENT_LEVEL + 1
        signIn()
        api.reachableNow()
        assertEquals(Verdict.AppTooOld, status.compat.value)
        levels = Compat.CLIENT_LEVEL + 2 to Compat.CLIENT_LEVEL // whoever runs it put the old way back
        api.reachableNow()
        assertEquals(Verdict.Ok, status.compat.value)
    }

    @Test fun signingOutForgetsIt() {
        levels = Compat.CLIENT_LEVEL + 2 to Compat.CLIENT_LEVEL + 1
        signIn()
        api.reachableNow()
        assertEquals(Verdict.AppTooOld, status.compat.value)
        runBlocking { prefs.clearSession() }
        assertEquals("the next server is another question", Verdict.Ok, status.compat.value)
    }
}
