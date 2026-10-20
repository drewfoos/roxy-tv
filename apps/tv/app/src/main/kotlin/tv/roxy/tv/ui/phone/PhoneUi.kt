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
import androidx.compose.foundation.gestures.snapping.rememberSnapFlingBehavior
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import android.content.Intent
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Bookmark
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Share
import androidx.compose.material.icons.outlined.ArrowCircleDown
import androidx.compose.material.icons.outlined.BookmarkBorder
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Shape
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.launch
import timber.log.Timber
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

/** 2:3 poster, no caption (grids). Progress belongs to episodes and Continue Watching, not to a title's poster. */
@Composable
fun PosterTile(item: Item, api: RoxyApi, modifier: Modifier = Modifier, onClick: () -> Unit) {
    Box(
        modifier = modifier.aspectRatio(2f / 3f).clip(RoundedCornerShape(6.dp)).background(RoxyColors.surfaceHigh).clickable(onClick = onClick),
    ) {
        RoxyImage(api.imageUrl(item, "poster", widthPx(PosterW) * 2), item.title, modifier = Modifier.fillMaxSize(), placeholderTextSize = 12)
    }
}

/**
 * Big title card for rows (shows and movies): 2:3 poster sized so two and a half fit
 * across the screen, the name under it, then "Dub | Sub" with the three-dot menu on the
 * right.
 */
@Composable
fun TitleCard(item: Item, api: RoxyApi, width: Dp, onClick: () -> Unit) {
    Column(modifier = Modifier.width(width)) {
        Box(
            modifier = Modifier.fillMaxWidth().aspectRatio(2f / 3f).clip(RoundedCornerShape(8.dp)).background(RoxyColors.surfaceHigh).clickable(onClick = onClick),
        ) {
            RoxyImage(api.imageUrl(item, "poster", widthPx(width) * 2), item.title, modifier = Modifier.fillMaxSize(), placeholderTextSize = 14)
        }
        // One line, so every card in a row is the same height (the full name is on the title page).
        Text(item.title, fontSize = 14.sp, lineHeight = 18.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 8.dp))
        Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.padding(top = 2.dp)) {
            Text(
                audioBadge(item) ?: listOfNotNull(item.year?.toString(), if (item.isMovie) "Movie" else null).joinToString("  ·  "),
                fontSize = 12.sp, color = RoxyColors.muted, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f),
            )
            CardMenu(item, api, onPlay = onClick)
        }
    }
}

/**
 * 16:9 card (episodes, and movies in the Recently added row): play badge, progress,
 * Dub | Sub pill, then a caption with the three-dot menu on the right.
 */
@Composable
fun ThumbTile(item: Item, api: RoxyApi, width: Dp = ThumbW, onClick: () -> Unit) {
    val isMovie = item.isMovie
    Column(modifier = Modifier.width(width)) {
        Box(
            modifier = Modifier.fillMaxWidth().aspectRatio(16f / 9f).clip(RoundedCornerShape(6.dp)).background(RoxyColors.surfaceHigh).clickable(onClick = onClick),
        ) {
            val url = if (isMovie) api.imageUrl(item, "backdrop", widthPx(width) * 2) ?: api.imageUrl(item, "poster", widthPx(width) * 2) else api.imageUrl(item, "thumb", widthPx(width) * 2)
            RoxyImage(url, item.episodeCode.ifEmpty { item.title }, modifier = Modifier.fillMaxSize(), placeholderTextSize = 12)
            PlayBadge(size = 38.dp, modifier = Modifier.align(Alignment.Center))
            audioBadge(item)?.let { BadgePill(it, modifier = Modifier.align(Alignment.TopStart).padding(6.dp)) }
            ProgressBar(item.progressFraction, modifier = Modifier.align(Alignment.BottomCenter))
        }
        Row(verticalAlignment = Alignment.Top, modifier = Modifier.padding(top = 6.dp)) {
            Column(modifier = Modifier.weight(1f)) {
                Text(if (isMovie) item.title else item.seriesTitle ?: item.title, fontSize = 13.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground, maxLines = 1, overflow = TextOverflow.Ellipsis)
                val sub = if (isMovie) listOfNotNull(item.year?.toString(), "Movie").joinToString("  ·  ") else listOf(item.episodeCode, item.title).filter { it.isNotEmpty() }.joinToString("  ")
                Text(sub, fontSize = 12.sp, color = RoxyColors.muted, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            CardMenu(item, api, onPlay = onClick, modifier = Modifier.padding(start = 4.dp))
        }
    }
}

/**
 * Continue Watching on the phone: one card nearly fills the width (the next peeks in at
 * the right, so swiping is obvious): the episode's own still (a movie's scene backdrop),
 * a progress bar along its bottom edge, and under it the show's name, then the episode
 * and what is left of it.
 */
@Composable
fun ContinueRow(title: String, items: List<Item>, api: RoxyApi, onClick: (Item) -> Unit, modifier: Modifier = Modifier) {
    val screen = LocalConfiguration.current.screenWidthDp.dp
    val cardWidth = screen - PhonePad - 44.dp
    Column(modifier = modifier.fillMaxWidth()) {
        Text(title, fontSize = 18.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(start = PhonePad, bottom = 10.dp))
        // Snaps card to card, so a swipe never leaves one half off the screen.
        val state = rememberLazyListState()
        LazyRow(state = state, flingBehavior = rememberSnapFlingBehavior(state), contentPadding = PaddingValues(horizontal = PhonePad), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            items(items, key = { it.id }) { item -> ContinueCard(item, api, width = cardWidth, onClick = { onClick(item) }) }
        }
    }
}

@Composable
private fun ContinueCard(item: Item, api: RoxyApi, width: Dp, onClick: () -> Unit) {
    val isMovie = item.isMovie
    Column(modifier = Modifier.width(width)) {
        Box(modifier = Modifier.fillMaxWidth().aspectRatio(16f / 9f).clip(RoundedCornerShape(8.dp)).background(RoxyColors.surfaceHigh).clickable(onClick = onClick)) {
            val px = widthPx(width) * 2
            val url = if (isMovie) api.imageUrl(item, "backdrop", px) ?: api.imageUrl(item, "poster", px) else api.imageUrl(item, "thumb", px)
            RoxyImage(url, item.seriesTitle ?: item.title, modifier = Modifier.fillMaxSize(), placeholderTextSize = 14)
            PlayBadge(size = 52.dp, modifier = Modifier.align(Alignment.Center))
            audioBadge(item)?.let { BadgePill(it, modifier = Modifier.align(Alignment.TopStart).padding(8.dp)) }
            // What is left, as a corner tag on the picture, where a long episode title cannot push it off.
            item.userData?.positionMs?.takeIf { it > 0 }?.let { pos -> item.runtimeMs?.takeIf { it > pos }?.let { "${((it - pos) / 60000L + 1).coerceAtLeast(1)}m left" } }
                ?.let { BadgePill(it, modifier = Modifier.align(Alignment.BottomEnd).padding(end = 8.dp, bottom = 12.dp)) }
            ProgressBar(item.progressFraction, modifier = Modifier.align(Alignment.BottomCenter))
        }
        Row(verticalAlignment = Alignment.Top, modifier = Modifier.padding(top = 8.dp)) {
            Column(modifier = Modifier.weight(1f)) {
                Text(if (isMovie) item.title else item.seriesTitle ?: item.title, fontSize = 15.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground, maxLines = 1, overflow = TextOverflow.Ellipsis)
                val what = if (isMovie) listOfNotNull(item.year?.toString(), "Movie") else listOf(item.episodeCode, item.title).filter { it.isNotEmpty() }
                Text(what.joinToString("  ·  "), fontSize = 13.sp, color = RoxyColors.muted, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 2.dp))
            }
            CardMenu(item, api, onPlay = onClick, modifier = Modifier.padding(start = 4.dp))
        }
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

/**
 * Titled horizontal row: big [TitleCard]s for shows and movies, 16:9 [ThumbTile]s for
 * episodes (Continue Watching); both sized so two and a half fit across the screen.
 */
@Composable
fun PhoneRow(title: String, items: List<Item>, api: RoxyApi, onClick: (Item) -> Unit, modifier: Modifier = Modifier) {
    val screen = LocalConfiguration.current.screenWidthDp.dp
    val cardWidth = ((screen - PhonePad - 10.dp * 2) / 2.5f)
    Column(modifier = modifier.fillMaxWidth()) {
        Text(title, fontSize = 18.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(start = PhonePad, bottom = 10.dp))
        LazyRow(contentPadding = PaddingValues(horizontal = PhonePad), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            items(items, key = { it.id }) { item ->
                if (item.isEpisode) ThumbTile(item, api, width = cardWidth, onClick = { onClick(item) })
                else TitleCard(item, api, width = cardWidth, onClick = { onClick(item) })
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
val OnBrand = Color(0xFF07182B)

/**
 * The hero's action pair, reused on title pages: blue pill Play / Resume filling the row
 * and the round bookmark beside it.
 */
@Composable
fun PlayAndSaveRow(label: String, item: Item, api: RoxyApi, modifier: Modifier = Modifier, onPlay: () -> Unit, onSaved: () -> Unit = {}) {
    Row(modifier = modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(14.dp)) {
        PlayButton(label, modifier = Modifier.weight(1f), height = 44.dp, pill = true, container = RoxyColors.accent, content = OnBrand, onClick = onPlay)
        SaveButton(item, api, onSaved = onSaved)
    }
}

/**
 * "Dub | Sub" from a playable item's audio tracks: English plus another language =
 * Dub | Sub (KonoSuba, or Toy Story with its extra dubs), another language only = Sub,
 * English only = Dub for animation and nothing otherwise. Null when the streams are
 * unknown (series resolved from the library carry no sources).
 */
fun audioBadge(play: Item?): String? {
    if (play == null) return null
    val fromServer = play.audioLanguages.map { it.lowercase() }
    val langs = (if (fromServer.isNotEmpty()) fromServer else play.primarySource?.streams.orEmpty().filter { it.type == "audio" }.mapNotNull { it.language?.lowercase() })
        .filter { it != "und" }.distinct()
    if (langs.isEmpty()) return null
    val eng = langs.any { it == "eng" || it == "en" }
    val other = langs.any { it != "eng" && it != "en" }
    // Anime = matched on AniList (the server says so); a Pixar film is "Animation" but not anime.
    val anime = play.isAnime || play.genres.any { it.equals("Anime", true) }
    return when {
        // Anime: the Dub / Sub vocabulary.
        anime && eng && other -> "Dub | Sub"
        anime && other -> "Sub"
        anime -> "Dub"
        // Everything else: the language names ("English", "English, French").
        else -> langs.take(3).joinToString(", ") { languageName(it) }
    }
}

/** "eng" → "English", via the platform's ISO 639 tables; unknown codes are shown upper-cased. */
fun languageName(code: String): String {
    val name = runCatching { java.util.Locale(code).getDisplayLanguage(java.util.Locale.ENGLISH) }.getOrNull()
    return if (name.isNullOrEmpty() || name.equals(code, ignoreCase = true)) code.uppercase() else name
}

/**
 * The facts line under a title: "★ 8.5 · 2016 · 4 Seasons · 31 Episodes · 24m". The
 * rating leads in the brand blue; the rest is muted. One line, so the title block reads
 * as a single taper: title, tagline, genres, facts.
 */
@Composable
fun FactsLine(rating: Double?, facts: List<String>, modifier: Modifier = Modifier) {
    val rest = facts.filter { it.isNotBlank() }
    if (rating == null && rest.isEmpty()) return
    Text(
        buildAnnotatedString {
            if (rating != null) {
                withStyle(SpanStyle(fontWeight = FontWeight.SemiBold)) { append("★ %.1f".format(rating)) }
                if (rest.isNotEmpty()) append("  ·  ")
            }
            append(rest.joinToString("  ·  "))
        },
        fontSize = 13.sp, color = Color.White.copy(alpha = 0.72f), textAlign = TextAlign.Center, modifier = modifier.padding(top = 8.dp),
    )
}

/** Small translucent pill over a card ("Dub | Sub"). */
@Composable
fun BadgePill(text: String, modifier: Modifier = Modifier) {
    Box(modifier = modifier.clip(RoundedCornerShape(4.dp)).background(Color(0xB3000000)).padding(horizontal = 6.dp, vertical = 2.dp)) {
        Text(text, color = Color.White, fontSize = 10.sp, fontWeight = FontWeight.SemiBold)
    }
}

/**
 * The three-dot menu on a card: Watch now, Download (every episode of a show, or the
 * episode / movie itself), Add to Watchlist (the show, for an episode), Mark as watched,
 * Share (TMDB / AniList page when known, else the title).
 */
@Composable
fun CardMenu(item: Item, api: RoxyApi, onPlay: () -> Unit, modifier: Modifier = Modifier) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var open by remember { mutableStateOf(false) }
    val startDownload = rememberDownloadStarter()
    val downloadLabel = when {
        item.isSeries -> "Download all episodes"
        item.isEpisode -> "Download episode"
        else -> "Download"
    }
    /** Resolve what the card stands for into downloadable items plus the title snapshot, then queue them. */
    fun download() = scope.launch {
        runCatching {
            when {
                item.isSeries -> {
                    val eps = api.items(type = "episode", parent = item.id, recursive = true, includeSources = true, sort = "index").items
                    eps.groupBy { it.seasonAired ?: 0 }.forEach { (season, list) ->
                        startDownload(list, item, if (season == 0) "Specials" else "Season $season")
                    }
                }
                item.isEpisode -> {
                    val full = if (item.mediaSources.isEmpty()) api.item(item.id) else item
                    val series = item.seriesId?.let { api.item(it) } ?: full
                    startDownload(listOf(full), series, full.seasonAired?.let { if (it == 0) "Specials" else "Season $it" })
                }
                else -> {
                    val full = if (item.mediaSources.isEmpty()) api.item(item.id) else item
                    startDownload(listOf(full), full, null)
                }
            }
        }.onFailure { Timber.w(it, "download from card failed") }
    }
    Box(modifier = modifier) {
        Icon(
            Icons.Filled.MoreVert, contentDescription = "More", tint = RoxyColors.muted,
            modifier = Modifier.size(22.dp).clickable { open = true },
        )
        DropdownMenu(expanded = open, onDismissRequest = { open = false }, containerColor = RoxyColors.surfaceHigh) {
            if (item.isPlayable || item.isSeries) {
                DropdownMenuItem(text = { Text("Watch now", color = Color.White) }, leadingIcon = { Icon(Icons.Filled.PlayArrow, null, tint = Color.White) }, onClick = { open = false; onPlay() })
                DropdownMenuItem(text = { Text(downloadLabel, color = Color.White) }, leadingIcon = { Icon(Icons.Outlined.ArrowCircleDown, null, tint = Color.White) }, onClick = { open = false; download() })
            }
            DropdownMenuItem(
                text = { Text("Add to Watchlist", color = Color.White) },
                leadingIcon = { Icon(Icons.Outlined.BookmarkBorder, null, tint = Color.White) },
                onClick = {
                    open = false
                    val target = if (item.isEpisode) item.seriesId ?: item.id else item.id
                    scope.launch { runCatching { api.setFavorite(target, true) }.onFailure { Timber.w(it, "favorite failed") } }
                },
            )
            if (item.isPlayable) {
                DropdownMenuItem(
                    text = { Text("Mark as watched", color = Color.White) },
                    leadingIcon = { Icon(Icons.Filled.CheckCircle, null, tint = Color.White) },
                    onClick = { open = false; scope.launch { runCatching { api.setPlayed(item.id, true) }.onFailure { Timber.w(it, "played failed") } } },
                )
            }
            DropdownMenuItem(
                text = { Text("Share", color = Color.White) },
                leadingIcon = { Icon(Icons.Filled.Share, null, tint = Color.White) },
                onClick = {
                    open = false
                    val name = if (item.isEpisode) listOfNotNull(item.seriesTitle, item.episodeCode.takeIf { it.isNotEmpty() }, item.title).joinToString(" ") else item.title
                    val text = listOfNotNull(name, item.shareUrl).joinToString("\n")
                    val send = Intent(Intent.ACTION_SEND).apply { type = "text/plain"; putExtra(Intent.EXTRA_TEXT, text) }
                    context.startActivity(Intent.createChooser(send, "Share"))
                },
            )
        }
    }
}

/** Round bookmark in the brand blue: adds the show to the Watchlist (favourite); filled once saved. */
@Composable
fun SaveButton(item: Item, api: RoxyApi, onSaved: () -> Unit = {}) {
    val scope = rememberCoroutineScope()
    var on by remember(item.id, item.userData?.isFavorite) { mutableStateOf(item.userData?.isFavorite == true) }
    Box(
        modifier = Modifier.size(44.dp).clip(CircleShape).border(2.dp, RoxyColors.accent, CircleShape).clickable {
            val want = !on
            on = want
            scope.launch {
                runCatching { api.setFavorite(item.id, want) }
                    .onFailure { Timber.w(it, "favorite failed"); on = !want }
                    .onSuccess { onSaved() }
            }
        },
        contentAlignment = Alignment.Center,
    ) {
        Icon(if (on) Icons.Filled.Bookmark else Icons.Outlined.BookmarkBorder, contentDescription = if (on) "Saved" else "Save", tint = RoxyColors.accent, modifier = Modifier.size(24.dp))
    }
}

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
