package tv.roxy.tv.player

import android.graphics.Rect

/**
 * What the system is told about the phone player's picture-in-picture window, and which page
 * may tell it. The activity owns one of these and sends the system whatever comes back.
 *
 * The settings belong to the page that claimed them last. One episode page opens another
 * (the Next Episode row), and the page left behind lives on until the new one has slid in:
 * its goodbye then wiped the new page's settings (a 4:3 episode opened that way got a 16:9
 * PiP window), and its player's last words could have done the same. Only the owner is
 * heard, and a page clears only what is still its own.
 */
class PipSettings {
    /**
     * What to tell the system. [source]: the picture's place in the window, which the
     * system shrinks the window towards (null: none worth sending, see
     * [PictureSurface.usableSource]). [autoEnter]: leaving the app now makes it the PiP
     * window (a video is playing).
     */
    data class Params(val shape: Pair<Int, Int>, val source: Rect?, val autoEnter: Boolean)

    private var owner: Any? = null
    private var onLeaving: (() -> Unit)? = null
    private var autoEnter: Boolean? = null
    private var source: Rect? = null
    /** The size of the window [source] was measured in (the whole one: never the PiP window). */
    private var sourceWindow: Pair<Int, Int> = 0 to 0
    private var shape: Pair<Int, Int> = DEFAULT_SHAPE

    /** A video is playing: leaving the app now would shrink it into the PiP window. */
    val armed: Boolean get() = autoEnter == true

    /** A page with a player takes the settings over. [onLeaving]: see [leaving]. */
    fun claim(owner: Any, onLeaving: () -> Unit) {
        this.owner = owner
        this.onLeaving = onLeaving
        autoEnter = null; source = null; sourceWindow = 0 to 0; shape = DEFAULT_SHAPE
    }

    /**
     * The picture's shape (the PiP window takes it, so it has no bands). Returns what to
     * send, or null when there is nothing to: not [owner]'s to set, no change, or the
     * player has not said yet whether it plays (its first word sends everything).
     */
    fun shape(owner: Any, shape: Pair<Int, Int>): Params? {
        if (this.owner !== owner || this.shape == shape) return null
        this.shape = shape
        return if (autoEnter == null) null else params()
    }

    /**
     * Whether the video plays, and where the picture is in a window of [windowWidth] by
     * [windowHeight]. The page reports the place only from the whole window: in the PiP
     * window it keeps the last one, which is also where the picture grows back to. Returns
     * what to send, or null when nothing changed or the settings are not [owner]'s.
     */
    fun playing(owner: Any, playing: Boolean, source: Rect?, windowWidth: Int, windowHeight: Int): Params? {
        if (this.owner !== owner) return null
        val changed = autoEnter != playing || this.source != source
        if (this.source != source) sourceWindow = windowWidth to windowHeight
        autoEnter = playing
        this.source = source?.let { Rect(it) }
        return if (changed) params() else null
    }

    /** The settings as they stand (for entering PiP by hand on Android 11 and older). */
    fun params(): Params = Params(
        shape = shape,
        source = source?.takeIf { PictureSurface.usableSource(it, shape, sourceWindow.first, sourceWindow.second) },
        autoEnter = autoEnter == true,
    )

    /** The page has gone. True when the settings were still its own: the system is then told "no PiP here". */
    fun clear(owner: Any): Boolean {
        if (this.owner !== owner) return false
        this.owner = null; onLeaving = null
        autoEnter = null; source = null; sourceWindow = 0 to 0; shape = DEFAULT_SHAPE
        return true
    }

    /** The app may be about to shrink into the PiP window (see MainActivity.onWindowFocusChanged). */
    fun leaving() {
        if (armed) onLeaving?.invoke()
    }

    private companion object {
        val DEFAULT_SHAPE = 16 to 9
    }
}
