@file:OptIn(androidx.compose.foundation.ExperimentalFoundationApi::class)

package tv.roxy.tv.ui.tv

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.collectIsFocusedAsState
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxScope
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material3.Icon
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.compositionLocalOf
import androidx.compose.ui.focus.focusProperties
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.lazy.LazyListPrefetchStrategy
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.snapshotFlow
import kotlinx.coroutines.flow.collectLatest
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.foundation.gestures.LocalBringIntoViewSpec
import androidx.compose.foundation.gestures.BringIntoViewSpec
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.RectangleShape
import androidx.compose.ui.graphics.Shadow
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.foundation.focusable
import androidx.compose.ui.draw.clip
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.NativeKeyEvent
import androidx.compose.ui.input.key.onKeyEvent
import androidx.compose.ui.input.key.onPreviewKeyEvent
import androidx.compose.ui.input.key.type
import androidx.tv.material3.Text
import tv.roxy.tv.ui.components.tapClick
import tv.roxy.tv.api.Item
import tv.roxy.tv.api.RoxyApi
import tv.roxy.tv.ui.components.ProgressBar
import tv.roxy.tv.ui.components.RoxyImage
import tv.roxy.tv.ui.components.widthPx
import tv.roxy.tv.ui.phone.ageLabel
import tv.roxy.tv.ui.phone.audioBadge
import androidx.compose.material.icons.filled.Replay
import tv.roxy.tv.ui.theme.RoxyColors

/*
 * The TV screens copy Crunchyroll's Android TV app. Every size here was measured from
 * 1920x1080 captures of it on the TCL (density 320: 1 dp = 2 px) and halved.
 */
object TvDims {
    /** The collapsed rail: icons only. */
    val RailWidth = 80.dp
    /** The rail with its labels, over the dimmed page. */
    val RailExpandedWidth = 264.dp
    val RailIcon = 24.dp
    val RailRow = 52.dp
    /** Where text starts on a page with the rail (hero copy, row titles). */
    val ContentStart = 85.dp
    /** The accent frame around a focused card, drawn outside it: cards keep their size. */
    val FocusBorder = 4.dp
    /** Card lists start one frame left of the text, so the image lines up with it. */
    val CardStart = ContentStart - FocusBorder
    /** Where a scrolled row is cut at the left: the middle of the collapsed rail. */
    val RowClip = RailWidth / 2
    val PosterCardWidth = 137.dp
    val PosterCaption = 52.dp
    val ContinueCardWidth = 282.dp
    val ContinueCaption = 69.dp
    /** Watchlist / History: five 16:9 cards across. */
    val GridCardWidth = 160.dp
    val GridCaption = 75.dp
    /** Browse / Search: five posters across, right of the category list. */
    val BrowseCardWidth = 108.dp
    val BrowseCaption = 57.dp
    val HeroButtonHeight = 47.dp
    val ListRowHeight = 50.dp
    val TabHeight = 45.dp
}

/** Dark text on the accent bars and buttons. */
val OnAccent = Color(0xFF07182B)

/** The lighter grey of secondary copy (facts lines, captions' second line). */
val TvSubtle = Color(0xFFC8C8CE)

/** Copy drawn over key art (synopsis, facts): near white, with a crisp one-pixel shadow. */
val TvCopy = Color(0xFFEDEDF1)
val TvShadowed = androidx.compose.ui.text.TextStyle(shadow = androidx.compose.ui.graphics.Shadow(Color(0xD9000000), androidx.compose.ui.geometry.Offset(0f, 2f), 0f))

/**
 * Which element of a page last had focus, by key, so coming back from a title page (or
 * from the rail) lands on the same card instead of the first one. Survives the page's
 * recreation through rememberSaveable; the requesters are rebuilt as the cards compose.
 */
class FocusMemory(private val saved: androidx.compose.runtime.MutableState<String?>) {
    var key: String?
        get() = saved.value
        set(value) { saved.value = value }

    private val requesters = HashMap<String, FocusRequester>()
    private val modifiers = HashMap<String, Modifier>()

    fun requester(key: String): FocusRequester = requesters.getOrPut(key) { FocusRequester() }

    /** [focusKey]'s modifier, built once per key: an equal modifier lets a card skip recomposition. */
    fun modifier(key: String): Modifier = modifiers.getOrPut(key) {
        Modifier.focusRequester(requester(key)).onFocusChanged { if (it.isFocused) this.key = key }
    }

    /** Focus the remembered element, or [fallback]; false when neither is on screen yet. */
    fun restore(fallback: String? = null): Boolean {
        for (k in listOfNotNull(key, fallback)) {
            val r = requesters[k] ?: continue
            val ok = runCatching { r.requestFocus() }.getOrDefault(false)
            if (ok) return true
        }
        return false
    }
}

/**
 * How a rail page takes focus: the page sets [enter] to focus its first (or remembered)
 * element, and the shell calls it when the rail hands over (OK, Right, Back on the rail)
 * or when the page is (re)entered. Provided by TvShell; pages register in a DisposableEffect.
 */
class PageEntry {
    var enter: () -> Boolean = { false }
    /** Which rail page registered [enter]: a page on its way out (still composed during the
     *  switch) must not take the focus meant for the one coming in. */
    var owner: Any? = null
}

val LocalPageEntry = compositionLocalOf<PageEntry?> { null }

/** The rail page being shown, as the shell knows it; pages register their entry under it. */
val LocalPageOwner = compositionLocalOf<Any?> { null }

/** Registers this page's way in for the shell, for as long as the page is composed. */
@Composable
fun RegisterPageEntry(enter: () -> Boolean) {
    val entry = LocalPageEntry.current
    val owner = LocalPageOwner.current
    // Keyed on the owner too: the first composition runs before navigation knows the page,
    // and a page that registered then (owner null) would never be handed focus back.
    DisposableEffect(entry, enter, owner) {
        entry?.enter = enter
        entry?.owner = owner
        onDispose { if (entry?.enter === enter) { entry.enter = { false }; entry.owner = null } }
    }
}

@Composable
fun rememberFocusMemory(): FocusMemory {
    val saved = rememberSaveable { mutableStateOf<String?>(null) }
    return remember { FocusMemory(saved) }
}

/** Registers this element with [memory] under [key]: focus on it is remembered and restorable. */
fun Modifier.focusKey(memory: FocusMemory, key: String): Modifier = this.then(memory.modifier(key))

/**
 * A focusable, clickable box with no look of its own: the caller draws from [focused].
 * A plain focusable with a key handler, not tv-material's Surface: its indication, scale,
 * glow and border machinery on every card was most of the composition cost on a TV's
 * processor. OK / Enter fire [onClick] once per press (repeats while held are ignored).
 */
@Composable
fun TvFocusable(
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    focusedBackground: Color = Color.Transparent,
    background: Color = Color.Transparent,
    shape: androidx.compose.ui.graphics.Shape = RectangleShape,
    onFocused: (() -> Unit)? = null,
    content: @Composable BoxScope.(focused: Boolean) -> Unit,
) {
    var focused by remember { mutableStateOf(false) }
    // Called straight from the focus change, not from an effect a frame later: the row and
    // page scrolls it starts then begin in the same frame as the focus move.
    val onFocusedNow by rememberUpdatedState(onFocused)
    Box(
        modifier = modifier
            .onFocusChanged { val f = it.isFocused; if (f && !focused) onFocusedNow?.invoke(); focused = f }
            .onKeyEvent { ev ->
                val ok = ev.key == Key.DirectionCenter || ev.key == Key.Enter || ev.key == Key.NumPadEnter
                if (enabled && ok && ev.type == KeyEventType.KeyDown && (ev.nativeKeyEvent as NativeKeyEvent).repeatCount == 0) { onClick(); true } else false
            }
            .focusable(enabled)
            .clip(shape)
            .background(if (focused) focusedBackground else background)
            .tapClick(onClick),
    ) { content(focused) }
}

/**
 * A card with Crunchyroll's focus treatment: a 4 dp accent frame drawn around the card
 * (image and caption together) without scaling it. The frame's space is always reserved,
 * so cards in a row touch frame to frame and sit 8 dp apart.
 */
@Composable
fun TvCard(onClick: () -> Unit, modifier: Modifier = Modifier, onFocused: (() -> Unit)? = null, content: @Composable BoxScope.() -> Unit) {
    // Focus only changes what is drawn, so it is read in the draw phase: moving focus along
    // a row redraws two frames and recomposes nothing. No touch handler: a remote drives it.
    val focused = remember { mutableStateOf(false) }
    val onFocusedNow by rememberUpdatedState(onFocused)
    val onClickNow by rememberUpdatedState(onClick)
    Box(
        modifier = modifier
            .onFocusChanged { val f = it.isFocused; if (f && !focused.value) onFocusedNow?.invoke(); focused.value = f }
            .onKeyEvent { ev ->
                val ok = ev.key == Key.DirectionCenter || ev.key == Key.Enter || ev.key == Key.NumPadEnter
                if (ok && ev.type == KeyEventType.KeyDown && (ev.nativeKeyEvent as NativeKeyEvent).repeatCount == 0) { onClickNow(); true } else false
            }
            .focusable()
            .drawWithContent {
                drawContent()
                if (focused.value) {
                    val w = TvDims.FocusBorder.toPx()
                    drawRect(RoxyColors.accent, topLeft = Offset(w / 2, w / 2), size = Size(size.width - w, size.height - w), style = Stroke(w))
                }
            }
            .padding(TvDims.FocusBorder),
    ) { content() }
}

/** Crunchyroll's episode code: "E2" in the first season, "S2 E1" after it; "" when unknown. */
fun episodeShort(item: Item): String {
    val e = item.indexAired ?: item.indexAbsolute ?: return ""
    val s = item.seasonAired
    return if (s == null || s <= 1) "E$e" else "S$s E$e"
}

/** The caption's action line for an episode card: "CONTINUE: E2" part-way through, "START WATCHING: E1" otherwise. */
fun continueLabel(item: Item): String {
    val code = episodeShort(item)
    val started = (item.userData?.positionMs ?: 0L) > 0
    val verb = if (started) "CONTINUE" else if (item.userData?.played == true) "WATCH AGAIN" else "START WATCHING"
    return if (code.isEmpty()) verb else "$verb: $code"
}

/** A watched episode's mark, as on the phone: a replay arrow in a dark disc over the dimmed still. */
@Composable
fun ReplayBadge(size: Dp, modifier: Modifier = Modifier) {
    Box(modifier = modifier.size(size).clip(androidx.compose.foundation.shape.CircleShape).background(Color(0x99000000)), contentAlignment = Alignment.Center) {
        Icon(Icons.Filled.Replay, contentDescription = "Watch again", tint = Color.White, modifier = Modifier.size(size * 0.6f))
    }
}

/** "23m left" part-way through, "23m" for a fresh episode; null when the runtime is unknown. */
fun minutesLabel(item: Item): String? {
    val len = item.runtimeMs ?: item.primarySource?.durationMs ?: return null
    if (len <= 0) return null
    val pos = item.userData?.positionMs ?: 0L
    if (pos > 0) {
        val left = ((len - pos) + 30_000) / 60_000
        return "${left.coerceAtLeast(1)}m left"
    }
    return "${(len + 30_000) / 60_000}m"
}

/** The grid card's title: the show for an episode, the title otherwise. */
fun cardTitle(item: Item): String = if (item.isEpisode) item.seriesTitle ?: item.title else item.title

/** Caption text of a card: fixed line count so every card in a row is the same height. */
@Composable
fun CaptionTitle(text: String, lines: Int, fontSize: androidx.compose.ui.unit.TextUnit, lineHeight: androidx.compose.ui.unit.TextUnit, color: Color = RoxyColors.onBackground) {
    Text(text, color = color, fontSize = fontSize, lineHeight = lineHeight, fontWeight = FontWeight.SemiBold, minLines = lines, maxLines = lines, overflow = TextOverflow.Ellipsis)
}

/** The "23m left" badge in the picture's corner: a dark pill (a blurred shadow is dear on a TV's GPU). */
@Composable
fun MinutesBadge(text: String, modifier: Modifier = Modifier) {
    Text(
        text, color = Color.White, fontSize = 14.sp, fontWeight = FontWeight.SemiBold,
        modifier = modifier.padding(end = 8.dp, bottom = 8.dp).background(Color(0xB3000000), RoundedCornerShape(3.dp)).padding(horizontal = 6.dp, vertical = 1.dp),
    )
}

/** A 2:3 poster with the dark caption box under it (home rows, browse, search). */
@Composable
fun PosterCard(item: Item, api: RoxyApi, width: Dp, captionHeight: Dp, onClick: () -> Unit, modifier: Modifier = Modifier, onFocused: (() -> Unit)? = null) {
    TvCard(onClick = onClick, modifier = modifier, onFocused = onFocused) {
        Column(modifier = Modifier.width(width)) {
            RoxyImage(api.imageUrl(item, "poster", widthPx(width)), item.title, modifier = Modifier.fillMaxWidth().aspectRatio(2f / 3f))
            Box(modifier = Modifier.fillMaxWidth().height(captionHeight).background(RoxyColors.surface).padding(horizontal = 8.dp, vertical = 8.dp)) {
                CaptionTitle(item.title, lines = 2, fontSize = 14.sp, lineHeight = 18.sp)
            }
        }
    }
}

/** A poster card for a title known only by a picture URL (TMDB's similar titles); [note] is a small label over the art. */
@Composable
fun UrlPosterCard(title: String, url: String?, width: Dp, captionHeight: Dp, onClick: () -> Unit, modifier: Modifier = Modifier, dim: Boolean = false, note: String? = null) {
    TvCard(onClick = onClick, modifier = modifier) {
        Column(modifier = Modifier.width(width).alpha(if (dim) 0.55f else 1f)) {
            Box(modifier = Modifier.fillMaxWidth().aspectRatio(2f / 3f)) {
                RoxyImage(url, title, modifier = Modifier.fillMaxSize())
                if (note != null) Text(note.uppercase(), color = Color.White, fontSize = 10.sp, fontWeight = FontWeight.Bold, letterSpacing = 0.5.sp, modifier = Modifier.align(Alignment.TopStart).padding(6.dp).background(Color(0xCC000000), RoundedCornerShape(3.dp)).padding(horizontal = 5.dp, vertical = 2.dp))
            }
            Box(modifier = Modifier.fillMaxWidth().height(captionHeight).background(RoxyColors.surface).padding(horizontal = 8.dp, vertical = 8.dp)) {
                CaptionTitle(title, lines = 2, fontSize = 14.sp, lineHeight = 18.sp)
            }
        }
    }
}

/**
 * A 16:9 card: the episode's still (or a title's key art), the minutes badge and progress
 * bar on the picture, then a caption with the title and [secondLine] ("CONTINUE: E2",
 * "E2 - 10/27/2026"). [titleLines] is 1 on Home's Continue Watching row and 2 in grids.
 */
@Composable
fun ThumbCard(
    item: Item, api: RoxyApi, width: Dp, captionHeight: Dp, titleLines: Int, secondLine: String?,
    onClick: () -> Unit, modifier: Modifier = Modifier, onFocused: (() -> Unit)? = null,
    badge: String? = minutesLabel(item), progress: Float? = item.progressFraction,
) {
    val imageType = if (item.isEpisode) "thumb" else "backdrop"
    val url = api.imageUrl(item, imageType, widthPx(width)) ?: api.imageUrl(item, "backdrop", widthPx(width)) ?: api.imageUrl(item, "poster", widthPx(width))
    TvCard(onClick = onClick, modifier = modifier, onFocused = onFocused) {
        Column(modifier = Modifier.width(width)) {
            Box(modifier = Modifier.fillMaxWidth().aspectRatio(16f / 9f)) {
                val watched = item.isWatched && (item.isEpisode || item.isMovie)
                RoxyImage(url, cardTitle(item), modifier = Modifier.fillMaxSize().alpha(if (watched) 0.45f else 1f))
                if (watched) {
                    ReplayBadge(size = 48.dp, modifier = Modifier.align(Alignment.Center))
                    MinutesBadge("Watched", modifier = Modifier.align(Alignment.BottomEnd).padding(bottom = 3.dp))
                } else {
                    if (badge != null) MinutesBadge(badge, modifier = Modifier.align(Alignment.BottomEnd).padding(bottom = 3.dp))
                    if (progress != null && progress < 1f) ProgressBar(progress, modifier = Modifier.align(Alignment.BottomCenter))
                }
            }
            Column(modifier = Modifier.fillMaxWidth().height(captionHeight).background(RoxyColors.surface).padding(horizontal = 12.dp, vertical = 12.dp)) {
                CaptionTitle(cardTitle(item), lines = titleLines, fontSize = 16.sp, lineHeight = 20.sp)
                if (secondLine != null) {
                    Spacer(Modifier.weight(1f))
                    Text(secondLine, color = TvSubtle, fontSize = 14.sp, fontWeight = FontWeight.Bold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
            }
        }
    }
}

/**
 * A titled row of cards. Episodes (and everything in Continue Watching, [thumbs]) are
 * 16:9 cards with "CONTINUE: E2", the rest posters. The focused card is kept at the row's
 * left edge, as Crunchyroll does, and the cards register with [memory] under "[rowKey]/[item id]".
 */
@OptIn(ExperimentalFoundationApi::class)
@Composable
fun TvRow(
    title: String, rowKey: String, items: List<Item>, api: RoxyApi, memory: FocusMemory,
    onClick: (Item) -> Unit, onRowFocused: (itemKey: String) -> Unit, modifier: Modifier = Modifier, thumbs: Boolean = false,
    upKey: () -> String? = { null }, downKey: () -> String? = { null },
) {
    // Up / Down targets are resolved when the remote moves focus, not in composition: the
    // rows above and below remember their last card in a plain map, and a focus move in one
    // row then recomposes no other row.
    val upTarget by rememberUpdatedState(upKey)
    val downTarget by rememberUpdatedState(downKey)
    val click by rememberUpdatedState(onClick)
    val rowFocused by rememberUpdatedState(onRowFocused)
    // Three cards of a row about to scroll into view are composed ahead of it.
    val rowState = rememberLazyListState(prefetchStrategy = remember { LazyListPrefetchStrategy(nestedPrefetchItemCount = 3) })
    // The focused card's index drives the scroll through a flow, so a focus change does
    // not recompose the row. The focused card lands at the content start; the cards before
    // it scroll on under the rail and are cut at its middle, as on Crunchyroll.
    // Every focus gain re-pins (a Pair, so the same card focused again still emits):
    // Compose's own scroll-into-view is switched off for the row (NoBringIntoView), since
    // it fought this scroll and dragged rows back to their start.
    val pivot = remember { mutableStateOf(-1 to 0) }
    LaunchedEffect(rowState) { snapshotFlow { pivot.value }.collectLatest { (i, _) -> if (i >= 0) rowState.animateScrollToItem(i) } }
    Column(modifier = modifier.fillMaxWidth().padding(top = 10.dp)) {
        Text(title, color = RoxyColors.muted, fontSize = 20.sp, lineHeight = 28.sp, fontWeight = FontWeight.Bold, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(start = TvDims.ContentStart))
        CompositionLocalProvider(LocalBringIntoViewSpec provides NoBringIntoView) {
        LazyRow(state = rowState, modifier = Modifier.padding(start = TvDims.RowClip), contentPadding = PaddingValues(start = TvDims.CardStart - TvDims.RowClip, end = TvDims.CardStart)) {
            itemsIndexed(items, key = { _, it -> it.id }, contentType = { _, it -> if (thumbs || it.isEpisode) 1 else 0 }) { i, item ->
                val key = "$rowKey/${item.id}"
                val focusMod = remember(key) {
                    Modifier.focusKey(memory, key).focusProperties {
                        upTarget()?.let { up = memory.requester(it) }
                        downTarget()?.let { down = memory.requester(it) }
                    }
                }
                val focused = remember(key, i) { { pivot.value = i to pivot.value.second + 1; rowFocused(key) } }
                val clicked = remember(item) { { click(item) } }
                if (thumbs || item.isEpisode) {
                    ThumbCard(item, api, width = TvDims.ContinueCardWidth, captionHeight = TvDims.ContinueCaption, titleLines = 1, secondLine = continueLabel(item), onClick = clicked, modifier = focusMod, onFocused = focused)
                } else {
                    PosterCard(item, api, width = TvDims.PosterCardWidth, captionHeight = TvDims.PosterCaption, onClick = clicked, modifier = focusMod, onFocused = focused)
                }
            }
        }
        }
    }
}

/** A scroll-into-view rule that never scrolls: the pages place focused cards themselves. */
@OptIn(ExperimentalFoundationApi::class)
val NoBringIntoView: BringIntoViewSpec = object : BringIntoViewSpec {
    override fun calculateScrollDistance(offset: Float, size: Float, containerSize: Float): Float = 0f
}

/**
 * A list row that turns into a full-width accent bar with dark text when focused (the
 * rail, Browse's categories, Settings, the title page's actions). [selected] (the page
 * shown, the chosen option) reads white instead of grey when not focused.
 */
@Composable
fun TvListRow(
    label: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    icon: ImageVector? = null,
    selected: Boolean = false,
    height: Dp = TvDims.ListRowHeight,
    startPadding: Dp = 20.dp,
    fontSize: androidx.compose.ui.unit.TextUnit = 18.sp,
    trailing: String? = null,
    checked: Boolean = false,
    onFocused: (() -> Unit)? = null,
) {
    TvFocusable(onClick = onClick, modifier = modifier, focusedBackground = RoxyColors.accent, onFocused = onFocused) { focused ->
        val color = when {
            focused -> OnAccent
            selected -> RoxyColors.onBackground
            else -> RoxyColors.muted
        }
        Row(modifier = Modifier.fillMaxWidth().height(height).padding(start = startPadding, end = 20.dp), verticalAlignment = Alignment.CenterVertically) {
            if (icon != null) {
                Icon(icon, contentDescription = null, tint = color, modifier = Modifier.size(24.dp))
                Spacer(Modifier.width(14.dp))
            }
            Text(label, color = color, fontSize = fontSize, fontWeight = FontWeight.Medium, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
            if (trailing != null) Text(trailing, color = color, fontSize = fontSize, maxLines = 1)
            if (checked) Icon(Icons.Filled.Check, contentDescription = "Selected", tint = color, modifier = Modifier.size(22.dp))
        }
    }
}

/**
 * The hero's wide buttons: outlined in white, filled with the accent when focused
 * (START WATCHING with its play triangle, MORE INFO).
 */
@Composable
fun TvButton(text: String, onClick: () -> Unit, modifier: Modifier = Modifier, play: Boolean = false, onFocused: (() -> Unit)? = null) {
    TvFocusable(onClick = onClick, modifier = modifier, focusedBackground = RoxyColors.accent, onFocused = onFocused) { focused ->
        val color = if (focused) OnAccent else Color.White
        Row(
            modifier = Modifier.height(TvDims.HeroButtonHeight).border(2.dp, if (focused) RoxyColors.accent else Color.White).padding(horizontal = 32.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            if (play) {
                Icon(Icons.Filled.PlayArrow, contentDescription = null, tint = color, modifier = Modifier.size(26.dp))
                Spacer(Modifier.width(6.dp))
            }
            Text(text.uppercase(), color = color, fontSize = 18.sp, fontWeight = FontWeight.Medium, letterSpacing = 0.5.sp, maxLines = 1)
        }
    }
}

/** One tab of a title page's strip: the selected one sits in a dark box, the focused one on the accent. */
@Composable
fun TvTab(text: String, selected: Boolean, onClick: () -> Unit, modifier: Modifier = Modifier, onFocused: (() -> Unit)? = null, onDown: (() -> Unit)? = null, onUp: (() -> Unit)? = null) {
    TvFocusable(
        onClick = onClick,
        modifier = modifier.onPreviewKeyEvent { ev ->
            if (ev.type != KeyEventType.KeyDown) return@onPreviewKeyEvent false
            when (ev.key) {
                Key.DirectionDown -> { onDown?.invoke(); onDown != null }
                Key.DirectionUp -> { onUp?.invoke(); onUp != null }
                else -> false
            }
        },
        focusedBackground = RoxyColors.accent,
        background = if (selected) RoxyColors.surfaceHigh else Color.Transparent, onFocused = onFocused,
    ) { focused ->
        Box(modifier = Modifier.height(TvDims.TabHeight).padding(horizontal = 16.dp), contentAlignment = Alignment.Center) {
            Text(
                text.uppercase(), color = if (focused) OnAccent else if (selected) RoxyColors.onBackground else RoxyColors.muted,
                fontSize = 18.sp, fontWeight = FontWeight.Medium, letterSpacing = 0.5.sp, maxLines = 1,
            )
        }
    }
}

/** The small grey "16+" box that leads a facts line. */
@Composable
fun AgePill(text: String) {
    Text(
        text, color = Color.White, fontSize = 13.sp, fontWeight = FontWeight.Bold, lineHeight = 15.sp,
        modifier = Modifier.background(Color(0xFF3A3A44), RoundedCornerShape(2.dp)).padding(horizontal = 6.dp, vertical = 2.dp),
    )
}

/**
 * Crunchyroll's facts line: the age box, then "Sub | Dub" and the genres, separated by
 * small diamonds. [play] is the item whose audio tracks decide the Sub / Dub wording.
 */
@Composable
fun FactsLine(title: Item, play: Item?, modifier: Modifier = Modifier, fontSize: androidx.compose.ui.unit.TextUnit = 15.sp) {
    val age = title.officialRating?.takeIf { it.isNotBlank() }?.let { ageLabel(it) }
    val parts = listOfNotNull(audioBadge(play ?: title), title.genres.take(3).joinToString(", ").ifEmpty { null })
    if (age == null && parts.isEmpty()) return
    Row(modifier = modifier, verticalAlignment = Alignment.CenterVertically) {
        if (age != null) AgePill(age)
        parts.forEach { part ->
            if (age != null || part !== parts.first()) Text("  ◆  ", color = TvSubtle, fontSize = 11.sp, style = TvShadowed)
            Text(part, color = TvCopy, fontSize = fontSize, maxLines = 1, overflow = TextOverflow.Ellipsis, style = TvShadowed)
        }
    }
}

/** The centred uppercase title of a list page (WATCHLIST, HISTORY, BROWSE). */
@Composable
fun PageTitle(text: String, modifier: Modifier = Modifier) {
    Box(modifier = modifier.fillMaxWidth().padding(start = TvDims.RailWidth).height(84.dp), contentAlignment = Alignment.Center) {
        Text(text.uppercase(), color = RoxyColors.muted, fontSize = 20.sp, letterSpacing = 1.sp, fontWeight = FontWeight.Medium, textAlign = TextAlign.Center)
    }
}

/** A grey line for empty and error states, in the page's content column. */
@Composable
fun PageMessage(text: String, modifier: Modifier = Modifier, color: Color = RoxyColors.muted) {
    Box(modifier = modifier.fillMaxWidth().padding(horizontal = TvDims.ContentStart, vertical = 24.dp)) {
        Text(text, color = color, fontSize = 16.sp)
    }
}
