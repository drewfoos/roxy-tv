package tv.roxy.tv.ui.phone

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.drawscope.Fill
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp

/**
 * Roxy's mark: a pointed witch hat (Roxy Migurdia's), drawn as a vector so it scales
 * crisply at any size. A soft, slightly drooping cone shaded from the accent blue into a
 * deep navy, a wide brim, a gold band with a small buckle and a sparkle at the tip.
 */
@Composable
fun WitchHat(modifier: Modifier = Modifier, size: Dp = 30.dp) {
    Canvas(modifier = modifier.size(size)) {
        val w = this.size.width
        val h = this.size.height
        val light = Color(0xFF7FB0FF)
        val mid = Color(0xFF3F74E6)
        val dark = Color(0xFF1C2E6B)
        val gold = Color(0xFFF2C35B)

        // Brim: wide ellipse, dark underside with a lighter top edge.
        drawOval(Brush.verticalGradient(listOf(mid, dark)), topLeft = Offset(w * 0.02f, h * 0.66f), size = Size(w * 0.96f, h * 0.26f))
        drawOval(light.copy(alpha = 0.35f), topLeft = Offset(w * 0.06f, h * 0.67f), size = Size(w * 0.88f, h * 0.12f))

        // Cone: base on the brim, tip top-right with a droop.
        val cone = Path().apply {
            moveTo(w * 0.24f, h * 0.74f)
            cubicTo(w * 0.33f, h * 0.44f, w * 0.44f, h * 0.22f, w * 0.60f, h * 0.02f)
            cubicTo(w * 0.70f, h * 0.10f, w * 0.72f, h * 0.20f, w * 0.65f, h * 0.30f)
            cubicTo(w * 0.70f, h * 0.48f, w * 0.73f, h * 0.62f, w * 0.76f, h * 0.74f)
            close()
        }
        drawPath(cone, Brush.linearGradient(listOf(light, mid, dark), start = Offset(w * 0.3f, h * 0.2f), end = Offset(w * 0.8f, h * 0.8f)), style = Fill)
        // A fold line on the cone for some depth.
        val fold = Path().apply {
            moveTo(w * 0.50f, h * 0.26f)
            cubicTo(w * 0.52f, h * 0.42f, w * 0.50f, h * 0.56f, w * 0.46f, h * 0.70f)
        }
        drawPath(fold, dark.copy(alpha = 0.45f), style = Stroke(width = w * 0.025f))

        // Band with a buckle.
        val band = Path().apply {
            moveTo(w * 0.27f, h * 0.64f)
            cubicTo(w * 0.44f, h * 0.72f, w * 0.58f, h * 0.72f, w * 0.74f, h * 0.64f)
            lineTo(w * 0.76f, h * 0.74f)
            cubicTo(w * 0.58f, h * 0.82f, w * 0.42f, h * 0.82f, w * 0.24f, h * 0.74f)
            close()
        }
        drawPath(band, gold, style = Fill)
        drawRect(dark, topLeft = Offset(w * 0.47f, h * 0.665f), size = Size(w * 0.08f, h * 0.10f))
        drawRect(gold, topLeft = Offset(w * 0.49f, h * 0.685f), size = Size(w * 0.04f, h * 0.06f))

        // Sparkle at the tip.
        val sx = w * 0.72f
        val sy = h * 0.08f
        val r = w * 0.07f
        drawLine(Color.White, Offset(sx - r, sy), Offset(sx + r, sy), strokeWidth = w * 0.02f)
        drawLine(Color.White, Offset(sx, sy - r), Offset(sx, sy + r), strokeWidth = w * 0.02f)
    }
}
