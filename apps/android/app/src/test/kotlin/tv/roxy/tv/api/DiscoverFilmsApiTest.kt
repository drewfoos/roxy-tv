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
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import tv.roxy.tv.data.Prefs
import tv.roxy.tv.data.Session
import tv.roxy.tv.ui.buttonFilms

/**
 * An anime series' films: read as the server tells them, and sent with a request only
 * when the caller names them. The requests are made by the app's own client against a
 * server on this machine.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = android.app.Application::class)
class DiscoverFilmsApiTest {
    private val context = ApplicationProvider.getApplicationContext<android.app.Application>()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined)
    private val server = RecordingServer { r ->
        when (r.path) {
            "/v1/system/info" -> 200 to """{"product":"roxy","id":"srv-1"}"""
            "/v1/discover/tv/500/films" -> 200 to """{"films":[
                {"anilistId":1,"tmdbId":11,"title":"Between","year":2019,"afterSeason":1,"eligible":true,"withRequest":true},
                {"anilistId":2,"tmdbId":12,"title":"Aside","afterSeason":-1,"eligible":false,"note":"Side story","withRequest":false},
                {"anilistId":3,"title":"Unknown","afterSeason":-1,"eligible":false,"note":"Not found on TMDB","withRequest":false}
              ],"auto":true,"requestsEnabled":true}"""
            "/v1/discover/request" -> 200 to """{"id":"r1","status":"sent"}"""
            else -> 404 to """{"error":"not found"}"""
        }
    }
    private val prefs = Prefs(context, scope)
    private val api = RoxyApi(prefs)

    init {
        runBlocking { prefs.saveSession(Session(serverUrl = server.url, token = "account-token")) }
    }

    @After fun stop() { server.close(); scope.cancel() }

    private fun requests() = server.seen.filter { it.path == "/v1/discover/request" }

    @Test fun theFilmsAreReadAsTheServerTellsThem() {
        val f = runBlocking { api.discoverFilms(500) }
        assertTrue(f.auto)
        assertTrue(f.requestsEnabled)
        assertEquals(listOf("Between", "Aside", "Unknown"), f.films.map { it.title })
        assertEquals(1, f.films[0].afterSeason)
        assertTrue(f.films[0].withRequest)
        assertEquals("Side story", f.films[1].note)
        assertNull("a film TMDB could not be matched to has no id to request", f.films[2].tmdbId)
        assertEquals(listOf(11), buttonFilms(f))
    }

    @Test fun theMainRequestCarriesTheMarkedFilms() {
        runBlocking { api.discoverRequest("tv", 500, films = buttonFilms(api.discoverFilms(500))) }
        assertTrue(requests().single().body, requests().single().body.contains("\"films\":[11]"))
    }

    @Test fun aSeasonsOrEpisodesRequestNamesNoFilm() {
        runBlocking {
            api.discoverRequest("tv", 500, listOf(2))
            api.discoverRequest("tv", 500, episodes = listOf("S0E1"))
            api.discoverRequest("movie", 12)
        }
        assertEquals(3, requests().size)
        requests().forEach { assertFalse(it.body, it.body.contains("films")) }
    }
}
