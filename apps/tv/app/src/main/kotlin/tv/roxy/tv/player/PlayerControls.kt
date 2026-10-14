@file:OptIn(androidx.compose.material3.ExperimentalMaterial3Api::class)

package tv.roxy.tv.player

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.focusable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.ClosedCaption
import androidx.compose.material.icons.filled.ClosedCaptionOff
import androidx.compose.material.icons.filled.Forward10
import androidx.compose.material.icons.filled.Fullscreen
import androidx.compose.material.icons.filled.FullscreenExit
import androidx.compose.material.icons.filled.Pause
import androidx.compose.material.icons.filled.PictureInPictureAlt
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Replay10
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material.icons.filled.SkipNext
import androidx.compose.material.icons.filled.VolumeOff
import androidx.compose.material.icons.filled.VolumeUp
import androidx.compose.material3.Icon
import androidx.compose.material3.Slider
import androidx.compose.material3.SliderDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.onKeyEvent
import androidx.compose.ui.input.key.type
import androidx.compose.ui.layout.layout
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.DpSize
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.IntSize
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.tv.material3.Text
import tv.roxy.tv.ui.components.tapClick

/** Snapshot of what the controls show; refreshed by the activity a few times a second. */
data class PlayerUiState(
    val title: String = "",
    val subtitle: String = "",            // series + SxxEyy, or the year for movies
    val positionSec: Double = 0.0,
    val durationSec: Double = 0.0,
    val bufferedSec: Double = 0.0,
    val paused: Boolean = false,
    val muted: Boolean = false,
    val speed: Double = 1.0,
    val subtitleTracks: List<MpvPlayer.Track> = emptyList(),
    val audioTracks: List<MpvPlayer.Track> = emptyList(),
    val currentSid: Int = 0,
    val currentAid: Int = 0,
    val quality: String = "",             // "1080p HEVC · hardware"
    val autoplay: Boolean = true,
    val fillScreen: Boolean = false,      // crop to fill (fullscreen button) vs letterbox
    val pipSupported: Boolean = false,
    val hasNext: Boolean = false,
)

/** What the controls ask the activity to do. */
interface PlayerActions {
    fun togglePause()
    fun seekTo(seconds: Double)
    fun seekBy(seconds: Int)
    fun selectSubtitle(id: Int)
    fun selectAudio(id: Int)
    fun setSpeed(speed: Double)
    fun toggleMute()
    fun toggleCaptions()
    fun toggleAutoplay()
    fun toggleFill()
    fun enterPip()
    fun next()
    fun back()
}

enum class Sheet { None, Settings, Subtitles, Audio, Quality, Speed }

private val Panel = Color(0xF21A1A22)
private val YtRed = Color(0xFFFF0033)
private val Muted = Color(0xFFB0B4BA)

/**
 * YouTube-style transport layer. Top: back + title. Centre (TV convenience): -10 s,
 * play/pause, +10 s. Bottom: red seek bar with a round thumb and scrubbing preview, then
 * a row with play/pause, volume, "elapsed / total" on the left and autoplay, captions,
 * settings (quality, speed, audio, subtitles), picture-in-picture, fullscreen on the right.
 * Tap or any key shows it; it auto-hides after a few seconds unless paused or a sheet is open.
 */
@Composable
fun PlayerControls(
    state: PlayerUiState,
    visible: Boolean,
    sheet: Sheet,
    onSheet: (Sheet) -> Unit,
    onInteraction: () -> Unit,
    actions: PlayerActions,
    trickplay: TrickplayPreview? = null,
) {
    if (!visible) return
    val playFocus = remember { FocusRequester() }
    LaunchedEffect(visible, sheet) { if (sheet == Sheet.None) runCatching { playFocus.requestFocus() } }

    Box(modifier = Modifier.fillMaxSize()) {
        Box(modifier = Modifier.fillMaxWidth().height(140.dp).align(Alignment.TopCenter).background(Brush.verticalGradient(0f to Color(0xB3000000), 1f to Color.Transparent)))
        Box(modifier = Modifier.fillMaxWidth().height(200.dp).align(Alignment.BottomCenter).background(Brush.verticalGradient(0f to Color.Transparent, 1f to Color(0xE6000000))))

        // Title row
        Row(modifier = Modifier.align(Alignment.TopStart).padding(horizontal = 24.dp, vertical = 20.dp), verticalAlignment = Alignment.CenterVertically) {
            IconButton(Icons.AutoMirrored.Filled.ArrowBack, "Back", onClick = actions::back, onFocus = onInteraction)
            Spacer(Modifier.width(12.dp))
            Column {
                Text(state.title, color = Color.White, fontSize = 20.sp, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                if (state.subtitle.isNotEmpty()) Text(state.subtitle, color = Muted, fontSize = 14.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }

        // Centre transport (big targets for the remote)
        if (sheet == Sheet.None) {
            Row(modifier = Modifier.align(Alignment.Center), horizontalArrangement = Arrangement.spacedBy(48.dp), verticalAlignment = Alignment.CenterVertically) {
                RoundButton(Icons.Filled.Replay10, "Back 10 seconds", size = 60.dp, onClick = { actions.seekBy(-10); onInteraction() }, onFocus = onInteraction)
                RoundButton(if (state.paused) Icons.Filled.PlayArrow else Icons.Filled.Pause, if (state.paused) "Play" else "Pause", size = 84.dp, iconSize = 44.dp, onClick = { actions.togglePause(); onInteraction() }, onFocus = onInteraction, focusRequester = playFocus)
                RoundButton(Icons.Filled.Forward10, "Forward 10 seconds", size = 60.dp, onClick = { actions.seekBy(10); onInteraction() }, onFocus = onInteraction)
            }
        }

        // Bottom: seek bar, then the YouTube button row
        Column(modifier = Modifier.align(Alignment.BottomCenter).fillMaxWidth().padding(horizontal = 24.dp, vertical = 12.dp)) {
            SeekBar(state, trickplay, modifier = Modifier.fillMaxWidth(), onSeek = { actions.seekTo(it); onInteraction() }, onStep = { actions.seekBy(it); onInteraction() }, onFocus = onInteraction)
            Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.fillMaxWidth().padding(top = 2.dp)) {
                // Left: play/pause, next, volume, time
                IconButton(if (state.paused) Icons.Filled.PlayArrow else Icons.Filled.Pause, if (state.paused) "Play" else "Pause", onClick = { actions.togglePause(); onInteraction() }, onFocus = onInteraction)
                if (state.hasNext) IconButton(Icons.Filled.SkipNext, "Next episode", onClick = { actions.next() }, onFocus = onInteraction)
                IconButton(if (state.muted) Icons.Filled.VolumeOff else Icons.Filled.VolumeUp, if (state.muted) "Unmute" else "Mute", onClick = { actions.toggleMute(); onInteraction() }, onFocus = onInteraction)
                Spacer(Modifier.width(6.dp))
                Text("${fmtTime(state.positionSec)} / ${fmtTime(state.durationSec)}", color = Color.White, fontSize = 13.sp)
                Spacer(Modifier.weight(1f))
                // Right: autoplay, captions, settings, PiP, fullscreen
                AutoplayToggle(state.autoplay, onClick = { actions.toggleAutoplay(); onInteraction() }, onFocus = onInteraction)
                IconButton(if (state.currentSid > 0) Icons.Filled.ClosedCaption else Icons.Filled.ClosedCaptionOff, "Subtitles", underline = state.currentSid > 0, onClick = { actions.toggleCaptions(); onInteraction() }, onLongClick = { onSheet(Sheet.Subtitles) }, onFocus = onInteraction)
                IconButton(Icons.Filled.Settings, "Settings", onClick = { onSheet(Sheet.Settings) }, onFocus = onInteraction)
                if (state.pipSupported) IconButton(Icons.Filled.PictureInPictureAlt, "Picture in picture", onClick = { actions.enterPip() }, onFocus = onInteraction)
                IconButton(if (state.fillScreen) Icons.Filled.FullscreenExit else Icons.Filled.Fullscreen, if (state.fillScreen) "Fit to screen" else "Fill screen", onClick = { actions.toggleFill(); onInteraction() }, onFocus = onInteraction)
            }
        }

        // Side sheet
        if (sheet != Sheet.None) {
            Box(modifier = Modifier.fillMaxHeight().width(380.dp).align(Alignment.CenterEnd).background(Panel).padding(24.dp)) {
                when (sheet) {
                    Sheet.Settings -> OptionSheet(
                        "Settings",
                        options = listOf(
                            "quality" to "Quality   ·   ${state.quality.ifEmpty { "Original" }}",
                            "speed" to "Playback speed   ·   ${if (state.speed == 1.0) "Normal" else "${state.speed}×"}",
                            "audio" to "Audio   ·   ${state.audioTracks.firstOrNull { it.id == state.currentAid }?.let { shortTrackName(it) } ?: "—"}",
                            "subtitles" to "Subtitles   ·   ${state.subtitleTracks.firstOrNull { it.id == state.currentSid }?.let { shortTrackName(it) } ?: "Off"}",
                        ),
                        selected = "", footer = null,
                        onPick = { onSheet(when (it) { "quality" -> Sheet.Quality; "speed" -> Sheet.Speed; "audio" -> Sheet.Audio; else -> Sheet.Subtitles }) },
                        onInteraction = onInteraction,
                    )
                    Sheet.Subtitles -> TrackSheet("Subtitles", state.subtitleTracks, state.currentSid, allowOff = true, onPick = { actions.selectSubtitle(it); onSheet(Sheet.None) }, onInteraction = onInteraction)
                    Sheet.Audio -> TrackSheet("Audio", state.audioTracks, state.currentAid, allowOff = false, onPick = { actions.selectAudio(it); onSheet(Sheet.None) }, onInteraction = onInteraction)
                    Sheet.Quality -> OptionSheet(
                        "Quality",
                        options = listOf("Original" to (state.quality.ifEmpty { "direct play" })),
                        selected = "Original",
                        footer = "Direct play: the file is sent as-is and decoded on this device. Server transcoding (lower bitrates, burned-in subtitles for weak devices) arrives in a later phase.",
                        onPick = { onSheet(Sheet.None) }, onInteraction = onInteraction,
                    )
                    Sheet.Speed -> OptionSheet(
                        "Playback speed",
                        options = listOf("0.5" to "0.5×", "0.75" to "0.75×", "1.0" to "Normal", "1.25" to "1.25×", "1.5" to "1.5×", "2.0" to "2×"),
                        selected = state.speed.toString(),
                        footer = null,
                        onPick = { actions.setSpeed(it.toDouble()); onSheet(Sheet.None) }, onInteraction = onInteraction,
                    )
                    Sheet.None -> {}
                }
            }
        }
    }
}

/**
 * Seek bar with scrubbing preview: YouTube red, round thumb, buffered range in grey.
 * Dragging shows the thumbnail for the hovered time and seeks on release; Left/Right steps
 * 10 s and shows the preview for a moment.
 */
@Composable
private fun SeekBar(state: PlayerUiState, trickplay: TrickplayPreview?, modifier: Modifier, onSeek: (Double) -> Unit, onStep: (Int) -> Unit, onFocus: () -> Unit) {
    var focused by remember { mutableStateOf(false) }
    val dur = state.durationSec.takeIf { it > 0 } ?: 1.0
    var dragFraction by remember { mutableStateOf<Float?>(null) }
    var stepPreviewSec by remember { mutableStateOf<Double?>(null) }
    var stepToken by remember { mutableStateOf(0) }
    LaunchedEffect(stepToken) { if (stepPreviewSec != null) { kotlinx.coroutines.delay(1500); stepPreviewSec = null } }
    val dragging = dragFraction != null
    // A finger held still on the thumb must keep the controls alive (auto-hide would dispose the slider mid-drag).
    LaunchedEffect(dragging) { while (dragging) { onFocus(); kotlinx.coroutines.delay(1500) } }
    val interaction = remember { MutableInteractionSource() }

    val previewSec: Double? = dragFraction?.let { it * dur } ?: stepPreviewSec
    val shownFraction = dragFraction ?: (state.positionSec / dur).toFloat().coerceIn(0f, 1f)

    BoxWithConstraints(modifier = modifier.height(28.dp)) {
        val barWidth = maxWidth
        if (previewSec != null) {
            val previewW = 240.dp
            val previewH = previewW * (trickplay?.manifest?.height ?: 180) / (trickplay?.manifest?.width ?: 320)
            val x = (barWidth * (previewSec / dur).toFloat().coerceIn(0f, 1f) - previewW / 2).coerceIn(0.dp, (barWidth - previewW).coerceAtLeast(0.dp))
            // Measured normally but reported as 0x0, so the preview floats above the bar without growing the row.
            Column(
                horizontalAlignment = Alignment.CenterHorizontally,
                modifier = Modifier
                    .layout { measurable, constraints ->
                        val p = measurable.measure(constraints.copy(minWidth = 0, minHeight = 0))
                        layout(0, 0) { p.placeRelative(x.roundToPx(), -(p.height + 8.dp.roundToPx())) }
                    }
                    .width(previewW),
            ) {
                Box(modifier = Modifier.size(previewW, previewH).clip(RoundedCornerShape(6.dp)).background(Color(0xFF202026)).border(BorderStroke(2.dp, Color.White), RoundedCornerShape(6.dp))) {
                    if (trickplay != null) TrickplayTile(trickplay, previewSec, modifier = Modifier.fillMaxSize())
                }
                Text(fmtTime(previewSec), color = Color.White, fontSize = 13.sp, fontWeight = FontWeight.SemiBold, modifier = Modifier.padding(top = 4.dp).background(Color(0xAA000000), RoundedCornerShape(4.dp)).padding(horizontal = 6.dp, vertical = 2.dp))
            }
        }
        // Track: grey base, lighter buffered range, red played range (drawn by the slider).
        Box(modifier = Modifier.fillMaxWidth().height(3.dp).align(Alignment.Center).clip(RoundedCornerShape(2.dp)).background(Color(0x66FFFFFF)))
        Box(modifier = Modifier.fillMaxWidth((state.bufferedSec / dur).toFloat().coerceIn(0f, 1f)).height(3.dp).align(Alignment.CenterStart).clip(RoundedCornerShape(2.dp)).background(Color(0x99FFFFFF)))
        val colors = SliderDefaults.colors(thumbColor = YtRed, activeTrackColor = YtRed, inactiveTrackColor = Color.Transparent)
        val big = focused || dragging
        Slider(
            value = shownFraction,
            onValueChange = { dragFraction = it; onFocus() },
            onValueChangeFinished = { dragFraction?.let { onSeek(it * dur) }; dragFraction = null },
            colors = colors,
            interactionSource = interaction,
            thumb = { SliderDefaults.Thumb(interactionSource = interaction, colors = colors, thumbSize = DpSize(if (big) 18.dp else 13.dp, if (big) 18.dp else 13.dp)) },
            track = { s -> SliderDefaults.Track(sliderState = s, colors = colors, modifier = Modifier.height(if (big) 5.dp else 3.dp), thumbTrackGapSize = 0.dp, trackInsideCornerSize = 0.dp, drawStopIndicator = null) },
            modifier = Modifier
                .fillMaxWidth()
                .align(Alignment.Center)
                .onFocusChanged { focused = it.isFocused; if (it.isFocused) onFocus() }
                .onKeyEvent { ev ->
                    if (ev.type != KeyEventType.KeyDown) return@onKeyEvent false
                    val step = when (ev.key) { Key.DirectionLeft -> -10; Key.DirectionRight -> 10; else -> return@onKeyEvent false }
                    val target = ((stepPreviewSec ?: state.positionSec) + step).coerceIn(0.0, dur)
                    onStep(step)
                    stepPreviewSec = target
                    stepToken++
                    true
                },
        )
    }
}

/** Draws the trickplay tile for [positionSec], loading its sheet on demand. */
@Composable
private fun TrickplayTile(trickplay: TrickplayPreview, positionSec: Double, modifier: Modifier) {
    val m = trickplay.manifest
    val tile = m.tileIndex(positionSec)
    val sheetIndex = m.sheetOf(tile)
    var image by remember(sheetIndex) { mutableStateOf(trickplay.cached(sheetIndex)) }
    LaunchedEffect(sheetIndex) { if (image == null) image = trickplay.sheet(sheetIndex) }
    val img = image ?: return
    Canvas(modifier = modifier) {
        drawImage(
            image = img,
            srcOffset = IntOffset(m.columnOf(tile) * m.width, m.rowOf(tile) * m.height),
            srcSize = IntSize(m.width, m.height),
            dstSize = IntSize(size.width.toInt(), size.height.toInt()),
        )
    }
}

/** Flat icon button for the bottom row: white icon, white focus ring. */
@Composable
private fun IconButton(icon: ImageVector, label: String, onClick: () -> Unit, onFocus: () -> Unit, onLongClick: (() -> Unit)? = null, underline: Boolean = false) {
    var focused by remember { mutableStateOf(false) }
    Box(
        contentAlignment = Alignment.Center,
        modifier = Modifier
            .size(44.dp)
            .onFocusChanged { focused = it.isFocused; if (it.isFocused) onFocus() }
            .focusable()
            .onKeyEvent { ev -> if (ev.type == KeyEventType.KeyDown && (ev.key == Key.DirectionCenter || ev.key == Key.Enter)) { onClick(); true } else false }
            .clip(CircleShape)
            .background(if (focused) Color(0x33FFFFFF) else Color.Transparent)
            .tapClick(onClick),
    ) {
        Column(horizontalAlignment = Alignment.CenterHorizontally) {
            Icon(icon, contentDescription = label, tint = Color.White, modifier = Modifier.size(26.dp))
            Box(modifier = Modifier.padding(top = 2.dp).width(18.dp).height(2.dp).background(if (underline) YtRed else Color.Transparent))
        }
    }
}

/** Big round centre button. */
@Composable
private fun RoundButton(icon: ImageVector, label: String, size: Dp, iconSize: Dp = 30.dp, onClick: () -> Unit, onFocus: () -> Unit, focusRequester: FocusRequester? = null) {
    var focused by remember { mutableStateOf(false) }
    Box(
        contentAlignment = Alignment.Center,
        modifier = Modifier
            .size(size)
            .then(if (focusRequester != null) Modifier.focusRequester(focusRequester) else Modifier)
            .onFocusChanged { focused = it.isFocused; if (it.isFocused) onFocus() }
            .focusable()
            .onKeyEvent { ev -> if (ev.type == KeyEventType.KeyDown && (ev.key == Key.DirectionCenter || ev.key == Key.Enter)) { onClick(); true } else false }
            .clip(CircleShape)
            .background(if (focused) Color.White else Color(0x66000000))
            .border(BorderStroke(2.dp, if (focused) Color.White else Color(0x66FFFFFF)), CircleShape)
            .tapClick(onClick),
    ) {
        Icon(icon, contentDescription = label, tint = if (focused) Color.Black else Color.White, modifier = Modifier.size(iconSize))
    }
}

/** YouTube's autoplay switch: a small pill with a knob that turns red when on. */
@Composable
private fun AutoplayToggle(on: Boolean, onClick: () -> Unit, onFocus: () -> Unit) {
    var focused by remember { mutableStateOf(false) }
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .onFocusChanged { focused = it.isFocused; if (it.isFocused) onFocus() }
            .focusable()
            .onKeyEvent { ev -> if (ev.type == KeyEventType.KeyDown && (ev.key == Key.DirectionCenter || ev.key == Key.Enter)) { onClick(); true } else false }
            .clip(RoundedCornerShape(20.dp))
            .background(if (focused) Color(0x33FFFFFF) else Color.Transparent)
            .tapClick(onClick)
            .padding(horizontal = 10.dp, vertical = 8.dp),
    ) {
        Text("Autoplay", color = Color.White, fontSize = 12.sp, modifier = Modifier.padding(end = 8.dp))
        Box(modifier = Modifier.width(34.dp).height(14.dp).clip(RoundedCornerShape(7.dp)).background(if (on) Color(0x99FFFFFF) else Color(0x55FFFFFF))) {
            Box(modifier = Modifier.size(18.dp).align(if (on) Alignment.CenterEnd else Alignment.CenterStart).clip(CircleShape).background(if (on) YtRed else Color(0xFFDDDDDD)), contentAlignment = Alignment.Center) {
                Icon(if (on) Icons.Filled.PlayArrow else Icons.Filled.Pause, contentDescription = null, tint = Color.White, modifier = Modifier.size(12.dp))
            }
        }
    }
}

@Composable
private fun TrackSheet(title: String, tracks: List<MpvPlayer.Track>, current: Int, allowOff: Boolean, onPick: (Int) -> Unit, onInteraction: () -> Unit) {
    val options = buildList {
        if (allowOff) add("0" to "Off")
        tracks.forEach { t ->
            val name = listOf(t.title, langName(t.lang), t.codec.uppercase()).filter { it.isNotEmpty() }.joinToString(" · ")
            add(t.id.toString() to name.ifEmpty { "Track ${t.id}" })
        }
    }
    OptionSheet(title, options, selected = current.toString(), footer = null, onPick = { onPick(it.toInt()) }, onInteraction = onInteraction)
}

@Composable
private fun OptionSheet(title: String, options: List<Pair<String, String>>, selected: String, footer: String?, onPick: (String) -> Unit, onInteraction: () -> Unit) {
    val first = remember { FocusRequester() }
    LaunchedEffect(title) { runCatching { first.requestFocus() } }
    val focusKey = (options.firstOrNull { it.first == selected } ?: options.firstOrNull())?.first
    Column {
        Text(title, color = Color.White, fontSize = 18.sp, fontWeight = FontWeight.SemiBold, modifier = Modifier.padding(bottom = 12.dp))
        LazyColumn(modifier = Modifier.weight(1f, fill = false)) {
            items(options, key = { it.first }) { (key, label) ->
                var focused by remember { mutableStateOf(false) }
                val isSel = key == selected
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    modifier = Modifier
                        .fillMaxWidth()
                        .then(if (key == focusKey) Modifier.focusRequester(first) else Modifier)
                        .onFocusChanged { focused = it.isFocused; if (it.isFocused) onInteraction() }
                        .focusable()
                        .onKeyEvent { ev -> if (ev.type == KeyEventType.KeyDown && (ev.key == Key.DirectionCenter || ev.key == Key.Enter)) { onPick(key); true } else false }
                        .clip(RoundedCornerShape(8.dp))
                        .background(if (focused) Color.White else Color.Transparent)
                        .tapClick { onPick(key) }
                        .padding(horizontal = 12.dp, vertical = 10.dp),
                ) {
                    if (selected.isNotEmpty()) {
                        Text(if (isSel) "●" else "○", color = if (focused) Color.Black else if (isSel) YtRed else Muted, fontSize = 14.sp)
                        Spacer(Modifier.width(12.dp))
                    }
                    Text(label, color = if (focused) Color.Black else Color.White, fontSize = 15.sp, maxLines = 2, overflow = TextOverflow.Ellipsis)
                }
            }
        }
        if (footer != null) Text(footer, color = Muted, fontSize = 12.sp, modifier = Modifier.padding(top = 12.dp))
    }
}

/** Language first ("English"), then a short title hint when several tracks share it. */
private fun shortTrackName(t: MpvPlayer.Track): String {
    val lang = langName(t.lang)
    return when {
        lang.isNotEmpty() && t.title.isNotEmpty() -> "$lang · ${t.title.take(18)}"
        lang.isNotEmpty() -> lang
        t.title.isNotEmpty() -> t.title.take(24)
        else -> "Track ${t.id}"
    }
}

fun langName(code: String): String = when (code.lowercase()) {
    "eng", "en" -> "English"
    "jpn", "ja" -> "Japanese"
    "spa", "es" -> "Spanish"
    "por", "pt" -> "Portuguese"
    "fra", "fre", "fr" -> "French"
    "deu", "ger", "de" -> "German"
    "ita", "it" -> "Italian"
    "rus", "ru" -> "Russian"
    "zho", "chi", "zh" -> "Chinese"
    "kor", "ko" -> "Korean"
    "ara", "ar" -> "Arabic"
    "" -> ""
    else -> code
}

fun fmtTime(d: Double): String {
    val s = d.toLong().coerceAtLeast(0)
    return if (s >= 3600) "%d:%02d:%02d".format(s / 3600, (s % 3600) / 60, s % 60) else "%d:%02d".format(s / 60, s % 60)
}
