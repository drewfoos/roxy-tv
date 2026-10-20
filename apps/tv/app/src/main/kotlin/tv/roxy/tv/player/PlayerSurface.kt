package tv.roxy.tv.player

import android.view.SurfaceView
import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.ui.platform.LocalView
import androidx.compose.runtime.DisposableEffect
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
 * The video and every layer over it: tap/double-tap/pinch handling, the zoom and
 * double-tap readouts, the transport controls, the next-episode card, the OSD and the
 * stats overlay. Draws wherever it is put: the whole screen in [PlayerActivity], a box
 * on the phone's episode page.
 */
@Composable
fun PlayerSurface(session: PlayerSession, modifier: Modifier = Modifier) {
    val player = session.player
    // The window flag belongs to the activity host; a page host needs the view's own flag,
    // or the phone dozes off in the middle of an episode.
    val view = LocalView.current
    DisposableEffect(view) { view.keepScreenOn = true; onDispose { view.keepScreenOn = false } }
    Box(modifier = modifier.background(Color.Black)) {
        AndroidView(
            modifier = Modifier.fillMaxSize(),
            factory = { ctx -> SurfaceView(ctx).apply { holder.addCallback(player) } },
        )
        // Tap: toggle the controls. Double-tap: seek -10 s on the left half, +10 s on
        // the right (YouTube), with a flash saying how far.
        Box(
            modifier = Modifier.fillMaxSize().pinchZoom(
                onZoom = { f, x, y -> session.onZoom(f, x, y) },
                onLevel = { level, active -> session.onZoomLevel(level, active) },
                onPinchStart = { session.onPinchStart() },
                videoAspect = { if (player.isFill()) null else player.videoAspect()?.toFloat() },
            ).pointerInput(Unit) {
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
        // Zoom readout: "1.5×" at the top while pinching and briefly after.
        if (session.zoomHud) {
            Box(modifier = Modifier.fillMaxSize()) {
                Text(
                    "%.1f×".format(session.zoomLevel),
                    color = Color.White, fontSize = 15.sp, fontWeight = FontWeight.SemiBold,
                    modifier = Modifier.align(Alignment.TopCenter).padding(top = 28.dp).background(Color(0x99000000), RoundedCornerShape(14.dp)).padding(horizontal = 12.dp, vertical = 5.dp),
                )
            }
        }
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
        PlayerControls(
            state = session.ui,
            visible = session.controlsVisible,
            sheet = session.sheet,
            onSheet = { s -> session.openSheet(s) },
            onInteraction = { session.showControls() },
            actions = session.actions,
            trickplay = session.trickplay,
        )
        session.nextCard?.let { card ->
            NextEpisodeCard(
                card = card, controlsVisible = session.controlsVisible,
                onPlayNow = { session.playNext() },
                onDismiss = { session.dismissNextCard() },
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
