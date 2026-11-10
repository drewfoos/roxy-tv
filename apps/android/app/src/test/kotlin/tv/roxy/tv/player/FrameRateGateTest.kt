package tv.roxy.tv.player

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/** The display's frame rate is asked for only once the first frame is up (see FrameRateGate). */
class FrameRateGateTest {
    // Found on the TV: asked for as the file loaded, the display's answer could come before
    // the first frame and hold the picture 2.6 to 3.4 s.
    @Test fun heldUntilTheFirstFrame() {
        val g = FrameRateGate()
        g.newFile()
        assertNull("not while the file loads", g.hint(23.976f))
        assertEquals(23.976f, g.firstFrame())
        assertNull("seeks restart playback too: asked once", g.firstFrame())
    }

    @Test fun aRateKnownAfterTheFirstFrameGoesAtOnce() {
        val g = FrameRateGate()
        g.newFile()
        assertNull("nothing held", g.firstFrame())
        assertEquals(25f, g.hint(25f))
    }

    @Test fun eachFileWaitsForItsOwnFirstFrame() {
        val g = FrameRateGate()
        g.newFile(); g.hint(23.976f); g.firstFrame()
        g.newFile()
        assertNull("the next episode is held again", g.hint(24f))
        assertEquals(24f, g.firstFrame())
    }

    @Test fun theLastFilesRateIsNotCarriedOver() {
        val g = FrameRateGate()
        g.newFile(); g.hint(23.976f); g.firstFrame()
        g.newFile()
        assertNull("no rate known for the new file yet", g.firstFrame())
    }
}
