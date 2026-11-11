package tv.roxy.tv.ui.components

import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * A page that could not load reloads when the server is back (OnServerBack). A page that
 * was not on screen at that moment (under the player, or a tab away) must catch up when it
 * is shown again: it used to keep its "Can't reach your server" until Try again.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class OnServerBackTest {
    private val returns = MutableStateFlow(0)
    private var seen = 0
    private var reloads = 0

    private fun TestScope.watch() =
        backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) { onReturns(returns, { seen }, { seen = it }) { reloads++ } }

    @Test fun aPageOnScreenReloadsAtEachReturn() = runTest {
        watch()
        assertEquals("showing the page is not a return", 0, reloads)
        returns.value = 1
        assertEquals(1, reloads)
        returns.value = 2
        assertEquals(2, reloads)
    }

    @Test fun aPageThatWasOffScreenCatchesUpWhenShownAgain() = runTest {
        val first = watch()
        first.cancel() // another page covers this one
        returns.value = 1 // the server comes back meanwhile
        assertEquals(0, reloads)
        watch() // back on screen, with what it last saw kept
        assertEquals("reloads once, by itself", 1, reloads)
        assertEquals(1, seen)
    }

    @Test fun comingBackWithNothingMissedDoesNotReload() = runTest {
        watch().cancel()
        watch()
        assertEquals(0, reloads)
    }
}
