package tv.roxy.tv.ui.phone

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Cast
import androidx.compose.material.icons.filled.CastConnected
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Forward10
import androidx.compose.material.icons.filled.Pause
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Replay10
import androidx.compose.material.icons.filled.SkipNext
import androidx.compose.material.icons.outlined.PhoneAndroid
import androidx.compose.material.icons.outlined.Tv
import androidx.compose.material3.BottomSheetDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.launch
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.RemoteCommand
import tv.roxy.tv.api.RemotePlayer
import tv.roxy.tv.ui.theme.RoxyColors

/**
 * The Cast icon: white when playing here, the brand blue "connected" glyph when a
 * target is set. Tapping opens the picker. One composable for the Home header, the
 * title pages and the player.
 */
@Composable
fun CastIcon(modifier: Modifier = Modifier, size: Int = 24) {
    val remote = RoxyApp.get(LocalContext.current).remote
    val target by remote.target.collectAsStateWithLifecycle()
    var open by remember { mutableStateOf(false) }
    Icon(
        if (target != null) Icons.Filled.CastConnected else Icons.Filled.Cast,
        contentDescription = if (target != null) "Playing on ${target?.name}" else "Play on another device",
        tint = if (target != null) RoxyColors.accent else Color.White,
        modifier = modifier.size(size.dp).clickable { open = true },
    )
    if (open) CastSheet(onDismiss = { open = false })
}

/** Plex's player picker: this phone, then every player signed in to the same login that is open now. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun CastSheet(onDismiss: () -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    val remote = app.remote
    val scope = rememberCoroutineScope()
    val target by remote.target.collectAsStateWithLifecycle()
    val players by remote.players.collectAsStateWithLifecycle()
    var loading by remember { mutableStateOf(true) }
    LaunchedEffect(Unit) { remote.refreshPlayers(); loading = false }
    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true), containerColor = RoxyColors.surface, dragHandle = { BottomSheetDefaults.DragHandle(color = RoxyColors.muted) }) {
        Column(modifier = Modifier.fillMaxWidth().padding(bottom = 24.dp).navigationBarsPadding()) {
            Text("Play on", fontSize = 18.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(horizontal = PhonePad, vertical = 8.dp))
            DeviceRow(Icons.Outlined.PhoneAndroid, "This phone", null, selected = target == null) { remote.select(null); onDismiss() }
            players.forEach { p ->
                val now = p.nowPlaying?.let { n -> (if (n.paused) "Paused  ·  " else "Playing  ·  ") + n.title.ifEmpty { "something" } }
                DeviceRow(Icons.Outlined.Tv, p.name, now, selected = target?.deviceId == p.deviceId) { remote.select(p); onDismiss() }
            }
            when {
                loading -> Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.padding(horizontal = PhonePad, vertical = 14.dp)) {
                    CircularProgressIndicator(color = RoxyColors.accent, strokeWidth = 2.dp, modifier = Modifier.size(16.dp))
                    Text("Looking for devices on your server…", fontSize = 13.sp, color = RoxyColors.muted, modifier = Modifier.padding(start = 12.dp))
                }
                players.isEmpty() -> Text(
                    "No other device is open right now. Open Roxy on the TV and it appears here; it has to be signed in to the same login.",
                    fontSize = 13.sp, lineHeight = 18.sp, color = RoxyColors.muted, modifier = Modifier.padding(horizontal = PhonePad, vertical = 12.dp),
                )
            }
            Spacer(Modifier.height(4.dp))
            Text("Scan again", fontSize = 13.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.accent, modifier = Modifier.padding(horizontal = PhonePad).clickable { scope.launch { loading = true; remote.refreshPlayers(); loading = false } }.padding(vertical = 8.dp))
        }
    }
}

@Composable
private fun DeviceRow(icon: ImageVector, name: String, sub: String?, selected: Boolean, onClick: () -> Unit) {
    Row(modifier = Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = PhonePad, vertical = 12.dp), verticalAlignment = Alignment.CenterVertically) {
        Icon(icon, contentDescription = null, tint = if (selected) RoxyColors.accent else RoxyColors.onBackground, modifier = Modifier.size(24.dp))
        Column(modifier = Modifier.weight(1f).padding(start = 14.dp)) {
            Text(name, fontSize = 15.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground, maxLines = 1, overflow = TextOverflow.Ellipsis)
            sub?.let { Text(it, fontSize = 12.sp, color = RoxyColors.muted, maxLines = 1, overflow = TextOverflow.Ellipsis) }
        }
        if (selected) Icon(Icons.Filled.Check, contentDescription = "Selected", tint = RoxyColors.accent, modifier = Modifier.size(20.dp))
    }
}

/**
 * The remote, above the bottom bar while a target is set: "Playing on Living room TV",
 * what it plays, and -10 / pause / +10 / next; the X disconnects and leaves the TV
 * playing. Netflix's and Plex's cast bars are the model.
 */
@Composable
fun RemoteBar() {
    val remote = RoxyApp.get(LocalContext.current).remote
    val target by remote.target.collectAsStateWithLifecycle()
    val t = target ?: return
    val scope = rememberCoroutineScope()
    val now = t.nowPlaying
    fun send(type: String, seconds: Int = 0) { scope.launch { remote.send(RemoteCommand(type = type, seconds = seconds)) } }
    Column(modifier = Modifier.fillMaxWidth().background(RoxyColors.surfaceHigh)) {
        Row(modifier = Modifier.fillMaxWidth().padding(start = PhonePad, end = 4.dp, top = 8.dp, bottom = 8.dp), verticalAlignment = Alignment.CenterVertically) {
            Icon(Icons.Filled.CastConnected, contentDescription = null, tint = RoxyColors.accent, modifier = Modifier.size(22.dp))
            Column(modifier = Modifier.weight(1f).padding(start = 12.dp, end = 8.dp)) {
                Text("On ${t.name}", fontSize = 13.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text(now?.title?.ifEmpty { null } ?: if (now != null) "Playing" else "Nothing playing yet. Pick something and it plays there.", fontSize = 11.sp, color = RoxyColors.muted, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            if (now != null) {
                BarButton(Icons.Filled.Replay10, "Back 10 seconds") { send("seekBy", -10) }
                BarButton(if (now.paused) Icons.Filled.PlayArrow else Icons.Filled.Pause, if (now.paused) "Play" else "Pause") { send("toggle") }
                BarButton(Icons.Filled.Forward10, "Forward 10 seconds") { send("seekBy", 10) }
                BarButton(Icons.Filled.SkipNext, "Next episode") { send("next") }
            }
            BarButton(Icons.Filled.Close, "Stop controlling", tint = RoxyColors.muted) { remote.select(null) }
        }
    }
}

@Composable
private fun BarButton(icon: ImageVector, description: String, tint: Color = Color.White, onClick: () -> Unit) {
    Box(modifier = Modifier.size(40.dp).clip(CircleShape).clickable(onClick = onClick), contentAlignment = Alignment.Center) {
        Icon(icon, contentDescription = description, tint = tint, modifier = Modifier.size(24.dp))
    }
}
