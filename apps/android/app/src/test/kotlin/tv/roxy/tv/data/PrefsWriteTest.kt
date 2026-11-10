package tv.roxy.tv.data

import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * The session writers called the way the screens call them: from a scope that the change
 * itself ends (the profile picker goes away when a profile is chosen, Account when signing
 * out). What is on disk afterwards is read by a second [Prefs], as the next start would.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = android.app.Application::class)
class PrefsWriteTest {
    private val context = ApplicationProvider.getApplicationContext<android.app.Application>()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined)
    private val prefs = Prefs(context, scope)
    private val signedIn = Session(
        serverUrl = "http://server.test", token = "account-token", userName = "drew", isAdmin = true,
        profileAvatarVersion = 3, profileAvatarKey = "k1", acceptedTerms = "1", setupInProgress = true,
    )

    @After fun stop() { scope.cancel() }

    private fun onDisk(): Session = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined).let { s -> Prefs(context, s).session.value.also { s.cancel() } }

    /** Runs [write] in a scope that is already ended when the writer is called, and waits for it. */
    private fun fromAnEndedScope(write: suspend () -> Unit) {
        val screen = CoroutineScope(Job() + Dispatchers.Unconfined)
        val done = screen.launch { screen.cancel(); write() }
        runBlocking { done.join() }
        assertTrue(done.isCancelled)
    }

    // The finding: the writer changed memory, then the cancelled caller's write to disk was
    // dropped. The picker closed on the chosen profile, the file still had none (the next
    // start asked again), and the next write of anything brought "no profile" back into memory.
    @Test fun aChosenProfileIsWrittenThoughItsScreenIsGone() {
        runBlocking { prefs.saveSession(signedIn) }
        fromAnEndedScope { prefs.selectProfile("p1", "Drew", hasAvatar = false) }
        assertEquals("on disk", "p1", onDisk().profileId)
        runBlocking { prefs.setDownloadsWifiOnly(false) } // any later write: memory follows the file
        assertEquals("still chosen", "p1", prefs.session.value.profileId)

        fromAnEndedScope { prefs.clearProfile() } // Switch profile
        assertNull(onDisk().profileId)
        assertEquals("the way back is kept", "p1", onDisk().lastProfileId)
    }

    // The same for signing out: a dropped write left the login in the file, so the next start
    // (or the next write of anything) was signed in again.
    @Test fun aSignOutIsWrittenThoughItsScreenIsGone() {
        runBlocking { prefs.saveSession(signedIn.copy(profileId = "p1")); prefs.cacheProfiles("[]") }
        fromAnEndedScope { prefs.clearSession() }
        assertNull("no login on disk", onDisk().token)
        runBlocking { prefs.setDownloadsWifiOnly(true) }
        assertNull("and none comes back", prefs.session.value.token)
    }

    // And signing in: the login screen goes away on the new session.
    @Test fun aSignInIsWrittenThoughItsScreenIsGone() {
        runBlocking { prefs.clearSession() }
        fromAnEndedScope { prefs.saveSession(signedIn) }
        assertEquals(signedIn, onDisk())
    }

    // Signing out left some of the session in the file (admin, the avatar's version and key,
    // setup in progress) and the profiles list in memory: memory said one thing, and the file,
    // which memory follows at the next write, another.
    @Test fun afterASignOutMemoryAndDiskSayTheSame() {
        runBlocking { prefs.saveSession(signedIn.copy(profileId = "p1")); prefs.cacheProfiles("[]") }
        runBlocking { prefs.clearSession() }
        val empty = Session(serverUrl = "http://server.test")
        assertEquals(empty, prefs.session.value)
        assertNull(prefs.cachedProfiles.value)
        assertEquals(empty, onDisk())
    }
}
