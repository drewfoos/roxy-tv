package tv.roxy.tv.data

import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.runBlocking
import okhttp3.Request
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import tv.roxy.tv.api.RecordingServer
import tv.roxy.tv.api.RoxyApi
import tv.roxy.tv.api.firstBest
import tv.roxy.tv.api.isThisServer

/**
 * The app kept one address for its server: the home one. Away from home with Tailscale on
 * it still asked the home address and said "Can't reach your server"; signed in with the
 * Tailscale address instead, everything went through Tailscale at home too. The server now
 * lists its addresses and the app uses the best one that answers.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = android.app.Application::class)
class ServerAddressesTest {
    private val home = "http://192.168.1.50:8097"
    private val tail = "http://100.64.0.7:8097"

    @Test fun theHomeAddressComesBeforeTheTailscaleOneWhicheverWasSignedInTo() {
        assertEquals(listOf(home, tail), Session(serverUrl = home, addresses = listOf(home, tail)).knownAddresses)
        assertEquals(listOf(home, tail), Session(serverUrl = tail, addresses = listOf(tail, home)).knownAddresses)
        assertEquals(listOf(home, "http://box.example.ts.net:8097"), Session(serverUrl = "http://box.example.ts.net:8097/", addresses = listOf("$home/")).knownAddresses)
    }

    @Test fun aServerWithOneAddressKeepsIt() {
        assertEquals(listOf(home), Session(serverUrl = home).knownAddresses)
        assertEquals(emptyList<String>(), Session().knownAddresses)
    }

    // The login must not cross the internet in the clear because a server listed an address.
    @Test fun aListedPlainHttpAddressOutsideHomeAndTailnetIsNotUsed() {
        val s = Session(serverUrl = home, addresses = listOf(home, "http://media.example.com:8097", "https://media.example.com", "ftp://192.168.1.9"))
        assertEquals(listOf(home, "https://media.example.com"), s.knownAddresses)
        assertEquals("an address someone typed stays", listOf("http://media.example.com:8097"), Session(serverUrl = "http://media.example.com:8097").knownAddresses)
    }

    // ---- which answer wins ----

    private fun pick(n: Int, vararg answers: Triple<Long, Int, Boolean>): Pair<Int, Long> {
        var clock = 0L
        val left = answers.toMutableList()
        val best = firstBest(n, betterWaitMs = 700, totalMs = 3_500, now = { clock }) { wait ->
            val a = left.firstOrNull()
            if (a == null || a.first > clock + wait) { clock += wait; null } else { left.removeAt(0); clock = maxOf(clock, a.first); a.second to a.third }
        }
        return best to clock
    }

    @Test fun atHomeTheHomeAddressWinsAtOnce() {
        assertEquals(0 to 5L, pick(2, Triple(5L, 0, true), Triple(40L, 1, true)))
    }

    @Test fun awayTheTailscaleAddressIsUsedAsSoonAsHomeIsKnownToBeGone() {
        assertEquals(1 to 80L, pick(2, Triple(10L, 0, false), Triple(80L, 1, true)))
    }

    @Test fun aSilentHomeAddressIsWaitedForBrieflyNotForItsTimeout() {
        assertEquals(1 to 780L, pick(2, Triple(80L, 1, true)))
    }

    @Test fun homeAnsweringALittleLaterStillWins() {
        assertEquals(0 to 300L, pick(2, Triple(80L, 1, true), Triple(300L, 0, true)))
    }

    @Test fun nothingAnsweringIsAway() {
        assertEquals(-1, pick(2, Triple(10L, 0, false), Triple(20L, 1, false)).first)
        assertEquals(-1 to 3_500L, pick(2))
    }

    @Test fun anotherAddressMustBeTheSameServer() {
        assertTrue(isThisServer("""{"product":"roxy","id":"srv1"}""", "srv1", other = true))
        assertFalse("another Roxy", isThisServer("""{"product":"roxy","id":"srv2"}""", "srv1", other = true))
        assertFalse("no id known: stay", isThisServer("""{"product":"roxy","id":""}""", "", other = true))
        assertFalse("not Roxy", isThisServer("""{"id":"srv1"}""", "srv1", other = true))
        assertTrue("the address signed in to only has to be Roxy", isThisServer("""{"product":"roxy"}""", "srv1", other = false))
    }

    // ---- through the app's own client ----

    private val context = ApplicationProvider.getApplicationContext<android.app.Application>()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined)
    private fun roxy(id: String) = RecordingServer { if (it.path == "/v1/system/info") 200 to """{"product":"roxy","id":"$id"}""" else 200 to "{}" }
    // Both stand-ins are on the loopback (the same kind of address), so the order is the server's list: a first.
    private val a = roxy("srv1")
    private val b = roxy("srv1")
    private val prefs = Prefs(context, scope)
    private val api = RoxyApi(prefs)

    @After fun stop() { a.close(); b.close(); scope.cancel() }

    private fun signIn(at: String, addresses: List<String>, serverId: String = "srv1") =
        runBlocking { prefs.saveSession(Session(serverUrl = at, token = "t", profileId = "pA", addresses = addresses, serverId = serverId)) }

    private fun paths(s: RecordingServer) = s.seen.map { it.path }.filter { it != "/v1/system/info" }

    @Test fun theAppMovesToABetterAddressThatAnswers() {
        signIn(b.url, listOf(a.url, b.url))

        assertTrue(runBlocking { api.reachable() })

        assertEquals(a.url, prefs.session.value.serverUrl)
        assertTrue("the address left is still known", b.url in prefs.session.value.knownAddresses)
        runBlocking { runCatching { api.item("e1") } }
        assertEquals(listOf("/v1/items/e1"), paths(a))
        assertEquals("Bearer t", a.seen.last().header("Authorization"))
        assertEquals(emptyList<String>(), paths(b))
    }

    @Test fun awayFromHomeTheAppMovesToTheAddressThatStillAnswers() {
        signIn(a.url, listOf(a.url, b.url))
        a.close()

        assertTrue(runBlocking { api.reachable() })

        assertEquals(b.url, prefs.session.value.serverUrl)
    }

    @Test fun theProbeCarriesNoLogin() {
        signIn(b.url, listOf(a.url, b.url))
        runBlocking { api.reachable() }
        assertTrue(a.seen.isNotEmpty() && b.seen.isNotEmpty())
        (a.seen + b.seen).forEach { assertNull(it.header("Authorization")); assertNull(it.header("X-Roxy-Profile")) }
    }

    // The same private address on another network is somebody else's machine.
    @Test fun anAddressAnsweredByAnotherServerIsNotUsed() {
        val stranger = roxy("srv2")
        try {
            signIn(b.url, listOf(stranger.url, b.url))
            assertTrue(runBlocking { api.reachable() })
            assertEquals(b.url, prefs.session.value.serverUrl)
            assertFalse("and with no id known nothing moves", run {
                signIn(b.url, listOf(a.url, b.url), serverId = "")
                runBlocking { api.reachable() }
                prefs.session.value.serverUrl == a.url
            })
        } finally { stranger.close() }
    }

    // A page is loading as the phone leaves the home Wi-Fi: its calls were written for the
    // home address. The look made for the first of them finds the other address, and the
    // call itself goes there, as do image addresses made before the move.
    @Test fun aCallWrittenForTheOldAddressGoesToTheNewOne() {
        signIn(a.url, listOf(a.url, b.url))
        val made = Request.Builder().url(a.url + "/v1/items/e9/image?w=300").get().build()
        a.close()
        api.awayGate = { !api.reachableNow() }

        runBlocking { runCatching { api.item("e1") } }
        api.http.newCall(made).execute().close()

        assertEquals(listOf("/v1/items/e1", "/v1/items/e9/image"), paths(b))
        assertEquals("w=300", b.seen.last().query)
        assertEquals(listOf("Bearer t", "Bearer t"), b.seen.filter { it.path != "/v1/system/info" }.map { it.header("Authorization") })
    }

    @Test fun whatTheServerListsIsKeptAndSigningOutForgetsIt() {
        signIn(home, emptyList(), serverId = "")
        assertTrue("new addresses: look for the best", runBlocking { prefs.setAddresses(listOf("$home/", tail), "srv1") })
        assertEquals(listOf(home, tail), prefs.session.value.addresses)
        assertFalse("the same again changes nothing", runBlocking { prefs.setAddresses(listOf(home, tail), "srv1") })
        assertFalse("an older server says nothing: what is known stays", runBlocking { prefs.setAddresses(emptyList(), "") })
        assertEquals(listOf(home, tail), prefs.session.value.addresses)
        assertEquals("kept across a restart", listOf(home, tail), Prefs(context, scope).session.value.addresses)

        runBlocking { prefs.clearSession() }
        assertEquals(emptyList<String>(), prefs.session.value.addresses)
        assertEquals("", prefs.session.value.serverId)
    }

    @Test fun theAddressMovedToIsTheOneAfterARestart() {
        signIn(b.url, listOf(a.url, b.url))
        prefs.useAddress(a.url)
        assertEquals(a.url, prefs.session.value.serverUrl)
        val until = System.currentTimeMillis() + 3_000
        while (Prefs(context, scope).session.value.serverUrl != a.url && System.currentTimeMillis() < until) Thread.sleep(20)
        assertEquals(a.url, Prefs(context, scope).session.value.serverUrl)

        prefs.useAddress("http://203.0.113.9:8097")
        assertEquals("an address the server never listed is not moved to", a.url, prefs.session.value.serverUrl)
    }
}
