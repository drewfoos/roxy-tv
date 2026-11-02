package tv.roxy.tv.ui.login

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
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
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.Smartphone
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.tv.material3.Text
import coil3.compose.AsyncImage
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.data.FoundServer
import tv.roxy.tv.ui.phone.OnBrand
import tv.roxy.tv.ui.phone.WitchHat
import tv.roxy.tv.ui.theme.RoxyColors
import tv.roxy.tv.ui.tv.TvFocusable
import tv.roxy.tv.ui.tv.TvTextField

/*
 * Sign-in on a TV, as on the web: the library's posters as a dimmed wall, "Roxy TV" over
 * one card, and under it which server this is with "Different server?". The card opens on
 * the phone code (typing a password with a remote is slow; TVs from Netflix to YouTube do
 * the same), with the user name and password one press away. Unlike the web, the TV finds
 * servers on the network by itself: with one found and none saved it is picked at once;
 * otherwise the server card lists them, with Scan again and a field to type an address.
 */

private val CardWidth = 620.dp
private const val WallColumns = 8

@Composable
fun TvLogin(
    state: LoginUiState,
    serverUrl: String, onServerUrl: (String) -> Unit,
    serverName: String?, serverReachable: Boolean?,
    username: String, onUsername: (String) -> Unit,
    password: String, onPassword: (String) -> Unit,
    agreed: Boolean, onAgreed: (Boolean) -> Unit,
    canConnect: Boolean, onConnect: () -> Unit,
    needsSetup: Boolean, setupAllowed: Boolean, setupName: String?, onOpenSetup: () -> Unit,
    onRescan: () -> Unit, onShowTerms: () -> Unit,
) {
    // Which card: the server picker while there is no server (or on "Different server?"),
    // else sign-in, by phone code first.
    var choosingServer by rememberSaveable { mutableStateOf(serverUrl.isBlank()) }
    var byPassword by rememberSaveable { mutableStateOf(false) }
    // None saved and exactly one server answering: that is the one.
    LaunchedEffect(state.servers, state.scanning) {
        if (serverUrl.isBlank() && !state.scanning && state.servers.size == 1) {
            onServerUrl(state.servers.first().url)
            choosingServer = false
        }
    }
    val showServers = choosingServer || serverUrl.isBlank()
    val pickServer = { url: String -> onServerUrl(url); choosingServer = false }

    Box(modifier = Modifier.fillMaxSize().background(RoxyColors.background)) {
        if (serverReachable == true) PosterWall(serverUrl)
        // The wall sits under a dark veil, darkest in the middle where the card is.
        Box(modifier = Modifier.fillMaxSize().background(Brush.radialGradient(0f to RoxyColors.background.copy(alpha = 0.93f), 1f to RoxyColors.background.copy(alpha = 0.72f), radius = 1400f)))
        // Centred as a block: the mark, the card, the server line, with room to breathe.
        Column(modifier = Modifier.fillMaxSize().padding(vertical = 16.dp), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.Center) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                WitchHat(size = 44.dp)
                Text("Roxy TV", fontSize = 30.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(start = 12.dp))
            }
            Spacer(Modifier.height(22.dp))
            Column(
                modifier = Modifier.width(CardWidth).clip(RoundedCornerShape(18.dp)).background(RoxyColors.surface.copy(alpha = 0.95f))
                    .border(1.dp, Color.White.copy(alpha = 0.08f), RoundedCornerShape(18.dp)).padding(horizontal = 36.dp, vertical = 30.dp),
            ) {
                when {
                    showServers -> ServerCard(state, serverUrl, onPick = pickServer, onRescan = onRescan, canGoBack = serverUrl.isNotBlank(), onBack = { choosingServer = false })
                    needsSetup -> SetupCard(setupAllowed, setupName, onOpenSetup)
                    serverReachable == false -> Unreachable(serverUrl, onRescan = { onRescan(); choosingServer = true })
                    byPassword -> PasswordCard(state, username, onUsername, password, onPassword, agreed, onAgreed, canConnect, onConnect, onShowTerms, onUsePhone = { byPassword = false })
                    else -> PhoneCard(state, serverUrl, onUsePassword = { byPassword = true }, onShowTerms = onShowTerms)
                }
            }
            if (!showServers) {
                Spacer(Modifier.height(20.dp))
                ServerLine(serverName, serverUrl, onChange = { choosingServer = true })
            }
        }
    }
}

/** The posters of the server being signed in to, as a wall behind everything (the web's sign-in). */
@Composable
private fun PosterWall(serverUrl: String) {
    val api = RoxyApp.get(LocalContext.current).api
    var count by remember(serverUrl) { mutableStateOf(0) }
    LaunchedEffect(serverUrl) { count = runCatching { api.showcaseCountAt(serverUrl) }.getOrDefault(0) }
    if (count == 0) return
    val base = serverUrl.trim().trimEnd('/').let { if (it.startsWith("http")) it else "http://$it" }
    Column(modifier = Modifier.fillMaxSize()) {
        repeat(3) { row ->
            Row(modifier = Modifier.fillMaxWidth()) {
                repeat(WallColumns) { col ->
                    val n = (row * WallColumns + col) % count
                    AsyncImage(
                        model = "$base/v1/showcase/$n", contentDescription = null, contentScale = ContentScale.Crop,
                        modifier = Modifier.weight(1f).aspectRatio(2f / 3f).padding(2.dp).clip(RoundedCornerShape(4.dp)),
                    )
                }
            }
        }
    }
}

@Composable
private fun CardTitle(title: String, sub: String?) {
    Text(title, fontSize = 26.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground)
    if (sub != null) Text(sub, fontSize = 15.sp, lineHeight = 21.sp, color = RoxyColors.muted, modifier = Modifier.padding(top = 6.dp))
    Spacer(Modifier.height(22.dp))
}

/** The phone way: the QR code and the six letters, approved on a phone that is signed in. */
@Composable
private fun PhoneCard(state: LoginUiState, serverUrl: String, onUsePassword: () -> Unit, onShowTerms: () -> Unit) {
    val api = RoxyApp.get(LocalContext.current).api
    val first = remember { FocusRequester() }
    LaunchedEffect(Unit) { repeat(6) { withFrameNanos { }; if (runCatching { first.requestFocus() }.getOrDefault(false)) return@LaunchedEffect } }
    // The QR at the left, the rest at the right: big enough to scan from across the room,
    // and the card stays short enough for one screen.
    val link = state.link
    Row(verticalAlignment = Alignment.CenterVertically) {
        Box(modifier = Modifier.size(196.dp).background(Color.White, RoundedCornerShape(12.dp)).padding(10.dp), contentAlignment = Alignment.Center) {
            if (link != null) AsyncImage(model = api.linkQrUrl(state.linkServer, link), contentDescription = "Sign-in QR code", modifier = Modifier.fillMaxSize())
            else CircularProgressIndicator(color = RoxyColors.accent, strokeWidth = 3.dp, modifier = Modifier.size(36.dp))
        }
        Column(modifier = Modifier.padding(start = 30.dp).weight(1f)) {
            Text("Sign in with your phone", fontSize = 26.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground)
            Text("Scan the code with a phone signed in to Roxy, then approve it there.", fontSize = 15.sp, lineHeight = 21.sp, color = RoxyColors.muted, modifier = Modifier.padding(top = 6.dp))
            Spacer(Modifier.height(16.dp))
            Text(
                "Or go to " + (link?.url ?: "$serverUrl/link").removePrefix("http://").removePrefix("https://").substringBeforeLast("/").let { "$it/link" }.replace("/link/link", "/link"),
                fontSize = 15.sp, color = RoxyColors.onBackground, maxLines = 1, overflow = TextOverflow.Ellipsis,
            )
            Text(link?.code?.chunked(3)?.joinToString(" ") ?: "••• •••", fontSize = 40.sp, fontWeight = FontWeight.ExtraBold, letterSpacing = 5.sp, color = RoxyColors.accent, modifier = Modifier.padding(top = 2.dp))
            state.linkError?.let { Text(it, fontSize = 13.sp, color = RoxyColors.error, modifier = Modifier.padding(top = 4.dp)) }
        }
    }
    Spacer(Modifier.height(26.dp))
    CardButton("Use user name and password", icon = Icons.Outlined.Person, primary = false, onClick = onUsePassword, modifier = Modifier.focusRequester(first))
    Spacer(Modifier.height(14.dp))
    TermsLine(onShowTerms)
}

/** The form: user name, password, the terms, Sign in; and back to the phone code. */
@Composable
private fun PasswordCard(
    state: LoginUiState, username: String, onUsername: (String) -> Unit, password: String, onPassword: (String) -> Unit,
    agreed: Boolean, onAgreed: (Boolean) -> Unit, canConnect: Boolean, onConnect: () -> Unit, onShowTerms: () -> Unit, onUsePhone: () -> Unit,
) {
    val first = remember { FocusRequester() }
    LaunchedEffect(Unit) { repeat(6) { withFrameNanos { }; if (runCatching { first.requestFocus() }.getOrDefault(false)) return@LaunchedEffect } }
    CardTitle("Sign in", null)
    Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
        TvTextField(username, onUsername, placeholder = "User name", icon = Icons.Outlined.Person, modifier = Modifier.focusRequester(first))
        TvTextField(password, onPassword, placeholder = "Password", icon = Icons.Outlined.Lock, keyboardType = KeyboardType.Password, imeAction = ImeAction.Done, password = true, onDone = { if (canConnect) onConnect() })
        // "I agree to the Terms of Use": OK on the box ticks it; Right goes to the blue
        // "Terms of Use", and OK there opens them (no separate button, as on the web).
        Row(verticalAlignment = Alignment.CenterVertically) {
            TvFocusable(onClick = { onAgreed(!agreed) }, shape = RoundedCornerShape(8.dp), focusedBackground = RoxyColors.surfaceHigh) { focused ->
                Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.padding(horizontal = 8.dp, vertical = 8.dp)) {
                    Box(
                        modifier = Modifier.size(22.dp).background(if (agreed) RoxyColors.accent else Color.Transparent, RoundedCornerShape(5.dp))
                            .border(2.dp, if (focused || agreed) RoxyColors.accent else RoxyColors.muted, RoundedCornerShape(5.dp)),
                        contentAlignment = Alignment.Center,
                    ) { if (agreed) Icon(Icons.Filled.Check, contentDescription = null, tint = OnBrand, modifier = Modifier.size(16.dp)) }
                    Text("I agree to the", color = RoxyColors.onBackground, fontSize = 14.sp, modifier = Modifier.padding(start = 12.dp))
                }
            }
            TvFocusable(onClick = onShowTerms, shape = RoundedCornerShape(6.dp), focusedBackground = RoxyColors.accent) { focused ->
                Text("Terms of Use", fontSize = 14.sp, fontWeight = FontWeight.SemiBold, color = if (focused) OnBrand else RoxyColors.accent, modifier = Modifier.padding(horizontal = 6.dp, vertical = 4.dp))
            }
        }
        Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            CardButton(if (state.busy) "Signing in…" else "Sign in", primary = true, enabled = canConnect, onClick = { if (canConnect) onConnect() }, modifier = Modifier.weight(1f))
            CardButton("Use your phone", icon = Icons.Outlined.Smartphone, primary = false, onClick = onUsePhone, modifier = Modifier.weight(1f))
        }
        state.error?.let { Text(it, color = RoxyColors.error, fontSize = 14.sp) }
        if (state.error == null && !agreed && username.isNotBlank()) Text("Tick the box to agree to the Terms of Use first.", color = RoxyColors.muted, fontSize = 13.sp)
    }
}

/** Pick the server: what the network scan found, Scan again, or type an address. */
@Composable
private fun ServerCard(state: LoginUiState, serverUrl: String, onPick: (String) -> Unit, onRescan: () -> Unit, canGoBack: Boolean, onBack: () -> Unit) {
    var typed by rememberSaveable { mutableStateOf(serverUrl) }
    val first = remember { FocusRequester() }
    LaunchedEffect(state.servers.isNotEmpty()) { repeat(6) { withFrameNanos { }; if (runCatching { first.requestFocus() }.getOrDefault(false)) return@LaunchedEffect } }
    CardTitle(
        "Choose your server",
        when {
            state.servers.isNotEmpty() -> "Found on your network. Pick one, or type its address below."
            state.scanning -> "Looking for Roxy servers on your network…"
            else -> "No servers answered on this network. Type the address, like 192.168.1.10:8097, or a Tailscale name."
        },
    )
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        state.servers.take(4).forEachIndexed { i, s -> ServerRow(s, selected = s.url.trimEnd('/') == serverUrl.trim().trimEnd('/'), onClick = { onPick(s.url) }, modifier = if (i == 0) Modifier.focusRequester(first) else Modifier) }
        Row(verticalAlignment = Alignment.CenterVertically) {
            TvTextField(typed, { typed = it }, placeholder = "Server address", icon = Icons.Outlined.Dns, keyboardType = KeyboardType.Uri, imeAction = ImeAction.Done,
                onDone = { if (typed.isNotBlank()) onPick(typed.trim()) },
                modifier = Modifier.weight(1f).then(if (state.servers.isEmpty()) Modifier.focusRequester(first) else Modifier))
            Spacer(Modifier.width(10.dp))
            CardButton("Connect", primary = true, enabled = typed.isNotBlank(), onClick = { if (typed.isNotBlank()) onPick(typed.trim()) }, modifier = Modifier.width(120.dp))
        }
        Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            CardButton(if (state.scanning) "Scanning…" else "Scan again", icon = Icons.Outlined.Refresh, primary = false, enabled = !state.scanning, onClick = onRescan, modifier = Modifier.weight(1f))
            if (canGoBack) CardButton("Back", primary = false, onClick = onBack, modifier = Modifier.weight(1f))
        }
    }
}

@Composable
private fun ServerRow(s: FoundServer, selected: Boolean, onClick: () -> Unit, modifier: Modifier = Modifier) {
    val shape = RoundedCornerShape(12.dp)
    TvFocusable(onClick = onClick, shape = shape, background = RoxyColors.surfaceHigh, focusedBackground = RoxyColors.surfaceHigh, modifier = modifier.fillMaxWidth()) { focused ->
        Row(modifier = Modifier.fillMaxWidth().height(56.dp).border(3.dp, if (focused) RoxyColors.accent else Color.Transparent, shape).padding(horizontal = 16.dp), verticalAlignment = Alignment.CenterVertically) {
            Icon(Icons.Outlined.Dns, contentDescription = null, tint = RoxyColors.accent, modifier = Modifier.size(22.dp))
            Column(modifier = Modifier.weight(1f).padding(start = 14.dp)) {
                Text(s.name, fontSize = 16.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground, maxLines = 1)
                Text(s.url.removePrefix("http://"), fontSize = 12.sp, color = RoxyColors.muted, maxLines = 1)
            }
            if (selected) Icon(Icons.Filled.Check, contentDescription = "Selected", tint = RoxyColors.accent, modifier = Modifier.size(20.dp))
        }
    }
}

@Composable
private fun SetupCard(setupAllowed: Boolean, setupName: String?, onOpenSetup: () -> Unit) {
    CardTitle("This server is new", "${setupName ?: "It"} has no accounts yet. Setting it up creates yours and chooses where your media lives.")
    if (setupAllowed) CardButton("Set up this server", primary = true, onClick = onOpenSetup)
    else Text("Open Roxy TV on a phone on the same network as the server to set it up.", color = RoxyColors.accent, fontSize = 14.sp, lineHeight = 19.sp)
}

@Composable
private fun Unreachable(serverUrl: String, onRescan: () -> Unit) {
    CardTitle("Can't reach the server", "Nothing answered at ${serverUrl.removePrefix("http://")}. Check the server is running and this TV is on the same network.")
    CardButton("Choose another server", icon = Icons.Outlined.Refresh, primary = true, onClick = onRescan)
}

/** "Signing in to pookiebox · 192.168.1.125:8097 · Different server?" under the card. */
@Composable
private fun ServerLine(name: String?, url: String, onChange: () -> Unit) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        Text("Signing in to ", fontSize = 15.sp, color = RoxyColors.muted)
        Text(name?.ifBlank { null } ?: "your server", fontSize = 15.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground)
        Text("  ·  ${url.removePrefix("http://").removePrefix("https://")}  ·  ", fontSize = 15.sp, color = RoxyColors.muted)
        TvFocusable(onClick = onChange, shape = RoundedCornerShape(6.dp), focusedBackground = RoxyColors.accent) { focused ->
            Text("Different server?", fontSize = 15.sp, fontWeight = FontWeight.SemiBold, color = if (focused) OnBrand else RoxyColors.accent, modifier = Modifier.padding(horizontal = 8.dp, vertical = 4.dp))
        }
    }
}

@Composable
private fun TermsLine(onShowTerms: () -> Unit) {
    Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.fillMaxWidth()) {
        Text("By signing in you agree to the", fontSize = 14.sp, color = RoxyColors.muted)
        TvFocusable(onClick = onShowTerms, shape = RoundedCornerShape(6.dp), focusedBackground = RoxyColors.accent) { focused ->
            Text("Terms of Use", fontSize = 14.sp, fontWeight = FontWeight.SemiBold, color = if (focused) OnBrand else RoxyColors.accent, modifier = Modifier.padding(horizontal = 6.dp, vertical = 3.dp))
        }
    }
}

/** A card's button: the accent pill (primary) or an outlined one; the focused one is filled with a white ring. */
@Composable
private fun CardButton(text: String, primary: Boolean, onClick: () -> Unit, modifier: Modifier = Modifier, enabled: Boolean = true, icon: androidx.compose.ui.graphics.vector.ImageVector? = null) {
    val shape = RoundedCornerShape(24.dp)
    TvFocusable(onClick = onClick, shape = shape, modifier = modifier.fillMaxWidth()) { focused ->
        val bg = when {
            primary && enabled -> RoxyColors.accent
            primary -> RoxyColors.accent.copy(alpha = 0.35f)
            focused -> RoxyColors.accent
            else -> Color.Transparent
        }
        val fg = when {
            primary || focused -> if (enabled) OnBrand else OnBrand.copy(alpha = 0.6f)
            else -> RoxyColors.onBackground
        }
        val ring = when {
            focused && primary -> Color.White
            primary -> Color.Transparent
            focused -> RoxyColors.accent
            else -> Color.White.copy(alpha = 0.25f)
        }
        Row(
            modifier = Modifier.fillMaxWidth().height(52.dp).background(bg, shape).border(if (focused) 3.dp else 1.5.dp, ring, shape).padding(horizontal = 14.dp),
            verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.Center,
        ) {
            if (icon != null) { Icon(icon, contentDescription = null, tint = fg, modifier = Modifier.size(20.dp)); Spacer(Modifier.width(8.dp)) }
            Text(text, color = fg, fontSize = 16.sp, fontWeight = FontWeight.Bold, maxLines = 1, overflow = TextOverflow.Ellipsis, textAlign = TextAlign.Center)
        }
    }
}
