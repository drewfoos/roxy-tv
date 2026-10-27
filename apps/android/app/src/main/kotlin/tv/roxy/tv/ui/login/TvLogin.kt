package tv.roxy.tv.ui.login

import androidx.compose.foundation.background
import androidx.compose.foundation.border
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
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.outlined.Dns
import androidx.compose.material.icons.outlined.Lock
import androidx.compose.material.icons.outlined.Person
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.tv.material3.Text
import tv.roxy.tv.data.FoundServer
import tv.roxy.tv.ui.phone.OnBrand
import tv.roxy.tv.ui.phone.WitchHat
import tv.roxy.tv.ui.theme.RoxyColors
import tv.roxy.tv.ui.tv.TvFocusable
import tv.roxy.tv.ui.tv.TvTextField

/**
 * The sign-in screen on a TV: two columns so it fits one 960x540 dp screen. The mark and
 * the help text at the left, the form at the right; every field, the found servers, the
 * Terms checkbox and Connect are reachable with the D-pad (see [TvTextField] for why the
 * keyboard only opens on OK).
 */
@Composable
fun TvLogin(
    state: LoginUiState,
    serverUrl: String, onServerUrl: (String) -> Unit,
    username: String, onUsername: (String) -> Unit,
    password: String, onPassword: (String) -> Unit,
    agreed: Boolean, onAgreed: (Boolean) -> Unit,
    canConnect: Boolean, onConnect: () -> Unit,
    needsSetup: Boolean, setupAllowed: Boolean, setupName: String?, onOpenSetup: () -> Unit,
    onRescan: () -> Unit, onShowTerms: () -> Unit,
) {
    Box(modifier = Modifier.fillMaxSize().background(RoxyColors.background)) {
        Box(modifier = Modifier.size(600.dp).align(Alignment.CenterStart).background(Brush.radialGradient(listOf(RoxyColors.accent.copy(alpha = 0.18f), Color.Transparent), radius = 700f)))
        Row(modifier = Modifier.fillMaxSize().padding(horizontal = 80.dp), verticalAlignment = Alignment.CenterVertically) {
            Column(modifier = Modifier.width(320.dp).fillMaxHeight(), verticalArrangement = Arrangement.Center) {
                WitchHat(size = 72.dp)
                Text("Roxy TV", fontSize = 32.sp, fontWeight = FontWeight.ExtraBold, letterSpacing = 1.5.sp, color = RoxyColors.onBackground, modifier = Modifier.padding(top = 14.dp))
                Text("Sign in to your server", fontSize = 15.sp, color = RoxyColors.muted, modifier = Modifier.padding(top = 4.dp))
                Text(
                    "Your server's address is where Roxy Server runs, like http://192.168.1.10:8096 or your Tailscale name.",
                    color = RoxyColors.muted.copy(alpha = 0.8f), fontSize = 13.sp, lineHeight = 18.sp, modifier = Modifier.padding(top = 28.dp, end = 24.dp),
                )
                if (needsSetup && !setupAllowed) Text(
                    "This server hasn't been set up yet. Open Roxy on a phone connected to the same network as the server to set it up.",
                    color = RoxyColors.accent, fontSize = 13.sp, lineHeight = 18.sp, modifier = Modifier.padding(top = 14.dp, end = 24.dp),
                )
            }
            Spacer(Modifier.width(60.dp))
            Column(modifier = Modifier.width(420.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                TvTextField(serverUrl, onServerUrl, placeholder = "Server address", icon = Icons.Outlined.Dns, keyboardType = KeyboardType.Uri)
                TvFoundServers(state, selectedUrl = serverUrl, onPick = { onServerUrl(it.url) }, onRescan = onRescan)
                if (needsSetup && setupAllowed) {
                    Column(modifier = Modifier.fillMaxWidth().background(RoxyColors.accent.copy(alpha = 0.12f), RoundedCornerShape(12.dp)).border(1.dp, RoxyColors.accent.copy(alpha = 0.4f), RoundedCornerShape(12.dp)).padding(14.dp)) {
                        Text("This server is new", fontSize = 16.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground)
                        Text("${setupName ?: "It"} has no accounts yet. Set it up to create yours and choose where your media lives.", fontSize = 13.sp, lineHeight = 18.sp, color = RoxyColors.muted, modifier = Modifier.padding(top = 4.dp, bottom = 10.dp))
                        TvPillButton("Set up this server", enabled = true, onClick = onOpenSetup)
                    }
                }
                if (!needsSetup) {
                    TvTextField(username, onUsername, placeholder = "User name", icon = Icons.Outlined.Person)
                    TvTextField(password, onPassword, placeholder = "Password", icon = Icons.Outlined.Lock, keyboardType = KeyboardType.Password, imeAction = ImeAction.Done, password = true, onDone = { if (canConnect) onConnect() })
                    Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.fillMaxWidth()) {
                        TvFocusable(onClick = { onAgreed(!agreed) }, shape = RoundedCornerShape(8.dp), focusedBackground = RoxyColors.surfaceHigh, modifier = Modifier.weight(1f)) { focused ->
                            Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.padding(horizontal = 8.dp, vertical = 6.dp)) {
                                Box(
                                    modifier = Modifier.size(22.dp).background(if (agreed) RoxyColors.accent else Color.Transparent, RoundedCornerShape(4.dp))
                                        .border(2.dp, if (focused || agreed) RoxyColors.accent else RoxyColors.muted, RoundedCornerShape(4.dp)),
                                    contentAlignment = Alignment.Center,
                                ) { if (agreed) Icon(Icons.Filled.Check, contentDescription = null, tint = OnBrand, modifier = Modifier.size(16.dp)) }
                                Text("I agree to the Terms of Use", color = RoxyColors.onBackground, fontSize = 14.sp, modifier = Modifier.padding(start = 10.dp))
                            }
                        }
                        TvFocusable(onClick = onShowTerms, shape = RoundedCornerShape(8.dp), focusedBackground = RoxyColors.surfaceHigh) { _ ->
                            Text("Read", color = RoxyColors.accent, fontSize = 14.sp, fontWeight = FontWeight.SemiBold, modifier = Modifier.padding(horizontal = 10.dp, vertical = 6.dp))
                        }
                    }
                    TvPillButton(if (state.busy) "Connecting…" else "Connect", enabled = canConnect, onClick = { if (canConnect) onConnect() })
                    state.error?.let { Text(it, color = RoxyColors.error, fontSize = 13.sp) }
                }
            }
        }
    }
}

/** The accent pill (Connect, Set up): dimmed while it cannot be pressed, a white ring when focused. */
@Composable
private fun TvPillButton(text: String, enabled: Boolean, onClick: () -> Unit) {
    val shape = RoundedCornerShape(25.dp)
    TvFocusable(onClick = onClick, shape = shape, background = if (enabled) RoxyColors.accent else RoxyColors.accent.copy(alpha = 0.35f), focusedBackground = if (enabled) RoxyColors.accent else RoxyColors.accent.copy(alpha = 0.35f), modifier = Modifier.fillMaxWidth()) { focused ->
        Box(modifier = Modifier.fillMaxWidth().height(50.dp).border(3.dp, if (focused) Color.White else Color.Transparent, shape), contentAlignment = Alignment.Center) {
            Text(text, color = if (enabled) OnBrand else OnBrand.copy(alpha = 0.6f), fontSize = 16.sp, fontWeight = FontWeight.Bold)
        }
    }
}

/** The servers found on the network, two at most so the form keeps fitting; OK puts one in the field. */
@Composable
private fun TvFoundServers(state: LoginUiState, selectedUrl: String, onPick: (FoundServer) -> Unit, onRescan: () -> Unit) {
    val picked = selectedUrl.trim().trimEnd('/').removePrefix("http://")
    Column(modifier = Modifier.fillMaxWidth()) {
        Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.fillMaxWidth().padding(horizontal = 4.dp)) {
            Text(
                when {
                    state.servers.isNotEmpty() -> "On your network"
                    state.scanning -> "Looking for servers on your network…"
                    else -> "No servers found on this network"
                },
                fontSize = 12.sp, color = RoxyColors.muted, modifier = Modifier.weight(1f),
            )
            if (state.scanning) CircularProgressIndicator(color = RoxyColors.accent, strokeWidth = 2.dp, modifier = Modifier.size(14.dp))
            else TvFocusable(onClick = onRescan, shape = RoundedCornerShape(6.dp), focusedBackground = RoxyColors.surfaceHigh) { _ ->
                Text("Scan again", fontSize = 12.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.accent, modifier = Modifier.padding(horizontal = 8.dp, vertical = 4.dp))
            }
        }
        state.servers.take(2).forEach { s ->
            val selected = s.url.removePrefix("http://") == picked
            TvFocusable(onClick = { onPick(s) }, shape = RoundedCornerShape(12.dp), background = RoxyColors.surfaceHigh, focusedBackground = RoxyColors.surfaceHigh, modifier = Modifier.fillMaxWidth().padding(top = 6.dp)) { focused ->
                Row(
                    modifier = Modifier.fillMaxWidth().height(46.dp).border(2.dp, if (focused) RoxyColors.accent else Color.Transparent, RoundedCornerShape(12.dp)).padding(horizontal = 14.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Icon(Icons.Outlined.Dns, contentDescription = null, tint = RoxyColors.accent, modifier = Modifier.size(20.dp))
                    Column(modifier = Modifier.weight(1f).padding(start = 12.dp)) {
                        Text(s.name, fontSize = 14.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground, maxLines = 1)
                        Text(s.url.removePrefix("http://"), fontSize = 12.sp, color = RoxyColors.muted, maxLines = 1)
                    }
                    if (selected) Icon(Icons.Filled.Check, contentDescription = "Selected", tint = RoxyColors.accent, modifier = Modifier.size(18.dp))
                }
            }
        }
    }
}
