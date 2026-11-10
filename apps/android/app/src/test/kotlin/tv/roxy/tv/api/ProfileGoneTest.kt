package tv.roxy.tv.api

import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.runBlocking
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import tv.roxy.tv.data.Prefs
import tv.roxy.tv.data.Session

/**
 * A profile deleted on another device. The server refuses requests that name it (409 with
 * the X-Roxy-Profile-Gone header) instead of quietly serving the login's default profile,
 * which had this device showing the deleted profile's name over another's watch history.
 * The app forgets the profile, which shows "Who's watching?". The requests are made by the
 * app's own client against a server on this machine.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = android.app.Application::class)
class ProfileGoneTest {
    private val context = ApplicationProvider.getApplicationContext<android.app.Application>()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined)
    private val gone = { r: RecordingServer.Seen -> r.header("X-Roxy-Profile") == "pGone" && r.path != "/v1/me/profiles" }
    private val server = RecordingServer(answerHeaders = { if (gone(it)) mapOf("X-Roxy-Profile-Gone" to "1") else emptyMap() }) { r ->
        when {
            gone(r) -> 409 to """{"error":"That profile no longer exists. Choose a profile again.","code":"profile_gone"}"""
            r.path == "/v1/me/played/taken" -> 409 to """{"error":"already there"}"""
            r.path == "/v1/me/profiles" -> 200 to """{"profiles":[{"id":"pB","name":"B"}],"current":"pB"}"""
            else -> 200 to "{}"
        }
    }
    private val prefs = Prefs(context, scope)
    private val api = RoxyApi(prefs)

    @After fun stop() { server.close(); scope.cancel() }

    private fun signIn(profile: String?, last: String? = profile) =
        runBlocking { prefs.saveSession(Session(serverUrl = server.url, token = "account-token", profileId = profile, profileName = "Name", lastProfileId = last)) }
    private fun codeOf(call: suspend () -> Unit): Int? = runBlocking { runCatching { call() }.exceptionOrNull() }.let { (it as? ApiException)?.code }

    @Test fun theRefusedProfileIsForgotten() {
        signIn("pGone")
        assertEquals(409, codeOf { api.progress("e1", 1_000) })
        val s = prefs.session.value
        assertNull("back to the picker", s.profileId)
        assertEquals("", s.profileName)
        assertNull("and it is not offered as the one used last", s.lastProfileId)
        assertEquals("still signed in", "account-token", s.token)
        // The picker's own request names no profile now, and is answered.
        assertEquals(listOf("pB"), runBlocking { api.profiles().profiles.map { it.id } })
        assertNull(server.seen.last().header("X-Roxy-Profile"))
    }

    // A queued report names the profile that made it, which may not be the active one: its
    // refusal says nothing about the profile in use.
    @Test fun aRefusalOfAnotherProfilesReportLeavesTheActiveOneAlone() {
        signIn("pB")
        assertEquals(409, codeOf { api.progress("e1", 1_000, profileId = "pGone") })
        assertEquals("pB", prefs.session.value.profileId)
    }

    @Test fun anotherConflictKeepsTheProfile() {
        signIn("pB")
        assertEquals(409, codeOf { api.setPlayed("taken", true) })
        assertEquals("pB", prefs.session.value.profileId)
    }

    @Test fun onlyAMarked409NamingAProfileIsARefusal() {
        assertEquals("pA", profileRefused(409, "1", "pA"))
        assertNull("another conflict", profileRefused(409, null, "pA"))
        assertNull("another status", profileRefused(404, "1", "pA"))
        assertNull("no profile was named", profileRefused(409, "1", null))
        assertNull(profileRefused(409, "1", ""))
    }
}
