package tv.roxy.tv.ui.phone

import android.app.Activity
import android.content.Context
import android.content.ContextWrapper
import android.content.pm.ActivityInfo
import android.content.res.Configuration
import android.view.OrientationEventListener
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
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.unit.Constraints
import androidx.compose.ui.text.rememberTextMeasurer
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.activity.compose.BackHandler
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.WindowInsetsControllerCompat
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.RoxyApp
import androidx.compose.ui.layout.boundsInWindow
import androidx.compose.ui.layout.onGloballyPositioned
import tv.roxy.tv.MainActivity
import tv.roxy.tv.api.Item
import tv.roxy.tv.api.CastMember
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
fun PhoneEpisode(episodeId: String, fallbackTitle: String, onBack: () -> Unit, onOpenEpisode: (Item) -> Unit, onOpenPerson: (CastMember) -> Unit = {}) {
    val context = LocalContext.current
    val app = RoxyApp.get(context)
    val scope = rememberCoroutineScope()
    val activity = remember(context) { context.findActivity() }
    val main = activity as? MainActivity
    val inPip = main?.inPip == true || main?.pipTransitioning == true
    var videoBounds by remember { mutableStateOf<android.graphics.Rect?>(null) }
    val view = LocalView.current
    val landscape = LocalConfiguration.current.orientation == Configuration.ORIENTATION_LANDSCAPE

    var episode by remember { mutableStateOf(app.items[episodeId]) }
    var session by remember { mutableStateOf<PlayerSession?>(null) }
    var siblings by remember { mutableStateOf<List<Item>>(emptyList()) }
    var error by remember { mutableStateOf<String?>(null) }
    var watchedKey by remember { mutableIntStateOf(0) }
    OnUserDataChanged { watchedKey++ }
    var expanded by rememberSaveable { mutableStateOf(false) }

    fun setFullscreen(on: Boolean) {
        activity?.requestedOrientation = if (on) ActivityInfo.SCREEN_ORIENTATION_SENSOR_LANDSCAPE else ActivityInfo.SCREEN_ORIENTATION_SENSOR_PORTRAIT
    }

    // The page follows the phone (within the system's rotation lock): unlocked while it is
    // open, back to portrait when it closes.
    DisposableEffect(activity) {
        activity?.requestedOrientation = ActivityInfo.SCREEN_ORIENTATION_FULL_USER
        onDispose { activity?.requestedOrientation = ActivityInfo.SCREEN_ORIENTATION_PORTRAIT }
    }
    // A fullscreen/exit button locks the orientation to get there. The lock is released
    // only once the phone is physically held the way the button asked AND the screen has
    // got there (YouTube's rule), and only with auto-rotate on. Two bugs before: the check
    // compared the sensor with the screen as it was, so pressing Fullscreen with the phone
    // upright (or flat on a table, the sensor flapping) "matched" at once and undid the
    // press; and with rotation lock on, releasing meant going back to portrait mid-video.
    val physical = rememberPhysicalOrientation()
    LaunchedEffect(landscape, physical) {
        val a = activity ?: return@LaunchedEffect
        val target = when (a.requestedOrientation) {
            ActivityInfo.SCREEN_ORIENTATION_SENSOR_LANDSCAPE -> true
            ActivityInfo.SCREEN_ORIENTATION_SENSOR_PORTRAIT -> false
            else -> return@LaunchedEffect // not locked
        }
        val autoRotate = android.provider.Settings.System.getInt(a.contentResolver, android.provider.Settings.System.ACCELEROMETER_ROTATION, 0) == 1
        if (autoRotate && physical == target && landscape == target) a.requestedOrientation = ActivityInfo.SCREEN_ORIENTATION_FULL_USER
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
            var mainActivity: MainActivity? = null
            var bounds: () -> android.graphics.Rect? = { null }
            var cast: () -> Unit = {}
            override fun back() = back.invoke() // the arrow leaves the page; fullscreen ends by the button or by turning the phone
            override fun cast() = cast.invoke()
            override val fullscreen: Boolean get() = isLandscape
            override fun toggleFullscreen() = setFullscreen(!isLandscape)
            override fun nowPlaying(req: PlayRequest) = nowPlaying.invoke(req)
            override val pipSupported: Boolean get() = mainActivity?.pipSupported == true
            override val inPip: Boolean get() = mainActivity?.inPip == true
            override fun enterPip() { mainActivity?.enterPip() }
            override fun playingChanged(playing: Boolean) { mainActivity?.updatePip(playing, bounds()) }
        }
    }
    host.mainActivity = main
    host.bounds = { videoBounds }
    var castSheet by remember { mutableStateOf(false) }
    host.cast = { castSheet = true }
    if (castSheet) CastSheet(onDismiss = { castSheet = false })
    // Playing state is reported on change only; the box's position changes on rotation,
    // so push it as well when it moves.
    LaunchedEffect(videoBounds) { val s = session ?: return@LaunchedEffect; main?.updatePip(!s.ui.paused, videoBounds) }
    LaunchedEffect(inPip) { if (inPip) session?.hideControls() }
    // The PiP shrink and expand animate the live surface; the resize bounce (a black frame
    // and an audio re-seek) must be off before the surface changes, so it is armed on the
    // activity's callback rather than from a Compose effect.
    DisposableEffect(main, session) {
        main?.onPipChanged = pip@{ entering ->
            val s = session ?: return@pip
            // The exit callback comes after the animation with the view laid out at its full
            // size; the box is the same shape as the window, so unpinning then is a no-op
            // when the size matches and a single resize when it does not.
            s.pinSurface(entering)
        }
        onDispose { main?.onPipChanged = null; main?.clearPip() }
    }
    host.isLandscape = landscape
    host.back = onBack
    host.nowPlaying = { req -> episode = req.item; expanded = false }

    LaunchedEffect(episodeId) {
        val seed = app.items[episodeId] ?: Item(id = episodeId, type = "episode", title = fallbackTitle)
        // Server away (restart, Wi-Fi drop): say so and keep trying, instead of a dead box.
        var wait = 3_000L
        while (true) {
            val r = playRequest(context, seed)
            r.onSuccess { req ->
                error = null
                episode = req.item
                if (session == null) session = PlayerSession(context, app, PlayerSession.Params.from(req, app.prefs.settings.value), scope, host)
            }
            val t = r.exceptionOrNull() ?: break
            Timber.w(t, "episode")
            val retry = t is java.io.IOException && t !is tv.roxy.tv.api.ApiException
            error = tv.roxy.tv.data.friendlyError(t) + if (retry) " Retrying…" else ""
            if (!retry) break
            kotlinx.coroutines.delay(wait)
            wait = (wait * 2).coerceAtMost(30_000L)
        }
    }
    var cast by remember { mutableStateOf<List<CastMember>>(emptyList()) }
    LaunchedEffect(episode?.id, episode?.isMovie) {
        val m = episode?.takeIf { it.isMovie } ?: return@LaunchedEffect
        cast = runCatching { app.api.itemDetails(m.id).tmdb?.cast.orEmpty() }.getOrDefault(emptyList())
    }
    // Episodes carry no rating or advisories of their own; the series' stand for them.
    var series by remember { mutableStateOf<Item?>(null) }
    LaunchedEffect(episode?.seriesId) {
        val seriesId = episode?.seriesId ?: return@LaunchedEffect
        series = app.items[seriesId] ?: runCatching { app.api.item(seriesId) }.getOrNull()
    }
    val seriesRating = series?.officialRating
    // The page's own episode (or film) was loaded once, for the player; its watched state
    // changes under it (its menu, finishing or rewatching it here). Refresh just that, so
    // its menu offers the right "Mark as watched / unwatched".
    LaunchedEffect(watchedKey) {
        if (watchedKey == 0) return@LaunchedEffect
        val id = episode?.id ?: return@LaunchedEffect
        runCatching { app.api.item(id) }.onSuccess { fresh -> if (episode?.id == id) episode = episode?.copy(userData = fresh.userData) }
    }
    LaunchedEffect(episode?.seriesId, watchedKey) {
        val seriesId = episode?.seriesId ?: return@LaunchedEffect
        runCatching { app.api.items(type = "episode", parent = seriesId, recursive = true, includeSources = true, sort = "index").items }
            .onSuccess { eps ->
                siblings = eps.sortedWith(compareBy({ (it.seasonAired ?: 0).let { s -> if (s == 0) Int.MAX_VALUE else s } }, { it.indexAired ?: it.indexAbsolute ?: 0 }))
            }
    }
    // Stopped means off screen: the PiP window was dismissed, or the app was left while
    // paused. Entering PiP never stops the activity, so this always pauses.
    LifecycleEventEffect(Lifecycle.Event.ON_STOP) { session?.let { it.reportProgress("stop"); it.player.setPaused(true) } }
    // The system back gesture in fullscreen drops back to the mini player, not off the page.
    BackHandler(enabled = landscape) { setFullscreen(false) }
    // Pin the session the effect was keyed on: the lambda must not read the state delegate
    // at dispose time, or the effect for "no session yet" would destroy the one just made.
    DisposableEffect(session) { val s = session; onDispose { s?.destroy() } }

    val ep = episode
    val next = siblings.dropWhile { it.id != ep?.id }.drop(1).firstOrNull { it.isPlayable }
    val full = landscape || inPip
    Column(modifier = Modifier.fillMaxSize().background(if (full) Color.Black else RoxyColors.background).then(if (full) Modifier else Modifier.statusBarsPadding())) {
        // The player keeps its place in the tree in both orientations and in the PiP window,
        // so the surface and the stream survive; only its size changes. Fullscreen is the
        // whole screen; mpv fits the picture inside it (pillars on this phone's 21.7:9).
        Box(
            modifier = (if (full) Modifier.fillMaxSize() else Modifier.fillMaxWidth().aspectRatio(16f / 9f)).onGloballyPositioned { c ->
                val b = c.boundsInWindow()
                val r = android.graphics.Rect(b.left.toInt(), b.top.toInt(), b.right.toInt(), b.bottom.toInt())
                if (r != videoBounds) videoBounds = r
            },
        ) {
            val s = session
            when {
                s != null -> PlayerSurface(s, modifier = Modifier.fillMaxSize())
                error != null -> Box(Modifier.fillMaxSize().background(Color.Black), contentAlignment = Alignment.Center) {
                    Text(error ?: "", color = Color.White, textAlign = androidx.compose.ui.text.style.TextAlign.Center, modifier = Modifier.padding(horizontal = 24.dp))
                }
                ep != null -> RoxyImage(app.api.imageUrl(ep, "thumb", widthPx(420.dp) * 2) ?: app.api.imageUrl(ep, "backdrop", widthPx(420.dp) * 2), ep.episodeCode, modifier = Modifier.fillMaxSize(), placeholderTextSize = 14)
            }
        }
        if (!full) Column(modifier = Modifier.fillMaxSize().verticalScroll(rememberScrollState()).navigationBarsPadding().padding(bottom = 24.dp)) {
            if (ep != null) {
                Column(modifier = Modifier.padding(horizontal = PhonePad, vertical = 14.dp)) {
                    // Series name over an episode; "Movie" over a film.
                    (ep.seriesTitle?.takeIf { it.isNotBlank() } ?: if (ep.isMovie) "Movie" else null)?.let { Text(it, fontSize = 13.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.accent, maxLines = 1, overflow = TextOverflow.Ellipsis) }
                    val code = if (ep.isMovie) null else (ep.indexAired ?: ep.indexAbsolute)?.let { "E$it" }
                    Text(
                        listOfNotNull(code, ep.title.takeIf { it.isNotBlank() }).joinToString(" - "),
                        fontSize = 16.sp, lineHeight = 21.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(top = 4.dp),
                    )
                    Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.padding(top = 8.dp)) {
                        val rating = ep.officialRating ?: seriesRating
                        val age = rating?.let { ageLabel(it) }
                        if (age != null) { AgePill(age, small = true) }
                        audioBadge(ep)?.let { badge -> Text(diamondJoined(listOfNotNull(if (age != null) "" else null, badge)), fontSize = 13.sp, color = Color.White.copy(alpha = 0.85f)) }
                    }
                    // The file's headline facts at the left (what differs between releases), the
                    // download and menu at the right.
                    Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.fillMaxWidth().padding(top = 8.dp)) {
                        Text(fileFacts(ep), fontSize = 12.sp, color = RoxyColors.muted, maxLines = 2, lineHeight = 16.sp, modifier = Modifier.weight(1f).padding(end = 8.dp))
                        val startDownload = rememberDownloadStarter()
                        if (ep.isPlayable) DownloadButton(ep, onDownload = { startDownload(listOf(ep), ep, null) }, size = 24.dp)
                        EpisodeMenu(ep, app.api, onMarkWatched = { on -> scope.launch { app.markWatched(ep.id, on) } })
                    }
                    ep.overview?.takeIf { it.isNotBlank() }?.let { ExpandableSynopsis(it, expanded, onToggle = { expanded = !expanded }) }
                    // Content advisory, Crunchyroll's row under the synopsis: the descriptors the
                    // server derived (TMDB's, or AniList's tags), else the rating's meaning.
                    (ep.officialRating ?: seriesRating)?.let { r ->
                        val descriptors = ep.advisories.ifEmpty { series?.advisories.orEmpty() }
                        val text = descriptors.takeIf { it.isNotEmpty() }?.joinToString(", ") ?: advisoryFor(r)
                        if (text != null) {
                            Text("Content Advisory", fontSize = 18.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(top = 22.dp))
                            Text(ageLabel(r), fontSize = 15.sp, lineHeight = 22.sp, color = AdvisoryGrey, modifier = Modifier.padding(top = 8.dp))
                            Text(text, fontSize = 15.sp, lineHeight = 22.sp, color = AdvisoryGrey)
                        }
                    }
                }
                if (next != null) {
                    Text("Next Episode", fontSize = 18.sp, color = RoxyColors.onBackground, modifier = Modifier.padding(horizontal = PhonePad, vertical = 6.dp))
                    val startDownload = rememberDownloadStarter()
                    PhoneEpisodeRow(
                        next, app.api,
                        onClick = { onOpenEpisode(next) },
                        onDownload = { startDownload(listOf(next), next, null) },
                        onMarkWatched = { on -> scope.launch { app.markWatched(next.id, on) } },
                    )
                }
                // A film's cast (the series page keeps it under More details): the player
                // page is where someone wonders who that voice is.
                if (ep.isMovie && cast.isNotEmpty()) {
                    Text("Cast", fontSize = 18.sp, color = RoxyColors.onBackground, modifier = Modifier.padding(horizontal = PhonePad, vertical = 6.dp))
                    CastRow(cast, modifier = Modifier.padding(top = 4.dp), onClick = onOpenPerson)
                }
                Box(modifier = Modifier.fillMaxWidth().padding(top = 14.dp), contentAlignment = Alignment.Center) {
                    Text(if (ep.isMovie) "Back to the movie" else "All Episodes", fontSize = 15.sp, fontWeight = FontWeight.Bold, color = RoxyColors.accent, modifier = Modifier.clickable(onClick = onBack).padding(horizontal = 16.dp, vertical = 8.dp))
                }
            } else if (error == null) {
                Spacer(Modifier.height(1.dp))
            }
        }
    }
}

/**
 * Three lines of synopsis with "...Read more" inline at the end of the third, the way
 * Crunchyroll does it; tapping anywhere expands, and a synopsis that fits gets no link.
 */
@Composable
internal fun ExpandableSynopsis(text: String, expanded: Boolean, onToggle: () -> Unit) {
    val style = TextStyle(fontSize = 15.sp, lineHeight = 22.sp, color = RoxyColors.onBackground.copy(alpha = 0.92f))
    val measurer = rememberTextMeasurer()
    BoxWithConstraints(modifier = Modifier.fillMaxWidth().padding(top = 6.dp).clickable(interactionSource = remember { MutableInteractionSource() }, indication = null, onClick = onToggle)) {
        val widthPx = constraints.maxWidth
        val link = "Read more"
        val shown: AnnotatedString = remember(text, expanded, widthPx) {
            if (expanded) return@remember AnnotatedString(text)
            val full = measurer.measure(AnnotatedString(text), style = style, constraints = Constraints(maxWidth = widthPx))
            if (full.lineCount <= 3) return@remember AnnotatedString(text)
            // Cut the text so that "... Read more" fits on the third line.
            val suffix = "\u2026 "
            var end = full.getLineEnd(2, visibleEnd = true)
            while (end > 0) {
                val candidate = buildAnnotatedString {
                    append(text.substring(0, end).trimEnd()); append(suffix)
                    withStyle(SpanStyle(color = RoxyColors.accent, fontWeight = FontWeight.SemiBold)) { append(link) }
                }
                val m = measurer.measure(candidate, style = style, constraints = Constraints(maxWidth = widthPx))
                if (m.lineCount <= 3) return@remember candidate
                end = text.lastIndexOf(' ', end - 2).takeIf { it > 0 } ?: (end - 1)
            }
            AnnotatedString(text)
        }
        Text(shown, style = style)
    }
}

/** Crunchyroll's advisory grey: readable, clearly secondary. */
private val AdvisoryGrey = Color(0xFFA0A0A8)

/** Facts joined by a small diamond, Crunchyroll's separator, drawn smaller than the text. */
private fun diamondJoined(parts: List<String>) = buildAnnotatedString {
    parts.forEachIndexed { i, part ->
        if (i > 0) withStyle(SpanStyle(fontSize = 7.sp, color = Color.White.copy(alpha = 0.6f))) { append("  ◆  ") }
        append(part)
    }
}

/** The TV Parental Guidelines / MPAA wording for a rating; null for ratings we do not know. */
private fun advisoryFor(rating: String): String? = when (rating.uppercase().removePrefix("US-")) {
    "TV-Y" -> "Suitable for all children"
    "TV-Y7" -> "Directed to older children"
    "TV-G", "G" -> "Suitable for all ages"
    "TV-PG", "PG" -> "Parental guidance suggested"
    "TV-14", "PG-13" -> "May be unsuitable for children under 14"
    "TV-MA", "R", "NC-17" -> "Mature audiences only"
    else -> null
}

/** "1080p · HEVC 10-bit · 8.1 Mb/s · 1.2 GB": the facts that tell releases apart. */
private fun fileFacts(ep: Item): String {
    val src = ep.primarySource ?: return ""
    val v = src.streams.firstOrNull { it.type == "video" }
    val codec = when (v?.codec?.lowercase()) { "hevc", "h265" -> "HEVC"; "h264", "avc" -> "H.264"; "av1" -> "AV1"; "vp9" -> "VP9"; null, "" -> null; else -> v.codec.uppercase() }
    // Films are often cropped a few lines short of a standard height (1038p, 1072p); say what people call it.
    val res = v?.height?.takeIf { it > 0 }?.let { h -> listOf(2160, 1440, 1080, 720, 576, 480).firstOrNull { h >= it * 0.94 }?.let { "${it}p" } ?: "${h}p" }
    val depth = v?.bitDepth?.takeIf { it > 8 }?.let { "$it-bit" }
    val mbps = if (src.size > 0 && (src.durationMs ?: 0L) > 0) src.size * 8.0 / (src.durationMs!! / 1000.0) / 1_000_000 else 0.0
    return listOfNotNull(
        res,
        listOfNotNull(codec, depth).joinToString(" ").ifEmpty { null },
        mbps.takeIf { it > 0 }?.let { "%.1f Mb/s".format(it) },
    ).joinToString("  ·  ")
}

/** True when the phone is physically held landscape, false upright, null until the sensor says. */
@Composable
private fun rememberPhysicalOrientation(): Boolean? {
    val context = LocalContext.current
    var landscape by remember { mutableStateOf<Boolean?>(null) }
    DisposableEffect(context) {
        val listener = object : OrientationEventListener(context) {
            override fun onOrientationChanged(degrees: Int) {
                if (degrees == ORIENTATION_UNKNOWN) return
                // Dead bands around the diagonals keep a tilted phone from flapping.
                val l = when (degrees) { in 60..120, in 240..300 -> true; in 0..30, in 150..210, in 330..359 -> false; else -> return }
                if (landscape != l) landscape = l
            }
        }
        if (listener.canDetectOrientation()) listener.enable()
        onDispose { listener.disable() }
    }
    return landscape
}

private tailrec fun Context.findActivity(): Activity? = when (this) {
    is Activity -> this
    is ContextWrapper -> baseContext.findActivity()
    else -> null
}
