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
import tv.roxy.tv.data.PlayerSettings
import tv.roxy.tv.data.Prefs
import tv.roxy.tv.data.Session
import tv.roxy.tv.ui.shownCast

/**
 * An anime's voice casts: the app tells the server which language this viewer listens in
 * (so the server can name the dub's cast first) and reads the casts as sent. A server that
 * sends none (an older one, or a title that is not anime) leaves the TMDB cast.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = android.app.Application::class)
class CastApiTest {
    private val context = ApplicationProvider.getApplicationContext<android.app.Application>()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined)
    private val server = RecordingServer { r ->
        when (r.path) {
            "/v1/system/info" -> 200 to """{"product":"roxy","id":"srv-1"}"""
            "/v1/items/anime/details", "/v1/discover/tv/500/details" -> 200 to """{"item":{"id":"anime","type":"series","title":"Show"},
                "tmdb":{"cast":[{"name":"From TMDB","character":"Sakuta"}]},
                "casts":[
                  {"language":"jpn","name":"Japanese","original":true,"members":[{"name":"Kaito Ishikawa","character":"Sakuta","tmdbId":7}]},
                  {"language":"eng","name":"English","members":[{"name":"Stephen Fu","character":"Sakuta"}]}],
                "castLanguage":"eng"}"""
            "/v1/items/plain/details" -> 200 to """{"item":{"id":"plain","type":"movie","title":"Film"},"tmdb":{"cast":[{"name":"From TMDB","character":"Lead"}]}}"""
            else -> 404 to """{"error":"not found"}"""
        }
    }
    private val prefs = Prefs(context, scope)
    private val api = RoxyApi(prefs)

    init {
        runBlocking { prefs.saveSession(Session(serverUrl = server.url, token = "account-token")) }
    }

    @After fun stop() { server.close(); scope.cancel() }

    private fun asked(path: String) = server.seen.last { it.path == path }.query

    @Test fun theCastsAreReadAsTheServerSendsThem() {
        val d = runBlocking { api.itemDetails("anime") }
        assertEquals(listOf("jpn", "eng"), d.casts.map { it.language })
        assertTrue(d.casts[0].original)
        assertEquals(7, d.casts[0].members.single().tmdbId)
        assertEquals("eng", d.castLanguage)
        assertEquals("Stephen Fu", shownCast(d).members.single().name)
    }

    @Test fun aTitleWithoutCastsKeepsTheTmdbCast() {
        val d = runBlocking { api.itemDetails("plain") }
        assertTrue(d.casts.isEmpty())
        assertEquals("From TMDB", shownCast(d).members.single().name)
    }

    @Test fun theViewersLanguageGoesWithBothCalls() {
        runBlocking { api.itemDetails("anime"); api.discoverMoreDetails("tv", 500) }
        assertEquals("the app's default is English", "audio=eng", asked("/v1/items/anime/details"))
        assertEquals("audio=eng", asked("/v1/discover/tv/500/details"))

        runBlocking { prefs.saveSettings(PlayerSettings(audioLangs = listOf("jpn", "eng"))); api.itemDetails("anime"); api.discoverMoreDetails("tv", 500) }
        assertEquals("audio=jpn", asked("/v1/items/anime/details"))
        assertEquals("audio=jpn", asked("/v1/discover/tv/500/details"))
    }
}
