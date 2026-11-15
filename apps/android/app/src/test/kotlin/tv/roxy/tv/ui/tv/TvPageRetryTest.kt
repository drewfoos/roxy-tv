package tv.roxy.tv.ui.tv

import android.os.Looper
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.test.SemanticsMatcher
import androidx.compose.ui.test.assert
import androidx.compose.ui.test.hasTestTag
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.runBlocking
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.RecordingServer
import tv.roxy.tv.data.Session
import tv.roxy.tv.ui.components.TvPageError

/**
 * The TV's Browse and Search with the library unreadable while the server IS there (an
 * error answer): the page retried once by itself and then only said so, with nothing to
 * press. It now has "Try again", and pressing it asks for the library again.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], qualifiers = "w960dp-h540dp-xhdpi")
class TvPageRetryTest {
    @get:Rule val compose = createComposeRule()

    private val server = RecordingServer { seen ->
        if (seen.path == "/v1/system/info") 200 to """{"product":"roxy"}""" else 500 to """{"error":{"code":"internal","message":"boom"}}"""
    }
    private val app get() = ApplicationProvider.getApplicationContext<RoxyApp>()

    @Before fun start() = runBlocking { app.prefs.saveSession(Session(serverUrl = server.url, token = "t", profileId = "p")) }
    @After fun stop() = server.close()

    private fun asked() = server.seen.count { it.path == "/v1/items" }

    private fun waitFor(what: String, ms: Long = 8_000, cond: () -> Boolean) {
        val until = System.currentTimeMillis() + ms
        while (true) {
            shadowOf(Looper.getMainLooper()).idle()
            compose.waitForIdle()
            if (runCatching(cond).getOrDefault(false)) return
            if (System.currentTimeMillis() > until) throw AssertionError("$what; server saw ${server.seen.map { it.path }}")
            Thread.sleep(20)
        }
    }

    private fun retryShown() = compose.onAllNodes(hasTestTag("page-retry")).fetchSemanticsNodes().isNotEmpty()

    /** The page's own retry (it makes one) is over: nothing more is asked for a while. */
    private fun settled(): Int {
        var n = asked()
        var since = System.currentTimeMillis()
        waitFor("the page stops asking by itself", ms = 15_000) {
            val now = asked()
            if (now != n) { n = now; since = System.currentTimeMillis() }
            System.currentTimeMillis() - since > 1_500
        }
        return n
    }

    @Test fun anErrorWithTheServerThereHasTryAgain() {
        var pressed = 0
        compose.setContent { TvPageError("Could not load the library", onRetry = { pressed++ }) }
        // Focusable: the remote can reach it.
        compose.onNodeWithTag("page-retry").assert(SemanticsMatcher.keyIsDefined(SemanticsProperties.Focused)).performClick()
        compose.waitForIdle()
        assertEquals(1, pressed)
    }

    @Test fun tryAgainOnBrowseAsksForTheLibraryAgain() {
        compose.setContent { TvBrowse(onOpenSeries = {}, onOpenMovie = {}) }
        waitFor("the error has its button") { retryShown() }
        val before = settled()
        assertTrue("the library was asked for", before >= 1)

        compose.onNodeWithTag("page-retry").performClick()

        waitFor("the library is asked for again") { asked() > before }
        waitFor("and failing again, the button is back") { retryShown() }
    }

    @Test fun tryAgainOnSearchAsksForTheLibraryAgain() {
        compose.setContent { TvSearch(onOpenSeries = {}, onOpenMovie = {}) }
        waitFor("the error has its button") { retryShown() }
        val before = settled()

        compose.onNodeWithTag("page-retry").performClick()

        waitFor("the library is asked for again") { asked() > before }
    }
}
