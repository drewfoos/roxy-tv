package tv.roxy.tv.player

import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.graphics.BlendMode
import androidx.compose.ui.graphics.Color

/**
 * Draws its content and then takes [fraction] of it away again, down to nothing: at 1 the
 * window shows whatever lies behind it there. [fraction] is read while drawing, so a value
 * that animates redraws and recomposes nothing.
 *
 * It is for the app's window on its way into picture-in-picture. The system makes the PiP
 * window by shrinking the app's whole window and cropping it to the picture only bit by
 * bit, so the rest of the page (a tall dark card under the video, with or without its text)
 * rides along most of the way. Taken away like this, the page is see-through: the home
 * screen shows where it was and the video alone flies to its corner. The video is not
 * touched: it is a surface of its own behind the window, seen through a hole in it, and a
 * hole stays a hole.
 *
 * Put it outside everything it should clear, with no layer between it and the window: the
 * erasing must land on the window itself.
 */
fun Modifier.seeThrough(fraction: () -> Float): Modifier = drawWithContent {
    drawContent()
    val f = fraction().coerceIn(0f, 1f)
    if (f > 0f) drawRect(Color.Black, alpha = f, blendMode = BlendMode.DstOut)
}
