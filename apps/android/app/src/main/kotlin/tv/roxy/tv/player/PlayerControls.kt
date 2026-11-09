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
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.core.tween
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.ui.graphics.Shadow
import androidx.compose.ui.text.TextStyle
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
import androidx.compose.material.icons.filled.CloseFullscreen
import androidx.compose.material.icons.filled.OpenInFull
import androidx.compose.material.icons.filled.FullscreenExit
import androidx.compose.material.icons.filled.Pause
import androidx.compose.material.icons.filled.PictureInPictureAlt
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Replay10
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material.icons.filled.Cast
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
import androidx.compose.runtime.withFrameNanos
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
    /** Waiting on the network, a seek or a (re)load while meant to be playing: the spinner. */
    val buffering: Boolean = false,
    /** Where a D-pad scrub will seek to (the seek bar previews it), or null. */
    val scrubSec: Double? = null,
    val muted: Boolean = false,
    val speed: Double = 1.0,
    val subtitleTracks: List<MpvPlayer.Track> = emptyList(),
    val audioTracks: List<MpvPlayer.Track> = emptyList(),
    val currentSid: Int = 0,
    val currentAid: Int = 0,
    val quality: String = "",             // "1080p HEVC · hardware"
    /** The Quality menu: Original plus the transcode presets this source can use. */
    val qualityOptions: List<QualityOption> = listOf(QualityOption.Original),
    val qualityKey: String = QualityOption.Original.key,
    /** Why only Original is offered (a download plays from the phone), or null. */
    val qualityNote: String? = null,
    val fillScreen: Boolean = false,      // crop to fill (fullscreen button) vs letterbox
    val pipSupported: Boolean = false,
    val hasNext: Boolean = false,
    /** The next-episode card is up: the Skip button yields its corner to it. */
    val nextCardShown: Boolean = false,
    val segments: List<MediaSegment> = emptyList(),
    val autoSkip: Boolean = false,
    /** Seconds the Skip button stays after a segment starts (0 = until it ends). */
    val skipWindowSec: Int = 20,
    /** Hosted on a page: false = in its 16:9 box, true = filling the screen; null = the player owns the screen. */
    val fullscreen: Boolean? = null,
    /** The age rating card flashed at the start of a title (null = not showing). */
    val advisory: Advisory? = null,
) {
    /** The skippable segment the playhead is inside, if any. */
    val activeSegment: MediaSegment? get() = segments.firstOrNull { positionSec >= it.startSec && positionSec < it.endSec - 1.0 }

    /** [activeSegment] while the Skip button is still meant to show (Crunchyroll's: a short while after it starts). */
    val skippableSegment: MediaSegment? get() = activeSegment?.takeIf { skipWindowSec <= 0 || positionSec < it.startSec + skipWindowSec }
}

/**
 * One Quality menu entry, named by resolution the way YouTube does. The top entry is the
 * file itself (direct play), labelled with its own resolution ("1080p"); the lower ones ask
 * the server for an H.264 transcode at that height and video bitrate.
 */
data class QualityOption(val key: String, val label: String, val height: Int, val kbps: Int) {
    val isOriginal: Boolean get() = height == 0

    companion object {
        val Original = QualityOption("original", "Best", 0, 0)
        val Presets = listOf(
            Original,
            QualityOption("2160", "4K", 2160, 40000),
            QualityOption("1440", "1440p", 1440, 16000),
            QualityOption("1080", "1080p", 1080, 8000),
            QualityOption("720", "720p", 720, 4000),
            QualityOption("480", "480p", 480, 1500),
        )

        /** "1080p" for a 1038- or 1080-line picture, "4K" for 2160 lines: what people call it. */
        fun resolutionLabel(height: Int): String? = height.takeIf { it > 0 }?.let { h ->
            when (val std = listOf(4320, 2160, 1440, 1080, 720, 576, 480, 360).firstOrNull { h >= it * 0.94 }) {
                4320 -> "8K"
                2160 -> "4K"
                null -> "${h}p"
                else -> "${std}p"
            }
        }

        /**
         * The menu for a video [sourceHeight] tall: the file itself under its resolution,
         * then only the presets that are actually smaller. Unknown height: "Best" and all.
         */
        fun forSource(sourceHeight: Int): List<QualityOption> {
            val res = resolutionLabel(sourceHeight)
            val top = if (res != null) Original.copy(label = res) else Original
            val smaller = Presets.filter { !it.isOriginal && (sourceHeight <= 0 || it.height < sourceHeight * 0.94) }
            return listOf(top) + smaller
        }

        fun byKey(key: String): QualityOption = Presets.firstOrNull { it.key == key } ?: Original
    }
}

/** What the controls ask the activity to do. */
interface PlayerActions {
    fun setQuality(key: String) {}
    fun togglePause()
    fun seekTo(seconds: Double)
    fun seekBy(seconds: Int)
    /** D-pad scrub: move the preview one step (bigger while the key is held); one seek when it settles. */
    fun scrub(direction: Int, repeat: Int) { seekBy(direction * 10) }
    /** Seek to the scrubbed position now (OK on the bar); false when not scrubbing. */
    fun commitScrub(): Boolean = false
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
    fun cast() {}
    /** The title box's "Go to Details" (TV): the show's (or the film's) page. */
    fun openDetails() {}
}

enum class Sheet { None, Settings, Subtitles, Audio, Quality, Speed }

private val Panel = Color(0xF21C1C22)
/** The seek bar, its thumb and the captions underline: Roxy's blue everywhere now. */
private val Bar = RoxyColors.accent
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
    // Keeps the card's text while it fades out (the state drops it at once).
    val lastAdvisory = remember { mutableStateOf<Advisory?>(null) }
    // Hosted on a page (the phone): Crunchyroll's layout. Back at the top-left; next,
    // settings and fullscreen at the top-right; bare -10 / pause / +10 in the middle;
    // the times over an edge-to-edge seek bar at the bottom and nothing else. The
    // full-screen host (TV, movies) keeps the YouTube layout with its button row.
    val cr = state.fullscreen != null
    // The age rating card (Netflix / Crunchyroll): top-left for a few seconds as a title
    // starts, under the controls' layer so it steps aside while they are up.
    AnimatedVisibility(
        visible = state.advisory != null && !visible, enter = fadeIn(tween(400)), exit = fadeOut(tween(600)),
        modifier = Modifier.fillMaxSize(),
    ) {
        Box(modifier = Modifier.fillMaxSize()) {
            (state.advisory ?: lastAdvisory.value)?.let { a ->
                lastAdvisory.value = a
                AdvisoryCard(a, compact = compact, modifier = Modifier.align(Alignment.TopStart).padding(start = if (compact) 10.dp else 32.dp, top = if (compact) 10.dp else 28.dp))
            }
        }
    }
    // Skip button lives outside the auto-hidden layer so it is there when you need it. On
    // the TV it sits bottom-right at one height, clear of the bar, so it doesn't slide when
    // the bar comes and goes; it takes focus when it appears so OK skips.
    val tv = !cr && !compact
    val skipFocus = remember { FocusRequester() }
    val skipSeg = state.skippableSegment?.takeIf { !state.autoSkip && !state.nextCardShown }
    LaunchedEffect(skipSeg?.type, skipSeg?.startSec) { if (tv && skipSeg != null && sheet == Sheet.None) { withFrameNanos { }; runCatching { skipFocus.requestFocus() } } }
    state.skippableSegment?.let { seg ->
        if (!state.autoSkip && !state.nextCardShown) {
            if (tv) SkipButton(
                seg.label, onClick = { actions.seekTo(seg.endSec); onInteraction() }, large = true, focusRequester = skipFocus,
                modifier = Modifier.fillMaxSize().padding(end = TvBarSide, bottom = TvSkipBottom),
            )
            else SkipButton(seg.label, onClick = { actions.seekTo(seg.endSec); onInteraction() }, compact = compact, modifier = Modifier.fillMaxSize().padding(end = if (compact) 10.dp else 32.dp, bottom = if (compact) 44.dp else if (cr) 56.dp else if (visible) 118.dp else 40.dp))
        }
    }
    if (!visible) return
    val playFocus = remember { FocusRequester() }
    // The TV opens on the seek bar (YouTube / Netflix): Left/Right scrub the timeline, Down
    // reaches the buttons. A phone (touch, or a keyboard) starts on play/pause.
    val seekFocus = remember { FocusRequester() }
    LaunchedEffect(visible, sheet) { if (sheet == Sheet.None && !(tv && skipSeg != null)) runCatching { (if (tv) seekFocus else playFocus).requestFocus() } }

    Box(modifier = Modifier.fillMaxSize()) {
        Box(modifier = Modifier.fillMaxWidth().height(if (compact) 70.dp else if (tv) 190.dp else 140.dp).align(Alignment.TopCenter).background(Brush.verticalGradient(0f to Color(0xB3000000), 1f to Color.Transparent)))
        Box(modifier = Modifier.fillMaxWidth().height(if (compact) 100.dp else if (tv) 260.dp else 200.dp).align(Alignment.BottomCenter).background(Brush.verticalGradient(0f to Color.Transparent, 1f to Color(0xE6000000))))

        // The TV (Crunchyroll's TV app): the title box at the top-left (focusable: OK opens the
        // show's page), settings and next episode at the top-right, nothing else up there.
        // While its settings panel is open the TV shows the panel alone (Crunchyroll's).
        if (tv && sheet == Sheet.None) TvTopBar(state, onDetails = { onInteraction(); actions.openDetails() }, onSettings = { onSheet(if (sheet == Sheet.None) Sheet.Settings else Sheet.None) }, onNext = { actions.next() }, onFocus = onInteraction, modifier = Modifier.align(Alignment.TopStart))
        // Title row, with the clock and "Ends at" top-right (local time zone).
        else if (!tv) Row(modifier = Modifier.align(Alignment.TopStart).fillMaxWidth().padding(start = if (compact) 8.dp else if (cr) 16.dp else TvBarSide - 8.dp, end = if (compact) 8.dp else if (cr) 16.dp else TvBarSide, top = if (compact) 4.dp else if (cr) 10.dp else 22.dp, bottom = if (compact) 4.dp else if (cr) 10.dp else 20.dp), verticalAlignment = Alignment.CenterVertically) {
            // No arrow in the page's fullscreen: the system back gesture drops to the mini player.
            if (state.fullscreen != true) {
                IconButton(Icons.AutoMirrored.Filled.ArrowBack, "Back", onClick = actions::back, onFocus = onInteraction, compact = compact, large = tv)
                Spacer(Modifier.width(if (compact) 6.dp else 12.dp))
            }
            Column(modifier = Modifier.weight(1f)) {
                when {
                    // The mini player carries no title: the page spells it out right under the video.
                    compact -> {}
                    // Crunchyroll's: the series on top, "E3 — Title" under it, readable at arm's length.
                    cr -> {
                        val (series, code) = splitSubtitle(state.subtitle)
                        if (series.isNotEmpty()) Text(series, color = Muted, fontSize = 13.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        Text(listOfNotNull(code, state.title.takeIf { it.isNotEmpty() }).joinToString(" — "), color = Color.White, fontSize = 16.sp, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    }
                    // The TV: the series over "E3 - Title", on at most 60 % of the width so the clock keeps its room.
                    else -> {
                        val (series, code) = splitSubtitle(state.subtitle)
                        if (series.isNotEmpty()) Text(series, color = Muted, fontSize = 17.sp, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.fillMaxWidth(0.5f))
                        Text(listOfNotNull(code, state.title.takeIf { it.isNotEmpty() }).joinToString(" \u2014 "), color = Color.White, fontSize = 22.sp, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.fillMaxWidth(0.5f))
                    }
                }
            }
            if (cr) {
                // Next, Cast (a placeholder until casting exists), settings (everything lives in
                // there: subtitles, audio, speed, skip), fullscreen.
                if (state.hasNext) IconButton(Icons.Filled.SkipNext, "Next episode", onClick = { actions.next() }, onFocus = onInteraction, compact = compact)
                IconButton(Icons.Filled.Cast, "Play on another device", onClick = { onInteraction(); actions.cast() }, onFocus = onInteraction, compact = compact)
                IconButton(Icons.Filled.Settings, "Settings", onClick = { onSheet(if (sheet == Sheet.None) Sheet.Settings else Sheet.None) }, onFocus = onInteraction, compact = compact)
                if (state.fullscreen == true) IconButton(Icons.Filled.CloseFullscreen, "Exit fullscreen", onClick = { actions.toggleFullscreen() }, onFocus = onInteraction, compact = compact)
                else IconButton(Icons.Filled.OpenInFull, "Fullscreen", onClick = { actions.toggleFullscreen() }, onFocus = onInteraction, compact = compact)
            }
        }

        // Centre transport: the phone page's bare -10 / pause / +10. The TV has none
        // (Crunchyroll's player keeps everything in the bottom row).
        if (sheet == Sheet.None && cr) {
            Row(modifier = Modifier.align(Alignment.Center), horizontalArrangement = Arrangement.spacedBy(if (compact) 36.dp else 48.dp), verticalAlignment = Alignment.CenterVertically) {
                RoundButton(Icons.Filled.Replay10, "Back 10 seconds", size = if (compact) 48.dp else 60.dp, iconSize = if (compact) 32.dp else if (cr) 36.dp else 30.dp, disc = !cr, onClick = { actions.seekBy(-10); onInteraction() }, onFocus = onInteraction)
                RoundButton(if (state.paused) Icons.Filled.PlayArrow else Icons.Filled.Pause, if (state.paused) "Play" else "Pause", size = if (compact) 64.dp else 76.dp, iconSize = if (compact) 50.dp else if (cr) 56.dp else 42.dp, disc = !cr, onClick = { actions.togglePause(); onInteraction() }, onFocus = onInteraction, focusRequester = playFocus)
                RoundButton(Icons.Filled.Forward10, "Forward 10 seconds", size = if (compact) 48.dp else 60.dp, iconSize = if (compact) 32.dp else if (cr) 36.dp else 30.dp, disc = !cr, onClick = { actions.seekBy(10); onInteraction() }, onFocus = onInteraction)
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
            SeekBar(state, trickplay, modifier = Modifier.fillMaxWidth(), onSeek = { actions.seekTo(it); onInteraction() }, actions = actions, onFocus = onInteraction, color = RoxyColors.accent, flush = true)
        }
        // The TV: elapsed time, the bar, the time left, and nothing else (Crunchyroll's TV
        // app). Everything else (subtitles, audio, speed, quality, auto-skip) is under the
        // gear. Focus starts on the bar: Left/Right scrub, OK plays and pauses.
        else if (sheet == Sheet.None) {
            if (state.paused) Box(modifier = Modifier.align(Alignment.Center).size(40.dp).clip(CircleShape).background(Bar), contentAlignment = Alignment.Center) {
                Icon(Icons.Filled.PlayArrow, contentDescription = null, tint = Color.White, modifier = Modifier.size(28.dp))
            }
            Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.align(Alignment.BottomCenter).fillMaxWidth().padding(start = TvTimesSide, end = TvTimesSide, bottom = TvBarBottom)) {
                Text(fmtTime(state.positionSec), color = Color.White, fontSize = 17.sp)
                SeekBar(state, trickplay, modifier = Modifier.weight(1f).padding(horizontal = 20.dp), onSeek = { actions.seekTo(it); onInteraction() }, actions = actions, onFocus = onInteraction, focusRequester = seekFocus)
                Text(fmtTime((state.durationSec - state.positionSec).coerceAtLeast(0.0)), color = Color.White, fontSize = 17.sp)
            }
        }

        // Settings popup (YouTube-style): anchored bottom-right above the button row; a
        // transparent scrim behind it closes it on tap.
        if (sheet != Sheet.None) {
            Box(modifier = Modifier.fillMaxSize().pointerInput(Unit) { detectTapGestures { onSheet(Sheet.None) } })
            Box(modifier = if (cr) Modifier.align(Alignment.TopEnd).padding(end = if (compact) 8.dp else 16.dp, top = if (compact) 40.dp else 56.dp, bottom = if (compact) 24.dp else 40.dp) else Modifier.align(Alignment.TopEnd).fillMaxHeight().fillMaxWidth(0.345f)) {
                CompositionLocalProvider(LocalCompactMenu provides compact, LocalTvPanel provides tv) { when (sheet) {
                    Sheet.Settings -> MenuPopup(
                        title = null,
                        rows = listOf(
                            MenuRow("quality", "Quality", state.qualityOptions.firstOrNull { it.key == state.qualityKey }?.label ?: QualityOption.byKey(state.qualityKey).label, submenu = true),
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
                        rows = state.qualityOptions.map { o -> MenuRow(o.key, o.label, checked = o.key == state.qualityKey) },
                        footer = state.qualityNote,
                        onPick = { actions.setQuality(it); onSheet(Sheet.None) }, onBack = { onSheet(Sheet.Settings) }, onInteraction = onInteraction,
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

/**
 * The TV's top bar: the title box (the series, "E14 - Title", "Go to Details"; outlined in
 * the accent colour when focused, OK opens the show's page) and, at the right, settings
 * and the next episode.
 */
@Composable
private fun TvTopBar(state: PlayerUiState, onDetails: () -> Unit, onSettings: () -> Unit, onNext: () -> Unit, onFocus: () -> Unit, modifier: Modifier) {
    Row(modifier = modifier.fillMaxWidth().padding(start = TvBarSide - 16.dp, end = TvBarSide, top = 24.dp), verticalAlignment = Alignment.Top) {
        var focused by remember { mutableStateOf(false) }
        val (series, code) = splitSubtitle(state.subtitle)
        Column(
            modifier = Modifier
                .fillMaxWidth(0.5f)
                .onFocusChanged { focused = it.isFocused; if (it.isFocused) onFocus() }
                .focusable()
                .onKeyEvent { ev -> if (ev.type == KeyEventType.KeyDown && (ev.key == Key.DirectionCenter || ev.key == Key.Enter)) { onDetails(); true } else false }
                .border(BorderStroke(2.dp, if (focused) Bar else Color.Transparent), RoundedCornerShape(4.dp))
                .tapClick(onDetails)
                .padding(horizontal = 16.dp, vertical = 10.dp),
        ) {
            if (series.isNotEmpty()) Text(series, color = Color.White, fontSize = 17.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(listOfNotNull(code, state.title.takeIf { it.isNotEmpty() }).joinToString(" - "), color = Color.White, fontSize = 26.sp, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
            if (focused) Text("Go to Details", color = Muted, fontSize = 14.sp, modifier = Modifier.padding(top = 2.dp))
        }
        Spacer(Modifier.weight(1f))
        IconButton(Icons.Filled.Settings, "Settings", onClick = onSettings, onFocus = onFocus, large = true)
        if (state.hasNext) IconButton(Icons.Filled.SkipNext, "Next episode", onClick = onNext, onFocus = onFocus, large = true)
    }
}

/** "KONOSUBA ...  S01E01" -> ("KONOSUBA ...", "E1"); a movie's year stays as the series line. */
private fun splitSubtitle(sub: String): Pair<String, String?> {
    val m = Regex("""\s*S(\d+)E(\d+)\s*$""").find(sub) ?: return sub to null
    return sub.substring(0, m.range.first).trim() to "E${m.groupValues[2].toInt()}"
}

/** True inside the page's 16:9 box: the popups shrink to fit it. */
private val LocalCompactMenu = compositionLocalOf { false }
/** The TV: menus are a full-height panel down the right side (Crunchyroll's TV app). */
private val LocalTvPanel = compositionLocalOf { false }

/** Netflix/YouTube-style "Skip intro" pill, bottom-right over the video. */
@Composable
private fun SkipButton(label: String, onClick: () -> Unit, modifier: Modifier, compact: Boolean = false, large: Boolean = false, align: Alignment = Alignment.BottomEnd, focusRequester: FocusRequester? = null) {
    var focused by remember { mutableStateOf(false) }
    Box(modifier = modifier, contentAlignment = align) {
        Row(
            verticalAlignment = Alignment.CenterVertically,
            modifier = Modifier
                .then(if (focusRequester != null) Modifier.focusRequester(focusRequester) else Modifier)
                .onFocusChanged { focused = it.isFocused }
                .focusable()
                .onKeyEvent { ev -> if (ev.type == KeyEventType.KeyDown && (ev.key == Key.DirectionCenter || ev.key == Key.Enter)) { onClick(); true } else false }
                .clip(RoundedCornerShape(4.dp))
                .background(if (focused) Color.White else Color(0xE6202020))
                .border(BorderStroke(1.dp, Color(0x99FFFFFF)), RoundedCornerShape(4.dp))
                .tapClick(onClick)
                .padding(horizontal = if (compact) 12.dp else if (large) 22.dp else 18.dp, vertical = if (compact) 6.dp else 12.dp),
        ) {
            Text(label, color = if (focused) Color.Black else Color.White, fontSize = if (compact) 12.sp else if (large) 19.sp else 15.sp, fontWeight = FontWeight.SemiBold)
            Icon(Icons.Filled.SkipNext, contentDescription = null, tint = if (focused) Color.Black else Color.White, modifier = Modifier.padding(start = 4.dp).size(if (compact) 16.dp else if (large) 26.dp else 20.dp))
        }
    }
}

/**
 * Seek bar drawn by hand (Material's slider insets the track for its thumb, which left
 * an un-red gap at the start and a bar narrower than the button row). YouTube red,
 * round thumb that grows on focus/drag, buffered range in lighter grey. Dragging shows
 * the trickplay tile for the hovered time and seeks on release. Left/Right scrub: the
 * thumb and the tile move at once, the step grows while the key is held, and one seek
 * follows when the presses stop (or on OK; OK on a resting bar is play/pause).
 */
@Composable
private fun SeekBar(state: PlayerUiState, trickplay: TrickplayPreview?, modifier: Modifier, onSeek: (Double) -> Unit, actions: PlayerActions, onFocus: () -> Unit, color: Color = Bar, flush: Boolean = false, focusRequester: FocusRequester? = null) {
    var focused by remember { mutableStateOf(false) }
    val dur = state.durationSec.takeIf { it > 0 } ?: 1.0
    var dragFraction by remember { mutableStateOf<Float?>(null) }
    val dragging = dragFraction != null
    LaunchedEffect(dragging) { while (dragging) { onFocus(); kotlinx.coroutines.delay(1500) } }

    val previewSec: Double? = dragFraction?.let { it * dur } ?: state.scrubSec
    val shownFraction = dragFraction ?: ((state.scrubSec ?: state.positionSec) / dur).toFloat().coerceIn(0f, 1f)
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
                .then(if (focusRequester != null) Modifier.focusRequester(focusRequester) else Modifier)
                .onFocusChanged { focused = it.isFocused; if (it.isFocused) onFocus() }
                .focusable()
                .onKeyEvent { ev ->
                    if (ev.type != KeyEventType.KeyDown) return@onKeyEvent false
                    when (ev.key) {
                        Key.DirectionLeft, Key.DirectionRight -> {
                            actions.scrub(if (ev.key == Key.DirectionLeft) -1 else 1, ev.nativeKeyEvent.repeatCount)
                            onFocus()
                            true
                        }
                        Key.DirectionCenter, Key.Enter -> {
                            if (ev.nativeKeyEvent.repeatCount == 0 && !actions.commitScrub()) actions.togglePause()
                            onFocus()
                            true
                        }
                        else -> false
                    }
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
private fun IconButton(icon: ImageVector, label: String, onClick: () -> Unit, onFocus: () -> Unit, underline: Boolean = false, compact: Boolean = false, tint: Color = Color.White, focusRequester: FocusRequester? = null, large: Boolean = false) {
    var focused by remember { mutableStateOf(false) }
    Box(
        contentAlignment = Alignment.Center,
        modifier = Modifier
            .size(if (compact) 42.dp else if (large) 60.dp else 44.dp)
            .then(if (focusRequester != null) Modifier.focusRequester(focusRequester) else Modifier)
            .onFocusChanged { focused = it.isFocused; if (it.isFocused) onFocus() }
            .focusable()
            .onKeyEvent { ev -> if (ev.type == KeyEventType.KeyDown && (ev.key == Key.DirectionCenter || ev.key == Key.Enter)) { onClick(); true } else false }
            .clip(CircleShape)
            .background(if (focused) Color(0x33FFFFFF) else Color.Transparent)
            .tapClick(onClick),
    ) {
        Column(horizontalAlignment = Alignment.CenterHorizontally) {
            Icon(icon, contentDescription = label, tint = tint, modifier = Modifier.size(if (compact) 28.dp else if (large) 36.dp else 26.dp))
            Box(modifier = Modifier.padding(top = 2.dp).width(if (compact) 14.dp else if (large) 24.dp else 18.dp).height(2.dp).background(if (underline) Bar else Color.Transparent))
        }
    }
}

/** The TV player's side inset, and where Skip sits: above the bar whether it shows or not. */
private val TvBarSide = 40.dp
private val TvSkipBottom = 180.dp // over the bar, which sits higher now
/** The TV's bar (Crunchyroll's TV app): its middle about 23% up the screen, the times 66 dp in. */
private val TvBarBottom = 108.dp
private val TvTimesSide = 66.dp

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
    if (LocalTvPanel.current) { TvMenuPanel(title ?: "Settings", rows, onPick, onInteraction, footer); return }
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

/**
 * The TV's menu: a full-height panel down the right third, its title at the top, then rows
 * with the setting at the left and its value at the right (or a tick on the choice), the
 * focused row filled with the accent colour, dark text on it. Back goes up a level
 * (PlayerActivity).
 */
@Composable
private fun TvMenuPanel(title: String, rows: List<MenuRow>, onPick: (String) -> Unit, onInteraction: () -> Unit, footer: String?) {
    val first = remember { FocusRequester() }
    LaunchedEffect(title, rows.size) { runCatching { first.requestFocus() } }
    val focusKey = rows.firstOrNull { it.checked && !it.submenu }?.key ?: rows.firstOrNull()?.key
    Column(
        modifier = Modifier
            .fillMaxHeight()
            .background(Color(0xFF17181C))
            .pointerInput(Unit) { detectTapGestures { } }
            .padding(start = 32.dp, end = 32.dp, top = 36.dp, bottom = 24.dp)
            .verticalScroll(rememberScrollState()),
    ) {
        Text(title, color = Color.White, fontSize = 26.sp, fontWeight = FontWeight.SemiBold, modifier = Modifier.padding(start = 16.dp, bottom = 32.dp))
        rows.forEach { row ->
            var focused by remember { mutableStateOf(false) }
            val ink = if (focused) Color(0xFF0B0B0D) else Color.White
            Row(
                verticalAlignment = Alignment.CenterVertically,
                modifier = Modifier
                    .fillMaxWidth()
                    .height(36.dp)
                    .then(if (row.key == focusKey) Modifier.focusRequester(first) else Modifier)
                    .onFocusChanged { focused = it.isFocused; if (it.isFocused) onInteraction() }
                    .focusable()
                    .onKeyEvent { ev -> if (ev.type == KeyEventType.KeyDown && (ev.key == Key.DirectionCenter || ev.key == Key.Enter)) { onPick(row.key); true } else false }
                    .background(if (focused) Bar else Color.Transparent)
                    .tapClick { onPick(row.key) }
                    .padding(horizontal = 16.dp),
            ) {
                Text(row.label, color = ink, fontSize = 17.sp, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
                when {
                    row.value != null -> Text(row.value, color = if (focused) ink else Muted, fontSize = 17.sp, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(start = 16.dp).widthIn(max = 160.dp))
                    row.checked -> Icon(Icons.Filled.Check, contentDescription = "Selected", tint = ink, modifier = Modifier.padding(start = 12.dp).size(20.dp))
                }
            }
        }
        if (footer != null) Text(footer, color = Muted, fontSize = 13.sp, modifier = Modifier.padding(start = 16.dp, top = 16.dp))
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
        Text(row.label, color = Color.White, fontSize = if (compact) 12.sp else 14.sp, fontWeight = if (isBack) FontWeight.SemiBold else FontWeight.Normal, maxLines = 1)
        if (row.value != null) {
            Spacer(Modifier.width(if (compact) 10.dp else 18.dp))
            Text(row.value, color = Muted, fontSize = if (compact) 11.sp else 13.sp, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false)) // the label always shows whole; the value gives way
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

/** What the rating card says: "14+" and the content descriptors ("Sexual Content, Violence"). */
data class Advisory(val rating: String, val descriptors: List<String>)

/**
 * The card itself: a short brand-blue bar at the left, the rating large, the descriptors
 * under it. Smaller in the page's 16:9 box.
 */
@Composable
private fun AdvisoryCard(a: Advisory, compact: Boolean, modifier: Modifier = Modifier) {
    Row(modifier = modifier.height(IntrinsicSize.Min), verticalAlignment = Alignment.CenterVertically) {
        Box(modifier = Modifier.width(if (compact) 3.dp else 4.dp).fillMaxHeight().background(RoxyColors.accent))
        Column(modifier = Modifier.padding(start = if (compact) 8.dp else 12.dp)) {
            Text(
                a.rating, color = Color.White, fontWeight = FontWeight.Bold, fontSize = if (compact) 15.sp else 22.sp,
                style = TextStyle(shadow = Shadow(Color(0xCC000000), blurRadius = 6f)),
            )
            if (a.descriptors.isNotEmpty()) Text(
                a.descriptors.joinToString(", "), color = Color.White.copy(alpha = 0.9f), fontSize = if (compact) 11.sp else 14.sp,
                maxLines = 2, overflow = TextOverflow.Ellipsis, modifier = Modifier.widthIn(max = if (compact) 220.dp else 420.dp),
                style = TextStyle(shadow = Shadow(Color(0xCC000000), blurRadius = 6f)),
            )
        }
    }
}
