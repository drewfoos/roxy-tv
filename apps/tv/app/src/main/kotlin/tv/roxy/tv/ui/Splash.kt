package tv.roxy.tv.ui

import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.material3.Text
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.Column
import tv.roxy.tv.ui.phone.WitchHat
import tv.roxy.tv.ui.theme.RoxyColors

/**
 * The launch screen: the witch hat breathing on the page colour with a soft glow behind
 * it, shown while the stored session is checked (and for at least a beat, so the app
 * never flashes straight from the icon into Home).
 */
@Composable
fun Splash() {
    val t = rememberInfiniteTransition(label = "splash")
    val pulse by t.animateFloat(0f, 1f, infiniteRepeatable(tween(1400, easing = LinearEasing), RepeatMode.Reverse), label = "pulse")
    val scale = 0.94f + 0.08f * pulse
    Box(modifier = Modifier.fillMaxSize().background(RoxyColors.background), contentAlignment = Alignment.Center) {
        Canvas(modifier = Modifier.size(260.dp)) {
            val r = size.minDimension / 2 * (0.55f + 0.25f * pulse)
            drawCircle(RoxyColors.accent.copy(alpha = 0.10f + 0.10f * pulse), radius = r)
            drawCircle(RoxyColors.accent.copy(alpha = 0.06f), radius = r * 1.35f)
        }
        Column(horizontalAlignment = Alignment.CenterHorizontally) {
            WitchHat(size = 96.dp, modifier = Modifier.graphicsLayer { scaleX = scale; scaleY = scale })
            Text("Roxy TV", color = RoxyColors.onBackground, fontSize = 22.sp, fontWeight = FontWeight.Bold, letterSpacing = 2.sp, modifier = Modifier.padding(top = 22.dp).graphicsLayer { alpha = 0.75f + 0.25f * pulse })
        }
    }
}
