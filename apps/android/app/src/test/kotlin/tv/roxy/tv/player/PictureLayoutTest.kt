package tv.roxy.tv.player

import android.graphics.Rect
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.getUnclippedBoundsInRoot
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.DpSize
import androidx.compose.ui.unit.dp
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import java.io.File

/**
 * Where the phone player's video goes in its box: at the picture's shape in every box, so
 * the one buffer under it is only ever scaled (see [PictureSurface]). Fullscreen used to be
 * a surface over the whole screen with mpv drawing the bars inside it: that surface could
 * not be the PiP window's source, and went into it squeezed.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], qualifiers = "w400dp-h800dp-mdpi")
class PictureLayoutTest {
    @get:Rule val compose = createComposeRule()

    private var box by mutableStateOf(DpSize(144.dp, 81.dp))
    private var shape by mutableStateOf<Pair<Int, Int>?>(16 to 9)
    private val places = ArrayList<Rect>()

    private fun show() = compose.setContent {
        Box(Modifier.size(box.width, box.height)) {
            Box(pictureModifier(shape) { places += it }.testTag("picture"))
        }
    }

    private fun assertPlace(left: Dp, top: Dp, width: Dp, height: Dp) {
        compose.waitForIdle()
        val b = compose.onNodeWithTag("picture").getUnclippedBoundsInRoot()
        assertEquals("left", left.value, b.left.value, 0.6f)
        assertEquals("top", top.value, b.top.value, 0.6f)
        assertEquals("width", width.value, (b.right - b.left).value, 0.6f)
        assertEquals("height", height.value, (b.bottom - b.top).value, 0.6f)
    }

    @Test fun thePictureFillsABoxOfItsOwnShape() {
        show()
        assertPlace(0.dp, 0.dp, 144.dp, 81.dp)
    }

    @Test fun inAWiderBoxItIsCentredBetweenPillars() {
        box = DpSize(312.dp, 144.dp) // fullscreen on a 21.7:9 phone
        show()
        assertPlace(28.dp, 0.dp, 256.dp, 144.dp)
    }

    @Test fun inATallerBoxItIsCentredBetweenBands() {
        box = DpSize(312.dp, 144.dp)
        shape = 239 to 100 // a scope film on the same screen
        show()
        assertPlace(0.dp, 6.7.dp, 312.dp, 130.5.dp)
    }

    /** The box changes (upright, fullscreen, the PiP window): the picture follows at its shape, and says where it is. */
    @Test fun thePictureFollowsItsBoxAndReportsItsPlace() {
        show()
        assertPlace(0.dp, 0.dp, 144.dp, 81.dp)
        box = DpSize(312.dp, 144.dp)
        assertPlace(28.dp, 0.dp, 256.dp, 144.dp)
        compose.runOnIdle { assertEquals(Rect(28, 0, 284, 144), places.last()) }
        box = DpSize(80.dp, 45.dp)
        assertPlace(0.dp, 0.dp, 80.dp, 45.dp)
        compose.runOnIdle {
            assertEquals(Rect(0, 0, 80, 45), places.last())
            // Each place it was given is the picture's shape: what the system needs of a PiP source.
            assertTrue(places.toString(), places.all { PictureSurface.usableSource(it, 16 to 9, 400, 800) })
        }
    }

    /** The TV's player: no shape, the video is the whole box and mpv fits the picture in it. */
    @Test fun withoutAShapeTheVideoIsTheWholeBox() {
        box = DpSize(312.dp, 144.dp)
        shape = null
        show()
        assertPlace(0.dp, 0.dp, 312.dp, 144.dp)
        compose.runOnIdle { assertTrue("no place is reported", places.isEmpty()) }
    }

    // ---- the wiring mpv cannot be run for (see MpvInitOrderTest) ----

    private fun source(name: String) = File("src/main/kotlin/tv/roxy/tv/$name").readText()

    /** The phone's page gives its player the picture's shape; the TV's activity gives none. */
    @Test fun onlyThePhonePageLaysThePictureOutItself() {
        assertTrue(Regex("""PlayerSurface\(s, modifier = Modifier\.fillMaxSize\(\), shape = shape,""").containsMatchIn(source("ui/phone/PhoneEpisode.kt")))
        assertTrue(Regex("""PlayerSurface\(s, modifier = Modifier\.fillMaxSize\(\)\)""").containsMatchIn(source("player/PlayerActivity.kt")))
    }

    /** With a shape the surface's buffer is fixed, before the surface exists and on every change. */
    @Test fun aShapedPictureHasAFixedBuffer() {
        val src = source("player/PlayerSurface.kt")
        assertTrue("on the new view, before its surface", "factory = { ctx -> SurfaceView(ctx).apply { applyRenderSize(render); holder.addCallback(player) } }" in src)
        assertTrue("and when the shape or the display changes", "update = { sv -> sv.applyRenderSize(render) }" in src)
        assertTrue("if (size != null) holder.setFixedSize(size.first, size.second) else holder.setSizeFromLayout()" in src)
    }

    /**
     * After a change of size mpv is made to draw the frame again by an option that CHANGES:
     * it ignores one set to the value it has, which is what "set the size again" was, and a
     * paused picture stayed drawn into the old buffer.
     */
    @Test fun theRedrawAfterAResizeChangesAnOption() {
        val src = source("player/MpvPlayer.kt")
        val body = src.substringAfter("private val redrawAfterResize = Runnable {").substringBefore("\n    }")
        assertTrue(body, "redrawNudge = !redrawNudge" in body)
        assertTrue(body, """mpv.setPropertyDouble("video-align-x", if (redrawNudge) 1e-4 else 0.0)""" in body)
        assertTrue("not the size it already has", "android-surface-size" !in body)
    }
}
