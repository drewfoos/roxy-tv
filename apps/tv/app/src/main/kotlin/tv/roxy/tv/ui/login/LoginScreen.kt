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
import timber.log.Timber
import tv.roxy.tv.BuildConfig
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.data.Session
import tv.roxy.tv.ui.components.PrimaryButton
import tv.roxy.tv.ui.components.RoxyTextField
import tv.roxy.tv.ui.theme.RoxyColors

data class LoginUiState(val busy: Boolean = false, val error: String? = null)

class LoginViewModel(private val app: RoxyApp) : ViewModel() {
    private val _state = MutableStateFlow(LoginUiState())
    val state: StateFlow<LoginUiState> = _state

    fun login(serverUrl: String, username: String, password: String) {
        val url = normalise(serverUrl)
        if (url == null) { _state.value = LoginUiState(error = "Enter the server URL, e.g. http://192.168.1.10:8096"); return }
        if (username.isBlank()) { _state.value = LoginUiState(error = "Enter a user name"); return }
        _state.value = LoginUiState(busy = true)
        viewModelScope.launch {
            val deviceId = Settings.Secure.getString(app.contentResolver, Settings.Secure.ANDROID_ID) ?: "unknown"
            runCatching {
                app.api.login(url, username.trim(), password, device = "${Build.MANUFACTURER} ${Build.MODEL}", deviceId = deviceId, version = BuildConfig.VERSION_NAME)
            }.onSuccess { resp ->
                Timber.i("Logged in to %s as %s", url, resp.user.name)
                // "I agree" was ticked to sign in: record it against the current terms version.
                val version = runCatching { app.api.legalAt(url, "terms").version }.getOrDefault("")
                app.prefs.saveSession(Session(serverUrl = url, token = resp.token, userName = resp.user.name, isAdmin = resp.user.isAdmin, acceptedTerms = version))
                _state.value = LoginUiState()
            }.onFailure { t ->
                Timber.w(t, "login failed")
                _state.value = LoginUiState(error = friendly(t))
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

    Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        Column(
            // 440dp on a TV; the full width (minus padding) on a phone in portrait.
            modifier = Modifier.widthIn(max = 440.dp).fillMaxWidth().imePadding().verticalScroll(rememberScrollState()).padding(24.dp),
            verticalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            Text("Roxy", style = MaterialTheme.typography.displaySmall, fontWeight = FontWeight.Bold, color = RoxyColors.accent)
            Text("Sign in to your Roxy Server", color = RoxyColors.muted, modifier = Modifier.padding(bottom = 8.dp))
            RoxyTextField(serverUrl, { serverUrl = it }, label = "Server URL", keyboardType = KeyboardType.Uri)
            RoxyTextField(username, { username = it }, label = "User name")
            RoxyTextField(password, { password = it }, label = "Password", password = true, keyboardType = KeyboardType.Password, imeAction = ImeAction.Done)
            // Signing in means agreeing to the terms. The text lives on the server, so the
            // link fetches it from the URL typed above; acceptance is recorded once logged in.
            var agreed by rememberSaveable { mutableStateOf(false) }
            var showTerms by rememberSaveable { mutableStateOf(false) }
            Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.fillMaxWidth()) {
                Checkbox(checked = agreed, onCheckedChange = { agreed = it }, colors = CheckboxDefaults.colors(checkedColor = RoxyColors.accent, uncheckedColor = RoxyColors.muted))
                Text(
                    buildAnnotatedString {
                        append("I agree to the ")
                        withStyle(SpanStyle(color = RoxyColors.accent, fontWeight = FontWeight.SemiBold)) { append("Terms of Use") }
                    },
                    color = RoxyColors.onBackground, fontSize = 14.sp,
                    modifier = Modifier.clickable { showTerms = true },
                )
            }
            PrimaryButton(
                text = if (state.busy) "Connecting..." else "Connect",
                enabled = !state.busy && agreed,
                onClick = { vm.login(serverUrl, username, password) },
                modifier = Modifier.fillMaxWidth().padding(top = 8.dp),
            )
            if (showTerms) {
                LoginTermsDialog(serverUrl = serverUrl, onDismiss = { showTerms = false }, onAgree = { agreed = true; showTerms = false })
            }
            state.error?.let { Text(it, color = RoxyColors.error, fontSize = 14.sp) }
        }
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
