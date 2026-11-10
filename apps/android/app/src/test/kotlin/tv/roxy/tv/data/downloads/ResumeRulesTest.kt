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

    // Found in review: a part that had every byte but was not renamed (the app was stopped
    // between the last byte and the rename) was resumed with "the rest from <its length>".
    // The server answers 416 to that, and the whole part was thrown away and fetched again.
    @Test fun aPartThatIsAlreadyTheWholeFileIsKept() {
        assertTrue(partIsWhole(5000, 5000, "\"abc\"", "bytes */5000"))
        assertTrue("the answer names no size", partIsWhole(5000, 5000, "\"abc\"", null))
        assertTrue("no total was recorded", partIsWhole(5000, 0, "\"abc\"", "bytes */5000"))
    }

    @Test fun aRejectedRangeOfAnotherFileStartsOver() {
        assertFalse("the file is shorter than the part", partIsWhole(5000, 5000, "\"abc\"", "bytes */4000"))
        assertFalse("the file is longer than the part", partIsWhole(5000, 5000, "\"abc\"", "bytes */6000"))
        assertFalse("not the total recorded", partIsWhole(5000, 6000, "\"abc\"", "bytes */5000"))
        assertFalse("no size known at all", partIsWhole(5000, 0, "\"abc\"", null))
        assertFalse("sent without If-Range: the file may be another", partIsWhole(5000, 5000, null, "bytes */5000"))
        assertFalse("an empty part", partIsWhole(0, 0, "\"abc\"", "bytes */0"))
    }
}
