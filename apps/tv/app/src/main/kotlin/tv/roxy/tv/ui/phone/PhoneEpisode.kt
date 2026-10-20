package tv.roxy.tv.ui.phone

import android.os.Handler
import android.os.Looper
import android.view.SurfaceView
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectHorizontalDragGestures
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Forward10
import androidx.compose.material.icons.filled.Fullscreen
import androidx.compose.material.icons.filled.Pause
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Replay
import androidx.compose.material.icons.filled.Replay10
import androidx.compose.material.icons.filled.SkipNext
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableDoubleStateOf
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.Item
import tv.roxy.tv.api.subtitleInfos
import tv.roxy.tv.diagnostics.DeviceReport
import tv.roxy.tv.player.MpvPlayer
import tv.roxy.tv.player.PlayerActivity
import tv.roxy.tv.ui.PlayRequest
import tv.roxy.tv.ui.components.RoxyImage
import tv.roxy.tv.ui.components.widthPx
import tv.roxy.tv.ui.playRequest
import tv.roxy.tv.ui.theme.RoxyColors

/**
 * Crunchyroll's episode page: the video playing inline at the top (16:9, pinned), then
 * the series name in the brand colour, "E3 - Title", "14+ ◆ Dub | Sub", the download
 * and menu, the synopsis with "Read more", one "Next Episode" row and an "All Episodes"
 * link back to the series. The fullscreen button hands the same stream, at the same
 * position, to the full player; coming back, playback resumes here from where it got to.
 */
@Composable
fun PhoneEpisode(episodeId: String, fallbackTitle: String, onBack: () -> Unit, onOpenEpisode: (Item) -> Unit) {
    val context = LocalContext.current
    val app = RoxyApp.get(context)
    val scope = rememberCoroutineScope()
    var episode by remember(episodeId) { mutableStateOf(app.items[episodeId]) }
    var req by remember(episodeId) { mutableStateOf<PlayRequest?>(null) }
    var siblings by remember(episodeId) { mutableStateOf<List<Item>>(emptyList()) }
    var error by remember(episodeId) { mutableStateOf<String?>(null) }
    var reloadKey by remember(episodeId) { mutableIntStateOf(0) }
    var playing by remember(episodeId) { mutableStateOf(true) }
    var wentFullscreen by remember { mutableStateOf(false) }
    var expanded by rememberSaveable(episodeId) { mutableStateOf(false) }

    LaunchedEffect(episodeId, reloadKey) {
        val seed = app.items[episodeId] ?: Item(id = episodeId, type = "episode", title = fallbackTitle)
        playRequest(context, seed)
            .onSuccess { r -> episode = r.item; req = r; playing = true }
            .onFailure { t -> Timber.w(t, "episode"); error = t.message }
        val seriesId = episode?.seriesId ?: return@LaunchedEffect
        if (siblings.isEmpty()) {
            runCatching { app.api.items(type = "episode", parent = seriesId, recursive = true, includeSources = true, sort = "index").items }
                .onSuccess { eps ->
                    siblings = eps.sortedWith(compareBy({ (it.seasonAired ?: 0).let { s -> if (s == 0) Int.MAX_VALUE else s } }, { it.indexAired ?: it.indexAbsolute ?: 0 }))
                }
        }
    }
    // Back from the full-screen player: fetch the position it reported and carry on here.
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) {
        if (wentFullscreen) { wentFullscreen = false; reloadKey++ }
    }

    val ep = episode
    val next = siblings.dropWhile { it.id != episodeId }.drop(1).firstOrNull { it.isPlayable }
    Column(modifier = Modifier.fillMaxSize().background(RoxyColors.background).statusBarsPadding()) {
        Box(modifier = Modifier.fillMaxWidth().aspectRatio(16f / 9f).background(Color.Black)) {
            val r = req
            when {
                r != null && playing -> InlinePlayer(
                    req = r,
                    hasNext = next != null,
                    onBack = onBack,
                    onNext = { next?.let { onOpenEpisode(it) } },
                    onFullscreen = { pos ->
                        wentFullscreen = true
                        playing = false
                        context.startActivity(PlayerActivity.intent(context, r.copy(startSeconds = pos), app.prefs.settings.value))
                    },
                    onEnded = { if (app.prefs.settings.value.autoplay) next?.let { onOpenEpisode(it) } },
                )
                error != null -> Text(error ?: "", color = RoxyColors.error, modifier = Modifier.align(Alignment.Center))
                else -> if (ep != null) RoxyImage(app.api.imageUrl(ep, "thumb", widthPx(420.dp) * 2), ep.episodeCode, modifier = Modifier.fillMaxSize(), placeholderTextSize = 14)
            }
        }
        Column(modifier = Modifier.fillMaxSize().verticalScroll(rememberScrollState()).navigationBarsPadding().padding(bottom = 24.dp)) {
            if (ep != null) {
                Column(modifier = Modifier.padding(horizontal = PhonePad, vertical = 14.dp)) {
                    ep.seriesTitle?.takeIf { it.isNotBlank() }?.let { Text(it, fontSize = 14.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.accent, maxLines = 1, overflow = TextOverflow.Ellipsis) }
                    val code = (ep.indexAired ?: ep.indexAbsolute)?.let { "E$it" }
                    Text(
                        listOfNotNull(code, ep.title.takeIf { it.isNotBlank() }).joinToString(" - "),
                        fontSize = 20.sp, lineHeight = 25.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(top = 6.dp),
                    )
                    Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.padding(top = 10.dp)) {
                        val age = ep.officialRating?.let { ageLabel(it) }
                        if (age != null) { AgePill(age); Spacer(Modifier.width(8.dp)) }
                        val facts = listOfNotNull(audioBadge(ep), ep.runtimeMinutes?.let { "${it}m" }, ep.airDate?.takeIf { it.isNotBlank() }?.let { "Aired $it" }).joinToString("  ◆  ")
                        if (facts.isNotEmpty()) Text(facts, fontSize = 13.sp, color = Color.White.copy(alpha = 0.85f))
                    }
                    Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.fillMaxWidth().padding(top = 8.dp)) {
                        Spacer(Modifier.weight(1f))
                        val startDownload = rememberDownloadStarter()
                        if (ep.isPlayable) DownloadButton(ep, onDownload = { startDownload(listOf(ep), ep, null) }, size = 24.dp)
                        EpisodeMenu(ep, app.api, onMarkWatched = { on -> scope.launch { runCatching { app.api.setPlayed(ep.id, on) }; reloadKey++ } })
                    }
                    ep.overview?.takeIf { it.isNotBlank() }?.let {
                        Text(
                            it, fontSize = 14.sp, lineHeight = 20.sp, color = RoxyColors.onBackground.copy(alpha = 0.9f),
                            maxLines = if (expanded) Int.MAX_VALUE else 3, overflow = TextOverflow.Ellipsis,
                            modifier = Modifier.padding(top = 6.dp).clickable(interactionSource = remember { MutableInteractionSource() }, indication = null) { expanded = !expanded },
                        )
                        if (!expanded) Text("Read more", fontSize = 14.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.accent, modifier = Modifier.align(Alignment.End).clickable { expanded = true }.padding(top = 2.dp))
                    }
                }
                if (next != null) {
                    Text("Next Episode", fontSize = 18.sp, color = RoxyColors.onBackground, modifier = Modifier.padding(horizontal = PhonePad, vertical = 6.dp))
                    val startDownload = rememberDownloadStarter()
                    PhoneEpisodeRow(
                        next, app.api, showOverview = false,
                        onClick = { onOpenEpisode(next) },
                        onDownload = { startDownload(listOf(next), next, null) },
                        onMarkWatched = { on -> scope.launch { runCatching { app.api.setPlayed(next.id, on) }; siblings = emptyList(); reloadKey++ } },
                    )
                }
                Box(modifier = Modifier.fillMaxWidth().padding(top = 14.dp), contentAlignment = Alignment.Center) {
                    Text("All Episodes", fontSize = 15.sp, fontWeight = FontWeight.Bold, color = RoxyColors.accent, modifier = Modifier.clickable(onClick = onBack).padding(horizontal = 16.dp, vertical = 8.dp))
                }
            } else if (error == null) {
                Spacer(Modifier.height(1.dp))
            }
        }
    }
}

/**
 * libmpv in a 16:9 box with Crunchyroll's page controls: back / next / fullscreen along
 * the top, -10 / pause / +10 in the middle, the times above a thin seek bar flush with
 * the bottom edge. Progress goes to the server every ten seconds, on pause and when the
 * page is left, like the full player.
 */
@Composable
private fun InlinePlayer(req: PlayRequest, hasNext: Boolean, onBack: () -> Unit, onNext: () -> Unit, onFullscreen: (positionSec: Int) -> Unit, onEnded: () -> Unit) {
    val context = LocalContext.current
    val app = RoxyApp.get(context)
    val settings = app.prefs.settings.value
    val view = LocalView.current
    var paused by remember { mutableStateOf(false) }
    var position by remember { mutableDoubleStateOf(req.startSeconds.toDouble()) }
    var duration by remember { mutableDoubleStateOf(0.0) }
    var controls by remember { mutableStateOf(true) }
    var ended by remember { mutableStateOf(false) }
    var scrubbing by remember { mutableStateOf(false) }
    val main = remember { Handler(Looper.getMainLooper()) }

    val player = remember(req.url) {
        MpvPlayer(
            context = context,
            options = MpvPlayer.Options(
                hwdec = settings.hwdec, vo = settings.vo, startSeconds = req.startSeconds, subMode = settings.subMode,
                audioLangs = settings.mpvAudioLangs, subLangs = settings.mpvSubLangs, anime = req.item.isAnime,
            ),
            fontsDir = app.fontsDir,
            displayRefreshRate = DeviceReport.refreshRate(context),
        ).also { p ->
            p.subtitleLanguageOverrides = subtitleInfos(req.item.primarySource?.streams.orEmpty()).filter { it.detectedLanguage != null }.associate { it.mpvId to it.effectiveLanguage }
        }
    }
    fun report(pos: Double) {
        val ms = (pos * 1000).toLong()
        if (ms <= 0) return
        app.scope.launch { runCatching { app.api.progress(req.item.id, ms) }.onFailure { Timber.w(it, "progress") } }
    }
    DisposableEffect(player) {
        player.listener = object : MpvPlayer.Listener {
            override fun onPauseChanged(paused: Boolean) {
                main.post { setPaused(paused) }
            }
            private fun setPaused(p: Boolean) { paused = p; if (p) report(player.timePosSeconds() ?: position) }
            override fun onEofReached() { main.post { ended = true; report(duration); onEnded() } }
        }
        player.play(req.url, req.startSeconds)
        view.keepScreenOn = true
        onDispose {
            view.keepScreenOn = false
            if (!ended) report(player.timePosSeconds() ?: position)
            player.listener = null
            player.destroy()
        }
    }
    LaunchedEffect(player) {
        var tick = 0
        while (true) {
            delay(500)
            if (!scrubbing) player.timePosSeconds()?.let { position = it }
            player.durationSeconds()?.let { duration = it }
            tick++
            if (tick % 20 == 0 && !paused) report(position)
        }
    }
    LaunchedEffect(controls, paused, scrubbing) { if (controls && !paused && !scrubbing) { delay(3000); controls = false } }

    Box(modifier = Modifier.fillMaxSize()) {
        AndroidView(modifier = Modifier.fillMaxSize(), factory = { ctx -> SurfaceView(ctx).apply { holder.addCallback(player) } })
        Box(
            modifier = Modifier.fillMaxSize().pointerInput(Unit) {
                detectTapGestures(
                    onTap = { controls = !controls },
                    onDoubleTap = { pos -> player.seekRelative(if (pos.x < size.width / 2) -10 else 10); controls = true },
                )
            },
        )
        if (controls) {
            Box(modifier = Modifier.fillMaxSize().background(Color(0x80000000)))
            Row(modifier = Modifier.align(Alignment.TopStart).fillMaxWidth().padding(horizontal = 6.dp, vertical = 2.dp), verticalAlignment = Alignment.CenterVertically) {
                InlineIcon(Icons.AutoMirrored.Filled.ArrowBack, "Back", onClick = onBack)
                Spacer(Modifier.weight(1f))
                if (hasNext) InlineIcon(Icons.Filled.SkipNext, "Next episode", onClick = onNext)
                InlineIcon(Icons.Filled.Fullscreen, "Fullscreen", onClick = { onFullscreen((player.timePosSeconds() ?: position).toInt()) })
            }
            Row(modifier = Modifier.align(Alignment.Center), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(44.dp)) {
                InlineIcon(Icons.Filled.Replay10, "Back 10 seconds", size = 48.dp, onClick = { player.seekRelative(-10) })
                InlineIcon(
                    if (ended) Icons.Filled.Replay else if (paused) Icons.Filled.PlayArrow else Icons.Filled.Pause,
                    if (paused) "Play" else "Pause", size = 64.dp,
                    onClick = { if (ended) { ended = false; player.seekTo(0.0); player.setPaused(false) } else player.togglePause() },
                )
                InlineIcon(Icons.Filled.Forward10, "Forward 10 seconds", size = 48.dp, onClick = { player.seekRelative(10) })
            }
            Column(modifier = Modifier.align(Alignment.BottomCenter).fillMaxWidth()) {
                Row(modifier = Modifier.fillMaxWidth().padding(horizontal = 10.dp, vertical = 6.dp)) {
                    Text(clock(position), color = Color.White, fontSize = 12.sp)
                    Spacer(Modifier.weight(1f))
                    Text(clock(duration), color = Color.White, fontSize = 12.sp)
                }
                // Thin bar on the bottom edge, draggable: played in the accent, the rest translucent.
                val fraction = if (duration > 0) (position / duration).toFloat().coerceIn(0f, 1f) else 0f
                Box(
                    modifier = Modifier.fillMaxWidth().height(14.dp).pointerInput(duration) {
                        detectHorizontalDragGestures(
                            onDragStart = { scrubbing = true; if (duration > 0) position = (it.x / size.width).coerceIn(0f, 1f) * duration },
                            onDragEnd = { scrubbing = false; player.seekTo(position) },
                            onDragCancel = { scrubbing = false },
                            onHorizontalDrag = { change, _ -> if (duration > 0) position = (change.position.x / size.width).coerceIn(0f, 1f) * duration },
                        )
                    }.pointerInput(duration) {
                        detectTapGestures { if (duration > 0) { position = (it.x / size.width).coerceIn(0f, 1f) * duration; player.seekTo(position) } }
                    },
                    contentAlignment = Alignment.BottomStart,
                ) {
                    Box(modifier = Modifier.fillMaxWidth().height(3.dp).background(Color.White.copy(alpha = 0.3f)))
                    Box(modifier = Modifier.fillMaxWidth(fraction).height(3.dp).background(RoxyColors.accent))
                    Box(modifier = Modifier.fillMaxHeight().width(1.dp)) // keeps the box's height when fraction is 0
                }
            }
        }
    }
}

/** A white glyph with a round touch target, like the full player's icons. */
@Composable
private fun InlineIcon(icon: ImageVector, description: String, size: androidx.compose.ui.unit.Dp = 40.dp, onClick: () -> Unit) {
    Icon(icon, contentDescription = description, tint = Color.White, modifier = Modifier.size(size).clip(CircleShape).clickable(onClick = onClick).padding(size / 5))
}

private fun clock(seconds: Double): String {
    val s = seconds.toInt().coerceAtLeast(0)
    val h = s / 3600; val m = (s % 3600) / 60; val sec = s % 60
    return if (h > 0) "%d:%02d:%02d".format(h, m, sec) else "%d:%02d".format(m, sec)
}
