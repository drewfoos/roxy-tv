package tv.roxy.tv.player

import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * A closing player gives its picture up while its surface is still there. Checked in the
 * source: the player itself needs a real libmpv to run. What it is for was measured on a
 * TV (the main thread held for 0.7 s and 0.6 s at each exit from a 4K film, while mpv
 * decoded it in software for an output that was gone).
 */
class ClosingPlayerTest {
    private val player = File("src/main/kotlin/tv/roxy/tv/player")

    @Test fun theActivityTellsThePlayerBeforeItCloses() {
        val activity = File(player, "PlayerActivity.kt").readText()
        val finish = activity.substring(activity.indexOf("    override fun finish() {"))
        val told = finish.indexOf("session.player.closing()")
        assertTrue("finish() tells the player first", told in 0 until finish.indexOf("super.finish()"))
    }

    @Test fun closingDropsThePictureAndNothingElse() {
        val mpv = File(player, "MpvPlayer.kt").readText()
        val closing = mpv.substring(mpv.indexOf("    fun closing() {")).substringBefore("\n    }")
        assertTrue(closing.contains("""mpv.setPropertyString("vid", "no")"""))
        // Not the file (its end would look like a lost stream to the session) and not the
        // pause (the page pauses, and reports the place, when it stops).
        assertTrue("no other call into mpv", Regex("""mpv\.""").findAll(closing).count() == 1)
    }
}
