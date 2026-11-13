package tv.roxy.tv.ui.components

import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import tv.roxy.tv.ui.theme.RoxyColors

/**
 * Roxy's loading circle: a faint ring with a quarter of it lit, turning (the web app's
 * `.spinner` is the same drawing). For a wait with nothing to draw a placeholder of,
 * because what comes is not known: how many films a series has, say. Where the shape of
 * what comes IS known, a skeleton takes its place instead.
 */
@Composable
fun LoadingCircle(size: Dp, modifier: Modifier = Modifier, color: Color = RoxyColors.accent, stroke: Dp = size / 8, tag: String = "loading-circle") {
    val turn by rememberInfiniteTransition(label = "loading-circle").animateFloat(
        initialValue = 0f, targetValue = 360f,
        animationSpec = infiniteRepeatable(tween(durationMillis = 900, easing = LinearEasing), RepeatMode.Restart), label = "turn",
    )
    Canvas(modifier = modifier.size(size).testTag(tag)) {
        val w = stroke.coerceAtLeast(1.5.dp).toPx()
        val inset = Offset(w / 2, w / 2)
        val box = Size(this.size.width - w, this.size.height - w)
        drawArc(color.copy(alpha = color.alpha * 0.25f), startAngle = 0f, sweepAngle = 360f, useCenter = false, topLeft = inset, size = box, style = Stroke(w))
        drawArc(color, startAngle = turn - 90f, sweepAngle = 90f, useCenter = false, topLeft = inset, size = box, style = Stroke(w, cap = StrokeCap.Round))
    }
}
