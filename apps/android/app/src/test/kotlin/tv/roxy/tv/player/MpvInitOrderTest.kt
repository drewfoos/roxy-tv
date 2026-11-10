package tv.roxy.tv.player

import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * mpv calls the player back on its own thread, which is only guaranteed to see fields
 * written before the observer was added (MPVLib adds and dispatches under one lock). A
 * field declared below that point may stay null to it: on the TV a NullPointerException
 * on trackGen in eventProperty aborted the app at a teardown (November 2026). mpv cannot run here, so this checks the source: observing is the last
 * thing the class does, with no field declared after it.
 */
class MpvInitOrderTest {
    @Test fun mpvIsObservedOnlyOnceEveryFieldIsSet() {
        val src = File("src/main/kotlin/tv/roxy/tv/player/MpvPlayer.kt").readLines()
        val observe = src.indexOfFirst { it.contains("mpv.addObserver(this)") }
        assertTrue("observer registration found", observe >= 0)
        val field = Regex("""^    (@Volatile )?((private|internal|protected|override) )*(val|var|lateinit var) \w+""")
        val after = src.drop(observe).filter { field.containsMatchIn(it) }
        assertTrue("fields declared after mpv is observed (null in its first callbacks): $after", after.isEmpty())
    }
}
