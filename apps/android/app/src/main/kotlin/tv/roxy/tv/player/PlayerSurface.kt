package tv.roxy.tv.player

import android.view.SurfaceView
import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxScope
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.platform.testTag
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.layout.boundsInWindow
import androidx.compose.ui.layout.onGloballyPositioned
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import kotlin.math.roundToInt
import androidx.compose.foundation.layout.size
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.tv.material3.Text

/**
 * The video and every layer over it: tap/double-tap handling, the double-tap readout, the transport controls, the next-episode card, the OSD and the
 * stats overlay. Draws wherever it is put: the whole screen in [PlayerActivity], a box
 * on the phone's episode page.
 *
 * [shape]: the picture's shape (width to height) when the host's box changes size under a
 * playing video (the phone's page: turning, fullscreen, picture-in-picture). The video is
 * then a view of that shape centred in the box, over a buffer of a fixed size that the
 * system scales (see [PictureSurface]); [onPictureBounds] hears where it is in the window.
 * Null (the TV): the video fills the box and mpv fits the picture inside it.
 */
@Composable
fun PlayerSurface(
    session: PlayerSession,
    modifier: Modifier = Modifier,
    shape: Pair<Int, Int>? = null,
    onPictureBounds: ((android.graphics.Rect) -> Unit)? = null,
) {
    val player = session.player
    // Both hosts keep the screen on through the view's own flag, and only while the video
    // plays: paused, the phone may doze and the TV reaches its screensaver as usual.
    val view = LocalView.current
    val playing = !session.ui.paused
    DisposableEffect(view, playing) { view.keepScreenOn = playing; onDispose { view.keepScreenOn = false } }
    Box(modifier = modifier.background(Color.Black)) {
        // The display's own size, not the window's: the same answer in the PiP window.
        val context = LocalContext.current
        val configuration = LocalConfiguration.current
        val render = remember(shape, configuration) {
            shape?.let { s -> PictureSurface.displaySides(context).let { (long, short) -> PictureSurface.renderSize(s, long, short) } }
        }
        val bounds = rememberUpdatedState(onPictureBounds)
        // Native HDR: the decoder fills the surface itself, so the view has to have the
        // picture's shape (a wide film filled the whole screen, stretched tall).
        val placed = shape ?: session.nativeShape.takeIf { session.nativeHdr }
        AndroidView(
            modifier = pictureModifier(placed) { r -> bounds.value?.invoke(r) },
            // The size goes on the holder before the surface exists: the first buffer is
            // already the picture's.
            factory = { ctx -> SurfaceView(ctx).apply { applyRenderSize(render); holder.addCallback(player) } },
            update = { sv -> sv.applyRenderSize(render) },
        )
        // The subtitles of a native-HDR picture: a see-through surface of their own, over
        // the video's and under everything the page draws. mpv draws them there itself
        // (MpvPlayer.subtitleSurface). Gone when mpv gave it up: with it goes whatever mpv
        // had to show on it on the way.
        if (session.nativeHdr && !session.subtitleSurfaceRefused && placed != null) {
            AndroidView(
                modifier = pictureModifier(placed) {},
                factory = { ctx ->
                    SurfaceView(ctx).apply {
                        setZOrderMediaOverlay(true)
                        holder.setFormat(android.graphics.PixelFormat.TRANSLUCENT)
                        holder.addCallback(player.subtitleSurface)
                    }
                },
            )
        }
        // Tap: toggle the controls. Double-tap: seek -10 s on the left half, +10 s on
        // the right (YouTube), with a flash saying how far. No pinch zoom anywhere: it was
        // buggy, inline and fullscreen alike (removed in 0.5.29; the Fill button crops).
        Box(
            modifier = Modifier.fillMaxSize().pointerInput(Unit) {
                // Compose's tap detector fires on the up even after a long drag (it only
                // checks consumption), so a swipe from the top edge for the status bar
                // or shade would count as a tap and pop the controls. Require the finger
                // to have stayed within touch slop.
                var down = Offset.Zero
                val slop = viewConfiguration.touchSlop
                fun moved(pos: Offset) = (pos - down).getDistance() > slop
                detectTapGestures(
                    onPress = { down = it },
                    onTap = { pos -> if (!moved(pos)) session.onTap() },
                    onDoubleTap = { pos -> if (!moved(pos)) session.onDoubleTap(back = pos.x < size.width / 2) },
                )
            },
        )
        if (session.doubleTapFlash != 0) {
            Box(modifier = Modifier.fillMaxSize()) {
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    modifier = Modifier
                        .align(if (session.doubleTapFlash < 0) Alignment.CenterStart else Alignment.CenterEnd)
                        .padding(horizontal = 96.dp)
                        .background(Color(0x66000000), CircleShape)
                        .padding(horizontal = 22.dp, vertical = 16.dp),
                ) {
                    Text(if (session.doubleTapFlash < 0) "◀◀ 10 s" else "10 s ▶▶", color = Color.White, fontSize = 18.sp, fontWeight = FontWeight.SemiBold)
                }
            }
        }
        // The spinner is the one layer of the player's own that the PiP window keeps: a video
        // still loading there, or starved on a slow link, was a plain black window with
        // nothing to say it was coming. (One call for both, so going into PiP while it
        // turns does not start its wait again.)
        BufferingSpinner(session.ui.buffering, compact = session.host.inPip || session.ui.fullscreen == false)
        if (session.host.inPip) return@Box
        PlayerControls(
            state = session.ui,
            visible = session.controlsVisible,
            sheet = session.sheet,
            onSheet = { s -> session.openSheet(s) },
            onInteraction = { session.showControls() },
            actions = session.actions,
            trickplay = session.trickplay,
            onLeaving = { session.hideControls(); session.host.leaving() },
        )
        session.nextCard?.let { card ->
            NextEpisodeCard(
                card = card,
                onPlayNow = { session.playNext() },
                onDismiss = { session.dismissNextCard() },
                compact = session.ui.fullscreen == false, pageHost = session.ui.fullscreen != null,
            )
        }
        session.osd?.let { text ->
            Box(
                modifier = Modifier
                    .align(Alignment.TopEnd)
                    .padding(32.dp)
                    .background(Color(0xB3000000), RoundedCornerShape(8.dp))
                    .padding(horizontal = 16.dp, vertical = 10.dp),
            ) {
                Text(text, color = Color.White, fontSize = 18.sp)
            }
        }
        if (session.showStats) {
            Column(
                modifier = Modifier
                    .align(Alignment.TopStart)
                    .padding(24.dp)
                    .background(Color(0xAA000000))
                    .padding(12.dp),
            ) {
                Text("${session.title}   [hwdec=${session.hwdec} vo=${session.vo}]", color = Color.White, fontSize = 14.sp, fontFamily = FontFamily.Monospace)
                session.statsLines.forEach { Text(it, color = Color(0xFFE8EAED), fontSize = 13.sp, fontFamily = FontFamily.Monospace) }
            }
            Column(
                modifier = Modifier
                    .align(Alignment.BottomStart)
                    .padding(24.dp)
                    .background(Color(0x99000000))
                    .padding(8.dp),
            ) {
                Text("libass / decoder log (newest last)", color = Color(0xFFFBBC04), fontSize = 11.sp, fontFamily = FontFamily.Monospace)
                session.fontLog.takeLast(12).forEach { Text(it, color = Color(0xFFBDC1C6), fontSize = 10.sp, fontFamily = FontFamily.Monospace, maxLines = 1) }
            }
        }
    }
}

/**
 * Where the video's view goes in the player's box. With a [shape] (width to height): as
 * large as fits in the box at that shape, centred, the box's own black showing as the bars
 * beside it; [onBounds] hears its place in the window whenever that changes. Without: the
 * whole box.
 */
internal fun BoxScope.pictureModifier(shape: Pair<Int, Int>?, onBounds: (android.graphics.Rect) -> Unit): Modifier =
    if (shape == null) Modifier.fillMaxSize()
    else Modifier.align(Alignment.Center).aspectRatio(shape.first.toFloat() / shape.second).onGloballyPositioned { c ->
        val b = c.boundsInWindow()
        onBounds(android.graphics.Rect(b.left.roundToInt(), b.top.roundToInt(), b.right.roundToInt(), b.bottom.roundToInt()))
    }

/**
 * A fixed buffer of [size] whatever the view measures (the system scales it into the view),
 * or, with no size, a buffer that follows the view. Setting the size it already has does
 * nothing.
 */
private fun SurfaceView.applyRenderSize(size: Pair<Int, Int>?) {
    if (size != null) holder.setFixedSize(size.first, size.second) else holder.setSizeFromLayout()
}

/**
 * The spinner over the picture while mpv waits on the network, a seek or a (re)load. It
 * appears only after a short grace period, so a quick seek inside the cache shows nothing.
 */
@Composable
internal fun BufferingSpinner(buffering: Boolean, compact: Boolean) {
    var shown by remember { mutableStateOf(false) }
    LaunchedEffect(buffering) {
        if (!buffering) { shown = false; return@LaunchedEffect }
        kotlinx.coroutines.delay(BUFFERING_GRACE_MS)
        shown = true
    }
    if (!shown) return
    Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        CircularProgressIndicator(
            color = Color.White,
            strokeWidth = if (compact) 3.dp else 4.dp,
            modifier = Modifier.size(if (compact) 36.dp else 56.dp).testTag("buffering"),
        )
    }
}

/** How long the video may wait (the network, a seek, a load) before the spinner shows. */
internal const val BUFFERING_GRACE_MS = 400L
