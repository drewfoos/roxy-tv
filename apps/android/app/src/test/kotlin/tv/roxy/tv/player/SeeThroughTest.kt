package tv.roxy.tv.player

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.toPixelMap
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.unit.dp
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/**
 * [seeThrough] takes what is drawn inside it away again, so the window shows what lies
 * behind it: the page on its way into the PiP window, where only the video is to be seen.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], qualifiers = "w200dp-h200dp-mdpi")
class SeeThroughTest {
    @get:Rule val compose = createComposeRule()

    private var page by mutableStateOf(true)
    private var fraction by mutableFloatStateOf(0f)

    private fun show() = compose.setContent {
        Box(Modifier.fillMaxSize().seeThrough { fraction }) {
            // A page: its background, and something on it.
            if (page) Box(Modifier.fillMaxSize().background(Color.Blue)) {
                Box(Modifier.size(40.dp).align(Alignment.Center).background(Color.Red))
            }
        }
    }

    private fun pixels(): Pair<Color, Color> {
        compose.waitForIdle()
        val px = compose.onRoot().captureToImage().toPixelMap()
        return px[px.width / 2, px.height / 2] to px[4, 4] // the red square, the blue background
    }

    private val nothing = Color.Transparent to Color.Transparent

    /**
     * At all of it nothing is left in the window there, not even what the window itself had
     * under the page (the test's window has a light background: it goes too). That is the
     * point: the erasing lands on the window, which then shows what is behind it.
     */
    @Test @GraphicsMode(GraphicsMode.Mode.NATIVE)
    fun atNoneThePageIsDrawnAndAtAllOfItNothingIs() {
        show()
        assertEquals(Color.Red to Color.Blue, pixels())

        // The window with no page in it: its own background, which is something.
        page = false
        val bare = pixels()
        assertEquals("the test's window has a background of its own", 1f, bare.second.alpha)
        page = true
        assertEquals(Color.Red to Color.Blue, pixels())

        fraction = 1f
        assertEquals("the page's background and what is on it are both gone", nothing, pixels())

        // And back: it is the same page again.
        fraction = 0f
        assertEquals(Color.Red to Color.Blue, pixels())
    }

    @Test @GraphicsMode(GraphicsMode.Mode.NATIVE)
    fun partWayThePageIsPartlyThere() {
        show()
        fraction = 0.5f
        val (square, background) = pixels()
        assertEquals("half of the red square: $square", 0.5f, square.alpha, 0.06f)
        assertEquals("half of the blue page: $background", 0.5f, background.alpha, 0.06f)
    }

    /** Values outside 0..1 (an animation overshooting) are the ends, not an error. */
    @Test @GraphicsMode(GraphicsMode.Mode.NATIVE)
    fun outOfRangeIsTheNearestEnd() {
        show()
        fraction = 1.4f
        assertEquals(nothing, pixels())
        fraction = -0.2f
        assertEquals(Color.Red to Color.Blue, pixels())
    }
}
