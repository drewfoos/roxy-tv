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
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.outlined.SystemUpdate
import androidx.compose.material3.Icon
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
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
import tv.roxy.tv.data.UpdateState
import tv.roxy.tv.diagnostics.DeviceReport
import tv.roxy.tv.player.AudioPassthrough
import tv.roxy.tv.player.QualityOption
import tv.roxy.tv.ui.components.widthPx
import tv.roxy.tv.ui.profiles.AvatarCircle
import tv.roxy.tv.ui.theme.RoxyColors
import android.os.Build

/*
 * Settings, as Crunchyroll's TV app lays them out: a dark left panel with the profile
 * (avatar, name, "Switch Profile") over a tinted header, then the settings in groups as
 * single lines with the current value at the right; the chosen setting's title, its
 * explanation and its options at the right, the current option ticked. Picking an option
 * saves it at once. Under the options, what this build and this TV are.
 */

private val PanelWidth = 295.dp
private val PanelStart = TvDims.RailWidth
private val RowStart = 40.dp
private val RowHeight = 50.dp

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
        Setting("audio", "Audio Language", langOptions, settings.audioLangs.firstOrNull() ?: "eng", hint = "Picked first when an episode has it; the other language is a press away in the player.") { save(settings.copy(audioLangs = listOf(it))) },
        Setting("sub", "Subtitle/CC Language", langOptions, settings.subLang) { save(settings.copy(subLang = it)) },
        Setting("submode", "Subtitles", PlayerSettings.subModeChoices.map { it to PlayerSettings.subModeLabel(it) }, settings.subMode, hint = PlayerSettings.subModeHint(settings.subMode)) { save(settings.copy(subMode = it)) },
        Setting("quality", "Video Quality", quality, settings.qualityWifi, hint = "Original plays the file as it is; a smaller size asks the server to convert it.") { save(settings.copy(qualityWifi = it)) },
        Setting("autoskip", "Auto-skip Intro & Outro", listOf("on" to "On", "off" to "Off"), if (settings.autoSkip) "on" else "off", hint = "Jumps past openings and endings the server has marked, without asking.") { save(settings.copy(autoSkip = it == "on")) },
        Setting("skipwindow", "Skip Button Stays For", PlayerSettings.skipWindowChoices.map { it.toString() to PlayerSettings.skipWindowLabel(it) }, settings.skipWindowSec.toString(), hint = "How long the Skip intro button stays on screen.") { save(settings.copy(skipWindowSec = it.toInt())) },
        Setting("nextcard", "Next-episode Countdown", listOf("on" to "On", "off" to "Off"), if (settings.nextCard) "on" else "off", hint = "Counts down to the next episode over the credits.") { save(settings.copy(nextCard = it == "on")) },
    )
    val advanced = listOf(
        Setting("passthrough", "Soundbar Passthrough", listOf("auto" to AudioPassthrough.autoLabel(passthroughFound), "off" to "Off (decode on the TV)"), settings.passthrough, hint = "Sends Dolby and DTS audio untouched over HDMI ARC for the soundbar to decode. A format the TV can't pass through is decoded here automatically; picking an option here tries them all again.") { tv.roxy.tv.player.MpvPlayer.resetPassthroughMemory(context); save(settings.copy(passthrough = it)) },
        Setting("hwdec", "Hardware Decoding", PlayerSettings.hwdecChoices.map { it to it }, settings.hwdec, hint = "mediacodec-copy is the safe default on Android TV; change it only if video stutters or shows wrong colours.") { save(settings.copy(hwdec = it)) },
        Setting("vo", "Video Output", PlayerSettings.voChoices.map { it to it }, settings.vo, hint = "How frames reach the screen. gpu works everywhere.") { save(settings.copy(vo = it)) },
        Setting("stats", "Stats Overlay", listOf("on" to "On", "off" to "Off"), if (settings.showStats) "on" else "off", hint = "Shows the decoder, frame drops and bitrate over the picture. Also toggled in the player with Up or Menu.") { save(settings.copy(showStats = it == "on")) },
    )
    val all = preferences + advanced
    var selected by rememberSaveable { mutableStateOf(all.first().key) }
    RegisterPageEntry { memory.restore("set/$selected") }
    val current = all.firstOrNull { it.key == selected }
    val updateNote by app.updates.checkNote.collectAsStateWithLifecycle()
    val updateState by app.updates.state.collectAsStateWithLifecycle()
    val who = session.profileName.ifEmpty { session.userName }

    Row(modifier = Modifier.fillMaxSize()) {
        Spacer(Modifier.width(PanelStart))
        LazyColumn(modifier = Modifier.width(PanelWidth).fillMaxHeight().background(RoxyColors.surface), contentPadding = PaddingValues(bottom = 24.dp)) {
            item(key = "profile") {
                // The profile over a tinted header: avatar, name, "Switch Profile".
                TvFocusable(onClick = onSwitchProfile, modifier = Modifier.fillMaxWidth().focusKey(memory, "profile"), focusedBackground = RoxyColors.accent) { focused ->
                    Row(
                        modifier = Modifier.fillMaxWidth().height(150.dp)
                            .then(if (focused) Modifier else Modifier.background(Brush.verticalGradient(0f to Color(0xFF16304D), 1f to RoxyColors.surface)))
                            .padding(start = RowStart, end = 20.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        val avatarUrl = if (session.profileHasAvatar && session.profileId != null) app.api.profileAvatarUrl(session.profileId!!, widthPx(56.dp) * 2, session.profileAvatarKey) else null
                        AvatarCircle(name = who, imageUrl = avatarUrl, size = 56.dp)
                        Column(modifier = Modifier.padding(start = 16.dp)) {
                            Text(who, color = if (focused) OnAccent else RoxyColors.onBackground, fontSize = 22.sp, fontWeight = FontWeight.Bold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                            Text("Switch Profile", color = if (focused) OnAccent else RoxyColors.muted, fontSize = 13.sp)
                        }
                    }
                }
            }
            item(key = "h1") { GroupHeading("${who.uppercase()}'S PREFERENCES") }
            items(preferences, key = { it.key }) { s -> SettingRow(s, selected == s.key, memory) { selected = s.key } }
            item(key = "h2") { GroupHeading("ADVANCED") }
            items(advanced, key = { it.key }) { s -> SettingRow(s, selected == s.key, memory) { selected = s.key } }
            item(key = "h3") { GroupHeading("APP") }
            item(key = "update") {
                val value = when (val u = updateState) {
                    is UpdateState.Available -> "${u.update.versionName} ready"
                    is UpdateState.Downloading -> "${(u.fraction * 100).toInt()}%"
                    is UpdateState.Ready -> "Install"
                    is UpdateState.Failed -> "Failed"
                    UpdateState.None -> updateNote?.substringBefore("  ·  ") ?: BuildConfig.VERSION_NAME
                }
                PanelRow(
                    label = "Check for updates", value = value, selected = selected == "update", memory = memory, key = "update",
                    onFocused = { selected = "update" },
                    onClick = { if (updateState is UpdateState.None) app.updates.checkNow() else app.updates.install() },
                )
            }
            item(key = "h4") { GroupHeading("ACCOUNT") }
            item(key = "logout") {
                PanelRow(
                    label = "Log Out", value = null, selected = false, memory = memory, key = "logout",
                    onFocused = { selected = "logout" },
                    onClick = {
                        scope.launch {
                            runCatching { app.api.logout() }.onFailure { Timber.w("logout: %s", it.message) }
                            app.prefs.clearSession()
                            onLoggedOut()
                        }
                    },
                )
            }
        }
        // The chosen setting's options, then what this build is.
        Column(modifier = Modifier.weight(1f).fillMaxHeight().padding(start = 60.dp, top = 52.dp, end = 60.dp)) {
            when {
                current != null -> {
                    Text(current.label, color = RoxyColors.onBackground, fontSize = 26.sp, fontWeight = FontWeight.Bold)
                    Text(current.hint ?: "", color = RoxyColors.muted, fontSize = 14.sp, lineHeight = 19.sp, minLines = 2, maxLines = 3, modifier = Modifier.padding(top = 6.dp).widthIn(max = 560.dp))
                    Spacer(Modifier.height(22.dp))
                    LazyColumn(modifier = Modifier.weight(1f).width(440.dp), contentPadding = PaddingValues(bottom = 16.dp)) {
                        items(current.options, key = { it.first }) { (value, label) ->
                            OptionRow(label, chosen = value == current.value, onClick = { current.apply(value) }, modifier = Modifier.focusKey(memory, "opt/${current.key}/$value"))
                        }
                    }
                }
                selected == "update" -> {
                    UpdatePane(updateState, updateNote, onInstall = { app.updates.install() }, onCheck = { app.updates.checkNow() }, memory = memory)
                    Spacer(Modifier.weight(1f))
                }
                else -> {
                    Text("Log Out", color = RoxyColors.onBackground, fontSize = 26.sp, fontWeight = FontWeight.Bold)
                    Text("Signs this TV out of ${session.serverUrl}. Downloads and settings on the TV are kept.", color = RoxyColors.muted, fontSize = 14.sp, lineHeight = 19.sp, modifier = Modifier.padding(top = 6.dp).widthIn(max = 560.dp))
                    Spacer(Modifier.weight(1f))
                }
            }
            AboutBlock(session.userName, session.serverUrl, context)
        }
    }
}

/** The update row's page at the right: the state, release notes, and Update / Install / Retry. */
@Composable
private fun UpdatePane(state: UpdateState, note: String?, onInstall: () -> Unit, onCheck: () -> Unit, memory: FocusMemory) {
    val update = when (state) {
        is UpdateState.Available -> state.update
        is UpdateState.Downloading -> state.update
        is UpdateState.Ready -> state.update
        is UpdateState.Failed -> state.update
        UpdateState.None -> null
    }
    Text("Check for updates", color = RoxyColors.onBackground, fontSize = 26.sp, fontWeight = FontWeight.Bold)
    val line = when (state) {
        is UpdateState.Available -> "Roxy ${update!!.versionName} is available. You have ${BuildConfig.VERSION_NAME}."
        is UpdateState.Downloading -> "Downloading Roxy ${update!!.versionName}  ·  ${(state.fraction * 100).toInt()}%"
        is UpdateState.Ready -> "Roxy ${update!!.versionName} is downloaded. Install opens Android's installer; the first time, it asks to allow installs from Roxy."
        is UpdateState.Failed -> "The download failed: ${state.message}"
        UpdateState.None -> note ?: "Builds published on your server install from here. You have ${BuildConfig.VERSION_NAME} (build ${BuildConfig.VERSION_CODE})."
    }
    Text(line, color = RoxyColors.muted, fontSize = 14.sp, lineHeight = 19.sp, modifier = Modifier.padding(top = 6.dp).widthIn(max = 560.dp))
    if (update != null && update.notes.isNotBlank()) {
        Text("WHAT'S NEW", color = RoxyColors.muted, fontSize = 11.sp, fontWeight = FontWeight.Bold, letterSpacing = 1.sp, modifier = Modifier.padding(top = 22.dp, bottom = 4.dp))
        Text(update.notes, color = RoxyColors.onBackground, fontSize = 15.sp, lineHeight = 21.sp, maxLines = 8, overflow = TextOverflow.Ellipsis, modifier = Modifier.widthIn(max = 560.dp))
    }
    Spacer(Modifier.height(22.dp))
    Row {
        when (state) {
            UpdateState.None -> OptionRow("Check now", chosen = false, icon = Icons.Outlined.SystemUpdate, onClick = onCheck, modifier = Modifier.width(300.dp).focusKey(memory, "opt/update/check"))
            is UpdateState.Downloading -> OptionRow("Downloading…", chosen = false, icon = Icons.Outlined.SystemUpdate, onClick = {}, modifier = Modifier.width(300.dp).focusKey(memory, "opt/update/wait"))
            is UpdateState.Ready -> OptionRow("Install", chosen = false, icon = Icons.Outlined.SystemUpdate, onClick = onInstall, modifier = Modifier.width(300.dp).focusKey(memory, "opt/update/install"))
            is UpdateState.Failed -> OptionRow("Retry", chosen = false, icon = Icons.Outlined.SystemUpdate, onClick = onInstall, modifier = Modifier.width(300.dp).focusKey(memory, "opt/update/retry"))
            is UpdateState.Available -> OptionRow("Update to ${update!!.versionName}", chosen = false, icon = Icons.Outlined.SystemUpdate, onClick = onInstall, modifier = Modifier.width(300.dp).focusKey(memory, "opt/update/go"))
        }
    }
}

/** This build, where it is signed in, and the TV it runs on, as labelled lines. */
@Composable
private fun AboutBlock(user: String, server: String, context: android.content.Context) {
    val device = remember { "${Build.MANUFACTURER} ${Build.MODEL}  ·  Android ${Build.VERSION.RELEASE}  ·  " + (if (Build.VERSION.SDK_INT >= 31) "${Build.SOC_MANUFACTURER} ${Build.SOC_MODEL}" else Build.HARDWARE) }
    val display = remember { DeviceReport.display(context).removePrefix("display: ").substringBefore(" modes=") + "  ·  " + DeviceReport.display(context).substringAfter("hdr=[").substringBefore("]").replace(",", ", ").ifEmpty { "no HDR" } }
    Row(modifier = Modifier.padding(bottom = 28.dp)) {
        AboutColumn("ROXY TV", "${BuildConfig.VERSION_NAME}  ·  build ${BuildConfig.VERSION_CODE}")
        Spacer(Modifier.width(40.dp))
        AboutColumn("SIGNED IN", "$user  ·  ${server.removePrefix("http://").removePrefix("https://")}")
        Spacer(Modifier.width(40.dp))
        AboutColumn("THIS TV", "$device\n$display")
    }
}

@Composable
private fun AboutColumn(heading: String, body: String) {
    Column {
        Text(heading, color = RoxyColors.muted, fontSize = 10.sp, fontWeight = FontWeight.Bold, letterSpacing = 1.sp)
        Text(body, color = RoxyColors.muted, fontSize = 12.sp, lineHeight = 16.sp, maxLines = 2, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 3.dp).widthIn(max = 420.dp))
    }
}

@Composable
private fun GroupHeading(text: String) {
    Text(text, color = RoxyColors.onBackground, fontSize = 11.sp, fontWeight = FontWeight.Bold, letterSpacing = 1.sp, modifier = Modifier.padding(start = RowStart, top = 26.dp, bottom = 4.dp))
}

/** A setting in the panel: focus shows its options at the right; the current value reads at the right of the line. */
@Composable
private fun SettingRow(s: Setting, selected: Boolean, memory: FocusMemory, onSelect: () -> Unit) {
    val valueLabel = s.options.firstOrNull { it.first == s.value }?.second ?: s.value
    PanelRow(label = s.label, value = valueLabel.substringBefore(" ("), selected = selected, memory = memory, key = "set/${s.key}", onFocused = onSelect, onClick = onSelect)
}

/** One line of the panel: the label at the left, the value (muted, or dark on the accent) at the right. */
@Composable
private fun PanelRow(label: String, value: String?, selected: Boolean, memory: FocusMemory, key: String, onFocused: () -> Unit, onClick: () -> Unit) {
    TvFocusable(onClick = onClick, modifier = Modifier.fillMaxWidth().focusKey(memory, key), focusedBackground = RoxyColors.accent, onFocused = onFocused) { focused ->
        Row(modifier = Modifier.fillMaxWidth().height(RowHeight).padding(start = RowStart, end = 20.dp), verticalAlignment = Alignment.CenterVertically) {
            Text(label, color = if (focused) OnAccent else if (selected) RoxyColors.onBackground else RoxyColors.muted, fontSize = 18.sp, fontWeight = FontWeight.Medium, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
            if (value != null) Text(value, color = if (focused) OnAccent.copy(alpha = 0.85f) else RoxyColors.muted, fontSize = 14.sp, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(start = 12.dp).widthIn(max = 110.dp))
        }
    }
}

/** One option at the right: the chosen one reads white with an accent tick. */
@Composable
private fun OptionRow(label: String, chosen: Boolean, onClick: () -> Unit, modifier: Modifier = Modifier, icon: androidx.compose.ui.graphics.vector.ImageVector? = null) {
    TvFocusable(onClick = onClick, modifier = modifier.fillMaxWidth(), focusedBackground = RoxyColors.accent) { focused ->
        Row(modifier = Modifier.fillMaxWidth().height(48.dp).padding(start = 16.dp, end = 14.dp), verticalAlignment = Alignment.CenterVertically) {
            if (icon != null) { Icon(icon, contentDescription = null, tint = if (focused) OnAccent else RoxyColors.onBackground, modifier = Modifier.width(22.dp)); Spacer(Modifier.width(12.dp)) }
            Text(label, color = if (focused) OnAccent else if (chosen) RoxyColors.onBackground else RoxyColors.muted, fontSize = 18.sp, fontWeight = if (chosen) FontWeight.SemiBold else FontWeight.Medium, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
            if (chosen) Icon(Icons.Filled.Check, contentDescription = "Selected", tint = if (focused) OnAccent else RoxyColors.accent, modifier = Modifier.width(22.dp))
        }
    }
}
