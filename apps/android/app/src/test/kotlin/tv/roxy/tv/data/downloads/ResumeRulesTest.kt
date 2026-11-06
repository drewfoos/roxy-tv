package tv.roxy.tv.data.downloads

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ResumeRulesTest {
    @Test fun partWithValidatorResumes() {
        assertEquals(1000L, resumeOffset(1000, "\"abc\""))
        assertEquals(1000L, resumeOffset(1000, "Thu, 05 Nov 2026 10:00:00 GMT"))
    }

    // The finding: a part without a validator used to be resumed with a bare Range, so a
    // file replaced on the server had its bytes appended to the old file's.
    @Test fun partWithoutValidatorRestarts() {
        assertEquals(0L, resumeOffset(1000, null))
    }

    @Test fun noPartStartsAtZero() {
        assertEquals(0L, resumeOffset(0, "\"abc\""))
        assertEquals(0L, resumeOffset(0, null))
    }

    @Test fun exactRestFits() {
        assertTrue(partialFits("bytes 1000-4999/5000", 1000, 5000))
        assertTrue(partialFits(" bytes 1000-4999/5000 ", 1000, 5000))
        assertTrue(partialFits("bytes 1000-4999/5000", 1000, 0)) // total not known yet
        assertTrue(partialFits("bytes 1000-4999/*", 1000, 5000)) // server does not say
    }

    @Test fun anotherStartIsRejected() {
        assertFalse(partialFits("bytes 0-4999/5000", 1000, 5000))
        assertFalse(partialFits("bytes 2000-4999/5000", 1000, 5000))
    }

    @Test fun anotherFileSizeIsRejected() {
        assertFalse(partialFits("bytes 1000-5999/6000", 1000, 5000))
    }

    @Test fun missingOrMalformedRangeIsRejected() {
        assertFalse(partialFits(null, 1000, 5000))
        assertFalse(partialFits("", 1000, 5000))
        assertFalse(partialFits("bytes */5000", 1000, 5000))
        assertFalse(partialFits("items 1000-4999/5000", 1000, 5000))
        assertFalse(partialFits("bytes 1000-999/5000", 1000, 5000))
        assertFalse(partialFits("bytes 1000-5000/5000", 1000, 5000)) // end past the size
    }
}
