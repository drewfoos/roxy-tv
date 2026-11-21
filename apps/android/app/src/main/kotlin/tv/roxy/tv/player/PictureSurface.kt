package tv.roxy.tv.player

import android.content.Context
import android.graphics.Point
import android.graphics.Rect
import android.os.Build
import android.view.WindowManager

/**
 * How the phone's player keeps its picture steady while its box changes size (turning the
 * phone, the fullscreen button, picture-in-picture).
 *
 * mpv draws with OpenGL into the surface, at the surface's size. Android video players are
 * different: a decoder fills a buffer of the VIDEO's size and the system scales it into
 * whatever box the app lays out, so a box that changes size (the system animates the PiP
 * window by scaling the app's surface) never asks the player for anything. Resizing mpv's
 * surface in step with the box is what went wrong before:
 *
 *  - mpv draws its first frame at a new size into a buffer that still has the old size (the
 *    graphics driver takes its next buffer right after a swap, before the size changes).
 *    Playing, that is one wrong frame (a flicker); paused, the wrong frame stayed.
 *  - a box that is not the picture's shape (the whole screen in fullscreen, with mpv
 *    drawing black bars inside it) cannot be handed to the system as the PiP source: the
 *    window is the picture's shape, the system falls back to a cross-fade through the app's
 *    icon, and a buffer kept from before was squeezed into the window.
 *
 * So the page's surface is given the PICTURE's shape and a fixed buffer ([renderSize]): the
 * picture as large as this screen ever shows it. The view is laid out at that shape in every
 * state (the box under the status bar, centred in fullscreen with the page painting the
 * bars, the whole PiP window) and the system scales the one buffer into it. Nothing reaches
 * mpv when the box changes; the buffer changes only with the picture's shape (another file).
 *
 * The TV's player keeps a surface that follows its view: its box never changes size.
 */
object PictureSurface {
    /**
     * The buffer for a picture of [shape] (width to height) on a display of [displayLong] by
     * [displayShort] pixels: [shape] fitted into the screen held sideways, which is the
     * largest the page ever shows it, so fullscreen is drawn pixel for pixel and the smaller
     * boxes are scaled down from it. Null when a size is not known.
     *
     * The derived side is rounded the way mpv's own fit truncates (it computes the height
     * from the width, then the width from the height, cutting fractions): up for a width
     * taken from the height, down for a height taken from the width. Rounded the other way
     * mpv found the picture one pixel short of the buffer and drew a black line beside it.
     */
    fun renderSize(shape: Pair<Int, Int>, displayLong: Int, displayShort: Int): Pair<Int, Int>? {
        val (sw, sh) = shape
        if (sw <= 0 || sh <= 0 || displayLong <= 0 || displayShort <= 0) return null
        return if (sw.toLong() * displayShort <= displayLong.toLong() * sh) {
            val w = (displayShort.toLong() * sw + sh - 1) / sh
            w.toInt().coerceIn(1, displayLong) to displayShort
        } else {
            displayLong to (displayLong.toLong() * sh / sw).toInt().coerceIn(1, displayShort)
        }
    }

    /**
     * The display's size in pixels, long side first, whatever the window is at the moment
     * (the PiP window and split screen report their own, smaller size as "the screen").
     */
    fun displaySides(context: Context): Pair<Int, Int> {
        val wm = context.getSystemService(Context.WINDOW_SERVICE) as? WindowManager
        val (w, h) = when {
            wm == null -> context.resources.displayMetrics.let { it.widthPixels to it.heightPixels }
            Build.VERSION.SDK_INT >= 30 -> runCatching { wm.maximumWindowMetrics.bounds.let { it.width() to it.height() } }
                .getOrElse { context.resources.displayMetrics.let { it.widthPixels to it.heightPixels } }
            else -> Point().also { @Suppress("DEPRECATION") wm.defaultDisplay.getRealSize(it) }.let { it.x to it.y }
        }
        return maxOf(w, h) to minOf(w, h)
    }

    /**
     * Whether [rect] (the picture's place in the window) can be the PiP source: the system
     * takes a source only when it lies inside the window, and animates from it only when it
     * has the PiP window's shape (within 0.01). Anything else gets the cross-fade through
     * the app's icon, so it is better not sent at all.
     */
    fun usableSource(rect: Rect?, shape: Pair<Int, Int>, windowWidth: Int, windowHeight: Int): Boolean {
        if (rect == null || rect.isEmpty || shape.first <= 0 || shape.second <= 0) return false
        if (rect.left < 0 || rect.top < 0 || rect.right > windowWidth || rect.bottom > windowHeight) return false
        val want = shape.first.toFloat() / shape.second
        return kotlin.math.abs(rect.width().toFloat() / rect.height() - want) <= 0.01f
    }
}

/**
 * Whether the page is in fullscreen, kept through picture-in-picture. The PiP window is a
 * small sideways box, so the window's own orientation says "landscape" there whatever the
 * page was; read as the page's, it hid the system bars, armed the fullscreen Back handler
 * and released the orientation lock while the app was a floating window. In PiP the answer
 * is the one from before it.
 */
class FullscreenMemory(initial: Boolean) {
    private var last = initial
    fun now(windowLandscape: Boolean, inPip: Boolean): Boolean {
        if (!inPip) last = windowLandscape
        return last
    }
}
