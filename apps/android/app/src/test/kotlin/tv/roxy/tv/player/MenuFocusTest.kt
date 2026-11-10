package tv.roxy.tv.player

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/** Which row a player menu opens on ([menuFocusKey]). */
class MenuFocusTest {
    private val settings = listOf(
        MenuRow("audio", "Audio", "English", submenu = true),
        MenuRow("subs", "Subtitles", "Off", submenu = true),
        MenuRow("autoskip", "Auto-skip intro & outro", "On", checked = true, toggle = true),
        MenuRow("autoplay", "Autoplay", "On", checked = true, toggle = true),
    )

    @Test fun settingsOpensOnItsFirstRowEvenWithASwitchOn() {
        assertEquals("audio", menuFocusKey(settings))
    }

    @Test fun aListOpensOnTheChoiceInForce() {
        val speeds = listOf(MenuRow("0.5", "0.5x"), MenuRow("1.0", "Normal", checked = true), MenuRow("2.0", "2x"))
        assertEquals("1.0", menuFocusKey(speeds))
    }

    @Test fun aChoiceInForceWinsOverASwitchAboveIt() {
        val rows = listOf(MenuRow("loop", "Loop", "On", checked = true, toggle = true), MenuRow("a", "A"), MenuRow("b", "B", checked = true))
        assertEquals("b", menuFocusKey(rows))
    }

    @Test fun withNothingChosenTheFirstRow() {
        assertEquals("a", menuFocusKey(listOf(MenuRow("a", "A"), MenuRow("b", "B"))))
        assertNull(menuFocusKey(emptyList()))
    }
}
