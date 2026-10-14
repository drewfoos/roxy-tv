package tv.roxy.tv.ui.phone

import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Shape
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import tv.roxy.tv.api.Item
import tv.roxy.tv.api.RoxyApi
import tv.roxy.tv.ui.components.ProgressBar
import tv.roxy.tv.ui.components.RoxyImage
import tv.roxy.tv.ui.components.widthPx
import tv.roxy.tv.ui.theme.RoxyColors

/*
 * Phone building blocks, Netflix-style: Material 3 supplies the theme, ripples, bottom
 * bar, dialogs and switches; the pieces that give the look (hero card, poster rows, the
 * white Play button, episode rows) are drawn here by hand so nothing reads as stock
 * Material.
 */

val PhonePad = 16.dp
val PosterW = 108.dp
val ThumbW = 156.dp

/** Netflix's secondary-button grey and chip grey. */
val ButtonGrey = Color(0xFF2B2B2B)
val ChipGrey = Color(0xFF333333)

@Composable
fun PhoneTheme(content: @Composable () -> Unit) {
    MaterialTheme(
        colorScheme = darkColorScheme(
            primary = Color.White,
            onPrimary = Color.Black,
            secondary = RoxyColors.accent,
            background = RoxyColors.background,
            onBackground = RoxyColors.onBackground,
            surface = RoxyColors.surface,
            onSurface = RoxyColors.onBackground,
            surfaceVariant = RoxyColors.surfaceHigh,
            onSurfaceVariant = RoxyColors.muted,
            error = RoxyColors.error,
        ),
        content = content,
    )
}

/** 2:3 poster with the watch-progress bar; no caption (Netflix). */
@Composable
fun PosterTile(item: Item, api: RoxyApi, modifier: Modifier = Modifier, onClick: () -> Unit) {
    Box(
        modifier = modifier.aspectRatio(2f / 3f).clip(RoundedCornerShape(6.dp)).background(RoxyColors.surfaceHigh).clickable(onClick = onClick),
    ) {
        RoxyImage(api.imageUrl(item, "poster", widthPx(PosterW) * 2), item.title, modifier = Modifier.fillMaxSize(), placeholderTextSize = 12)
        ProgressBar(item.progressFraction, modifier = Modifier.align(Alignment.BottomCenter))
    }
}

/** 16:9 episode thumb with a play badge, progress and a two-line caption (Continue Watching). */
@Composable
fun ThumbTile(item: Item, api: RoxyApi, width: Dp = ThumbW, onClick: () -> Unit) {
    Column(modifier = Modifier.width(width)) {
        Box(
            modifier = Modifier.fillMaxWidth().aspectRatio(16f / 9f).clip(RoundedCornerShape(6.dp)).background(RoxyColors.surfaceHigh).clickable(onClick = onClick),
        ) {
            RoxyImage(api.imageUrl(item, "thumb", widthPx(width) * 2), item.episodeCode.ifEmpty { item.title }, modifier = Modifier.fillMaxSize(), placeholderTextSize = 12)
            PlayBadge(size = 38.dp, modifier = Modifier.align(Alignment.Center))
            ProgressBar(item.progressFraction, modifier = Modifier.align(Alignment.BottomCenter))
        }
        Text(item.seriesTitle ?: item.title, fontSize = 13.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 6.dp))
        Text(listOf(item.episodeCode, item.title).filter { it.isNotEmpty() }.joinToString("  "), fontSize = 12.sp, color = RoxyColors.muted, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

/** Translucent ring with a play triangle, drawn over thumbnails. */
@Composable
fun PlayBadge(size: Dp = 40.dp, modifier: Modifier = Modifier) {
    Box(
        modifier = modifier.size(size).clip(CircleShape).background(Color(0x66000000)).border(1.5.dp, Color.White.copy(alpha = 0.9f), CircleShape),
        contentAlignment = Alignment.Center,
    ) {
        Icon(Icons.Filled.PlayArrow, contentDescription = null, tint = Color.White, modifier = Modifier.size(size * 0.6f))
    }
}

/** Titled horizontal row: thumbs for episodes, posters for everything else. */
@Composable
fun PhoneRow(title: String, items: List<Item>, api: RoxyApi, onClick: (Item) -> Unit, modifier: Modifier = Modifier) {
    Column(modifier = modifier.fillMaxWidth()) {
        Text(title, fontSize = 17.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(start = PhonePad, bottom = 8.dp))
        LazyRow(contentPadding = PaddingValues(horizontal = PhonePad), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            items(items, key = { it.id }) { item ->
                if (item.isEpisode) ThumbTile(item, api, onClick = { onClick(item) })
                else PosterTile(item, api, modifier = Modifier.width(PosterW), onClick = { onClick(item) })
            }
        }
    }
}

/** White primary button: black icon and bold label. [pill] rounds it fully (the hero); otherwise 4 dp corners (Netflix). */
@Composable
fun PlayButton(
    text: String,
    modifier: Modifier = Modifier,
    icon: ImageVector = Icons.Filled.PlayArrow,
    height: Dp = 42.dp,
    pill: Boolean = false,
    container: Color = Color.White,
    content: Color = Color.Black,
    onClick: () -> Unit,
) {
    Row(
        modifier = modifier.height(height).clip(RoundedCornerShape(if (pill) height / 2 else 4.dp)).background(container).clickable(onClick = onClick).padding(horizontal = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.Center,
    ) {
        Icon(icon, contentDescription = null, tint = content, modifier = Modifier.size(24.dp))
        Spacer(Modifier.width(6.dp))
        Text(text, color = content, fontSize = 15.sp, fontWeight = FontWeight.Bold, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

/** Dark text on the brand blue (the hat's colour), like Crunchyroll's black-on-orange. */
val OnBrand = Color(0xFF0A1230)

/** Netflix's grey secondary button. */
@Composable
fun GreyButton(text: String, modifier: Modifier = Modifier, icon: ImageVector? = null, onClick: () -> Unit) {
    Row(
        modifier = modifier.height(42.dp).clip(RoundedCornerShape(4.dp)).background(ButtonGrey).clickable(onClick = onClick).padding(horizontal = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.Center,
    ) {
        if (icon != null) {
            Icon(icon, contentDescription = null, tint = Color.White, modifier = Modifier.size(22.dp))
            Spacer(Modifier.width(6.dp))
        }
        Text(text, color = Color.White, fontSize = 15.sp, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

/** Pill chip: white when selected, dark grey otherwise; [trailing] adds an icon (dropdown arrow). */
@Composable
fun Chip(text: String, selected: Boolean, trailing: ImageVector? = null, onClick: () -> Unit) {
    Row(
        modifier = Modifier.height(34.dp).clip(RoundedCornerShape(17.dp)).background(if (selected) Color.White else ChipGrey).clickable(onClick = onClick).padding(start = 16.dp, end = if (trailing != null) 8.dp else 16.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(text, color = if (selected) Color.Black else Color.White, fontSize = 13.sp, fontWeight = FontWeight.SemiBold, maxLines = 1)
        if (trailing != null) Icon(trailing, contentDescription = null, tint = if (selected) Color.Black else Color.White, modifier = Modifier.size(20.dp))
    }
}

/** Round translucent back button drawn over a backdrop. */
@Composable
fun BackCircle(onClick: () -> Unit, modifier: Modifier = Modifier) {
    Box(
        modifier = modifier.size(38.dp).clip(CircleShape).background(Color(0x99000000)).clickable(onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "Back", tint = Color.White, modifier = Modifier.size(22.dp))
    }
}

/** Section heading inside a detail page or Settings. */
@Composable
fun SectionTitle(text: String, modifier: Modifier = Modifier) {
    Text(text, fontSize = 17.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = modifier.padding(horizontal = PhonePad, vertical = 8.dp))
}

/** Small "watched" tick shown on episodes the user has finished. */
@Composable
fun WatchedCheck(modifier: Modifier = Modifier, size: Dp = 16.dp) {
    Icon(Icons.Filled.CheckCircle, contentDescription = "Watched", tint = Color.White, modifier = modifier.size(size))
}

/** Roxy's app mark: a small rounded accent square with an R, drawn over the hero. */
@Composable
fun RoxyMark(modifier: Modifier = Modifier, size: Dp = 28.dp) {
    Box(modifier = modifier.size(size).clip(RoundedCornerShape(size / 4)).background(RoxyColors.accent), contentAlignment = Alignment.Center) {
        Text("R", color = Color.White, fontSize = (size.value * 0.62f).sp, fontWeight = FontWeight.Black)
    }
}

/* ---------- Skeletons ----------
 * Shimmering placeholders shaped like the content they stand in for, so a screen never
 * says "Loading…". They will change as the layouts change; keep each next to the screen
 * it mirrors. */

@Composable
fun shimmerBrush(): Brush {
    val transition = rememberInfiniteTransition(label = "shimmer")
    val x by transition.animateFloat(
        initialValue = 0f, targetValue = 1f,
        animationSpec = infiniteRepeatable(tween(1300, easing = LinearEasing)), label = "shimmerX",
    )
    val width = 900f
    val start = -width + x * (2400f + width)
    return Brush.linearGradient(
        colors = listOf(Color(0xFF1C1C24), Color(0xFF2A2A34), Color(0xFF1C1C24)),
        start = Offset(start, 0f), end = Offset(start + width, 0f),
    )
}

/** One shimmering block. */
@Composable
fun Bone(modifier: Modifier, shape: Shape = RoundedCornerShape(6.dp)) {
    Box(modifier = modifier.clip(shape).background(shimmerBrush()))
}

/** A text line placeholder. */
@Composable
fun BoneLine(width: Dp, height: Dp = 14.dp, modifier: Modifier = Modifier) {
    Bone(modifier.width(width).height(height), shape = RoundedCornerShape(4.dp))
}

/** Skeleton for a poster row: title line plus three and a half posters. */
@Composable
fun RowSkeleton(modifier: Modifier = Modifier, thumbs: Boolean = false) {
    Column(modifier = modifier.fillMaxWidth()) {
        BoneLine(width = 150.dp, height = 18.dp, modifier = Modifier.padding(start = PhonePad, bottom = 10.dp))
        Row(modifier = Modifier.padding(start = PhonePad), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            repeat(4) {
                if (thumbs) Bone(Modifier.width(ThumbW).aspectRatio(16f / 9f))
                else Bone(Modifier.width(PosterW).aspectRatio(2f / 3f))
            }
        }
    }
}

/** Skeleton for a 3-column poster grid. */
@Composable
fun GridSkeleton(modifier: Modifier = Modifier, rows: Int = 4) {
    Column(modifier = modifier.fillMaxWidth().padding(horizontal = PhonePad), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        repeat(rows) {
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                repeat(3) { Bone(Modifier.weight(1f).aspectRatio(2f / 3f)) }
            }
        }
    }
}
