package tv.roxy.tv.player

import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Wiring inside MpvPlayer that needs a real libmpv to run, so it is checked in the source
 * (as MpvInitOrderTest does): the pieces it wires (PassthroughWatch, FrameRateGate,
 * loadTrackOptions) are tested on their own.
 */
class MpvWiringTest {
    private val src = File("src/main/kotlin/tv/roxy/tv/player/MpvPlayer.kt").readText()

    /** The body of `fun name(` (or of the `when` branch starting at [name]), by brace counting. */
    private fun body(name: String): String {
        val at = src.indexOf(name)
        assertTrue("$name found", at >= 0)
        val open = src.indexOf('{', at)
        var depth = 0
        for (i in open until src.length) {
            when (src[i]) { '{' -> depth++; '}' -> if (--depth == 0) return src.substring(open, i + 1) }
        }
        error("unbalanced $name")
    }

    @Test fun aNewFileStopsTheOldFilesPassthroughWatch() {
        assertTrue("loadPending cancels the passthrough watch", "stopPassthroughWatch()" in body("private fun loadPending()"))
    }

    @Test fun thePassthroughReloadGoesThroughTheListener() {
        val b = body("private fun passthroughFailed(")
        val asked = b.indexOf("onPassthroughFailed")
        val played = b.indexOf("play(")
        assertTrue("the listener is asked first, the player reopens only if it declines", asked >= 0 && (played < 0 || asked < played))
    }

    // mpv keeps aid/sid for later files: a load that left either unset opened on the last
    // file's numbers. Both go in on every load (see loadTrackOptions).
    @Test fun everyLoadSetsBothTracks() {
        val b = body("private fun loadPending()")
        assertTrue("loadPending sets aid and sid from loadTrackOptions", Regex("""\n\s*for \(\(name, value\) in MpvTracks\.loadTrackOptions\(""").containsMatchIn(b))
        assertTrue("before the loadfile", b.indexOf("loadTrackOptions(") < b.indexOf("\"loadfile\""))
    }

    @Test fun theFrameRateGateIsResetOnStartFile() {
        assertTrue("START_FILE resets the gate", "frameRateGate.fileStarted()" in body("MPVLib.MpvEvent.MPV_EVENT_START_FILE ->"))
        val b = body("private fun loadPending()")
        assertTrue("the load is told to the gate only after the pending-URL check", b.indexOf("pendingUrl ?: return") in 0 until b.indexOf("frameRateGate.loadRequested()"))
    }
}
