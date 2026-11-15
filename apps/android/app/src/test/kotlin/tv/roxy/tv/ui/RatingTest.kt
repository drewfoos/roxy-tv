package tv.roxy.tv.ui

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class RatingTest {
    /** The providers score out of ten; every screen shows stars out of five. */
    @Test
    fun starsAreOutOfFive() {
        assertEquals("3.8", starScore(7.6))
        assertEquals("5.0", starScore(10.0))
        assertNull(starScore(null))
        assertNull(starScore(0.0)) // "no score", not zero stars
    }

    @Test
    fun countIsShortened() {
        assertNull(ratingCountLabel(null))
        assertNull(ratingCountLabel(0))
        assertEquals("(87)", ratingCountLabel(87))
        assertEquals("(4K)", ratingCountLabel(4037))
        assertEquals("(42.9K)", ratingCountLabel(42950))
        assertEquals("(1.2M)", ratingCountLabel(1_250_000))
    }

    @Test
    fun lineHasTheCountOnlyWhenKnown() {
        assertEquals("★ 3.8 (4K)", ratingLine(7.6, 4037))
        assertEquals("★ 3.8", ratingLine(7.6))
        assertNull(ratingLine(null, 4037))
    }
}
