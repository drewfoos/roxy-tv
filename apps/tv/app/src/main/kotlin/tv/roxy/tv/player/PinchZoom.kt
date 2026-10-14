package tv.roxy.tv.player

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.tween
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.gestures.calculateCentroid
import androidx.compose.foundation.gestures.calculatePan
import androidx.compose.foundation.gestures.calculateZoom
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.input.pointer.positionChanged
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch
import kotlin.math.round

/**
 * Pinch zoom the way YouTube's player does it:
 *
 *  - two fingers zoom about the point between them and pan; one finger pans once zoomed;
 *  - the level persists after the fingers lift and is shown as "1.5×";
 *  - the level is quantised to 0.1× steps, so finger tremor does not make it jitter;
 *  - letting go at or near 1× snaps the picture back to normal;
 *  - the player is updated once per frame, never once per touch event.
 *
 * [onZoom] receives (factor, panX, panY) in mpv's units: factor 1..[maxZoom], pan as a
 * fraction of the scaled video size. [onLevel] gets the display level and whether a
 * gesture is in progress, for the readout.
 */
@Composable
fun Modifier.pinchZoom(
    onZoom: (factor: Double, panX: Double, panY: Double) -> Unit,
    onLevel: (level: Float, active: Boolean) -> Unit,
    onPinchStart: () -> Unit,
    maxZoom: Float = 8f,
): Modifier {
    val scope = rememberCoroutineScope()
    val state = remember { ZoomState(scope) }
    state.onZoom = onZoom
    state.onLevel = onLevel
    state.onPinchStart = onPinchStart
    state.maxZoom = maxZoom

    return this.pointerInput(state) {
        awaitEachGesture {
            awaitFirstDown(requireUnconsumed = false)
            val slop = viewConfiguration.touchSlop
            var started = false
            var singleDrag = Offset.Zero
            do {
                val event = awaitPointerEvent()
                val pressed = event.changes.count { it.pressed }
                if (pressed == 0) break
                val zoom = event.calculateZoom()
                val pan = event.calculatePan()
                if (!started) {
                    if (pressed >= 2) {
                        started = true
                    } else if (state.scale > 1f) {
                        // One finger only pans when already zoomed in, and only after slop,
                        // so taps and double taps still reach the controls.
                        singleDrag += pan
                        if (singleDrag.getDistance() > slop) started = true else continue
                    } else continue
                    state.begin()
                }
                val centroid = event.calculateCentroid(useCurrent = false)
                state.apply(zoom, pan, centroid, size.width, size.height)
                event.changes.forEach { if (it.positionChanged()) it.consume() }
            } while (true)
            if (started) state.end(size.width, size.height)
        }
    }
}

private class ZoomState(private val scope: CoroutineScope) {
    var scale = 1f
    private var raw = 1f // unrounded scale while a gesture is in progress
    var offset = Offset.Zero // video centre relative to the view centre, screen px
    var maxZoom = 8f
    var onZoom: (Double, Double, Double) -> Unit = { _, _, _ -> }
    var onLevel: (Float, Boolean) -> Unit = { _, _ -> }
    var onPinchStart: () -> Unit = {}
    private var snapJob: Job? = null
    private var pushJob: Job? = null
    private val anim = Animatable(0f)

    fun begin() {
        snapJob?.cancel()
        raw = scale
        onPinchStart()
    }

    fun apply(zoom: Float, pan: Offset, centroid: Offset, w: Int, h: Int) {
        val old = scale
        // Accumulate the finger spread continuously, but apply it in 0.1× steps so the
        // level reads as a number and tremor does not creep. (Comparing each event against
        // the rounded level instead would swallow every small movement.)
        raw = (raw * zoom).coerceIn(1f, maxZoom)
        val new = round(raw * 10f) / 10f
        // Keep the point between the fingers fixed while scaling about it, then pan.
        val c = Offset(centroid.x - w / 2f, centroid.y - h / 2f)
        var off = (offset - c) * (new / old) + c + pan
        // Never reveal the area outside the picture.
        val bx = (new - 1f) * w / 2f
        val by = (new - 1f) * h / 2f
        off = Offset(off.x.coerceIn(-bx, bx), off.y.coerceIn(-by, by))
        scale = new
        offset = off
        onLevel(new, true)
        if (pushJob?.isActive != true) {
            pushJob = scope.launch { withFrameNanos { }; push(w, h) }
        }
    }

    fun end(w: Int, h: Int) {
        onLevel(scale, false)
        if (scale > 1.15f) return
        // Let go near normal size: ease back to 1×.
        snapJob = scope.launch {
            val from = scale
            val fromOff = offset
            anim.snapTo(0f)
            anim.animateTo(1f, tween(200)) {
                scale = from + (1f - from) * value
                offset = fromOff * (1f - value)
                push(w, h)
            }
            scale = 1f
            raw = 1f
            offset = Offset.Zero
            push(w, h)
            onLevel(1f, false)
        }
    }

    private fun push(w: Int, h: Int) {
        // mpv pan is a fraction of the scaled video size; the picture spans the view width
        // (landscape, fit), so scaled size ≈ view size × scale on each axis.
        val s = scale
        onZoom(s.toDouble(), (offset.x / (w * s)).toDouble(), (offset.y / (h * s)).toDouble())
    }
}
