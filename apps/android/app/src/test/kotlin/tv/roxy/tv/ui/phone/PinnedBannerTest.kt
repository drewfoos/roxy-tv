package tv.roxy.tv.ui.phone

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.toPixelMap
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/**
 * A title page's picture stays put while the page slides up over it. A row of the page
 * that paints no ground of its own (a film's own row, right under the tabs) showed the
 * picture through itself wherever it reached it: a see-through spot on the film's page.
 * The picture ends where the title block does, so no row under that block can.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], qualifiers = "w360dp-h800dp-mdpi") // 1 dp = 1 px
class PinnedBannerTest {
    @get:Rule val compose = createComposeRule()
    private val list = LazyListState()
    private val ground = Color.Black
    private val picture = Color.Red
    private val block = Color.Blue

    /** The picture (520 high), a short title block (60), a row that paints nothing (300), and more page under it. */
    private fun show(blockHeight: Int = 60) = compose.setContent {
        Box(Modifier.fillMaxSize().background(ground)) {
            LazyColumn(state = list, modifier = Modifier.fillMaxSize()) {
                item(key = "banner") { PinnedBanner(list) { Box(Modifier.fillMaxSize().background(picture)) } }
                item(key = "info") { Box(Modifier.fillMaxWidth().height(blockHeight.dp).background(block)) }
                item(key = "row") { Box(Modifier.fillMaxWidth().height(300.dp)) }
                item(key = "rest") { Box(Modifier.fillMaxWidth().height(3000.dp)) }
            }
        }
    }

    private fun scrolledBy(px: Int) {
        compose.runOnIdle { runBlocking { list.scrollToItem(0, px) } }
        compose.waitForIdle()
    }

    private fun at(y: Int): Color = compose.onRoot().captureToImage().toPixelMap()[180, y]

    @Test @GraphicsMode(GraphicsMode.Mode.NATIVE)
    fun aRowThatPaintsNothingDoesNotShowThePicture() {
        show()
        // Slid up by 200: the block is at 320 to 380, the bare row from 380 to 680, and the
        // picture, still there at over a third of its strength, reaches down to 520.
        scrolledBy(200)
        assertEquals("the title block, solid", block, at(350))
        assertEquals("the bare row over where the picture reaches: the page's ground, not the picture", ground, at(450))
        assertEquals(ground, at(385))
        assertEquals(ground, at(515))
        // Above the block the picture is still what is seen, darkened by the slide.
        val above = at(100)
        assertTrue("the picture above the title block, dimmed: $above", above.red > 0.25f && above.red < 0.5f && above.blue == 0f)
        assertEquals("right down to the block", above, at(318))
    }

    @Test @GraphicsMode(GraphicsMode.Mode.NATIVE)
    fun atRestThePictureIsWhole() {
        show()
        scrolledBy(0)
        assertEquals(picture, at(10))
        assertEquals("to its last line", picture, at(519))
        assertEquals(block, at(540))
    }

    /** Slid far enough the picture is gone altogether, as before. */
    @Test @GraphicsMode(GraphicsMode.Mode.NATIVE)
    fun slidFarThePictureIsGone() {
        show()
        scrolledBy(330)
        assertEquals(ground, at(100))
        assertEquals(ground, at(300))
    }

    /** Where the title block ends, as the cut needs it: nothing while the block is off the page's top. */
    @Test fun theFootIsTheTitleBlocksEnd() {
        show(blockHeight = 90)
        scrolledBy(0)
        assertEquals(610f, bannerFoot(list))
        scrolledBy(200)
        assertEquals(410f, bannerFoot(list))
        compose.runOnIdle { runBlocking { list.scrollToItem(2, 0) } }
        compose.waitForIdle()
        assertEquals(null, bannerFoot(list))
    }
}
