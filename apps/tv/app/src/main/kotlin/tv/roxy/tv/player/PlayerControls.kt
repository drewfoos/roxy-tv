package tv.roxy.tv.player

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.focusable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
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
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.onKeyEvent
import androidx.compose.ui.input.key.type
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
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
    val speed: Double = 1.0,
    val subtitleTracks: List<MpvPlayer.Track> = emptyList(),
    val audioTracks: List<MpvPlayer.Track> = emptyList(),
    val currentSid: Int = 0,
    val currentAid: Int = 0,
    val quality: String = "",             // "1080p HEVC · hardware"
)

/** What the controls ask the activity to do. */
interface PlayerActions {
    fun togglePause()
    fun seekTo(seconds: Double)
    fun seekBy(seconds: Int)
    fun selectSubtitle(id: Int)
    fun selectAudio(id: Int)
    fun setSpeed(speed: Double)
    fun back()
}

enum class Sheet { None, Subtitles, Audio, Quality, Speed }

private val Scrim = Color(0xCC0E0E12)
private val Panel = Color(0xF21A1A22)
private val Accent = Color(0xFF4F8CFF)
private val Muted = Color(0xFFB0B4BA)
private val Focus = Color.White

/**
 * YouTube-style transport layer: title row, centre play/pause with ±10 s, seek bar with
 * times, and a bottom row (Subtitles, Audio, Quality, Speed) that opens a side sheet.
 * D-pad: everything is focusable; Left/Right on the seek bar seeks ±10 s. Touch: tap
 * toggles visibility, controls are tappable. Auto-hides after [autoHideMs] unless paused
 * or a sheet is open.
 */
@Composable
fun PlayerControls(
    state: PlayerUiState,
    visible: Boolean,
    sheet: Sheet,
    onSheet: (Sheet) -> Unit,
    onInteraction: () -> Unit,
    actions: PlayerActions,
) {
    if (!visible) return
    val playFocus = remember { FocusRequester() }
    LaunchedEffect(visible, sheet) { if (sheet == Sheet.None) runCatching { playFocus.requestFocus() } }

    Box(modifier = Modifier.fillMaxSize()) {
        // Gradients top and bottom so text reads over any frame.
        Box(modifier = Modifier.fillMaxWidth().height(140.dp).align(Alignment.TopCenter).background(Brush.verticalGradient(0f to Color(0xB3000000), 1f to Color.Transparent)))
        Box(modifier = Modifier.fillMaxWidth().height(220.dp).align(Alignment.BottomCenter).background(Brush.verticalGradient(0f to Color.Transparent, 1f to Color(0xD9000000))))

        // Title row
        Row(modifier = Modifier.align(Alignment.TopStart).padding(horizontal = 32.dp, vertical = 24.dp), verticalAlignment = Alignment.CenterVertically) {
            ControlButton("‹", size = 44.dp, onClick = actions::back, onFocus = onInteraction)
            Spacer(Modifier.width(16.dp))
            Column {
                Text(state.title, color = Color.White, fontSize = 20.sp, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                if (state.subtitle.isNotEmpty()) Text(state.subtitle, color = Muted, fontSize = 14.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }

        // Centre transport
        if (sheet == Sheet.None) {
            Row(modifier = Modifier.align(Alignment.Center), horizontalArrangement = Arrangement.spacedBy(40.dp), verticalAlignment = Alignment.CenterVertically) {
                ControlButton("⟲10", size = 64.dp, fontSize = 15.sp, onClick = { actions.seekBy(-10); onInteraction() }, onFocus = onInteraction)
                // Text glyphs with variation selector 0xFE0E force the text (not emoji) presentation.
                ControlButton(if (state.paused) "▶︎" else "❚❚", size = 88.dp, fontSize = if (state.paused) 36.sp else 30.sp, onClick = { actions.togglePause(); onInteraction() }, onFocus = onInteraction, focusRequester = playFocus)
                ControlButton("10⟳", size = 64.dp, fontSize = 15.sp, onClick = { actions.seekBy(10); onInteraction() }, onFocus = onInteraction)
            }
        }

        // Bottom: times + seek bar + option buttons
        Column(modifier = Modifier.align(Alignment.BottomCenter).fillMaxWidth().padding(horizontal = 32.dp, vertical = 20.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(fmtTime(state.positionSec), color = Color.White, fontSize = 13.sp)
                Spacer(Modifier.width(12.dp))
                SeekBar(state, modifier = Modifier.weight(1f), onSeek = { actions.seekTo(it); onInteraction() }, onStep = { actions.seekBy(it); onInteraction() }, onFocus = onInteraction)
                Spacer(Modifier.width(12.dp))
                Text(fmtTime(state.durationSec), color = Muted, fontSize = 13.sp)
            }
            Spacer(Modifier.height(10.dp))
            Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                val sub = state.subtitleTracks.firstOrNull { it.id == state.currentSid }
                PillButton("Subtitles", sub?.let { shortTrackName(it) } ?: "Off", selected = sheet == Sheet.Subtitles, onClick = { onSheet(Sheet.Subtitles) }, onFocus = onInteraction)
                val aud = state.audioTracks.firstOrNull { it.id == state.currentAid }
                PillButton("Audio", aud?.let { shortTrackName(it) } ?: "—", selected = sheet == Sheet.Audio, onClick = { onSheet(Sheet.Audio) }, onFocus = onInteraction)
                PillButton("Quality", state.quality.ifEmpty { "Original" }, selected = sheet == Sheet.Quality, onClick = { onSheet(Sheet.Quality) }, onFocus = onInteraction)
                PillButton("Speed", if (state.speed == 1.0) "Normal" else "${state.speed}×", selected = sheet == Sheet.Speed, onClick = { onSheet(Sheet.Speed) }, onFocus = onInteraction)
            }
        }

        // Side sheet
        if (sheet != Sheet.None) {
            Box(modifier = Modifier.fillMaxHeight().width(380.dp).align(Alignment.CenterEnd).background(Panel).padding(24.dp)) {
                when (sheet) {
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
                        "Speed",
                        options = listOf("0.5" to "0.5×", "0.75" to "0.75×", "1.0" to "Normal", "1.25" to "1.25×", "1.5" to "1.5×", "2.0" to "2×"),
                        selected = state.speed.toString().let { if (it == "1.0") "1.0" else it },
                        footer = null,
                        onPick = { actions.setSpeed(it.toDouble()); onSheet(Sheet.None) }, onInteraction = onInteraction,
                    )
                    Sheet.None -> {}
                }
            }
        }
    }
}

@Composable
private fun SeekBar(state: PlayerUiState, modifier: Modifier, onSeek: (Double) -> Unit, onStep: (Int) -> Unit, onFocus: () -> Unit) {
    var focused by remember { mutableStateOf(false) }
    val dur = state.durationSec.takeIf { it > 0 } ?: 1.0
    Box(modifier = modifier) {
        // Buffered range behind the slider.
        Box(modifier = Modifier.fillMaxWidth().height(4.dp).align(Alignment.Center).clip(RoundedCornerShape(2.dp)).background(Color(0x55FFFFFF)))
        Box(modifier = Modifier.fillMaxWidth((state.bufferedSec / dur).toFloat().coerceIn(0f, 1f)).height(4.dp).align(Alignment.CenterStart).clip(RoundedCornerShape(2.dp)).background(Color(0x99FFFFFF)))
        Slider(
            value = (state.positionSec / dur).toFloat().coerceIn(0f, 1f),
            onValueChange = { onSeek(it * dur) },
            colors = SliderDefaults.colors(thumbColor = if (focused) Focus else Accent, activeTrackColor = Accent, inactiveTrackColor = Color.Transparent),
            modifier = Modifier
                .fillMaxWidth()
                .onFocusChanged { focused = it.isFocused; if (it.isFocused) onFocus() }
                .onKeyEvent { ev ->
                    if (ev.type != KeyEventType.KeyDown) return@onKeyEvent false
                    when (ev.key) {
                        Key.DirectionLeft -> { onStep(-10); true }
                        Key.DirectionRight -> { onStep(10); true }
                        else -> false
                    }
                },
        )
    }
}

@Composable
private fun ControlButton(glyph: String, size: androidx.compose.ui.unit.Dp, fontSize: androidx.compose.ui.unit.TextUnit = 24.sp, onClick: () -> Unit, onFocus: () -> Unit, focusRequester: FocusRequester? = null) {
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
            .background(if (focused) Color(0xFFFFFFFF) else Color(0x66000000))
            .border(BorderStroke(2.dp, if (focused) Focus else Color(0x66FFFFFF)), CircleShape)
            .tapClick(onClick),
    ) {
        Text(glyph, color = if (focused) Color.Black else Color.White, fontSize = fontSize, fontWeight = FontWeight.Bold)
    }
}

@Composable
private fun PillButton(label: String, value: String, selected: Boolean, onClick: () -> Unit, onFocus: () -> Unit) {
    var focused by remember { mutableStateOf(false) }
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .onFocusChanged { focused = it.isFocused; if (it.isFocused) onFocus() }
            .focusable()
            .onKeyEvent { ev -> if (ev.type == KeyEventType.KeyDown && (ev.key == Key.DirectionCenter || ev.key == Key.Enter)) { onClick(); true } else false }
            .clip(RoundedCornerShape(20.dp))
            .background(if (focused || selected) Color.White else Color(0x66000000))
            .border(BorderStroke(1.dp, Color(0x66FFFFFF)), RoundedCornerShape(20.dp))
            .tapClick(onClick)
            .padding(horizontal = 14.dp, vertical = 8.dp),
    ) {
        val fg = if (focused || selected) Color.Black else Color.White
        Text(label, color = fg, fontSize = 13.sp, fontWeight = FontWeight.SemiBold)
        Text("  $value", color = if (focused || selected) Color(0xFF444444) else Muted, fontSize = 13.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
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
                        .then(if (key == (options.firstOrNull { it.first == selected } ?: options.first()).first) Modifier.focusRequester(first) else Modifier)
                        .onFocusChanged { focused = it.isFocused; if (it.isFocused) onInteraction() }
                        .focusable()
                        .onKeyEvent { ev -> if (ev.type == KeyEventType.KeyDown && (ev.key == Key.DirectionCenter || ev.key == Key.Enter)) { onPick(key); true } else false }
                        .clip(RoundedCornerShape(8.dp))
                        .background(if (focused) Color.White else Color.Transparent)
                        .tapClick { onPick(key) }
                        .padding(horizontal = 12.dp, vertical = 10.dp),
                ) {
                    Text(if (isSel) "●" else "○", color = if (focused) Color.Black else if (isSel) Accent else Muted, fontSize = 14.sp)
                    Spacer(Modifier.width(12.dp))
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
