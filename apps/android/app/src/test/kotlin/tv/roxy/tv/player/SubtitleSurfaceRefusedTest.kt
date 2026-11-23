package tv.roxy.tv.player

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * The page takes the subtitle surface away when mpv says it gave it up. mpv may have had
 * to show a buffer it could not write on the way (Android gives a locked buffer back only
 * by showing it), and only removing the surface takes that off the screen.
 */
class SubtitleSurfaceRefusedTest {
    private val vo = "vo/mediacodec_embed"

    @Test fun mpvsOwnWordsForGivingTheSurfaceUpAreHeard() {
        assertTrue(subtitleSurfaceRefused(vo, "The subtitle surface has the wrong format (4): not used."))
        assertTrue(subtitleSurfaceRefused(vo, "The subtitle surface cannot be used (1920x802, format 2)."))
    }

    @Test fun nothingElseIsTakenForIt() {
        // A buffer that could not be had this once (the surface on its way out): not given up.
        assertFalse(subtitleSurfaceRefused(vo, "The subtitle surface cannot be drawn to."))
        assertFalse(subtitleSurfaceRefused(vo, "Subtitles on their own surface, 1920x802."))
        // The same words from elsewhere (a subtitle's own text is never a log line of the VO).
        assertFalse(subtitleSurfaceRefused("sub/ass", "The subtitle surface has the wrong format"))
        assertFalse(subtitleSurfaceRefused("cplayer", "The subtitle surface cannot be used"))
    }

    // The words are mpv's, from Roxy's patch: reworded there and not here, nothing is heard.
    @Test fun thePatchSaysTheseWords() {
        val patch = File("../tools/libmpv-patches/mpv/0001-mediacodec-embed-subtitle-surface.patch").readText()
        assertEquals(SUBTITLE_SURFACE_REFUSALS, SUBTITLE_SURFACE_REFUSALS.filter { it in patch })
    }

    @Test fun thePlayerPassesItOnAndThePageActs() {
        val player = File("src/main/kotlin/tv/roxy/tv/player")
        assertTrue(File(player, "MpvPlayer.kt").readText().contains("if (subtitleSurfaceRefused(prefix, line)) post { listener?.onSubtitleSurfaceRefused() }"))
        assertTrue(File(player, "PlayerSurface.kt").readText().contains("session.nativeHdr && !session.subtitleSurfaceRefused && placed != null"))
    }
}
