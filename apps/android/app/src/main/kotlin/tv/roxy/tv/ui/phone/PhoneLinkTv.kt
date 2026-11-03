package tv.roxy.tv.ui.phone

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.outlined.QrCodeScanner
import androidx.compose.material.icons.outlined.Tv
import androidx.compose.material3.Icon
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
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.tv.material3.Text
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.ApiException
import tv.roxy.tv.api.LinkInfo
import tv.roxy.tv.ui.theme.RoxyColors

private const val CodeLength = 6

/**
 * Account > Sign in a TV. A header card with the TV and the three steps, then Scan (the
 * in-app scanner, with zoom) or the code typed into six boxes; once a code checks out,
 * a card names the TV asking and the account it will use, with Sign in this TV; then a
 * done state. [initialCode] comes from a sign-in link opened in the app (http://server/link/CODE).
 * It is deliberately NOT filled in: a link can come from anyone (a chat, a web page), and
 * approving it would sign the sender's device in to this account. The person types the code
 * the TV in front of them shows instead.
 */
@Composable
fun PhoneLinkTv(onBack: () -> Unit, initialCode: String = "") {
    val context = LocalContext.current
    val app = RoxyApp.get(context)
    val scope = rememberCoroutineScope()
    val userName = app.prefs.session.value.userName
    val fromLink = initialCode.isNotBlank()
    var code by remember { mutableStateOf("") }
    var info by remember { mutableStateOf<LinkInfo?>(null) }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var done by remember { mutableStateOf(false) }
    var scanning by remember { mutableStateOf(false) }

    fun report(what: String) { scope.launch { runCatching { app.api.reportCrash("scan: $what") } } }

    suspend fun lookUp() {
        if (code.length < CodeLength) return
        busy = true; error = null; info = null
        runCatching { app.api.linkInfo(code) }
            .onSuccess { info = it }
            .onFailure { t -> error = if ((t as? ApiException)?.code == 404) "That code has expired or is wrong. The TV shows a fresh one every ten minutes." else t.message ?: "Could not check the code." }
        busy = false
    }

    if (scanning) {
        QrScanner(
            onResult = { raw ->
                scanning = false
                // The QR carries http://server/link/CODE; a bare code works too.
                val found = raw.substringAfterLast("/link/", raw).substringBefore('?').uppercase().filter { it.isLetterOrDigit() }
                if (found.length >= CodeLength) { code = found.take(CodeLength); scope.launch { lookUp() } }
                else { error = "That QR code isn't a Roxy sign-in code."; report("not a code: ${raw.take(80)}") }
            },
            onClose = { scanning = false },
            onError = { msg -> scanning = false; error = msg; report(msg) },
        )
        return
    }

    Column(modifier = Modifier.fillMaxSize().statusBarsPadding().imePadding()) {
        Row(modifier = Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
            BackCircle(onBack)
            Text("Sign in a TV or browser", fontSize = 20.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(start = 12.dp))
        }
        Column(
            modifier = Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(horizontal = PhonePad).padding(bottom = 32.dp),
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            val asking = info
            when {
                done -> DoneCard(asking?.device ?: "The TV", userName)
                asking != null -> ApproveCard(
                    info = asking, code = code, userName = userName, busy = busy,
                    onApprove = {
                        if (busy || asking.approved) return@ApproveCard
                        busy = true
                        scope.launch {
                            runCatching { app.api.linkApprove(code) }
                                .onSuccess { done = true; delay(2500); onBack() }
                                .onFailure { t -> error = t.message ?: "Could not sign the TV in." }
                            busy = false
                        }
                    },
                    onCancel = { info = null; code = ""; error = null },
                )
                else -> {
                    if (fromLink) Text(
                        "You opened a sign-in link. To be safe, type the code shown on the TV's screen below. Only do this for a TV you are standing in front of.",
                        fontSize = 14.sp, lineHeight = 20.sp, color = RoxyColors.onBackground,
                        modifier = Modifier.fillMaxWidth().clip(RoundedCornerShape(12.dp)).background(RoxyColors.accent.copy(alpha = 0.14f)).padding(14.dp),
                    )
                    StepsCard()
                    PlayButton(
                        "Scan the TV's code", icon = Icons.Outlined.QrCodeScanner, height = 52.dp, pill = true,
                        container = RoxyColors.accent, content = OnBrand, modifier = Modifier.fillMaxWidth(),
                        onClick = { error = null; scanning = true },
                    )
                    OrDivider()
                    Text("Type the code shown under the QR", fontSize = 13.sp, color = RoxyColors.muted, modifier = Modifier.fillMaxWidth(), textAlign = TextAlign.Center)
                    CodeBoxes(code, onChange = { code = it; error = null; if (it.length == CodeLength) scope.launch { lookUp() } })
                    if (busy) Text("Checking…", fontSize = 13.sp, color = RoxyColors.muted, modifier = Modifier.fillMaxWidth(), textAlign = TextAlign.Center)
                }
            }
            error?.let {
                Text(it, fontSize = 13.sp, lineHeight = 18.sp, color = RoxyColors.error, modifier = Modifier.fillMaxWidth().clip(RoundedCornerShape(12.dp)).background(RoxyColors.error.copy(alpha = 0.1f)).padding(12.dp))
            }
        }
    }
}

/** The TV glyph in a soft accent glow, and the three steps. */
@Composable
private fun StepsCard() {
    Column(
        modifier = Modifier.fillMaxWidth().clip(RoundedCornerShape(20.dp))
            .background(Brush.verticalGradient(listOf(RoxyColors.accent.copy(alpha = 0.18f), RoxyColors.surface)))
            .padding(20.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Box(modifier = Modifier.size(72.dp).clip(CircleShape).background(RoxyColors.accent.copy(alpha = 0.2f)), contentAlignment = Alignment.Center) {
            Icon(Icons.Outlined.Tv, contentDescription = null, tint = RoxyColors.accent, modifier = Modifier.size(38.dp))
        }
        Text("Sign in on your TV without typing", fontSize = 17.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, textAlign = TextAlign.Center, modifier = Modifier.padding(top = 14.dp, bottom = 14.dp))
        Step(1, "Open Roxy on the TV.")
        Step(2, "Scan the QR on its sign-in screen, or type the code.")
        Step(3, "Check the TV's name and tap Sign in this TV.")
    }
}

@Composable
private fun Step(n: Int, text: String) {
    Row(modifier = Modifier.fillMaxWidth().padding(vertical = 5.dp), verticalAlignment = Alignment.CenterVertically) {
        Box(modifier = Modifier.size(24.dp).clip(CircleShape).background(RoxyColors.accent), contentAlignment = Alignment.Center) {
            Text("$n", fontSize = 12.sp, fontWeight = FontWeight.Bold, color = OnBrand)
        }
        Text(text, fontSize = 14.sp, lineHeight = 19.sp, color = RoxyColors.onBackground, modifier = Modifier.padding(start = 12.dp))
    }
}

@Composable
private fun OrDivider() {
    Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.fillMaxWidth().padding(vertical = 2.dp)) {
        Box(Modifier.weight(1f).height(1.dp).background(RoxyColors.surfaceHigh))
        Text("OR", fontSize = 11.sp, fontWeight = FontWeight.Bold, letterSpacing = 1.sp, color = RoxyColors.muted, modifier = Modifier.padding(horizontal = 12.dp))
        Box(Modifier.weight(1f).height(1.dp).background(RoxyColors.surfaceHigh))
    }
}

/** Six boxes over one hidden field: tap to type; letters and digits only, upper case. */
@Composable
private fun CodeBoxes(code: String, onChange: (String) -> Unit) {
    BasicTextField(
        value = code,
        onValueChange = { onChange(it.uppercase().filter { c -> c.isLetterOrDigit() }.take(CodeLength)) },
        singleLine = true,
        keyboardOptions = KeyboardOptions(capitalization = KeyboardCapitalization.Characters, keyboardType = KeyboardType.Ascii, imeAction = ImeAction.Done, autoCorrectEnabled = false),
        textStyle = TextStyle(color = Color.Transparent),
        modifier = Modifier.fillMaxWidth(),
        decorationBox = { inner ->
            Box {
                Box(Modifier.size(1.dp)) { inner() }
                Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.CenterHorizontally), verticalAlignment = Alignment.CenterVertically) {
                    repeat(CodeLength) { i ->
                        if (i == 3) Box(Modifier.width(10.dp).height(2.dp).background(RoxyColors.muted))
                        val active = i == code.length
                        Box(
                            modifier = Modifier.width(44.dp).height(56.dp).clip(RoundedCornerShape(10.dp)).background(RoxyColors.surface)
                                .border(2.dp, if (active) RoxyColors.accent else RoxyColors.surfaceHigh, RoundedCornerShape(10.dp)),
                            contentAlignment = Alignment.Center,
                        ) { Text(code.getOrNull(i)?.toString() ?: "", fontSize = 24.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground) }
                    }
                }
            }
        },
    )
}

/** Which TV is asking, the code to match against the TV's screen, and the approve button. */
@Composable
private fun ApproveCard(info: LinkInfo, code: String, userName: String, busy: Boolean, onApprove: () -> Unit, onCancel: () -> Unit) {
    Column(
        modifier = Modifier.fillMaxWidth().clip(RoundedCornerShape(20.dp)).background(RoxyColors.surface).padding(20.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Box(modifier = Modifier.size(64.dp).clip(CircleShape).background(RoxyColors.accent.copy(alpha = 0.2f)), contentAlignment = Alignment.Center) {
            Icon(Icons.Outlined.Tv, contentDescription = null, tint = RoxyColors.accent, modifier = Modifier.size(34.dp))
        }
        // The name is whatever the asking device says it is: shown, but not something to trust.
        Text(info.device.take(60).ifBlank { "A TV" }, fontSize = 20.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, textAlign = TextAlign.Center, maxLines = 2, modifier = Modifier.padding(top = 12.dp))
        Text("wants to sign in as $userName", fontSize = 14.sp, color = RoxyColors.muted, textAlign = TextAlign.Center, modifier = Modifier.padding(top = 2.dp))
        networkLine(info.network)?.let { (text, warn) ->
            Text(text, fontSize = 14.sp, fontWeight = FontWeight.SemiBold, color = if (warn) RoxyColors.error else RoxyColors.accent, textAlign = TextAlign.Center, modifier = Modifier.padding(top = 8.dp))
        }
        Text(
            code.chunked(3).joinToString("  "), fontSize = 30.sp, fontWeight = FontWeight.ExtraBold, letterSpacing = 4.sp, color = RoxyColors.accent,
            modifier = Modifier.padding(top = 18.dp).clip(RoundedCornerShape(12.dp)).background(RoxyColors.background).padding(horizontal = 20.dp, vertical = 8.dp),
        )
        Text("Make sure this matches the code on its screen.", fontSize = 13.sp, color = RoxyColors.muted, textAlign = TextAlign.Center, modifier = Modifier.padding(top = 8.dp))
        Text("Only approve a TV you are standing in front of.", fontSize = 13.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground, textAlign = TextAlign.Center, modifier = Modifier.padding(top = 4.dp, bottom = 20.dp))
        PlayButton(
            when { busy -> "Signing in…"; info.approved -> "Already approved"; else -> "Sign in this TV" }, icon = Icons.Filled.Check, height = 52.dp, pill = true,
            container = if (busy || info.approved) RoxyColors.surfaceHigh else RoxyColors.accent, content = if (busy || info.approved) RoxyColors.muted else OnBrand,
            modifier = Modifier.fillMaxWidth(), onClick = onApprove,
        )
        Text("Not this device", fontSize = 14.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.muted, modifier = Modifier.padding(top = 14.dp).clip(RoundedCornerShape(8.dp)).clickable(onClick = onCancel).padding(horizontal = 12.dp, vertical = 6.dp))
    }
}

/** Where the asking device connected from (newer servers say), and whether to warn. */
private fun networkLine(network: String): Pair<String, Boolean>? = when (network.lowercase()) {
    "home" -> "From your home network" to false
    "tailscale" -> "From Tailscale" to false
    "server" -> "From the server machine itself" to false
    "internet" -> "From the internet — be careful" to true
    else -> null
}

@Composable
private fun DoneCard(device: String, userName: String) {
    Column(
        modifier = Modifier.fillMaxWidth().clip(RoundedCornerShape(20.dp)).background(RoxyColors.surface).padding(vertical = 32.dp, horizontal = 20.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Icon(Icons.Filled.CheckCircle, contentDescription = null, tint = RoxyColors.accent, modifier = Modifier.size(64.dp))
        Text("You're all set", fontSize = 20.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(top = 12.dp))
        Spacer(Modifier.height(6.dp))
        Text("$device is signing in as $userName. Pick a profile there.", fontSize = 14.sp, lineHeight = 20.sp, color = RoxyColors.muted, textAlign = TextAlign.Center)
    }
}
