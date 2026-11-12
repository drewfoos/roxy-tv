package tv.roxy.tv.ui.phone

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Close
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
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
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.AddressCheck
import tv.roxy.tv.api.AddressSettings
import tv.roxy.tv.ui.components.RoxyTextField
import tv.roxy.tv.ui.theme.RoxyColors

/**
 * The list to save when [typed] is added to [saved], or null when there is nothing to add:
 * nothing typed, or an address already in the list (however its case or last slash is
 * written). The address goes to the server as typed; the server decides whether it is one.
 */
fun withAddress(saved: List<String>, typed: String): List<String>? {
    val t = typed.trim()
    if (t.isEmpty()) return null
    fun key(s: String) = s.trim().trimEnd('/').lowercase()
    if (saved.any { key(it) == key(t) }) return null
    return saved + t
}

/** What to tell the admin about one address after a save, and whether it is good news. */
fun addressCheckWords(c: AddressCheck): Pair<Boolean, String> = when (c.state) {
    "ok" -> true to "${c.address} reaches this server."
    else -> {
        val why = if (c.state == "notRoxy") "something else answered there" else "nothing answered there"
        false to "${c.address} is saved, but the server could not confirm it from where it runs ($why). That can be normal for an address meant for outside your home. The apps check it themselves before they use it."
    }
}

/**
 * Admin: the server's other addresses, like its Tailscale one (the web app's page of the
 * same name). Signed-in phones and TVs are told them and use whichever answers: the home
 * address at home, another when away. Each add or remove saves the whole list.
 */
@Composable
fun PhoneServerAddresses(onBack: () -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    val scope = rememberCoroutineScope()
    var settings by remember { mutableStateOf<AddressSettings?>(null) }
    var typed by remember { mutableStateOf("") }
    var error by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    LaunchedEffect(Unit) {
        runCatching { app.api.addressSettings() }.onSuccess { settings = it }.onFailure { t -> Timber.w(t, "address settings"); error = t.message }
    }

    fun save(list: List<String>, clearTyped: Boolean) {
        if (busy) return
        busy = true
        error = null
        scope.launch {
            runCatching { app.api.saveAddressSettings(list) }
                .onSuccess {
                    settings = it
                    if (clearTyped) typed = ""
                    // This phone learns the new list now, not at its next start.
                    runCatching { app.api.me() }.onSuccess { me -> if (app.prefs.setAddresses(me.addresses, me.serverId)) app.server.checkSoon() }
                }
                .onFailure { e -> error = e.message }
            busy = false
        }
    }

    Column(modifier = Modifier.fillMaxSize().statusBarsPadding()) {
        Row(modifier = Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
            BackCircle(onBack)
            Text("Server addresses", fontSize = 20.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(start = 12.dp))
        }
        Column(
            modifier = Modifier.fillMaxSize().verticalScroll(rememberScrollState()).imePadding().navigationBarsPadding().padding(horizontal = PhonePad, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Text("Other ways to reach this server, like its Tailscale address. Signed-in phones and TVs learn them and use whichever answers: the home address at home, another one when away.", fontSize = 13.sp, lineHeight = 18.sp, color = RoxyColors.muted)
            val s = settings
            if (s == null) {
                Text(error ?: "…", fontSize = 13.sp, color = if (error != null) RoxyColors.error else RoxyColors.muted)
                return@Column
            }
            if (s.advertised.isNotEmpty()) AddressRow("Home address", s.advertised, onRemove = null)
            if (s.fromEnvironment) {
                Text("These are set by ROXY_OTHER_URLS where the server is started. Change them there, or remove that setting to edit them here.", fontSize = 13.sp, lineHeight = 18.sp, color = RoxyColors.muted)
                s.addresses.forEach { AddressRow("Other address", it, onRemove = null) }
                return@Column
            }
            if (s.addresses.isEmpty()) Text("No other address yet.", fontSize = 13.sp, color = RoxyColors.muted)
            s.addresses.forEach { a -> AddressRow("Other address", a, onRemove = { save(s.addresses - a, clearTyped = false) }) }
            s.checks.forEach { c ->
                val (good, words) = addressCheckWords(c)
                Text(words, fontSize = 13.sp, lineHeight = 18.sp, color = if (good) RoxyColors.accent else RoxyColors.muted)
            }

            RoxyTextField(typed, { typed = it; error = null }, label = "Add an address", keyboardType = KeyboardType.Uri, imeAction = ImeAction.Done, modifier = Modifier.padding(top = 8.dp))
            Text("For example http://100.64.0.7:8097 or https://roxy.example.com. Plain http is only for a home-network or Tailscale IP address, or a .ts.net name; anything else needs https.", fontSize = 12.sp, lineHeight = 16.sp, color = RoxyColors.muted)
            error?.let { Text(it, fontSize = 13.sp, lineHeight = 18.sp, color = RoxyColors.error) }
            PlayButton(if (busy) "Checking…" else "Add address", icon = Icons.Filled.Add, pill = true, container = RoxyColors.accent, content = OnBrand, modifier = Modifier.fillMaxWidth(), onClick = {
                when (val list = withAddress(s.addresses, typed)) {
                    null -> if (typed.isNotBlank()) error = "That address is already in the list."
                    else -> save(list, clearTyped = true)
                }
            })
        }
    }
}

@Composable
private fun AddressRow(label: String, address: String, onRemove: (() -> Unit)?) {
    Row(
        modifier = Modifier.fillMaxWidth().clip(RoundedCornerShape(8.dp)).background(RoxyColors.surfaceHigh).padding(horizontal = 12.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(modifier = Modifier.weight(1f)) {
            Text(label, fontSize = 12.sp, color = RoxyColors.muted)
            Text(address, fontSize = 15.sp, color = RoxyColors.onBackground)
        }
        if (onRemove != null) {
            Icon(Icons.Filled.Close, contentDescription = "Remove $address", tint = RoxyColors.muted, modifier = Modifier.size(40.dp).clip(RoundedCornerShape(20.dp)).clickable(onClick = onRemove).padding(8.dp))
        }
    }
}
