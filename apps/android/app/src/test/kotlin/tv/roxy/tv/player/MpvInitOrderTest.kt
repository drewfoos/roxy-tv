package tv.roxy.tv.player

import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * mpv calls the player back on its own thread as soon as it is observed. If a field the
 * callbacks use is declared below that point, it is still null when the first callback
 * comes: on the TV a NullPointerException on trackGen in eventProperty aborted the app
 * (November 2026). mpv cannot run here, so this checks the source: observing is the last
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
