package tv.roxy.tv.ui.phone

import org.junit.Assert.assertEquals
import org.junit.Test

/** Home tapped while Home is the page reloads it; no other tab does anything on a second tap. */
class TabTapTest {
    @Test fun homeTappedOnHomeReloads() = assertEquals(TabTap.ReloadHome, tabTap(Tab.Home, Tab.Home))

    @Test fun anotherTabOpens() {
        assertEquals(TabTap.Go, tabTap(Tab.Browse, Tab.Home))
        assertEquals(TabTap.Go, tabTap(Tab.Home, Tab.Discover))
        assertEquals(TabTap.Go, tabTap(null, Tab.Home))
    }

    @Test fun theOtherTabsStayPut() = Tab.entries.filter { it != Tab.Home }.forEach { assertEquals(TabTap.Stay, tabTap(it, it)) }
}
