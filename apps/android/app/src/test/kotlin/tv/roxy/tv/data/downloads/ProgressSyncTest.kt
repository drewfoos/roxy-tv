package tv.roxy.tv.data.downloads

import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.long
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import tv.roxy.tv.api.RecordingServer
import tv.roxy.tv.api.RoxyApi
import tv.roxy.tv.data.Prefs
import tv.roxy.tv.data.Session

/**
 * What the queued watch progress really sends to the server: the requests are made by the
 * app's own client against a server on this machine that keeps them.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = android.app.Application::class)
class ProgressSyncTest {
    private val context = ApplicationProvider.getApplicationContext<android.app.Application>()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined)
    private val server = RecordingServer()
    private val prefs = Prefs(context, scope)

    @After fun stop() { server.close(); scope.cancel() }

    private fun signIn(profile: String?) = runBlocking { prefs.saveSession(Session(serverUrl = server.url, token = "account-token", profileId = profile)) }
    private fun ageOf(r: RecordingServer.Seen): Long = Json.parseToJsonElement(r.body).jsonObject.getValue("ageMs").jsonPrimitive.long

    // Found in review: the age of a queued report was the difference of two readings of the
    // date. A date set between the two (by hand, by the network, a phone that starts in 1970)
    // made a report from a minute ago days old, so the server threw it away as out of date, or
    // "from the future" (age 0), so it overwrote what another device did since. Within one boot
    // the age is counted by the time since boot, which nothing can set.
    @Test fun theAgeOfAReportDoesNotFollowTheDate() {
        var wall = 1_700_000_000_000L
        var sinceBoot = 500_000L
        val d = Downloads(context, prefs, scope, clock = DeviceClock({ wall }, { sinceBoot }, { 7 }))
        signIn("pA")
        d.recordProgress("e1", 60_000, reportedToServer = false)
        wall += 3 * 24 * 3_600_000L // the date jumps three days ahead ...
        sinceBoot += 90_000 // ... while a minute and a half goes by
        assertNull(runBlocking { d.syncProgress(RoxyApi(prefs)) })
        assertEquals("a minute and a half old", 90_000L, ageOf(server.seen.single()))

        // And the other way: the date set back to before the report was made.
        d.recordProgress("e2", 30_000, reportedToServer = false)
        wall -= 10 * 24 * 3_600_000L
        sinceBoot += 20_000
        assertNull(runBlocking { d.syncProgress(RoxyApi(prefs)) })
        assertEquals("twenty seconds old, not 'just now'", 20_000L, ageOf(server.seen.last()))
    }

    // After a restart the time since boot starts again: only the dates are left, as before.
    // The same for an entry queued by an older build, which has no such reading.
    @Test fun afterARestartTheAgeIsByTheDates() {
        val old = PendingProgress("pA", "e1", 1, at = 1_000_000)
        assertEquals("an older build's entry", 5_000L, pendingAgeMs(old, wallNow = 1_005_000, elapsedNow = 77, bootCount = 3))
        val p = old.copy(elapsedAt = 900_000, bootCount = 3)
        assertEquals("same boot: by the time since boot", 40_000L, pendingAgeMs(p, wallNow = 9_999_999_999, elapsedNow = 940_000, bootCount = 3))
        assertEquals("another boot: by the dates", 5_000L, pendingAgeMs(p, wallNow = 1_005_000, elapsedNow = 940_000, bootCount = 4))
        assertEquals("the device does not number its boots: by the dates", 5_000L, pendingAgeMs(p.copy(bootCount = null), wallNow = 1_005_000, elapsedNow = 940_000, bootCount = null))
        assertEquals("a date set back: never a negative age", 0L, pendingAgeMs(old, wallNow = 500, elapsedNow = 0, bootCount = null))
    }
}
