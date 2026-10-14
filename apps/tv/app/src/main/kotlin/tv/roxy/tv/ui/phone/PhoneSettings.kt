package tv.roxy.tv.ui.phone

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ChevronRight
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Icon
import androidx.compose.material3.RadioButton
import androidx.compose.material3.RadioButtonDefaults
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.BuildConfig
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.data.PlayerSettings
import tv.roxy.tv.diagnostics.DeviceReport
import tv.roxy.tv.ui.theme.RoxyColors

private class Pick(val title: String, val options: List<String>, val selected: String, val onPick: (String) -> Unit)

/** Settings as a phone list: tap a row for a choice dialog, switches for the toggles. */
@Composable
fun PhoneSettings(bottomInset: Dp) {
    val context = LocalContext.current
    val app = RoxyApp.get(context)
    val settings by app.prefs.settings.collectAsStateWithLifecycle()
    val session by app.prefs.session.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    val device = remember { DeviceReport.soc() + "\n" + DeviceReport.display(context) }
    var pick by remember { mutableStateOf<Pick?>(null) }

    fun save(s: PlayerSettings) = scope.launch { app.prefs.saveSettings(s) }

    Column(modifier = Modifier.fillMaxSize().verticalScroll(rememberScrollState()).statusBarsPadding().padding(bottom = bottomInset + 16.dp)) {
        Text("Settings", fontSize = 24.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(start = PhonePad, end = PhonePad, top = 12.dp))
        Text("${session.userName} @ ${session.serverUrl}", fontSize = 13.sp, color = RoxyColors.muted, modifier = Modifier.padding(horizontal = PhonePad, vertical = 4.dp))

        SectionTitle("Player", modifier = Modifier.padding(top = 12.dp))
        SettingRow("Hardware decoding", settings.hwdec) {
            pick = Pick("Hardware decoding", PlayerSettings.hwdecChoices, settings.hwdec) { save(settings.copy(hwdec = it)) }
        }
        SettingRow("Video output", settings.vo) {
            pick = Pick("Video output", PlayerSettings.voChoices, settings.vo) { save(settings.copy(vo = it)) }
        }
        SettingRow("Audio language", settings.audioLangs.label()) {
            pick = Pick("Audio language", PlayerSettings.audioChoices.map { it.label() }, settings.audioLangs.label()) { picked ->
                PlayerSettings.audioChoices.firstOrNull { it.label() == picked }?.let { save(settings.copy(audioLangs = it)) }
            }
        }
        SettingRow("Subtitles", settings.subMode.subLabel()) {
            pick = Pick("Subtitles", PlayerSettings.subModeChoices.map { it.subLabel() }, settings.subMode.subLabel()) { picked ->
                PlayerSettings.subModeChoices.firstOrNull { it.subLabel() == picked }?.let { save(settings.copy(subMode = it)) }
            }
        }
        SwitchRow("Auto-skip intro & outro", settings.autoSkip) { save(settings.copy(autoSkip = it)) }
        SwitchRow("Stats overlay", settings.showStats) { save(settings.copy(showStats = it)) }
        Text(
            "Subtitles: \"full dialogue\" picks the dialogue track (for Japanese audio), \"signs & songs\" the signs track (for an English dub).",
            color = RoxyColors.muted, fontSize = 12.sp, lineHeight = 16.sp, modifier = Modifier.padding(horizontal = PhonePad, vertical = 8.dp),
        )

        SectionTitle("Account", modifier = Modifier.padding(top = 12.dp))
        Row(
            modifier = Modifier.fillMaxWidth().clickable {
                scope.launch {
                    runCatching { app.api.logout() }.onFailure { Timber.w("logout: %s", it.message) }
                    app.prefs.clearSession() // AppRoot switches to the login screen
                }
            }.padding(horizontal = PhonePad, vertical = 14.dp),
        ) { Text("Log out", fontSize = 16.sp, color = RoxyColors.onBackground) }

        SectionTitle("About", modifier = Modifier.padding(top = 12.dp))
        Text("Roxy TV ${BuildConfig.VERSION_NAME} (${BuildConfig.APPLICATION_ID})", color = RoxyColors.muted, fontSize = 12.sp, modifier = Modifier.padding(horizontal = PhonePad))
        Text(device, color = RoxyColors.muted, fontSize = 11.sp, fontFamily = FontFamily.Monospace, modifier = Modifier.padding(horizontal = PhonePad, vertical = 4.dp))
    }

    pick?.let { p ->
        AlertDialog(
            onDismissRequest = { pick = null },
            containerColor = RoxyColors.surfaceHigh,
            titleContentColor = RoxyColors.onBackground,
            textContentColor = RoxyColors.onBackground,
            title = { Text(p.title) },
            text = {
                Column {
                    p.options.forEach { opt ->
                        Row(
                            modifier = Modifier.fillMaxWidth().clickable { p.onPick(opt); pick = null }.padding(vertical = 10.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            RadioButton(selected = opt == p.selected, onClick = null, colors = RadioButtonDefaults.colors(selectedColor = Color.White, unselectedColor = RoxyColors.muted))
                            Text(opt, fontSize = 15.sp, modifier = Modifier.padding(start = 10.dp))
                        }
                    }
                }
            },
            confirmButton = { TextButton(onClick = { pick = null }) { Text("Cancel", color = Color.White) } },
        )
    }
}

@Composable
private fun SettingRow(label: String, value: String, onClick: () -> Unit) {
    Row(
        modifier = Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = PhonePad, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(modifier = Modifier.weight(1f)) {
            Text(label, fontSize = 16.sp, color = RoxyColors.onBackground)
            Text(value, fontSize = 13.sp, color = RoxyColors.muted)
        }
        Icon(Icons.Filled.ChevronRight, contentDescription = null, tint = RoxyColors.muted)
    }
}

@Composable
private fun SwitchRow(label: String, checked: Boolean, onChange: (Boolean) -> Unit) {
    Row(
        modifier = Modifier.fillMaxWidth().clickable { onChange(!checked) }.padding(horizontal = PhonePad, vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(label, fontSize = 16.sp, color = RoxyColors.onBackground, modifier = Modifier.weight(1f))
        Switch(
            checked = checked, onCheckedChange = onChange,
            colors = SwitchDefaults.colors(checkedThumbColor = Color.White, checkedTrackColor = RoxyColors.accent, uncheckedThumbColor = RoxyColors.muted, uncheckedTrackColor = ChipGrey),
        )
    }
}

private fun List<String>.label() = when (this) {
    listOf("eng", "jpn") -> "English dub, else Japanese"
    listOf("jpn", "eng") -> "Japanese, else English"
    else -> joinToString(",")
}

private fun String.subLabel() = when (this) {
    "full" -> "full dialogue"
    "signs" -> "signs & songs"
    "none" -> "off"
    else -> this
}
