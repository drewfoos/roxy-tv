package tv.roxy.tv.ui.tv

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.tv.material3.Text
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.BuildConfig
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.data.Languages
import tv.roxy.tv.data.PlayerSettings
import tv.roxy.tv.diagnostics.DeviceReport
import tv.roxy.tv.player.AudioPassthrough
import tv.roxy.tv.player.QualityOption
import tv.roxy.tv.ui.theme.RoxyColors

/*
 * Settings, as Crunchyroll's TV app lays them out: a dark left panel with the profile's
 * name and "Switch Profile" on top, then the settings in two groups, and the chosen
 * setting's options at the right. Picking an option saves it at once.
 */

private val PanelWidth = 295.dp
private val PanelStart = TvDims.RailWidth

/** One setting: its label, the choices (value to label) and the current value. */
private class Setting(val key: String, val label: String, val options: List<Pair<String, String>>, val value: String, val hint: String? = null, val apply: (String) -> Unit)

@Composable
fun TvSettings(onSwitchProfile: () -> Unit, onLoggedOut: () -> Unit) {
    val context = LocalContext.current
    val app = RoxyApp.get(context)
    val settings by app.prefs.settings.collectAsStateWithLifecycle()
    val session by app.prefs.session.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    val memory = rememberFocusMemory()
    fun save(s: PlayerSettings) = scope.launch { app.prefs.saveSettings(s) }

    val langs = remember { Languages.all.take(16) }
    val langOptions = remember(langs) { langs.map { it.iso3 to it.name } }
    val passthroughFound = remember { AudioPassthrough.detect(context) }
    val quality = remember { QualityOption.Presets.map { it.key to it.label } }

    val preferences = listOf(
        Setting("audio", "Audio Language", langOptions, settings.audioLangs.firstOrNull() ?: "eng") { save(settings.copy(audioLangs = listOf(it))) },
        Setting("sub", "Subtitle/CC Language", langOptions, settings.subLang) { save(settings.copy(subLang = it)) },
        Setting("submode", "Subtitles", PlayerSettings.subModeChoices.map { it to PlayerSettings.subModeLabel(it) }, settings.subMode, hint = PlayerSettings.subModeHint(settings.subMode)) { save(settings.copy(subMode = it)) },
        Setting("quality", "Video Quality", quality, settings.qualityWifi) { save(settings.copy(qualityWifi = it)) },
        Setting("autoskip", "Auto-skip Intro & Outro", listOf("on" to "On", "off" to "Off"), if (settings.autoSkip) "on" else "off") { save(settings.copy(autoSkip = it == "on")) },
        Setting("skipwindow", "Skip Button Stays For", PlayerSettings.skipWindowChoices.map { it.toString() to PlayerSettings.skipWindowLabel(it) }, settings.skipWindowSec.toString()) { save(settings.copy(skipWindowSec = it.toInt())) },
        Setting("nextcard", "Next-episode Countdown", listOf("on" to "On", "off" to "Off"), if (settings.nextCard) "on" else "off") { save(settings.copy(nextCard = it == "on")) },
    )
    val advanced = listOf(
        Setting("passthrough", "Soundbar Passthrough", listOf("auto" to AudioPassthrough.autoLabel(passthroughFound), "off" to "Off (decode on the TV)"), settings.passthrough, hint = "Sends Dolby and DTS audio untouched over HDMI ARC; the soundbar then plays the surround and controls the volume.") { save(settings.copy(passthrough = it)) },
        Setting("hwdec", "Hardware Decoding", PlayerSettings.hwdecChoices.map { it to it }, settings.hwdec) { save(settings.copy(hwdec = it)) },
        Setting("vo", "Video Output", PlayerSettings.voChoices.map { it to it }, settings.vo) { save(settings.copy(vo = it)) },
        Setting("stats", "Stats Overlay", listOf("on" to "On", "off" to "Off"), if (settings.showStats) "on" else "off", hint = "Also toggled in the player with Up or Menu.") { save(settings.copy(showStats = it == "on")) },
    )
    val all = preferences + advanced
    var selected by rememberSaveable { mutableStateOf(all.first().key) }
    RegisterPageEntry { memory.restore("set/$selected") }
    val current = all.firstOrNull { it.key == selected } ?: all.first()
    val device = remember { DeviceReport.soc() + "\n" + DeviceReport.display(context) }

    Row(modifier = Modifier.fillMaxSize()) {
        Spacer(Modifier.width(PanelStart))
        LazyColumn(modifier = Modifier.width(PanelWidth).fillMaxHeight().background(RoxyColors.surface), contentPadding = PaddingValues(bottom = 24.dp)) {
            item(key = "profile") {
                TvFocusable(onClick = onSwitchProfile, modifier = Modifier.fillMaxWidth().focusKey(memory, "profile"), focusedBackground = RoxyColors.accent) { focused ->
                    Column(modifier = Modifier.fillMaxWidth().padding(start = 40.dp, top = 56.dp, bottom = 22.dp, end = 20.dp)) {
                        Text(session.profileName.ifEmpty { session.userName }, color = if (focused) OnAccent else RoxyColors.onBackground, fontSize = 22.sp, fontWeight = FontWeight.Bold, maxLines = 1)
                        Text("Switch Profile", color = if (focused) OnAccent else RoxyColors.muted, fontSize = 13.sp)
                    }
                }
            }
            item(key = "h1") { GroupHeading("${session.profileName.ifEmpty { "Your" }.uppercase()}'S PREFERENCES") }
            items(preferences, key = { it.key }) { s -> SettingRow(s, selected == s.key, memory) { selected = s.key } }
            item(key = "h2") { GroupHeading("ADVANCED") }
            items(advanced, key = { it.key }) { s -> SettingRow(s, selected == s.key, memory) { selected = s.key } }
            item(key = "h3") { GroupHeading("APP") }
            item(key = "update") {
                val note by app.updates.checkNote.collectAsStateWithLifecycle()
                TvFocusable(onClick = { app.updates.checkNow() }, modifier = Modifier.fillMaxWidth().focusKey(memory, "update"), focusedBackground = RoxyColors.accent) { focused ->
                    Column(modifier = Modifier.fillMaxWidth().padding(start = 40.dp, end = 16.dp, top = 9.dp, bottom = 9.dp)) {
                        Text("Check for updates", color = if (focused) OnAccent else RoxyColors.muted, fontSize = 18.sp, fontWeight = FontWeight.Medium, maxLines = 1)
                        Text(note ?: "Newer builds on your server install from here", color = if (focused) OnAccent.copy(alpha = 0.8f) else RoxyColors.muted, fontSize = 13.sp, maxLines = 1)
                    }
                }
            }
            item(key = "h4") { GroupHeading("ACCOUNT") }
            item(key = "logout") {
                TvListRow(
                    label = "Log Out", height = 44.dp, startPadding = 40.dp, fontSize = 18.sp,
                    onClick = {
                        scope.launch {
                            runCatching { app.api.logout() }.onFailure { Timber.w("logout: %s", it.message) }
                            app.prefs.clearSession()
                            onLoggedOut()
                        }
                    },
                    modifier = Modifier.focusKey(memory, "logout"),
                )
            }
        }
        // The chosen setting's options, then what this build is.
        Column(modifier = Modifier.weight(1f).fillMaxHeight().padding(start = 60.dp, top = 56.dp, end = 60.dp)) {
            Text(current.label, color = RoxyColors.onBackground, fontSize = 26.sp, fontWeight = FontWeight.Bold)
            current.hint?.let { Text(it, color = RoxyColors.muted, fontSize = 14.sp, lineHeight = 19.sp, modifier = Modifier.padding(top = 6.dp)) }
            Spacer(Modifier.height(18.dp))
            LazyColumn(modifier = Modifier.weight(1f).width(400.dp), contentPadding = PaddingValues(bottom = 16.dp)) {
                items(current.options, key = { it.first }) { (value, label) ->
                    TvListRow(
                        label = label, selected = value == current.value, checked = value == current.value, height = 44.dp, startPadding = 16.dp, fontSize = 18.sp,
                        onClick = { current.apply(value) }, modifier = Modifier.focusKey(memory, "opt/${current.key}/$value"),
                    )
                }
            }
            Text("Roxy TV ${BuildConfig.VERSION_NAME} · ${session.userName} @ ${session.serverUrl}", color = RoxyColors.muted, fontSize = 12.sp)
            Text(device, color = RoxyColors.muted.copy(alpha = 0.7f), fontSize = 11.sp, fontFamily = FontFamily.Monospace, modifier = Modifier.padding(top = 4.dp, bottom = 24.dp))
        }
    }
}

@Composable
private fun GroupHeading(text: String) {
    Text(text, color = RoxyColors.onBackground, fontSize = 11.sp, fontWeight = FontWeight.Bold, letterSpacing = 1.sp, modifier = Modifier.padding(start = 40.dp, top = 24.dp, bottom = 6.dp))
}

/** A setting in the panel: focus shows its options at the right; the current value reads under the label. */
@Composable
private fun SettingRow(s: Setting, selected: Boolean, memory: FocusMemory, onSelect: () -> Unit) {
    val valueLabel = s.options.firstOrNull { it.first == s.value }?.second ?: s.value
    TvFocusable(onClick = onSelect, modifier = Modifier.fillMaxWidth().focusKey(memory, "set/${s.key}"), focusedBackground = RoxyColors.accent, onFocused = onSelect) { focused ->
        Column(modifier = Modifier.fillMaxWidth().padding(start = 40.dp, end = 16.dp, top = 9.dp, bottom = 9.dp)) {
            Text(s.label, color = if (focused) OnAccent else if (selected) RoxyColors.onBackground else RoxyColors.muted, fontSize = 18.sp, fontWeight = FontWeight.Medium, maxLines = 1)
            Text(valueLabel, color = if (focused) OnAccent.copy(alpha = 0.8f) else RoxyColors.muted, fontSize = 13.sp, maxLines = 1)
        }
    }
}
