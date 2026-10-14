package tv.roxy.tv.ui.theme

import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color
import androidx.tv.material3.MaterialTheme
import androidx.tv.material3.darkColorScheme

/** Roxy's palette: near-black background, soft grey surfaces, one cool accent for focus and actions. */
object RoxyColors {
    val background = Color(0xFF0B0B0F)
    val surface = Color(0xFF17171E)
    val surfaceHigh = Color(0xFF23232C)
    val accent = Color(0xFF4F8CFF)
    val onBackground = Color(0xFFF2F2F5)
    val muted = Color(0xFF9AA0A6)
    val focusBorder = Color.White
    val progress = Color(0xFFE5484D)
    val error = Color(0xFFFF6B6B)
}

@Composable
fun RoxyTheme(content: @Composable () -> Unit) {
    MaterialTheme(
        colorScheme = darkColorScheme(
            primary = RoxyColors.accent,
            onPrimary = Color.White,
            background = RoxyColors.background,
            onBackground = RoxyColors.onBackground,
            surface = RoxyColors.surface,
            onSurface = RoxyColors.onBackground,
            surfaceVariant = RoxyColors.surfaceHigh,
            onSurfaceVariant = RoxyColors.muted,
            border = Color(0xFF3A3A44),
            error = RoxyColors.error,
        ),
        content = content,
    )
}
