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
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import tv.roxy.tv.data.Prefs
import tv.roxy.tv.data.Session
import tv.roxy.tv.ui.phone.addressCheckWords
import tv.roxy.tv.ui.phone.withAddress

/**
 * Account → Server addresses (admin): the server's other addresses are read and saved as
 * one list, and a refused save is told in the server's own words. The requests are made
 * by the app's own client against a server on this machine.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = android.app.Application::class)
class AddressSettingsTest {
    private val context = ApplicationProvider.getApplicationContext<android.app.Application>()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined)
    private val server = RecordingServer { r ->
        when {
            // The app looks for the server before its first call (see AwayGate).
            r.path == "/v1/system/info" -> 200 to """{"product":"roxy","id":"srv-1"}"""
            r.path != "/v1/settings/addresses" -> 404 to """{"error":"not found"}"""
            r.method == "GET" -> 200 to """{"advertised":"http://192.168.1.50:8097","addresses":["http://100.64.0.7:8097"],"fromEnvironment":false}"""
            r.body.contains("100.64.0.9") -> 400 to """{"error":"http://100.64.0.9:8097 is a different Roxy server, not this one. Nothing was saved."}"""
            else -> 200 to """{"advertised":"http://192.168.1.50:8097","addresses":["http://100.64.0.7:8097","https://roxy.example.com"],"fromEnvironment":false,
                "checks":[{"address":"http://100.64.0.7:8097","state":"ok"},{"address":"https://roxy.example.com","state":"unreachable"}]}"""
        }
    }
    private val prefs = Prefs(context, scope)
    private val api = RoxyApi(prefs)

    init {
        runBlocking { prefs.saveSession(Session(serverUrl = server.url, token = "account-token")) }
    }

    @After fun stop() { server.close(); scope.cancel() }

    private fun settingsCalls() = server.seen.filter { it.path == "/v1/settings/addresses" }

    @Test fun theSavedListIsRead() {
        val s = runBlocking { api.addressSettings() }
        assertEquals("http://192.168.1.50:8097", s.advertised)
        assertEquals(listOf("http://100.64.0.7:8097"), s.addresses)
        assertEquals(false, s.fromEnvironment)
        assertEquals(emptyList<AddressCheck>(), s.checks)
        assertEquals("GET", settingsCalls().single().method)
        assertEquals("Bearer account-token", settingsCalls().single().header("Authorization"))
    }

    @Test fun aSaveSendsTheWholeListAndBringsBackWhatEachCheckFound() {
        val s = runBlocking { api.saveAddressSettings(listOf("http://100.64.0.7:8097", "https://roxy.example.com")) }
        val sent = settingsCalls().single()
        assertEquals("PUT", sent.method)
        assertEquals("""{"addresses":["http://100.64.0.7:8097","https://roxy.example.com"]}""", sent.body)
        assertEquals(listOf("http://100.64.0.7:8097", "https://roxy.example.com"), s.addresses)
        assertEquals(listOf(AddressCheck("http://100.64.0.7:8097", "ok"), AddressCheck("https://roxy.example.com", "unreachable")), s.checks)
    }

    // An empty list is how the last address is removed: it must be sent as a list, not left out.
    @Test fun removingTheLastAddressSendsAnEmptyList() {
        runBlocking { api.saveAddressSettings(emptyList()) }
        assertEquals("""{"addresses":[]}""", settingsCalls().single().body)
    }

    @Test fun aRefusedSaveSaysWhyInTheServersWords() {
        val e = runBlocking { runCatching { api.saveAddressSettings(listOf("http://100.64.0.9:8097")) }.exceptionOrNull() }
        assertEquals(400, (e as ApiException).code)
        assertEquals("http://100.64.0.9:8097 is a different Roxy server, not this one. Nothing was saved.", e.message)
    }

    @Test fun addingAnAddress() {
        val saved = listOf("http://100.64.0.7:8097")
        assertEquals(listOf("http://100.64.0.7:8097", "https://roxy.example.com"), withAddress(saved, "  https://roxy.example.com "))
        assertEquals(listOf("https://roxy.example.com"), withAddress(emptyList(), "https://roxy.example.com"))
        assertNull("nothing typed", withAddress(saved, "   "))
        assertNull("already there", withAddress(saved, "http://100.64.0.7:8097"))
        assertNull("already there, written another way", withAddress(saved, " HTTP://100.64.0.7:8097/ "))
    }

    @Test fun whatACheckSays() {
        assertEquals(true to "http://100.64.0.7:8097 reaches this server.", addressCheckWords(AddressCheck("http://100.64.0.7:8097", "ok")))
        val (good, quiet) = addressCheckWords(AddressCheck("https://roxy.example.com", "unreachable"))
        assertEquals(false, good)
        assertTrue(quiet, quiet.startsWith("https://roxy.example.com is saved") && quiet.contains("nothing answered there"))
        assertTrue(addressCheckWords(AddressCheck("https://roxy.example.com", "notRoxy")).second.contains("something else answered there"))
        // A state a newer server might add is never called good news.
        assertEquals(false, addressCheckWords(AddressCheck("https://roxy.example.com", "later")).first)
    }
}
