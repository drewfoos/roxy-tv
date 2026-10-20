package tv.roxy.tv.ui.components

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.collectIsFocusedAsState
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.tv.material3.Border
import androidx.tv.material3.Button
import androidx.tv.material3.ButtonDefaults
import androidx.tv.material3.Card
import androidx.tv.material3.CardDefaults
import androidx.tv.material3.MaterialTheme
import androidx.tv.material3.Text
import coil3.compose.AsyncImage
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import coil3.request.ImageRequest
import coil3.compose.LocalPlatformContext
import tv.roxy.tv.api.Item
import tv.roxy.tv.api.RoxyApi
import tv.roxy.tv.ui.theme.RoxyColors

/** Horizontal margin that keeps content inside TV overscan. */
val ScreenPadding = 48.dp

/**
 * tv-material clickables (Surface, Button, Card) only fire on D-pad Enter; they ignore pointer
 * taps. This adds a tap handler so the same screens work by touch on a phone. Tap gestures
 * never come from key events, so D-pad clicks are not doubled.
 */
fun Modifier.tapClick(onClick: () -> Unit): Modifier =
    pointerInput(onClick) { detectTapGestures(onTap = { onClick() }) }

val PosterWidth = 112.dp
val ThumbWidth = 176.dp

/** Image with a dark tile + title underneath; the tile stays visible when there is no image or it fails. */
/**
 * Remembers the exact URL (with its `?w=` size) each image path was last shown at, so a
 * bigger view of the same art can start from the bitmap Coil already holds in memory
 * instead of a grey box while the larger file arrives.
 */
object ImageMemo {
    private val last = LinkedHashMap<String, String>()

    fun saw(url: String) { synchronized(last) { last[url.substringBefore('?')] = url; if (last.size > 600) last.remove(last.keys.first()) } }

    /** The URL this image path was last loaded at, if any view has shown it. */
    fun lastFor(url: String?): String? = url?.let { synchronized(last) { last[it.substringBefore('?')] } }
}

/**
 * [placeholderKey]: a URL already loaded elsewhere (see [ImageMemo]); its cached bitmap
 * stands in until [url] lands.
 */
@Composable
fun RoxyImage(url: String?, title: String, modifier: Modifier = Modifier, placeholderTextSize: Int = 14, placeholderKey: String? = null) {
    if (url != null) ImageMemo.saw(url)
    // The title only stands in when there is no picture or it failed; while one loads the
    // card is a plain tile (Netflix / Crunchyroll), so text never flashes behind art.
    var failed by remember(url) { mutableStateOf(false) }
    Box(modifier = modifier.background(RoxyColors.surfaceHigh), contentAlignment = Alignment.Center) {
        if (url == null || failed) Text(
            title,
            color = RoxyColors.muted,
            fontSize = placeholderTextSize.sp,
            textAlign = TextAlign.Center,
            maxLines = 3,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.padding(8.dp),
        )
        if (url != null) {
            val context = LocalPlatformContext.current
            val request = remember(url, placeholderKey) {
                ImageRequest.Builder(context).data(url).apply { if (placeholderKey != null && placeholderKey != url) placeholderMemoryCacheKey(placeholderKey) }.build()
            }
            AsyncImage(model = request, contentDescription = title, contentScale = ContentScale.Crop, modifier = Modifier.fillMaxSize(), onError = { failed = true })
        }
    }
}

@Composable
fun ProgressBar(fraction: Float?, modifier: Modifier = Modifier) {
    if (fraction == null) return
    Box(modifier = modifier.fillMaxWidth().height(4.dp).background(Color(0x88000000))) {
        Box(modifier = Modifier.fillMaxHeight().fillMaxWidth(fraction).background(RoxyColors.progress))
    }
}

/** Convert a card width to a pixel size hint for the server's `?w=` resizing. */
@Composable
fun widthPx(width: Dp): Int {
    val density = LocalDensity.current
    return remember(width, density) { with(density) { width.roundToPx() } }
}

/** A focus-scaling card: poster 2:3 for series/movies/seasons, 16:9 thumb with series + SxxEyy for episodes. */
@Composable
fun MediaCard(item: Item, api: RoxyApi, onClick: () -> Unit, modifier: Modifier = Modifier) {
    val isThumb = item.isEpisode
    val width = if (isThumb) ThumbWidth else PosterWidth
    val imageType = if (isThumb) "thumb" else "poster"
    val url = api.imageUrl(item, imageType, widthPx(width) * 2)
    Column(modifier = modifier.width(width)) {
        Card(
            onClick = onClick,
            shape = CardDefaults.shape(RoundedCornerShape(6.dp)),
            scale = CardDefaults.scale(focusedScale = 1.1f),
            border = CardDefaults.border(focusedBorder = Border(BorderStroke(3.dp, RoxyColors.focusBorder), shape = RoundedCornerShape(6.dp))),
            colors = CardDefaults.colors(containerColor = RoxyColors.surfaceHigh),
            modifier = Modifier.width(width).aspectRatio(if (isThumb) 16f / 9f else 2f / 3f).tapClick(onClick),
        ) {
            Box(modifier = Modifier.fillMaxSize()) {
                RoxyImage(url, if (isThumb) item.episodeCode.ifEmpty { item.title } else item.title, modifier = Modifier.fillMaxSize())
                ProgressBar(item.progressFraction, modifier = Modifier.align(Alignment.BottomCenter))
            }
        }
        if (isThumb) {
            Text(item.seriesTitle ?: item.title, fontSize = 13.sp, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 6.dp))
            Text(listOf(item.episodeCode, item.title).filter { it.isNotEmpty() }.joinToString("  "), fontSize = 12.sp, color = RoxyColors.muted, maxLines = 1, overflow = TextOverflow.Ellipsis)
        } else {
            Text(item.title, fontSize = 13.sp, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 6.dp))
        }
    }
}

/** Titled horizontal row of [MediaCard]s. Vertical padding leaves room for the focus scale. */
@Composable
fun MediaRow(title: String, items: List<Item>, api: RoxyApi, onClick: (Item) -> Unit, modifier: Modifier = Modifier) {
    Column(modifier = modifier.fillMaxWidth()) {
        Text(title, style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold, modifier = Modifier.padding(start = ScreenPadding, bottom = 8.dp))
        LazyRow(
            contentPadding = PaddingValues(horizontal = ScreenPadding, vertical = 10.dp),
            horizontalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            items(items, key = { it.id }) { item ->
                MediaCard(item, api, onClick = { onClick(item) })
            }
        }
    }
}

/** Accent-coloured primary action (Play, Connect). */
@Composable
fun PrimaryButton(text: String, onClick: () -> Unit, modifier: Modifier = Modifier, enabled: Boolean = true) {
    Button(
        onClick = onClick,
        enabled = enabled,
        modifier = modifier.tapClick { if (enabled) onClick() },
        colors = ButtonDefaults.colors(
            containerColor = RoxyColors.accent,
            contentColor = Color.White,
            focusedContainerColor = Color.White,
            focusedContentColor = RoxyColors.background,
        ),
    ) { Text(text, fontWeight = FontWeight.SemiBold) }
}

/** Neutral secondary action. [selected] draws it as the chosen option of a choice row. */
@Composable
fun SecondaryButton(text: String, onClick: () -> Unit, modifier: Modifier = Modifier, selected: Boolean = false) {
    Button(
        onClick = onClick,
        modifier = modifier.tapClick(onClick),
        colors = ButtonDefaults.colors(
            containerColor = if (selected) RoxyColors.accent.copy(alpha = 0.35f) else RoxyColors.surfaceHigh,
            contentColor = RoxyColors.onBackground,
            focusedContainerColor = Color.White,
            focusedContentColor = RoxyColors.background,
        ),
        border = if (selected) ButtonDefaults.border(border = Border(BorderStroke(1.dp, RoxyColors.accent))) else ButtonDefaults.border(),
    ) { Text(text) }
}

/** Label + row of options; the selected one is highlighted. */
@Composable
fun ChoiceRow(label: String, options: List<String>, selected: String, onSelect: (String) -> Unit) {
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.padding(vertical = 4.dp)) {
        Text(label, modifier = Modifier.width(200.dp), color = RoxyColors.muted)
        options.forEach { opt -> SecondaryButton(opt, onClick = { onSelect(opt) }, selected = opt == selected) }
    }
}

/** Dark single-line text field with a visible focus border (tv-material has no TextField). */
@Composable
fun RoxyTextField(
    value: String,
    onValueChange: (String) -> Unit,
    label: String,
    modifier: Modifier = Modifier,
    password: Boolean = false,
    keyboardType: KeyboardType = KeyboardType.Text,
    imeAction: ImeAction = ImeAction.Next,
) {
    val interaction = remember { MutableInteractionSource() }
    val focused by interaction.collectIsFocusedAsState()
    Column(modifier = modifier) {
        Text(label, fontSize = 12.sp, color = RoxyColors.muted, modifier = Modifier.padding(bottom = 4.dp))
        BasicTextField(
            value = value,
            onValueChange = onValueChange,
            singleLine = true,
            interactionSource = interaction,
            textStyle = TextStyle(color = RoxyColors.onBackground, fontSize = 16.sp),
            cursorBrush = SolidColor(RoxyColors.accent),
            visualTransformation = if (password) PasswordVisualTransformation() else VisualTransformation.None,
            keyboardOptions = KeyboardOptions(keyboardType = keyboardType, imeAction = imeAction, autoCorrectEnabled = false),
            modifier = Modifier
                .fillMaxWidth()
                .clip(RoundedCornerShape(6.dp))
                .background(RoxyColors.surfaceHigh)
                .border(2.dp, if (focused) RoxyColors.focusBorder else Color.Transparent, RoundedCornerShape(6.dp))
                .padding(horizontal = 14.dp, vertical = 12.dp),
        )
    }
}

@Composable
fun CenteredMessage(text: String, modifier: Modifier = Modifier, color: Color = RoxyColors.muted) {
    Box(modifier = modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        Text(text, color = color, textAlign = TextAlign.Center, modifier = Modifier.padding(ScreenPadding))
    }
}

/** "2019 · Action, Comedy · 8.1" style line. */
fun Item.metaLine(): String = listOfNotNull(
    (year ?: premiered?.take(4)?.toIntOrNull())?.toString(),
    genres.take(3).joinToString(", ").ifEmpty { null },
    communityRating?.let { "%.1f".format(it) },
    officialRating,
    runtimeMinutes?.let { "$it min" },
).joinToString("  ·  ")
