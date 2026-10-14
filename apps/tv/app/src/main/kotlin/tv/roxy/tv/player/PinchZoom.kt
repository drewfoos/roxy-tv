package tv.roxy.tv.player

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.tween
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.calculateCentroid
import androidx.compose.foundation.gestures.calculatePan
import androidx.compose.foundation.gestures.calculateZoom
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.input.pointer.PointerEventPass
import androidx.compose.ui.input.pointer.pointerInput
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch

/**
 * YouTube-style pinch zoom: two fingers zoom the video up to [maxZoom] and pan it; when
 * the fingers lift, the picture animates back to normal. Single-finger taps and drags are
 * left alone (passed through to the tap/double-tap layer below), and the controls are
 * told to hide the moment a pinch starts.
 */
@Composable
fun Modifier.pinchZoom(
    onZoom: (factor: Double, panX: Double, panY: Double) -> Unit,
    onPinchStart: () -> Unit,
    maxZoom: Float = 8f,
): Modifier {
    val scope = rememberCoroutineScope()
    val zoom = remember { Animatable(1f) }
    val panX = remember { Animatable(0f) }
    val panY = remember { Animatable(0f) }
    var snapJob: Job? = remember { null }
    return this.pointerInput(Unit) {
        awaitEachGesture {
            var pinching = false
            var factor = zoom.value
            var px = panX.value
            var py = panY.value
            while (true) {
                val event = awaitPointerEvent(PointerEventPass.Initial)
                val down = event.changes.count { it.pressed }
                if (down >= 2) {
                    if (!pinching) {
                        pinching = true
                        snapJob?.cancel()
                        onPinchStart()
                    }
                    val z = event.calculateZoom()
                    val pan = event.calculatePan()
                    val centroid = event.calculateCentroid()
                    val newFactor = (factor * z).coerceIn(1f, maxZoom)
                    // Zoom about the fingers' centroid: keep the point under them fixed.
                    val cx = (centroid.x - size.width / 2f) / size.width
                    val cy = (centroid.y - size.height / 2f) / size.height
                    val ratio = newFactor / factor
                    px = (px - cx) * ratio + cx + pan.x / (size.width * newFactor)
                    py = (py - cy) * ratio + cy + pan.y / (size.height * newFactor)
                    factor = newFactor
                    // Keep the picture covering the screen: pan is bounded by (factor-1)/2.
                    val bound = (factor - 1f) / (2f * factor)
                    px = px.coerceIn(-bound, bound)
                    py = py.coerceIn(-bound, bound)
                    scope.launch { zoom.snapTo(factor); panX.snapTo(px); panY.snapTo(py) }
                    onZoom(factor.toDouble(), px.toDouble(), py.toDouble())
                    event.changes.forEach { it.consume() }
                } else if (pinching && down < 2) {
                    // Fingers lifted: animate back to normal.
                    event.changes.forEach { it.consume() }
                    snapJob = scope.launch {
                        val spec = tween<Float>(260)
                        val jobs = listOf(
                            launch { zoom.animateTo(1f, spec) { onZoom(value.toDouble(), panX.value.toDouble(), panY.value.toDouble()) } },
                            launch { panX.animateTo(0f, spec) },
                            launch { panY.animateTo(0f, spec) },
                        )
                        jobs.forEach { it.join() }
                        onZoom(1.0, 0.0, 0.0)
                    }
                    break
                }
                if (down == 0) break // single-finger gestures belong to the tap layer
            }
        }
    }
}
