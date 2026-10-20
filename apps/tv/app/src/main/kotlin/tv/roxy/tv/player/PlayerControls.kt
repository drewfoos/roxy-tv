package tv.roxy.tv.player

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.focusable
import androidx.compose.foundation.gestures.detectDragGestures
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.Check
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
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.rememberScrollState
import androidx.compose.runtime.compositionLocalOf
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.onKeyEvent
import androidx.compose.ui.input.key.type
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.input.InputMode
import androidx.compose.ui.platform.LocalInputModeManager
import androidx.compose.ui.layout.layout
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Constraints
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.IntSize
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.tv.material3.Text
import tv.roxy.tv.api.MediaSegment
import tv.roxy.tv.ui.components.tapClick
import tv.roxy.tv.ui.theme.RoxyColors

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
    val fillScreen: Boolean = false,      // crop to fill (fullscreen button) vs letterbox
    val pipSupported: Boolean = false,
    val hasNext: Boolean = false,
    /** The next-episode card is up: the Skip button yields its corner to it. */
    val nextCardShown: Boolean = false,
    val segments: List<MediaSegment> = emptyList(),
    val autoSkip: Boolean = false,
    /** Hosted on a page: false = in its 16:9 box, true = filling the screen; null = the player owns the screen. */
    val fullscreen: Boolean? = null,
) {
    /** The skippable segment the playhead is inside, if any. */
    val activeSegment: MediaSegment? get() = segments.firstOrNull { positionSec >= it.startSec && positionSec < it.endSec - 1.0 }
}

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
    fun toggleAutoSkip()
    fun toggleFill()
    fun enterPip()
    fun toggleFullscreen()
    fun next()
    fun back()
}

enum class Sheet { None, Settings, Subtitles, Audio, Quality, Speed }

private val Panel = Color(0xF21C1C22)
private val YtRed = Color(0xFFFF0033)
private val Muted = Color(0xFFB0B4BA)

/**
 * YouTube-style transport layer. Top: back + title. Centre (TV convenience): -10 s,
 * play/pause, +10 s. Bottom: red seek bar with a round thumb and scrubbing preview, then
 * a row with play/pause, next, volume, "elapsed / total" on the left and captions,
 * settings, picture-in-picture, fullscreen on the right. Settings opens a YouTube-style
 * popup above the gear; tapping anywhere else closes it. A "Skip intro/outro" button
 * appears over the video while inside a segment (even with the controls hidden).
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
    // In the page's 16:9 box everything is scaled down: smaller discs and icons, no clock,
    // tighter popups that stay inside the box.
    val compact = state.fullscreen == false
    // Hosted on a page (the phone): Crunchyroll's layout. Back at the top-left; next,
    // settings and fullscreen at the top-right; bare -10 / pause / +10 in the middle;
    // the times over an edge-to-edge seek bar at the bottom and nothing else. The
    // full-screen host (TV, movies) keeps the YouTube layout with its button row.
    val cr = state.fullscreen != null
    // Skip button lives outside the auto-hidden layer so it is there when you need it.
    state.activeSegment?.let { seg ->
        if (!state.autoSkip && !state.nextCardShown) {
            SkipButton(seg.label, onClick = { actions.seekTo(seg.endSec); onInteraction() }, modifier = Modifier.fillMaxSize().padding(end = if (compact) 12.dp else 32.dp, bottom = if (visible) (if (compact) 60.dp else 118.dp) else (if (compact) 16.dp else 40.dp)))
        }
    }
    if (!visible) return
    val playFocus = remember { FocusRequester() }
    LaunchedEffect(visible, sheet) { if (sheet == Sheet.None) runCatching { playFocus.requestFocus() } }

    Box(modifier = Modifier.fillMaxSize()) {
        Box(modifier = Modifier.fillMaxWidth().height(if (compact) 70.dp else 140.dp).align(Alignment.TopCenter).background(Brush.verticalGradient(0f to Color(0xB3000000), 1f to Color.Transparent)))
        Box(modifier = Modifier.fillMaxWidth().height(if (compact) 100.dp else 200.dp).align(Alignment.BottomCenter).background(Brush.verticalGradient(0f to Color.Transparent, 1f to Color(0xE6000000))))

        // Title row, with the clock and "Ends at" top-right (local time zone).
        Row(modifier = Modifier.align(Alignment.TopStart).fillMaxWidth().padding(horizontal = if (compact) 8.dp else if (cr) 16.dp else 24.dp, vertical = if (compact) 4.dp else if (cr) 10.dp else 20.dp), verticalAlignment = Alignment.CenterVertically) {
            IconButton(Icons.AutoMirrored.Filled.ArrowBack, "Back", onClick = actions::back, onFocus = onInteraction, compact = compact)
            Spacer(Modifier.width(if (compact) 6.dp else 12.dp))
            Column(modifier = Modifier.weight(1f)) {
                when {
                    // The mini player carries no title: the page spells it out right under the video.
                    compact -> {}
                    // Crunchyroll's: the series small on top, "E3 — Title" under it, both modest.
                    cr -> {
                        val (series, code) = splitSubtitle(state.subtitle)
                        if (series.isNotEmpty()) Text(series, color = Muted, fontSize = 12.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        Text(listOfNotNull(code, state.title.takeIf { it.isNotEmpty() }).joinToString(" — "), color = Color.White, fontSize = 15.sp, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    }
                    else -> {
                        Text(state.title, color = Color.White, fontSize = 20.sp, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        if (state.subtitle.isNotEmpty()) Text(state.subtitle, color = Muted, fontSize = 14.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    }
                }
            }
            if (cr) {
                // Next, settings (everything lives in there: subtitles, audio, speed, skip), fullscreen.
                if (state.hasNext) IconButton(Icons.Filled.SkipNext, "Next episode", onClick = { actions.next() }, onFocus = onInteraction, compact = compact)
                IconButton(Icons.Filled.Settings, "Settings", onClick = { onSheet(if (sheet == Sheet.None) Sheet.Settings else Sheet.None) }, onFocus = onInteraction, compact = compact)
                if (state.fullscreen == true) IconButton(Icons.Filled.FullscreenExit, "Exit fullscreen", onClick = { actions.toggleFullscreen() }, onFocus = onInteraction, compact = compact)
                else IconButton(Icons.Filled.Fullscreen, "Fullscreen", onClick = { actions.toggleFullscreen() }, onFocus = onInteraction, compact = compact)
            } else {
                Spacer(Modifier.width(16.dp))
                Column(horizontalAlignment = Alignment.End) {
                    Text(clockNow(), color = Color.White, fontSize = 16.sp, fontWeight = FontWeight.SemiBold)
                    val remaining = (state.durationSec - state.positionSec) / state.speed.coerceAtLeast(0.25)
                    if (state.durationSec > 0) Text("Ends at ${endsAt(remaining)}", color = Muted, fontSize = 13.sp)
                }
            }
        }

        // Centre transport (big targets for the remote)
        if (sheet == Sheet.None) {
            Row(modifier = Modifier.align(Alignment.Center), horizontalArrangement = Arrangement.spacedBy(if (compact) 36.dp else 48.dp), verticalAlignment = Alignment.CenterVertically) {
                RoundButton(Icons.Filled.Replay10, "Back 10 seconds", size = if (compact) 40.dp else 60.dp, iconSize = if (compact) 26.dp else if (cr) 36.dp else 30.dp, disc = !cr, onClick = { actions.seekBy(-10); onInteraction() }, onFocus = onInteraction)
                RoundButton(if (state.paused) Icons.Filled.PlayArrow else Icons.Filled.Pause, if (state.paused) "Play" else "Pause", size = if (compact) 52.dp else 76.dp, iconSize = if (compact) 36.dp else if (cr) 52.dp else 42.dp, disc = !cr, onClick = { actions.togglePause(); onInteraction() }, onFocus = onInteraction, focusRequester = playFocus)
                RoundButton(Icons.Filled.Forward10, "Forward 10 seconds", size = if (compact) 40.dp else 60.dp, iconSize = if (compact) 26.dp else if (cr) 36.dp else 30.dp, disc = !cr, onClick = { actions.seekBy(10); onInteraction() }, onFocus = onInteraction)
            }
        }

        // Bottom. On the page: the elapsed and total times, then the seek bar flush with the
        // edges (Crunchyroll). Elsewhere: seek bar, then the YouTube button row, sharing an inset.
        if (cr) Column(modifier = Modifier.align(Alignment.BottomCenter).fillMaxWidth()) {
            Row(modifier = Modifier.fillMaxWidth().padding(horizontal = if (compact) 10.dp else 16.dp)) {
                Text(fmtTime(state.positionSec), color = Color.White, fontSize = if (compact) 11.sp else 13.sp)
                Spacer(Modifier.weight(1f))
                Text(fmtTime(state.durationSec), color = Color.White, fontSize = if (compact) 11.sp else 13.sp)
            }
            SeekBar(state, trickplay, modifier = Modifier.fillMaxWidth(), onSeek = { actions.seekTo(it); onInteraction() }, onStep = { actions.seekBy(it); onInteraction() }, onFocus = onInteraction, color = RoxyColors.accent, flush = true)
        }
        else Column(modifier = Modifier.align(Alignment.BottomCenter).fillMaxWidth().padding(horizontal = 24.dp, vertical = 10.dp)) {
            SeekBar(state, trickplay, modifier = Modifier.fillMaxWidth(), onSeek = { actions.seekTo(it); onInteraction() }, onStep = { actions.seekBy(it); onInteraction() }, onFocus = onInteraction)
            Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.fillMaxWidth()) {
                if (!compact) IconButton(if (state.paused) Icons.Filled.PlayArrow else Icons.Filled.Pause, if (state.paused) "Play" else "Pause", onClick = { actions.togglePause(); onInteraction() }, onFocus = onInteraction)
                if (state.hasNext) IconButton(Icons.Filled.SkipNext, "Next episode", onClick = { actions.next() }, onFocus = onInteraction, compact = compact)
                if (!compact) IconButton(if (state.muted) Icons.Filled.VolumeOff else Icons.Filled.VolumeUp, if (state.muted) "Unmute" else "Mute", onClick = { actions.toggleMute(); onInteraction() }, onFocus = onInteraction)
                Spacer(Modifier.width(6.dp))
                Text("${fmtTime(state.positionSec)} / ${fmtTime(state.durationSec)}", color = Color.White, fontSize = if (compact) 11.sp else 13.sp)
                Spacer(Modifier.weight(1f))
                IconButton(if (state.currentSid > 0) Icons.Filled.ClosedCaption else Icons.Filled.ClosedCaptionOff, "Subtitles", underline = state.currentSid > 0, onClick = { actions.toggleCaptions(); onInteraction() }, onFocus = onInteraction, compact = compact)
                IconButton(Icons.Filled.Settings, "Settings", onClick = { onSheet(if (sheet == Sheet.None) Sheet.Settings else Sheet.None) }, onFocus = onInteraction, compact = compact)
                if (state.pipSupported) IconButton(Icons.Filled.PictureInPictureAlt, "Picture in picture", onClick = { actions.enterPip() }, onFocus = onInteraction, compact = compact)
                when (state.fullscreen) {
                    // On a page the button turns the phone; the crop-to-fill toggle stays for the full-screen host (pinch zooms either way).
                    null -> IconButton(if (state.fillScreen) Icons.Filled.FullscreenExit else Icons.Filled.Fullscreen, if (state.fillScreen) "Fit to screen" else "Fill screen", onClick = { actions.toggleFill(); onInteraction() }, onFocus = onInteraction)
                    false -> IconButton(Icons.Filled.Fullscreen, "Fullscreen", onClick = { actions.toggleFullscreen() }, onFocus = onInteraction, compact = true)
                    true -> IconButton(Icons.Filled.FullscreenExit, "Exit fullscreen", onClick = { actions.toggleFullscreen() }, onFocus = onInteraction)
                }
            }
        }

        // Settings popup (YouTube-style): anchored bottom-right above the button row; a
        // transparent scrim behind it closes it on tap.
        if (sheet != Sheet.None) {
            Box(modifier = Modifier.fillMaxSize().pointerInput(Unit) { detectTapGestures { onSheet(Sheet.None) } })
            Box(modifier = if (cr) Modifier.align(Alignment.TopEnd).padding(end = if (compact) 8.dp else 16.dp, top = if (compact) 40.dp else 56.dp, bottom = if (compact) 24.dp else 40.dp) else Modifier.align(Alignment.BottomEnd).padding(end = 24.dp, bottom = 64.dp)) {
                CompositionLocalProvider(LocalCompactMenu provides compact) { when (sheet) {
                    Sheet.Settings -> MenuPopup(
                        title = null,
                        rows = listOf(
                            MenuRow("quality", "Quality", state.quality.ifEmpty { "Original" }, submenu = true),
                            MenuRow("speed", "Playback speed", if (state.speed == 1.0) "Normal" else "${state.speed}×", submenu = true),
                            MenuRow("audio", "Audio", state.audioTracks.firstOrNull { it.id == state.currentAid }?.let { shortTrackName(it) } ?: "—", submenu = true),
                            MenuRow("subtitles", "Subtitles", state.subtitleTracks.firstOrNull { it.id == state.currentSid }?.let { shortTrackName(it) } ?: "Off", submenu = true),
                            MenuRow("autoskip", "Auto-skip intro & outro", if (state.autoSkip) "On" else "Off", checked = state.autoSkip),
                        ),
                        onPick = { key ->
                            when (key) {
                                "quality" -> onSheet(Sheet.Quality)
                                "speed" -> onSheet(Sheet.Speed)
                                "audio" -> onSheet(Sheet.Audio)
                                "subtitles" -> onSheet(Sheet.Subtitles)
                                "autoskip" -> { actions.toggleAutoSkip(); onInteraction() }
                            }
                        },
                        onInteraction = onInteraction,
                    )
                    Sheet.Subtitles -> MenuPopup(
                        title = "Subtitles",
                        rows = listOf(MenuRow("0", "Off", checked = state.currentSid == 0)) + state.subtitleTracks.map { t -> MenuRow(t.id.toString(), trackLabel(t), checked = t.id == state.currentSid) },
                        onPick = { actions.selectSubtitle(it.toInt()); onSheet(Sheet.None) }, onBack = { onSheet(Sheet.Settings) }, onInteraction = onInteraction,
                    )
                    Sheet.Audio -> MenuPopup(
                        title = "Audio",
                        rows = state.audioTracks.map { t -> MenuRow(t.id.toString(), trackLabel(t), checked = t.id == state.currentAid) },
                        onPick = { actions.selectAudio(it.toInt()); onSheet(Sheet.None) }, onBack = { onSheet(Sheet.Settings) }, onInteraction = onInteraction,
                    )
                    Sheet.Quality -> MenuPopup(
                        title = "Quality",
                        rows = listOf(MenuRow("original", "Original", state.quality.ifEmpty { "direct play" }, checked = true)),
                        footer = "Direct play: the file is decoded on this device as-is. Lower qualities arrive with server transcoding.",
                        onPick = { onSheet(Sheet.None) }, onBack = { onSheet(Sheet.Settings) }, onInteraction = onInteraction,
                    )
                    Sheet.Speed -> MenuPopup(
                        title = "Playback speed",
                        rows = listOf("0.5" to "0.5×", "0.75" to "0.75×", "1.0" to "Normal", "1.25" to "1.25×", "1.5" to "1.5×", "2.0" to "2×").map { (k, l) -> MenuRow(k, l, checked = state.speed.toString() == k) },
                        onPick = { actions.setSpeed(it.toDouble()); onSheet(Sheet.None) }, onBack = { onSheet(Sheet.Settings) }, onInteraction = onInteraction,
                    )
                    Sheet.None -> {}
                } }
            }
        }
    }
}

/** "KONOSUBA ...  S01E01" -> ("KONOSUBA ...", "E1"); a movie's year stays as the series line. */
private fun splitSubtitle(sub: String): Pair<String, String?> {
    val m = Regex("""\s*S(\d+)E(\d+)\s*$""").find(sub) ?: return sub to null
    return sub.substring(0, m.range.first).trim() to "E${m.groupValues[2].toInt()}"
}

/** True inside the page's 16:9 box: the popups shrink to fit it. */
private val LocalCompactMenu = compositionLocalOf { false }

/** Netflix/YouTube-style "Skip intro" pill, bottom-right over the video. */
@Composable
private fun SkipButton(label: String, onClick: () -> Unit, modifier: Modifier) {
    var focused by remember { mutableStateOf(false) }
    Box(modifier = modifier, contentAlignment = Alignment.BottomEnd) {
        Row(
            verticalAlignment = Alignment.CenterVertically,
            modifier = Modifier
                .onFocusChanged { focused = it.isFocused }
                .focusable()
                .onKeyEvent { ev -> if (ev.type == KeyEventType.KeyDown && (ev.key == Key.DirectionCenter || ev.key == Key.Enter)) { onClick(); true } else false }
                .clip(RoundedCornerShape(4.dp))
                .background(if (focused) Color.White else Color(0xE6202020))
                .border(BorderStroke(1.dp, Color(0x99FFFFFF)), RoundedCornerShape(4.dp))
                .tapClick(onClick)
                .padding(horizontal = 18.dp, vertical = 12.dp),
        ) {
            Text(label, color = if (focused) Color.Black else Color.White, fontSize = 15.sp, fontWeight = FontWeight.SemiBold)
            Icon(Icons.Filled.SkipNext, contentDescription = null, tint = if (focused) Color.Black else Color.White, modifier = Modifier.padding(start = 6.dp).size(20.dp))
        }
    }
}

/**
 * Seek bar drawn by hand (Material's slider insets the track for its thumb, which left
 * an un-red gap at the start and a bar narrower than the button row). YouTube red,
 * round thumb that grows on focus/drag, buffered range in lighter grey. Dragging shows
 * the trickplay tile for the hovered time and seeks on release; Left/Right steps 10 s.
 */
@Composable
private fun SeekBar(state: PlayerUiState, trickplay: TrickplayPreview?, modifier: Modifier, onSeek: (Double) -> Unit, onStep: (Int) -> Unit, onFocus: () -> Unit, color: Color = YtRed, flush: Boolean = false) {
    var focused by remember { mutableStateOf(false) }
    val dur = state.durationSec.takeIf { it > 0 } ?: 1.0
    var dragFraction by remember { mutableStateOf<Float?>(null) }
    var stepPreviewSec by remember { mutableStateOf<Double?>(null) }
    var stepToken by remember { mutableStateOf(0) }
    LaunchedEffect(stepToken) { if (stepPreviewSec != null) { kotlinx.coroutines.delay(1500); stepPreviewSec = null } }
    val dragging = dragFraction != null
    LaunchedEffect(dragging) { while (dragging) { onFocus(); kotlinx.coroutines.delay(1500) } }

    val previewSec: Double? = dragFraction?.let { it * dur } ?: stepPreviewSec
    val shownFraction = dragFraction ?: (state.positionSec / dur).toFloat().coerceIn(0f, 1f)
    val bufferedFraction = (state.bufferedSec / dur).toFloat().coerceIn(0f, 1f)
    val big = focused || dragging

    BoxWithConstraints(modifier = modifier.height(if (flush) 20.dp else 32.dp)) {
        val barWidth = maxWidth
        // Preview bubble: measured unconstrained and reported as 0x0 so it floats above the bar.
        if (previewSec != null) {
            val m = trickplay?.manifest
            val previewW = 240.dp
            val previewH = if (m != null && m.width > 0) previewW * m.height / m.width else previewW * 9 / 16
            val x = (barWidth * (previewSec / dur).toFloat().coerceIn(0f, 1f) - previewW / 2).coerceIn(0.dp, (barWidth - previewW).coerceAtLeast(0.dp))
            Column(
                horizontalAlignment = Alignment.CenterHorizontally,
                modifier = Modifier.layout { measurable, _ ->
                    val p = measurable.measure(Constraints())
                    layout(0, 0) { p.placeRelative(x.roundToPx(), -(p.height + 6.dp.roundToPx())) }
                }.width(previewW),
            ) {
                if (trickplay != null) {
                    Box(modifier = Modifier.size(previewW, previewH).clip(RoundedCornerShape(6.dp)).background(Color(0xFF202026)).border(BorderStroke(2.dp, Color.White), RoundedCornerShape(6.dp))) {
                        TrickplayTile(trickplay, previewSec, modifier = Modifier.fillMaxSize())
                    }
                }
                Text(fmtTime(previewSec), color = Color.White, fontSize = 13.sp, fontWeight = FontWeight.SemiBold, modifier = Modifier.padding(top = 4.dp).background(Color(0xCC000000), RoundedCornerShape(4.dp)).padding(horizontal = 6.dp, vertical = 2.dp))
            }
        }
        Canvas(
            modifier = Modifier
                .fillMaxSize()
                .onFocusChanged { focused = it.isFocused; if (it.isFocused) onFocus() }
                .focusable()
                .onKeyEvent { ev ->
                    if (ev.type != KeyEventType.KeyDown) return@onKeyEvent false
                    val step = when (ev.key) { Key.DirectionLeft -> -10; Key.DirectionRight -> 10; else -> return@onKeyEvent false }
                    val target = ((stepPreviewSec ?: state.positionSec) + step).coerceIn(0.0, dur)
                    onStep(step)
                    stepPreviewSec = target
                    stepToken++
                    true
                }
                .pointerInput(dur) {
                    detectTapGestures { pos -> onSeek((pos.x / size.width).toDouble().coerceIn(0.0, 1.0) * dur) }
                }
                .pointerInput(dur) {
                    detectDragGestures(
                        onDragStart = { pos -> dragFraction = (pos.x / size.width).coerceIn(0f, 1f) },
                        onDrag = { change, _ -> dragFraction = (change.position.x / size.width).coerceIn(0f, 1f) },
                        onDragEnd = { dragFraction?.let { onSeek(it * dur) }; dragFraction = null },
                        onDragCancel = { dragFraction = null },
                    )
                },
        ) {
            val trackH = (if (big) 5.dp else 3.dp).toPx()
            val y = size.height / 2
            val r = CornerRadius(trackH / 2)
            drawRoundRect(Color(0x66FFFFFF), topLeft = Offset(0f, y - trackH / 2), size = Size(size.width, trackH), cornerRadius = r)
            drawRoundRect(Color(0x99FFFFFF), topLeft = Offset(0f, y - trackH / 2), size = Size(size.width * bufferedFraction, trackH), cornerRadius = r)
            drawRoundRect(color, topLeft = Offset(0f, y - trackH / 2), size = Size(size.width * shownFraction, trackH), cornerRadius = r)
            // Segment markers (intro/outro) as slightly brighter ticks on the track.
            for (seg in state.segments) {
                val sx = (seg.startSec / dur).toFloat().coerceIn(0f, 1f) * size.width
                val ex = (seg.endSec / dur).toFloat().coerceIn(0f, 1f) * size.width
                drawRoundRect(Color(0x55FFFFFF), topLeft = Offset(sx, y - trackH / 2), size = Size((ex - sx).coerceAtLeast(2f), trackH), cornerRadius = r)
            }
            drawCircle(color, radius = (if (big) 9.dp else 6.dp).toPx(), center = Offset(size.width * shownFraction, y))
        }
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

/** Flat icon button for the bottom row: white icon, white focus ring, optional red underline. */
@Composable
private fun IconButton(icon: ImageVector, label: String, onClick: () -> Unit, onFocus: () -> Unit, underline: Boolean = false, compact: Boolean = false) {
    var focused by remember { mutableStateOf(false) }
    Box(
        contentAlignment = Alignment.Center,
        modifier = Modifier
            .size(if (compact) 34.dp else 44.dp)
            .onFocusChanged { focused = it.isFocused; if (it.isFocused) onFocus() }
            .focusable()
            .onKeyEvent { ev -> if (ev.type == KeyEventType.KeyDown && (ev.key == Key.DirectionCenter || ev.key == Key.Enter)) { onClick(); true } else false }
            .clip(CircleShape)
            .background(if (focused) Color(0x33FFFFFF) else Color.Transparent)
            .tapClick(onClick),
    ) {
        Column(horizontalAlignment = Alignment.CenterHorizontally) {
            Icon(icon, contentDescription = label, tint = Color.White, modifier = Modifier.size(if (compact) 20.dp else 26.dp))
            Box(modifier = Modifier.padding(top = 2.dp).width(if (compact) 14.dp else 18.dp).height(2.dp).background(if (underline) YtRed else Color.Transparent))
        }
    }
}

/** Big round centre button. */
@Composable
private fun RoundButton(icon: ImageVector, label: String, size: Dp, iconSize: Dp = 30.dp, disc: Boolean = true, onClick: () -> Unit, onFocus: () -> Unit, focusRequester: FocusRequester? = null) {
    var focused by remember { mutableStateOf(false) }
    // Only a remote/keyboard shows focus; on a phone the three discs look identical.
    val showFocus = focused && LocalInputModeManager.current.inputMode == InputMode.Keyboard
    Box(
        contentAlignment = Alignment.Center,
        modifier = Modifier
            .size(size)
            .then(if (focusRequester != null) Modifier.focusRequester(focusRequester) else Modifier)
            .onFocusChanged { focused = it.isFocused; if (it.isFocused) onFocus() }
            .focusable()
            .onKeyEvent { ev -> if (ev.type == KeyEventType.KeyDown && (ev.key == Key.DirectionCenter || ev.key == Key.Enter)) { onClick(); true } else false }
            .clip(CircleShape)
            // Plain translucent dark discs like YouTube's, no outline; a remote's focus adds a white ring.
            .background(if (showFocus) Color(0x55FFFFFF) else if (disc) Color(0x59000000) else Color.Transparent)
            .then(if (showFocus) Modifier.border(BorderStroke(2.dp, Color.White), CircleShape) else Modifier)
            .tapClick(onClick),
    ) {
        Icon(icon, contentDescription = label, tint = Color.White, modifier = Modifier.size(iconSize))
    }
}

private data class MenuRow(val key: String, val label: String, val value: String? = null, val submenu: Boolean = false, val checked: Boolean = false)

/**
 * YouTube's settings popup: a compact dark panel, rows of "Label · value ›" for
 * submenus, or "✓ Label" for choices. Submenus get a back row at the top.
 */
@Composable
private fun MenuPopup(title: String?, rows: List<MenuRow>, onPick: (String) -> Unit, onInteraction: () -> Unit, onBack: (() -> Unit)? = null, footer: String? = null) {
    val first = remember { FocusRequester() }
    LaunchedEffect(title, rows.size) { runCatching { first.requestFocus() } }
    val focusKey = rows.firstOrNull { it.checked }?.key ?: rows.firstOrNull()?.key
    val compact = LocalCompactMenu.current
    Column(
        modifier = Modifier
            .widthIn(min = if (compact) 180.dp else 260.dp, max = if (compact) 250.dp else 360.dp)
            .clip(RoundedCornerShape(10.dp))
            .background(Panel)
            .pointerInput(Unit) { detectTapGestures { } } // swallow taps so the scrim does not close it
            .padding(vertical = if (compact) 4.dp else 8.dp)
            .verticalScroll(rememberScrollState()),
    ) {
        if (title != null) {
            MenuRowView(MenuRow("back", title), isBack = true, focusRequester = null, onClick = { onBack?.invoke() }, onInteraction = onInteraction)
            Box(modifier = Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 4.dp).height(1.dp).background(Color(0x33FFFFFF)))
        }
        rows.forEach { row ->
            MenuRowView(row, isBack = false, focusRequester = if (row.key == focusKey) first else null, onClick = { onPick(row.key) }, onInteraction = onInteraction)
        }
        if (footer != null && !compact) Text(footer, color = Muted, fontSize = 11.sp, modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp).widthIn(max = 320.dp))
    }
}

@Composable
private fun MenuRowView(row: MenuRow, isBack: Boolean, focusRequester: FocusRequester?, onClick: () -> Unit, onInteraction: () -> Unit) {
    var focused by remember { mutableStateOf(false) }
    val compact = LocalCompactMenu.current
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .fillMaxWidth()
            .then(if (focusRequester != null) Modifier.focusRequester(focusRequester) else Modifier)
            .onFocusChanged { focused = it.isFocused; if (it.isFocused) onInteraction() }
            .focusable()
            .onKeyEvent { ev -> if (ev.type == KeyEventType.KeyDown && (ev.key == Key.DirectionCenter || ev.key == Key.Enter)) { onClick(); true } else false }
            .background(if (focused) Color(0x33FFFFFF) else Color.Transparent)
            .tapClick(onClick)
            .padding(horizontal = if (compact) 10.dp else 16.dp, vertical = if (compact) 6.dp else 11.dp),
    ) {
        val glyph = if (compact) 16.dp else 20.dp
        when {
            isBack -> Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "Back", tint = Color.White, modifier = Modifier.size(glyph))
            row.submenu -> Spacer(Modifier.width(glyph))
            else -> Icon(Icons.Filled.Check, contentDescription = null, tint = if (row.checked) Color.White else Color.Transparent, modifier = Modifier.size(glyph))
        }
        Spacer(Modifier.width(if (compact) 8.dp else 14.dp))
        Text(row.label, color = Color.White, fontSize = if (compact) 12.sp else 14.sp, fontWeight = if (isBack) FontWeight.SemiBold else FontWeight.Normal, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
        if (row.value != null) {
            Spacer(Modifier.width(if (compact) 10.dp else 18.dp))
            Text(row.value, color = Muted, fontSize = if (compact) 11.sp else 13.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        if (row.submenu) Icon(Icons.AutoMirrored.Filled.KeyboardArrowRight, contentDescription = null, tint = Muted, modifier = Modifier.padding(start = 4.dp).size(20.dp))
    }
}

private fun trackLabel(t: MpvPlayer.Track): String =
    listOf(langName(t.lang), t.title, t.codec.uppercase()).filter { it.isNotEmpty() }.joinToString(" · ").ifEmpty { "Track ${t.id}" }

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

private val clockFormat: java.text.DateFormat get() = java.text.DateFormat.getTimeInstance(java.text.DateFormat.SHORT)

/** Current wall-clock time in the device's locale and zone ("3:07 AM"). */
fun clockNow(): String = clockFormat.format(java.util.Date())

/** Wall-clock time [remainingSec] from now ("3:30 AM"). */
fun endsAt(remainingSec: Double): String = clockFormat.format(java.util.Date(System.currentTimeMillis() + (remainingSec * 1000).toLong()))

fun fmtTime(d: Double): String {
    val s = d.toLong().coerceAtLeast(0)
    return if (s >= 3600) "%d:%02d:%02d".format(s / 3600, (s % 3600) / 60, s % 60) else "%d:%02d".format(s / 60, s % 60)
}
