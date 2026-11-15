package tv.roxy.tv.ui.phone

import androidx.compose.ui.test.hasContentDescription
import androidx.compose.ui.test.hasTestTag
import androidx.compose.ui.test.junit4.createComposeRule
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * Home tapped in the bottom bar while Home is the page: the bar's Home icon is the
 * loading circle until the reload is done, and the house again after. On another tab
 * the house stays (a reload still running behind is not that tab's news).
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], qualifiers = "w412dp-h915dp-port-xxhdpi")
class HomeTabReloadingTest {
    @get:Rule val compose = createComposeRule()

    private fun bar(current: Tab, reloading: Boolean) {
        compose.mainClock.autoAdvance = false // the circle turns for ever
        compose.setContent { BottomBar(current = current, profileName = "Drew", avatarUrl = null, homeReloading = reloading, onSelect = {}) }
        compose.mainClock.advanceTimeBy(100)
    }

    private fun circles() = compose.onAllNodes(hasTestTag("home-tab-reloading"), useUnmergedTree = true).fetchSemanticsNodes().size
    private fun houses() = compose.onAllNodes(hasContentDescription("Home"), useUnmergedTree = true).fetchSemanticsNodes().size

    @Test fun theHomeIconIsTheCircleWhileItReloads() {
        bar(Tab.Home, reloading = true)
        assertEquals(1, circles())
        assertEquals("in the icon's place", 0, houses())
    }

    @Test fun theHouseOtherwise() {
        bar(Tab.Home, reloading = false)
        assertEquals(0, circles())
        assertEquals(1, houses())
    }

    @Test fun onAnotherTabTheHouseStays() {
        bar(Tab.Browse, reloading = true)
        assertEquals(0, circles())
        assertEquals(1, houses())
    }
}
