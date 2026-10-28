package tv.roxy.tv.ui.phone

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.outlined.QrCodeScanner
import androidx.compose.material.icons.outlined.Tv
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.compose.runtime.Composable
import com.google.zxing.client.android.Intents
import com.journeyapps.barcodescanner.ScanContract
import com.journeyapps.barcodescanner.ScanOptions
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.tv.material3.Text
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.ApiException
import tv.roxy.tv.api.LinkInfo
import tv.roxy.tv.ui.components.RoxyTextField
import tv.roxy.tv.ui.theme.RoxyColors

/**
 * Account > Sign in a TV: scan the QR on the TV's sign-in screen (ZXing, which asks for
 * the camera itself) or type its code, see which TV asks, and approve it;
 * the TV then signs in as this account. [initialCode] comes from a scanned link opened
 * in the app (http://server/link/CODE).
 */
@Composable
fun PhoneLinkTv(onBack: () -> Unit, initialCode: String = "") {
    val context = LocalContext.current
    val app = RoxyApp.get(context)
    val scope = rememberCoroutineScope()
    var code by remember { mutableStateOf(initialCode) }
    var info by remember { mutableStateOf<LinkInfo?>(null) }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var done by remember { mutableStateOf(false) }
    val clean = code.uppercase().filter { it.isLetterOrDigit() }

    suspend fun lookUp() {
        if (clean.length < 6) return
        busy = true; error = null; info = null
        runCatching { app.api.linkInfo(clean) }
            .onSuccess { info = it }
            .onFailure { t -> error = if ((t as? ApiException)?.code == 404) "That code has expired or is wrong. The TV shows a fresh one every ten minutes." else t.message ?: "Could not check the code." }
        busy = false
    }
    LaunchedEffect(initialCode) { if (initialCode.isNotEmpty()) lookUp() }

    // The in-app scanner (ZXing): it asks for the camera itself and needs nothing from Play
    // services. Problems are also sent to the server's log so they can be diagnosed there.
    fun report(what: String) { scope.launch { runCatching { app.api.reportCrash("scan: $what") } } }
    val scanner = rememberLauncherForActivityResult(ScanContract()) { result ->
        val raw = result.contents
        if (raw == null) {
            // Cancelled, or the camera was refused.
            if (result.originalIntent?.hasExtra(Intents.Scan.MISSING_CAMERA_PERMISSION) == true) {
                error = "Roxy needs the camera to scan. Allow it in the phone's settings, or type the code."
                report("camera permission refused")
            }
            return@rememberLauncherForActivityResult
        }
        // The QR carries http://server/link/CODE; a bare code works too.
        val found = raw.substringAfterLast("/link/", raw).substringBefore('?').uppercase().filter { it.isLetterOrDigit() }
        if (found.length >= 6) { code = found; scope.launch { lookUp() } } else { error = "That is not a Roxy sign-in code."; report("not a code: ${raw.take(80)}") }
    }
    fun scan() {
        error = null
        runCatching {
            scanner.launch(ScanOptions().setDesiredBarcodeFormats(ScanOptions.QR_CODE).setPrompt("Point at the code on the TV").setBeepEnabled(false).setOrientationLocked(false))
        }.onFailure { t ->
            Timber.w(t, "scan failed to start")
            error = "The scanner could not open (${t.message}). Type the code instead."
            report(t.stackTraceToString().take(4000))
        }
    }

    Column(modifier = Modifier.fillMaxSize().statusBarsPadding().imePadding()) {
        Row(modifier = Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
            BackCircle(onBack)
            Text("Sign in a TV", fontSize = 20.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(start = 12.dp))
        }
        Column(
            modifier = Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(horizontal = PhonePad),
            verticalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            when {
                done -> {
                    Text("Done", fontSize = 18.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground)
                    Text("${info?.device ?: "The TV"} is signing in as ${app.prefs.session.value.userName}. Pick a profile on the TV.", fontSize = 14.sp, lineHeight = 20.sp, color = RoxyColors.muted)
                }
                info != null -> {
                    val i = info!!
                    Text("${i.device} (${i.client}) wants to sign in as ${app.prefs.session.value.userName}.", fontSize = 15.sp, lineHeight = 21.sp, color = RoxyColors.onBackground)
                    Text("Only approve this if the code on the TV is  ${clean.chunked(3).joinToString(" ")}.", fontSize = 13.sp, lineHeight = 18.sp, color = RoxyColors.muted)
                    PlayButton(
                        if (busy) "Signing in…" else if (i.approved) "Already approved" else "Sign in this TV", icon = Icons.Filled.Check, pill = true,
                        container = if (busy || i.approved) RoxyColors.surface else RoxyColors.accent, content = if (busy || i.approved) RoxyColors.muted else OnBrand,
                        modifier = Modifier.fillMaxWidth(),
                        onClick = {
                            if (busy || i.approved) return@PlayButton
                            busy = true
                            scope.launch {
                                runCatching { app.api.linkApprove(clean) }
                                    .onSuccess { done = true; delay(2500); onBack() }
                                    .onFailure { t -> error = t.message ?: "Could not sign the TV in." }
                                busy = false
                            }
                        },
                    )
                    PlayButton("Not this TV", icon = Icons.Outlined.Tv, pill = true, container = RoxyColors.surface, content = RoxyColors.onBackground, modifier = Modifier.fillMaxWidth(), onClick = { info = null; code = "" })
                }
                else -> {
                    Text(
                        "Open Roxy on the TV. Its sign-in screen shows a QR code and a six-letter code. Scan the QR here, or type the code, and the TV signs in as you.",
                        fontSize = 13.sp, lineHeight = 18.sp, color = RoxyColors.muted,
                    )
                    PlayButton("Scan the TV's code", icon = Icons.Outlined.QrCodeScanner, pill = true, container = RoxyColors.accent, content = OnBrand, modifier = Modifier.fillMaxWidth(), onClick = { scan() })
                    RoxyTextField(code, { code = it.uppercase(); error = null }, label = "Or type the code", keyboardType = KeyboardType.Ascii, imeAction = ImeAction.Done)
                    PlayButton(
                        if (busy) "Checking…" else "Continue", icon = Icons.Filled.Check, pill = true,
                        container = if (clean.length >= 6 && !busy) RoxyColors.accent else RoxyColors.surface, content = if (clean.length >= 6 && !busy) OnBrand else RoxyColors.muted,
                        modifier = Modifier.fillMaxWidth(),
                        onClick = { if (!busy) scope.launch { lookUp() } },
                    )
                }
            }
            error?.let { Text(it, fontSize = 13.sp, color = RoxyColors.error) }
        }
    }
}
