package tv.roxy.tv.ui.phone

import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * The player's box on the portrait episode page was always 16:9: a wider film played with
 * black bands above and below it. The box takes the picture's shape, within limits.
 */
class PlayerBoxShapeTest {
    @Test fun `the box is the picture's shape`() {
        assertEquals(1920 to 1080, playerBoxShape(1920, 1080))
        assertEquals(1920 to 804, playerBoxShape(1920, 804))
        assertEquals(1440 to 1080, playerBoxShape(1440, 1080))
    }

    @Test fun `an unknown size is 16 to 9`() {
        assertEquals(16 to 9, playerBoxShape(0, 0))
        assertEquals(16 to 9, playerBoxShape(1920, 0))
        assertEquals(16 to 9, playerBoxShape(null))
    }

    @Test fun `no taller than 4 to 3 and no flatter than 2,39 to 1`() {
        assertEquals(4 to 3, playerBoxShape(1080, 1920))
        assertEquals(239 to 100, playerBoxShape(3840, 1080))
    }
}
