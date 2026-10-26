package tv.roxy.tv.ui.login

import android.os.Build
import android.provider.Settings
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import tv.roxy.tv.ui.phone.WitchHat
import tv.roxy.tv.ui.phone.PlayButton
import tv.roxy.tv.ui.phone.OnBrand
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.draw.clip
import androidx.compose.material3.Icon
import androidx.compose.material.icons.outlined.VisibilityOff
import androidx.compose.material.icons.outlined.Visibility
import androidx.compose.material.icons.outlined.Person
import androidx.compose.material.icons.outlined.Lock
import androidx.compose.material.icons.outlined.Dns
import androidx.compose.material.icons.automirrored.filled.Login
import androidx.compose.material.icons.Icons
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.interaction.collectIsFocusedAsState
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.border
import androidx.compose.foundation.background
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.runtime.remember
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.material3.TextButton
import androidx.compose.material3.CheckboxDefaults
import androidx.compose.material3.Checkbox
import androidx.compose.material3.AlertDialog
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.clickable
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.tv.material3.MaterialTheme
import androidx.tv.material3.Text
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import tv.roxy.tv.data.FoundServer
import tv.roxy.tv.data.ServerFinder
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.catch
import kotlinx.coroutines.flow.update
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material.icons.filled.Check
import timber.log.Timber
import tv.roxy.tv.BuildConfig
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.data.Session
import tv.roxy.tv.ui.components.PrimaryButton
import tv.roxy.tv.ui.components.RoxyTextField
import tv.roxy.tv.ui.theme.RoxyColors

data class LoginUiState(
    val busy: Boolean = false,
    val error: String? = null,
    /** Servers found on the network so far, in the order they answered. */
    val servers: List<FoundServer> = emptyList(),
    val scanning: Boolean = false,
)

class LoginViewModel(private val app: RoxyApp) : ViewModel() {
    private val _state = MutableStateFlow(LoginUiState())
    val state: StateFlow<LoginUiState> = _state
    private var scanJob: Job? = null

    init { scan() }

    /** Look for servers on the local network (the beacon's broadcast plus an HTTP sweep); rows appear as they answer. */
    fun scan() {
        scanJob?.cancel()
        scanJob = viewModelScope.launch {
            _state.update { it.copy(scanning = true, servers = emptyList()) }
            ServerFinder.find(app)
                .catch { Timber.w(it, "server scan") }
                .collect { found -> _state.update { it.copy(servers = it.servers + found) } }
            _state.update { it.copy(scanning = false) }
        }
    }

    fun login(serverUrl: String, username: String, password: String) {
        val url = normalise(serverUrl)
        if (url == null) { _state.update { it.copy(error = "Enter the server URL, e.g. http://192.168.1.10:8096") }; return }
        if (username.isBlank()) { _state.update { it.copy(error = "Enter a user name") }; return }
        _state.update { it.copy(busy = true, error = null) }
        viewModelScope.launch {
            val deviceId = Settings.Secure.getString(app.contentResolver, Settings.Secure.ANDROID_ID) ?: "unknown"
            runCatching {
                app.api.login(url, username.trim(), password, device = "${Build.MANUFACTURER} ${Build.MODEL}", deviceId = deviceId, version = BuildConfig.VERSION_NAME)
            }.onSuccess { resp ->
                Timber.i("Logged in to %s as %s", url, resp.user.name)
                // "I agree" was ticked to sign in: record it against the current terms version.
                val version = runCatching { app.api.legalAt(url, "terms").version }.getOrDefault("")
                app.prefs.saveSession(Session(serverUrl = url, token = resp.token, userName = resp.user.name, isAdmin = resp.user.isAdmin, acceptedTerms = version))
                _state.update { it.copy(busy = false) }
            }.onFailure { t ->
                Timber.w(t, "login failed")
                _state.update { it.copy(busy = false, error = friendly(t)) }
            }
        }
    }

    private fun normalise(raw: String): String? {
        var s = raw.trim().trimEnd('/')
        if (s.isEmpty()) return null
        if (!s.startsWith("http://") && !s.startsWith("https://")) s = "http://$s"
        return s
    }

    private fun friendly(t: Throwable): String = when {
        t is tv.roxy.tv.api.ApiException && t.code == 401 -> "Wrong user name or password"
        t is tv.roxy.tv.api.ApiException -> "Server error: HTTP ${t.code}"
        else -> "Cannot reach the server: ${t.message ?: t::class.simpleName}"
    }
}

@Composable
fun LoginScreen() {
    val context = LocalContext.current
    val app = RoxyApp.get(context)
    val vm: LoginViewModel = viewModel { LoginViewModel(app) }
    val state by vm.state.collectAsStateWithLifecycle()

    val savedUrl = app.prefs.session.value.serverUrl
    var serverUrl by rememberSaveable { mutableStateOf(savedUrl.ifEmpty { BuildConfig.DEFAULT_SERVER_URL }) }
    var username by rememberSaveable { mutableStateOf(BuildConfig.DEFAULT_USERNAME) }
    var password by rememberSaveable { mutableStateOf(BuildConfig.DEFAULT_PASSWORD) }
    var showPassword by rememberSaveable { mutableStateOf(false) }
    // Signing in means agreeing to the terms. The text lives on the server, so the link
    // fetches it from the URL typed above; acceptance is recorded once logged in.
    var agreed by rememberSaveable { mutableStateOf(false) }
    var showTerms by rememberSaveable { mutableStateOf(false) }
    val canConnect = !state.busy && agreed && serverUrl.isNotBlank() && username.isNotBlank()

    // A brand-new server (no accounts yet) gets the setup wizard instead of a sign-in form.
    var setup by remember { mutableStateOf<tv.roxy.tv.api.SetupStatus?>(null) }
    var openSetup by rememberSaveable { mutableStateOf(false) }
    LaunchedEffect(serverUrl) {
        setup = null
        val url = serverUrl.trim().trimEnd('/').let { if (it.isEmpty() || it.startsWith("http")) it else "http://$it" }
        if (url.isEmpty()) return@LaunchedEffect
        kotlinx.coroutines.delay(500) // typing: wait for a pause
        setup = runCatching { app.api.setupStatus(url) }.getOrNull()
    }
    val needsSetup = setup?.required == true
    if (openSetup && needsSetup && setup?.allowed == true) {
        val url = serverUrl.trim().trimEnd('/').let { if (it.startsWith("http")) it else "http://$it" }
        tv.roxy.tv.ui.setup.SetupWizard(
            serverUrl = url,
            serverName = setup?.serverName?.ifEmpty { null } ?: url.removePrefix("http://"),
            onCancel = { openSetup = false },
        )
        return
    }

    Box(modifier = Modifier.fillMaxSize().background(RoxyColors.background)) {
        // A soft blue glow behind the mark, like the splash, fading into the page.
        Box(
            modifier = Modifier.fillMaxWidth().height(420.dp).align(Alignment.TopCenter)
                .background(Brush.radialGradient(listOf(RoxyColors.accent.copy(alpha = 0.22f), Color.Transparent), radius = 900f)),
        )
        Column(
            // 440dp on a TV; the full width (minus padding) on a phone in portrait.
            modifier = Modifier.align(Alignment.Center).widthIn(max = 440.dp).fillMaxWidth().statusBarsPadding().navigationBarsPadding().imePadding()
                .verticalScroll(rememberScrollState()).padding(horizontal = 28.dp, vertical = 24.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            WitchHat(size = 84.dp, modifier = Modifier.padding(top = 8.dp))
            Text("Roxy TV", fontSize = 30.sp, fontWeight = FontWeight.ExtraBold, letterSpacing = 1.5.sp, color = RoxyColors.onBackground, modifier = Modifier.padding(top = 14.dp))
            Text("Sign in to your server", fontSize = 14.sp, color = RoxyColors.muted, modifier = Modifier.padding(top = 4.dp, bottom = 32.dp))

            LoginField(serverUrl, { serverUrl = it }, placeholder = "Server address", icon = Icons.Outlined.Dns, keyboardType = KeyboardType.Uri)
            FoundServers(state, selectedUrl = serverUrl, onPick = { serverUrl = it.url }, onRescan = vm::scan)
            // A brand-new server: offer setup instead of a sign-in it can't answer yet.
            if (needsSetup && setup?.allowed == true) {
                Column(
                    modifier = Modifier.fillMaxWidth().padding(top = 14.dp).clip(RoundedCornerShape(16.dp))
                        .background(RoxyColors.accent.copy(alpha = 0.12f)).border(1.dp, RoxyColors.accent.copy(alpha = 0.4f), RoundedCornerShape(16.dp)).padding(16.dp),
                ) {
                    Text("This server is new", fontSize = 16.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground)
                    Text(
                        "${setup?.serverName?.ifEmpty { null } ?: "It"} has no accounts yet. Set it up to create yours and choose where your media lives.",
                        fontSize = 13.sp, lineHeight = 18.sp, color = RoxyColors.muted, modifier = Modifier.padding(top = 4.dp, bottom = 12.dp),
                    )
                    PlayButton(
                        text = "Set up this server", icon = Icons.AutoMirrored.Filled.Login, height = 46.dp, pill = true,
                        container = RoxyColors.accent, content = OnBrand, modifier = Modifier.fillMaxWidth(), onClick = { openSetup = true },
                    )
                }
            }
            // No accounts exist on a new server, so there's nothing to sign in to yet.
            if (!needsSetup) {
                Spacer(Modifier.height(12.dp))
                LoginField(username, { username = it }, placeholder = "User name", icon = Icons.Outlined.Person)
                Spacer(Modifier.height(12.dp))
                LoginField(
                    password, { password = it }, placeholder = "Password", icon = Icons.Outlined.Lock,
                    keyboardType = KeyboardType.Password, imeAction = ImeAction.Done,
                    visualTransformation = if (showPassword) VisualTransformation.None else PasswordVisualTransformation(),
                    trailing = {
                        Icon(
                            if (showPassword) Icons.Outlined.VisibilityOff else Icons.Outlined.Visibility, contentDescription = if (showPassword) "Hide password" else "Show password",
                            tint = RoxyColors.muted, modifier = Modifier.size(22.dp).clickable { showPassword = !showPassword },
                        )
                    },
                    onDone = { if (canConnect) vm.login(serverUrl, username, password) },
                )

                Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.fillMaxWidth().padding(top = 14.dp, bottom = 6.dp)) {
                    Checkbox(checked = agreed, onCheckedChange = { agreed = it }, colors = CheckboxDefaults.colors(checkedColor = RoxyColors.accent, uncheckedColor = RoxyColors.muted, checkmarkColor = OnBrand))
                    Text(
                        buildAnnotatedString {
                            append("I agree to the ")
                            withStyle(SpanStyle(color = RoxyColors.accent, fontWeight = FontWeight.SemiBold)) { append("Terms of Use") }
                        },
                        color = RoxyColors.onBackground, fontSize = 14.sp,
                        modifier = Modifier.clickable { showTerms = true },
                    )
                }
                PlayButton(
                    text = if (state.busy) "Connecting…" else "Connect",
                    icon = Icons.AutoMirrored.Filled.Login, height = 50.dp, pill = true,
                    container = if (canConnect) RoxyColors.accent else RoxyColors.accent.copy(alpha = 0.35f),
                    content = if (canConnect) OnBrand else OnBrand.copy(alpha = 0.6f),
                    modifier = Modifier.fillMaxWidth().padding(top = 8.dp),
                    onClick = { if (canConnect) vm.login(serverUrl, username, password) },
                )
                state.error?.let {
                    Text(it, color = RoxyColors.error, fontSize = 13.sp, textAlign = TextAlign.Center, modifier = Modifier.padding(top = 14.dp))
                }
            }
            if (needsSetup && setup?.allowed != true) Text(
                "This server hasn't been set up yet. Open Roxy on a phone connected to the same network as the server to set it up.",
                color = RoxyColors.accent, fontSize = 13.sp, lineHeight = 18.sp, textAlign = TextAlign.Center, modifier = Modifier.padding(top = 14.dp),
            )
            Text(
                "Your server's address is where Roxy Server runs, like http://192.168.1.10:8096 or your Tailscale name.",
                color = RoxyColors.muted.copy(alpha = 0.8f), fontSize = 12.sp, lineHeight = 16.sp, textAlign = TextAlign.Center, modifier = Modifier.padding(top = 28.dp),
            )
            if (showTerms) {
                LoginTermsDialog(serverUrl = serverUrl, onDismiss = { showTerms = false }, onAgree = { agreed = true; showTerms = false })
            }
        }
    }
}

/**
 * "On your network": the servers the scan found, one row each, tap to put its address
 * in the field (Jellyfin's and Plex's login screens do the same). While scanning with
 * nothing found yet it says so with a small spinner; after a scan with nothing found,
 * a line and "Scan again".
 */
@Composable
private fun FoundServers(state: LoginUiState, selectedUrl: String, onPick: (FoundServer) -> Unit, onRescan: () -> Unit) {
    val picked = selectedUrl.trim().trimEnd('/').removePrefix("http://")
    Column(modifier = Modifier.fillMaxWidth().padding(top = 10.dp)) {
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
            else Text("Scan again", fontSize = 12.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.accent, modifier = Modifier.clickable(onClick = onRescan).padding(4.dp))
        }
        state.servers.forEach { s ->
            val interaction = remember { MutableInteractionSource() }
            val focused by interaction.collectIsFocusedAsState()
            val selected = s.url.removePrefix("http://") == picked
            Row(
                modifier = Modifier.fillMaxWidth().padding(top = 8.dp).height(54.dp).clip(RoundedCornerShape(14.dp)).background(RoxyColors.surfaceHigh)
                    .border(1.5.dp, if (focused) RoxyColors.accent else Color.Transparent, RoundedCornerShape(14.dp))
                    .clickable(interactionSource = interaction, indication = null) { onPick(s) }.padding(horizontal = 16.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Icon(Icons.Outlined.Dns, contentDescription = null, tint = RoxyColors.accent, modifier = Modifier.size(22.dp))
                Column(modifier = Modifier.weight(1f).padding(start = 12.dp)) {
                    Text(s.name, fontSize = 15.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground, maxLines = 1)
                    Text(s.url.removePrefix("http://"), fontSize = 12.sp, color = RoxyColors.muted, maxLines = 1)
                }
                if (selected) Icon(Icons.Filled.Check, contentDescription = "Selected", tint = RoxyColors.accent, modifier = Modifier.size(20.dp))
            }
        }
    }
}

/** Rounded dark field with a leading icon and a brand-blue ring on focus; works by touch and D-pad. */
@Composable
private fun LoginField(
    value: String,
    onValueChange: (String) -> Unit,
    placeholder: String,
    icon: ImageVector,
    keyboardType: KeyboardType = KeyboardType.Text,
    imeAction: ImeAction = ImeAction.Next,
    visualTransformation: VisualTransformation = VisualTransformation.None,
    trailing: (@Composable () -> Unit)? = null,
    onDone: () -> Unit = {},
) {
    val interaction = remember { MutableInteractionSource() }
    val focused by interaction.collectIsFocusedAsState()
    Row(
        modifier = Modifier.fillMaxWidth().height(54.dp).clip(RoundedCornerShape(14.dp)).background(RoxyColors.surfaceHigh)
            .border(1.5.dp, if (focused) RoxyColors.accent else Color.Transparent, RoundedCornerShape(14.dp)).padding(horizontal = 16.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(icon, contentDescription = null, tint = if (focused) RoxyColors.accent else RoxyColors.muted, modifier = Modifier.size(22.dp))
        Box(modifier = Modifier.weight(1f).padding(start = 12.dp)) {
            if (value.isEmpty()) Text(placeholder, color = RoxyColors.muted, fontSize = 16.sp)
            BasicTextField(
                value = value, onValueChange = onValueChange, singleLine = true, interactionSource = interaction,
                textStyle = TextStyle(color = RoxyColors.onBackground, fontSize = 16.sp),
                cursorBrush = SolidColor(RoxyColors.accent),
                visualTransformation = visualTransformation,
                keyboardOptions = KeyboardOptions(keyboardType = keyboardType, imeAction = imeAction),
                keyboardActions = KeyboardActions(onDone = { onDone() }),
                modifier = Modifier.fillMaxWidth(),
            )
        }
        trailing?.invoke()
    }
}

/** The Terms of Use fetched from the typed server URL (no login needed), in a dialog with Agree. */
@Composable
private fun LoginTermsDialog(serverUrl: String, onDismiss: () -> Unit, onAgree: () -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    var body by remember { mutableStateOf<String?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(serverUrl) {
        runCatching { app.api.legalAt(serverUrl, "terms") }.onSuccess { body = it.body }.onFailure { error = "Could not load the terms from that server: ${it.message}" }
    }
    AlertDialog(
        onDismissRequest = onDismiss,
        containerColor = RoxyColors.surfaceHigh, titleContentColor = RoxyColors.onBackground, textContentColor = RoxyColors.onBackground,
        title = { Text("Terms of Use") },
        text = {
            Column(modifier = Modifier.verticalScroll(rememberScrollState())) {
                val b = body
                val e = error
                when {
                    b != null -> Text(b.replace("**", ""), fontSize = 13.sp, lineHeight = 19.sp)
                    e != null -> Text(e, color = RoxyColors.error, fontSize = 13.sp)
                    else -> Text("Loading the terms from the server…", color = RoxyColors.muted, fontSize = 13.sp)
                }
            }
        },
        confirmButton = { TextButton(onClick = onAgree, enabled = body != null) { Text("I agree", color = RoxyColors.accent) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Close", color = RoxyColors.muted) } },
    )
}
