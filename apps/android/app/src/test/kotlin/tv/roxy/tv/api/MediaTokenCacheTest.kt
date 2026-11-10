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
import java.util.concurrent.atomic.AtomicInteger

/**
 * The media tokens the player puts into stream URLs: which one the app's own client hands
 * out, against a server on this machine that makes a new one for every request and names
 * the sign-in it was asked by.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = android.app.Application::class)
class MediaTokenCacheTest {
    private val context = ApplicationProvider.getApplicationContext<android.app.Application>()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined)
    private val made = AtomicInteger()
    private val server = RecordingServer { r ->
        val by = r.header("Authorization").orEmpty().removePrefix("Bearer ")
        200 to """{"token":"media-${made.incrementAndGet()}-of-$by","expiresAt":"2099-01-01T00:00:00Z"}"""
    }
    private val prefs = Prefs(context, scope)
    private val api = RoxyApi(prefs)

    @After fun stop() { server.close(); scope.cancel() }

    private fun signIn(account: String?) = runBlocking { prefs.saveSession(Session(serverUrl = server.url, token = account)) }
    private fun asked() = server.seen.map { it.path }

    // The finding: the cache was keyed by source alone and lived as long as the process. A
    // media token names the sign-in's device on the server and ends with it, so after signing
    // out and in again, replaying the same thing within a day sent the dead token.
    @Test fun aTokenOfAnEarlierSignInIsNotHandedOut() {
        signIn("first")
        assertEquals("media-1-of-first", runBlocking { api.mediaToken("s1") })
        assertEquals("reused while the sign-in is the same", "media-1-of-first", runBlocking { api.mediaToken("s1") })
        assertEquals("media-1-of-first", api.cachedMediaToken("s1"))
        assertEquals(listOf("/v1/sources/s1/token"), asked())

        signIn(null) // signed out ...
        assertNull(api.cachedMediaToken("s1"))
        signIn("second") // ... and in again
        assertNull("nothing of the old sign-in", api.cachedMediaToken("s1"))
        assertEquals("asked for again, as the new sign-in", "media-2-of-second", runBlocking { api.mediaToken("s1") })
        assertEquals("media-2-of-second", api.cachedMediaToken("s1"))
    }

    // For the player, when the server refuses a stream: the token of that source is dropped
    // and the next one is asked for; other sources keep theirs.
    @Test fun aForgottenTokenIsAskedForAgain() {
        signIn("first")
        runBlocking { api.mediaToken("s1"); api.mediaToken("s2") }
        api.forgetMediaToken("s1")
        assertNull(api.cachedMediaToken("s1"))
        assertEquals("media-2-of-first", api.cachedMediaToken("s2"))
        assertEquals("media-3-of-first", runBlocking { api.mediaToken("s1") })
        assertEquals("media-2-of-first", runBlocking { api.mediaToken("s2") })
        assertEquals(listOf("/v1/sources/s1/token", "/v1/sources/s2/token", "/v1/sources/s1/token"), asked())
    }
}
