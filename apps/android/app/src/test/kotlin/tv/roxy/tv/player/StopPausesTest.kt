package tv.roxy.tv.player

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * A stopped player pauses, in PiP too. Entering PiP never stops the activity; a stop in
 * PiP is the window closed or the screen turned off, while isInPictureInPictureMode can
 * still read true, and the pause it skipped left audio playing with nothing on screen.
 * mpv cannot run here (see MpvInitOrderTest), so this checks PlayerSession.onStop's source.
 */
class StopPausesTest {
    @Test fun onStopPausesEvenInPip() {
        val src = File("src/main/kotlin/tv/roxy/tv/player/PlayerSession.kt").readLines()
        val start = src.indexOfFirst { it.trim() == "fun onStop() {" }
        assertTrue("onStop found", start >= 0)
        val end = (start until src.size).first { src[it] == "    }" }
        val body = src.subList(start + 1, end)
        assertTrue("onStop pauses: $body", body.any { it.trim() == "player.setPaused(true)" })
        assertFalse("the pause depends on nothing (PiP included): $body", body.any { "inPip" in it })
    }
}
