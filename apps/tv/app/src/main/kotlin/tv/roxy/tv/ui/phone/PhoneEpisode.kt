package tv.roxy.tv.ui.phone

import android.app.Activity
import android.content.Context
import android.content.ContextWrapper
import android.content.pm.ActivityInfo
import android.content.res.Configuration
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.WindowInsetsControllerCompat
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.Item
import tv.roxy.tv.data.downloads.formatBytes
import tv.roxy.tv.player.PlayerSession
import tv.roxy.tv.player.PlayerSurface
import tv.roxy.tv.ui.PlayRequest
import tv.roxy.tv.ui.components.RoxyImage
import tv.roxy.tv.ui.components.widthPx
import tv.roxy.tv.ui.playRequest
import tv.roxy.tv.ui.theme.RoxyColors

/**
 * Crunchyroll's episode page. The one player ([PlayerSession], the same as the
 * full-screen activity's) sits in a 16:9 box at the top; turn the phone, or tap
 * fullscreen, and that box fills the screen with playback carrying on. Under it: the
 * series name in the brand colour, "E3 - Title", "14+ ◆ Dub | Sub", download and menu,
 * the synopsis, one Next Episode row and an All Episodes link back to the series. When
 * autoplay moves to the next episode, the text follows.
 */
@Composable
fun PhoneEpisode(episodeId: String, fallbackTitle: String, onBack: () -> Unit, onOpenEpisode: (Item) -> Unit) {
    val context = LocalContext.current
    val app = RoxyApp.get(context)
    val scope = rememberCoroutineScope()
    val activity = remember(context) { context.findActivity() }
    val view = LocalView.current
    val landscape = LocalConfiguration.current.orientation == Configuration.ORIENTATION_LANDSCAPE

    var episode by remember { mutableStateOf(app.items[episodeId]) }
    var session by remember { mutableStateOf<PlayerSession?>(null) }
    var siblings by remember { mutableStateOf<List<Item>>(emptyList()) }
    var error by remember { mutableStateOf<String?>(null) }
    var watchedKey by remember { mutableIntStateOf(0) }
    var expanded by rememberSaveable { mutableStateOf(false) }
    var overflows by remember { mutableStateOf(false) }

    fun setFullscreen(on: Boolean) {
        activity?.requestedOrientation = if (on) ActivityInfo.SCREEN_ORIENTATION_SENSOR_LANDSCAPE else ActivityInfo.SCREEN_ORIENTATION_SENSOR_PORTRAIT
    }

    // The page follows the phone (within the system's rotation lock): unlocked while it is
    // open, back to portrait when it closes.
    DisposableEffect(activity) {
        activity?.requestedOrientation = ActivityInfo.SCREEN_ORIENTATION_FULL_USER
        onDispose { activity?.requestedOrientation = ActivityInfo.SCREEN_ORIENTATION_PORTRAIT }
    }
    // After a fullscreen/exit button locks the orientation to get there, release the lock
    // once the phone has turned, so turning it by hand works again.
    LaunchedEffect(landscape) {
        delay(1500)
        activity?.requestedOrientation = ActivityInfo.SCREEN_ORIENTATION_FULL_USER
    }
    // System bars go with the orientation: hidden over the fullscreen video.
    LaunchedEffect(landscape) {
        val window = activity?.window ?: return@LaunchedEffect
        WindowInsetsControllerCompat(window, view).apply {
            if (landscape) { hide(WindowInsetsCompat.Type.systemBars()); systemBarsBehavior = WindowInsetsControllerCompat.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE } else show(WindowInsetsCompat.Type.systemBars())
        }
    }

    val host = remember {
        object : PlayerSession.Host {
            var isLandscape = false
            var back: () -> Unit = {}
            var nowPlaying: (PlayRequest) -> Unit = {}
            override fun back() = back.invoke() // the arrow leaves the page; fullscreen ends by the button or by turning the phone
            override val fullscreen: Boolean get() = isLandscape
            override fun toggleFullscreen() = setFullscreen(!isLandscape)
            override fun nowPlaying(req: PlayRequest) = nowPlaying.invoke(req)
        }
    }
    host.isLandscape = landscape
    host.back = onBack
    host.nowPlaying = { req -> episode = req.item; expanded = false }

    LaunchedEffect(episodeId) {
        val seed = app.items[episodeId] ?: Item(id = episodeId, type = "episode", title = fallbackTitle)
        playRequest(context, seed)
            .onSuccess { r ->
                episode = r.item
                if (session == null) session = PlayerSession(context, app, PlayerSession.Params.from(r, app.prefs.settings.value), scope, host)
            }
            .onFailure { t -> Timber.w(t, "episode"); error = t.message }
    }
    LaunchedEffect(episode?.seriesId, watchedKey) {
        val seriesId = episode?.seriesId ?: return@LaunchedEffect
        runCatching { app.api.items(type = "episode", parent = seriesId, recursive = true, includeSources = true, sort = "index").items }
            .onSuccess { eps ->
                siblings = eps.sortedWith(compareBy({ (it.seasonAired ?: 0).let { s -> if (s == 0) Int.MAX_VALUE else s } }, { it.indexAired ?: it.indexAbsolute ?: 0 }))
            }
    }
    LifecycleEventEffect(Lifecycle.Event.ON_STOP) { session?.onStop() }
    // Pin the session the effect was keyed on: the lambda must not read the state delegate
    // at dispose time, or the effect for "no session yet" would destroy the one just made.
    DisposableEffect(session) { val s = session; onDispose { s?.destroy() } }

    val ep = episode
    val next = siblings.dropWhile { it.id != ep?.id }.drop(1).firstOrNull { it.isPlayable }
    Column(modifier = Modifier.fillMaxSize().background(if (landscape) Color.Black else RoxyColors.background).then(if (landscape) Modifier else Modifier.statusBarsPadding())) {
        // The player keeps its place in the tree in both orientations, so the surface and
        // the stream survive the turn; only its size changes.
        Box(modifier = if (landscape) Modifier.fillMaxSize() else Modifier.fillMaxWidth().aspectRatio(16f / 9f)) {
            val s = session
            when {
                s != null -> PlayerSurface(s, modifier = Modifier.fillMaxSize())
                error != null -> Text(error ?: "", color = RoxyColors.error, modifier = Modifier.align(Alignment.Center).background(Color.Black))
                ep != null -> RoxyImage(app.api.imageUrl(ep, "thumb", widthPx(420.dp) * 2), ep.episodeCode, modifier = Modifier.fillMaxSize(), placeholderTextSize = 14)
            }
        }
        if (!landscape) Column(modifier = Modifier.fillMaxSize().verticalScroll(rememberScrollState()).navigationBarsPadding().padding(bottom = 24.dp)) {
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
                    // The file's headline facts at the left (what differs between releases), the
                    // download and menu at the right.
                    Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.fillMaxWidth().padding(top = 8.dp)) {
                        Text(fileFacts(ep), fontSize = 12.sp, color = RoxyColors.muted, maxLines = 2, lineHeight = 16.sp, modifier = Modifier.weight(1f).padding(end = 8.dp))
                        val startDownload = rememberDownloadStarter()
                        if (ep.isPlayable) DownloadButton(ep, onDownload = { startDownload(listOf(ep), ep, null) }, size = 24.dp)
                        EpisodeMenu(ep, app.api, onMarkWatched = { on -> scope.launch { runCatching { app.api.setPlayed(ep.id, on) }; watchedKey++ } })
                    }
                    ep.overview?.takeIf { it.isNotBlank() }?.let {
                        Text(
                            it, fontSize = 14.sp, lineHeight = 20.sp, color = RoxyColors.onBackground.copy(alpha = 0.9f),
                            maxLines = if (expanded) Int.MAX_VALUE else 3, overflow = TextOverflow.Ellipsis,
                            onTextLayout = { r -> if (!expanded) overflows = r.hasVisualOverflow },
                            modifier = Modifier.padding(top = 6.dp).clickable(interactionSource = remember { MutableInteractionSource() }, indication = null) { expanded = !expanded },
                        )
                        // Only when there is more to read: a synopsis that fits gets no link.
                        if (!expanded && overflows) Text("Read more", fontSize = 14.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.accent, modifier = Modifier.align(Alignment.End).clickable { expanded = true }.padding(top = 2.dp))
                    }
                }
                if (next != null) {
                    Text("Next Episode", fontSize = 18.sp, color = RoxyColors.onBackground, modifier = Modifier.padding(horizontal = PhonePad, vertical = 6.dp))
                    val startDownload = rememberDownloadStarter()
                    PhoneEpisodeRow(
                        next, app.api, showOverview = false,
                        onClick = { onOpenEpisode(next) },
                        onDownload = { startDownload(listOf(next), next, null) },
                        onMarkWatched = { on -> scope.launch { runCatching { app.api.setPlayed(next.id, on) }; watchedKey++ } },
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

/** "1080p · HEVC 10-bit · 8.1 Mb/s · 1.2 GB": the facts that tell releases apart. */
private fun fileFacts(ep: Item): String {
    val src = ep.primarySource ?: return ""
    val v = src.streams.firstOrNull { it.type == "video" }
    val codec = when (v?.codec?.lowercase()) { "hevc", "h265" -> "HEVC"; "h264", "avc" -> "H.264"; "av1" -> "AV1"; "vp9" -> "VP9"; null, "" -> null; else -> v.codec.uppercase() }
    val res = v?.height?.takeIf { it > 0 }?.let { "${it}p" }
    val depth = v?.bitDepth?.takeIf { it > 8 }?.let { "$it-bit" }
    val mbps = if (src.size > 0 && (src.durationMs ?: 0L) > 0) src.size * 8.0 / (src.durationMs!! / 1000.0) / 1_000_000 else 0.0
    return listOfNotNull(
        res,
        listOfNotNull(codec, depth).joinToString(" ").ifEmpty { null },
        mbps.takeIf { it > 0 }?.let { "%.1f Mb/s".format(it) },
        src.size.takeIf { it > 0 }?.let { formatBytes(it) },
    ).joinToString("  ·  ")
}

private tailrec fun Context.findActivity(): Activity? = when (this) {
    is Activity -> this
    is ContextWrapper -> baseContext.findActivity()
    else -> null
}
