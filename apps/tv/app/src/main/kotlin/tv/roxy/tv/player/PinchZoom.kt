package tv.roxy.tv.player

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.tween
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.Modifier
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.input.pointer.PointerInputChange
import androidx.compose.ui.input.pointer.changedToDown
import androidx.compose.ui.input.pointer.changedToUp
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.input.pointer.positionChanged
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch
import kotlin.math.max
import kotlin.math.round

/**
 * Pinch zoom the way YouTube's player does it:
 *
 *  - two fingers zoom about the point between them and pan; one finger pans once zoomed;
 *  - the level persists after the fingers lift and is shown as "1.5×";
 *  - the level is quantised to 0.1× steps, so finger tremor does not make it jitter;
 *  - letting go at or near 1× eases back to normal;
 *  - the player is updated once per frame, never once per touch event.
 *
 * [onZoom] receives (factor, panX, panY) in mpv's units: factor 1..[maxZoom], pan as a
 * fraction of the scaled video size. [onLevel] gets the level and whether fingers are
 * down, for the readout. [videoAspect] is the fitted picture's width/height (null when
 * unknown or when it already fills the screen); it keeps panning inside the picture.
 */
@Composable
fun Modifier.pinchZoom(
    onZoom: (factor: Double, panX: Double, panY: Double) -> Unit,
    onLevel: (level: Float, active: Boolean) -> Unit,
    onPinchStart: () -> Unit,
    videoAspect: () -> Float?,
    maxZoom: Float = 8f,
): Modifier {
    val scope = rememberCoroutineScope()
    val state = remember { ZoomState(scope) }
    state.onZoom = onZoom
    state.onLevel = onLevel
    state.onPinchStart = onPinchStart
    state.videoAspect = videoAspect
    state.maxZoom = maxZoom

    return this.onSizeChanged { state.resetIfResized(it.width, it.height) }.pointerInput(state) {
        awaitEachGesture {
            val first = awaitFirstDown(requireUnconsumed = false)
            val slop = viewConfiguration.touchSlop
            var started = false
            var singleDrag = Offset.Zero
            // Only fingers that went down in this gesture count. After the system steals a
            // touch (status-bar swipe) Compose can keep a stale "pressed" pointer around,
            // and counting it would turn the next tap into a two-finger zoom.
            val live = mutableSetOf(first.id)
            do {
                val event = awaitPointerEvent()
                event.changes.forEach { if (it.changedToDown()) live += it.id; if (it.changedToUp()) live -= it.id }
                val fingers = event.changes.filter { it.pressed && it.id in live }
                if (fingers.isEmpty()) break
                val zoom = zoomOf(fingers)
                val pan = panOf(fingers)
                if (!started) {
                    if (fingers.size >= 2) {
                        started = true
                    } else if (state.scale > 1f) {
                        // One finger only pans when already zoomed in, and only after slop,
                        // so taps and double taps still reach the controls.
                        singleDrag += pan
                        if (singleDrag.getDistance() > slop) started = true else continue
                    } else continue
                    state.begin(size.width, size.height)
                }
                state.apply(zoom, pan, centroidOf(fingers, previous = true))
                fingers.forEach { if (it.positionChanged()) it.consume() }
            } while (true)
            if (started) state.end()
        }
    }
}

// The same maths as Compose's calculateZoom/calculatePan/calculateCentroid, over a chosen
// subset of pointers.
private fun centroidOf(fingers: List<PointerInputChange>, previous: Boolean): Offset {
    var sum = Offset.Zero
    fingers.forEach { sum += if (previous) it.previousPosition else it.position }
    return sum / fingers.size.toFloat()
}

private fun spreadOf(fingers: List<PointerInputChange>, previous: Boolean): Float {
    val c = centroidOf(fingers, previous)
    var sum = 0f
    fingers.forEach { sum += ((if (previous) it.previousPosition else it.position) - c).getDistance() }
    return sum / fingers.size
}

private fun zoomOf(fingers: List<PointerInputChange>): Float {
    if (fingers.size < 2) return 1f
    val before = spreadOf(fingers, previous = true)
    val after = spreadOf(fingers, previous = false)
    return if (before == 0f || after == 0f) 1f else after / before
}

private fun panOf(fingers: List<PointerInputChange>): Offset =
    centroidOf(fingers, previous = false) - centroidOf(fingers, previous = true)

private class ZoomState(private val scope: CoroutineScope) {
    var scale = 1f
    var offset = Offset.Zero // video centre relative to the view centre, screen px
    var maxZoom = 8f
    var onZoom: (Double, Double, Double) -> Unit = { _, _, _ -> }
    var onLevel: (Float, Boolean) -> Unit = { _, _ -> }
    var onPinchStart: () -> Unit = {}
    var videoAspect: () -> Float? = { null }

    private var raw = 1f // unrounded scale while a gesture is in progress
    private var w = 1; private var h = 1 // view size
    private var fw = 1f; private var fh = 1f // fitted picture size at 1×
    private var snapJob: Job? = null
    private var sizeSeen: Pair<Int, Int>? = null

    /** The view was resized (16:9 box <-> full screen): back to 1x, matching the player's own reset. */
    fun resetIfResized(viewW: Int, viewH: Int) {
        val size = viewW to viewH
        if (sizeSeen != null && sizeSeen != size) {
            snapJob?.cancel()
            val wasZoomed = scale != 1f
            scale = 1f; raw = 1f; offset = Offset.Zero
            if (wasZoomed) onLevel(1f, false) // no "1.0x" flash when there was nothing to reset
        }
        sizeSeen = size
    }
    private var pushJob: Job? = null
    private val anim = Animatable(0f)

    fun begin(viewW: Int, viewH: Int) {
        snapJob?.cancel()
        w = viewW; h = viewH
        val a = videoAspect()
        val va = w.toFloat() / h
        if (a == null || a <= 0f) { fw = w.toFloat(); fh = h.toFloat() }
        else if (a > va) { fw = w.toFloat(); fh = w / a } else { fh = h.toFloat(); fw = h * a }
        raw = scale
        onPinchStart()
    }

    fun apply(zoom: Float, pan: Offset, centroid: Offset) {
        val old = scale
        // Accumulate the finger spread continuously, but apply it in 0.1× steps so the
        // level reads as a number and tremor does not creep. (Comparing each event against
        // the rounded level instead would swallow every small movement.)
        raw = (raw * zoom).coerceIn(1f, maxZoom)
        val new = round(raw * 10f) / 10f
        // Keep the point between the fingers fixed while scaling about it, then pan.
        val c = Offset(centroid.x - w / 2f, centroid.y - h / 2f)
        set(new, (offset - c) * (new / old) + c + pan)
        onLevel(new, true)
        if (pushJob?.isActive != true) {
            pushJob = scope.launch { withFrameNanos { }; push() }
        }
    }

    fun end() {
        onLevel(scale, false)
        if (scale > 1.15f) return
        // Let go near normal size: ease back to 1×.
        snapJob = scope.launch {
            val from = scale
            val fromOff = offset
            anim.snapTo(0f)
            anim.animateTo(1f, tween(200)) {
                set(from + (1f - from) * value, fromOff * (1f - value))
                push()
            }
            set(1f, Offset.Zero)
            raw = 1f
            push()
            onLevel(1f, false)
        }
    }

    /** Apply a scale and offset, keeping the picture over the view (no area outside it shown). */
    private fun set(s: Float, off: Offset) {
        val bx = max(0f, (fw * s - w) / 2f)
        val by = max(0f, (fh * s - h) / 2f)
        scale = s
        offset = Offset(off.x.coerceIn(-bx, bx), off.y.coerceIn(-by, by))
    }

    private fun push() {
        // mpv pan is a fraction of the scaled picture size.
        val s = scale
        onZoom(s.toDouble(), (offset.x / (fw * s)).toDouble(), (offset.y / (fh * s)).toDouble())
    }
}
