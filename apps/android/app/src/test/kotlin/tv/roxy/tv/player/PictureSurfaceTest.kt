package tv.roxy.tv.player

import android.graphics.Rect
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import tv.roxy.tv.ui.phone.playerBoxShape

/**
 * The phone player's picture is one buffer of the picture's shape that the system scales
 * into whatever box the page lays out (see [PictureSurface]). Before, the surface followed
 * the box: mpv drew its first frame after every change of size into a buffer of the old
 * size (a paused video stayed that way after leaving fullscreen), and a fullscreen surface
 * with mpv's black bars inside it went into the PiP window squeezed, by way of a cross-fade
 * through the app's icon.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class PictureSurfaceTest {
    // ---- the buffer ----

    @Test fun theBufferIsThePictureFittedInTheScreenHeldSideways() {
        // Galaxy S25 Ultra at WQHD+: 3120 x 1440.
        assertEquals(2560 to 1440, PictureSurface.renderSize(1920 to 1080, 3120, 1440))
        assertEquals(1920 to 1440, PictureSurface.renderSize(1440 to 1080, 3120, 1440))
        assertEquals(3120 to 1306, PictureSurface.renderSize(1920 to 804, 3120, 1440))
        // A 1080p phone, and a 16:9 screen (the picture is the whole screen).
        assertEquals(1920 to 1080, PictureSurface.renderSize(1920 to 1080, 2400, 1080))
        assertEquals(1920 to 1080, PictureSurface.renderSize(3840 to 2160, 1920, 1080))
        // The shape given in lowest terms, as the box's is when the size is not known.
        assertEquals(2560 to 1440, PictureSurface.renderSize(16 to 9, 3120, 1440))
    }

    @Test fun theBufferNeverLeavesTheScreen() {
        for (shape in listOf(1920 to 1080, 4 to 3, 239 to 100, 1864 to 1008, 720 to 480, 1 to 1)) {
            for ((long, short) in listOf(3120 to 1440, 2400 to 1080, 2340 to 1080, 1920 to 1080, 1600 to 720, 2960 to 1848)) {
                val (w, h) = PictureSurface.renderSize(shape, long, short)!!
                assertTrue("$shape on $long x $short: $w x $h", w in 1..long && h in 1..short)
                assertTrue("one side is the screen's: $w x $h", w == long || h == short)
                val want = shape.first.toDouble() / shape.second
                assertEquals("$shape on $long x $short keeps its shape", want, w.toDouble() / h, want / minOf(w, h) * 2)
            }
        }
    }

    @Test fun noBufferWithoutASize() {
        assertNull(PictureSurface.renderSize(0 to 9, 3120, 1440))
        assertNull(PictureSurface.renderSize(16 to 0, 3120, 1440))
        assertNull(PictureSurface.renderSize(16 to 9, 0, 0))
    }

    /**
     * mpv's own fit of a picture into a window (video/out/aspect.c, aspect_calc_panscan with
     * panscan 0), float arithmetic and truncation as there. [w], [h]: the picture's size.
     */
    private fun mpvFit(w: Int, h: Int, windowW: Int, windowH: Int): Pair<Int, Int> {
        var fwidth = windowW
        var fheight = (windowW.toFloat() / w * h).toDouble().toInt()
        if (fheight > windowH || fheight < h) {
            val tmpw = (windowH.toFloat() / h * w).toDouble().toInt()
            if (tmpw <= windowW) { fheight = windowH; fwidth = tmpw }
        }
        return fwidth to fheight
    }

    /**
     * The buffer has the picture's shape, so mpv must find that the picture fills it: a
     * buffer rounded the other way left mpv a pixel short, and it drew a black line beside
     * the picture. A picture no larger than the buffer (the usual case: the buffer is the
     * screen's size) fills it exactly, and so do the plain 16:9 sizes scaled down.
     */
    @Test fun mpvFillsTheBufferWithTheUsualPictures() {
        val pictures = listOf(1920 to 1080, 1280 to 720, 3840 to 2160, 1440 to 1080, 960 to 720, 640 to 480, 1920 to 804, 1920 to 1040, 1920 to 1038, 1864 to 1008, 848 to 480)
        val screens = listOf(3120 to 1440, 2400 to 1080, 2340 to 1080, 1920 to 1080, 2560 to 1440, 1600 to 720)
        var checked = 0
        for ((w, h) in pictures) for ((long, short) in screens) {
            val shape = playerBoxShape(w, h)
            assertEquals("$w x $h is shown in its own shape", w to h, shape)
            val buffer = PictureSurface.renderSize(shape, long, short)!!
            val scaledDown = w > buffer.first || h > buffer.second
            if (scaledDown && w * 9 != h * 16) continue
            assertEquals("$w x $h in a buffer of $buffer on $long x $short", buffer, mpvFit(w, h, buffer.first, buffer.second))
            checked++
        }
        assertTrue("$checked cases", checked >= 50)
    }

    /**
     * A picture flatter than 2.39:1 (the flattest PiP window the system takes) is in a 2.39
     * box, and mpv draws the rest as thin bands inside the buffer: the one case with bars
     * of mpv's own.
     */
    @Test fun aFlatterPictureKeepsItsBandsInsideTheBuffer() {
        assertEquals(239 to 100, playerBoxShape(1920, 800))
        val buffer = PictureSurface.renderSize(239 to 100, 3120, 1440)!!
        assertEquals(3120 to 1305, buffer)
        assertEquals(3120 to 1300, mpvFit(1920, 800, buffer.first, buffer.second))
    }

    /**
     * Any other picture of its own shape. A picture larger than the buffer is fitted by mpv
     * from the height back to the width, cutting fractions both times, which can leave it up
     * to 3 pixels narrower than the buffer (of thousands); never more, and never shorter by
     * more than one.
     */
    @Test fun mpvIsNeverMoreThanAFewPixelsShortOfTheBuffer() {
        val screens = listOf(3120 to 1440, 2400 to 1080, 2340 to 1080, 1920 to 1080, 2560 to 1600, 1600 to 720, 2960 to 1848)
        var checked = 0
        for (w in 320..3840 step 8) for (h in 240..2160 step 8) {
            val shape = playerBoxShape(w, h)
            if (shape != w to h) continue // a clamped box: mpv draws its bars inside it, as meant
            for ((long, short) in screens) {
                val buffer = PictureSurface.renderSize(shape, long, short)!!
                val fit = mpvFit(w, h, buffer.first, buffer.second)
                assertTrue("$w x $h in $buffer: mpv fits $fit", buffer.first - fit.first in 0..3 && buffer.second - fit.second in 0..1)
                checked++
            }
        }
        assertTrue("shapes were checked", checked > 100_000)
    }

    // ---- the PiP source ----

    @Test fun aSourceOfThePictureShapeInsideTheWindowIsUsable() {
        // The box under the status bar, and the picture centred in fullscreen.
        assertTrue(PictureSurface.usableSource(Rect(0, 128, 1440, 938), 1920 to 1080, 1440, 3120))
        assertTrue(PictureSurface.usableSource(Rect(280, 0, 2840, 1440), 1920 to 1080, 3120, 1440))
        assertTrue(PictureSurface.usableSource(Rect(0, 128, 1440, 731), 1920 to 804, 1440, 3120))
    }

    @Test fun aSourceOfAnotherShapeIsNot() {
        // The whole sideways screen for a 16:9 picture: what the page sent before. The system
        // refuses it and cross-fades through the app's icon instead.
        assertFalse(PictureSurface.usableSource(Rect(0, 0, 3120, 1440), 1920 to 1080, 3120, 1440))
        // The previous episode's 16:9 box for a 4:3 picture.
        assertFalse(PictureSurface.usableSource(Rect(0, 128, 1440, 938), 1440 to 1080, 1440, 3120))
    }

    @Test fun aSourceOutsideTheWindowOrEmptyIsNot() {
        assertFalse(PictureSurface.usableSource(null, 16 to 9, 1440, 3120))
        assertFalse(PictureSurface.usableSource(Rect(), 16 to 9, 1440, 3120))
        // Half-way through the page sliding in.
        assertFalse(PictureSurface.usableSource(Rect(324, 128, 1764, 938), 16 to 9, 1440, 3120))
        // Measured in the PiP window but judged against the whole one, and the reverse.
        assertFalse(PictureSurface.usableSource(Rect(0, 128, 1440, 938), 16 to 9, 788, 443))
        assertFalse(PictureSurface.usableSource(Rect(0, 0, 788, 443), 16 to 9, 0, 0))
    }

    // ---- fullscreen, through the PiP window ----

    @Test fun fullscreenIsTheWindowLyingSideways() {
        val m = FullscreenMemory(false)
        assertFalse(m.now(windowLandscape = false, inPip = false))
        assertTrue(m.now(windowLandscape = true, inPip = false))
        assertFalse(m.now(windowLandscape = false, inPip = false))
    }

    /** The PiP window is a small sideways box whatever the page was. */
    @Test fun thePipWindowSaysNothingAboutFullscreen() {
        val upright = FullscreenMemory(false)
        assertFalse("from the upright page", upright.now(windowLandscape = true, inPip = true))
        assertFalse("still, while it floats", upright.now(windowLandscape = true, inPip = true))
        assertFalse("and back on the upright page", upright.now(windowLandscape = false, inPip = false))

        val sideways = FullscreenMemory(false)
        assertTrue(sideways.now(windowLandscape = true, inPip = false))
        assertTrue("from fullscreen", sideways.now(windowLandscape = true, inPip = true))
        // A tall PiP window (a vertical video's) is no more "not fullscreen" than a wide one is.
        assertTrue(sideways.now(windowLandscape = false, inPip = true))
        assertFalse("expanded with the phone upright", sideways.now(windowLandscape = false, inPip = false))
    }
}
