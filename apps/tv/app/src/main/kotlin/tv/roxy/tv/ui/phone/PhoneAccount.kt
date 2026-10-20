package tv.roxy.tv.ui.phone

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
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
import tv.roxy.tv.data.Languages
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.material.icons.outlined.ClosedCaption
import androidx.compose.material.icons.filled.Search
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.layout.heightIn
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.draw.clip
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.material.icons.outlined.Wifi
import androidx.compose.material.icons.outlined.Webhook
import androidx.compose.material.icons.outlined.Tune
import androidx.compose.material.icons.outlined.Translate
import androidx.compose.material.icons.outlined.SwitchAccount
import androidx.compose.material.icons.outlined.Subtitles
import androidx.compose.material.icons.outlined.Shield
import androidx.compose.material.icons.outlined.PlaylistPlay
import androidx.compose.material.icons.outlined.SkipNext
import androidx.compose.material.icons.outlined.Timer
import androidx.compose.material.icons.outlined.Info
import androidx.compose.material.icons.outlined.FastForward
import androidx.compose.material.icons.outlined.Download
import androidx.compose.material.icons.outlined.Dns
import androidx.compose.material.icons.outlined.Description
import androidx.compose.material.icons.filled.ExpandMore
import androidx.compose.material.icons.filled.ExpandLess
import androidx.compose.material.icons.automirrored.outlined.Logout
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.background
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.BuildConfig
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.Profile
import tv.roxy.tv.data.PlayerSettings
import tv.roxy.tv.data.downloads.formatBytes
import tv.roxy.tv.ui.components.widthPx
import tv.roxy.tv.ui.profiles.AvatarCircle
import tv.roxy.tv.ui.profiles.ProfilePicker
import tv.roxy.tv.ui.theme.RoxyColors

private class Pick(val title: String, val options: List<String>, val selected: String, val onPick: (String) -> Unit)

/**
 * Account tab: profile header, then the settings as a phone list. Items that need server
 * work (profiles, notifications, password, email, legal pages) are listed greyed with
 * "Coming soon" so the shape of the page is already right.
 */
@Composable
fun PhoneAccount(bottomInset: Dp, onOpenDownloads: () -> Unit = {}, onOpenArrSettings: () -> Unit = {}, onOpenWebhooks: () -> Unit = {}, onOpenLegal: (String) -> Unit = {}) {
    val context = LocalContext.current
    val app = RoxyApp.get(context)
    val settings by app.prefs.settings.collectAsStateWithLifecycle()
    val session by app.prefs.session.collectAsStateWithLifecycle()
    val downloadIndex by app.downloads.index.collectAsStateWithLifecycle()
    val wifiOnly by app.prefs.downloadsWifiOnly.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    var pick by remember { mutableStateOf<Pick?>(null) }
    var editingProfile by remember { mutableStateOf(false) }

    fun save(s: PlayerSettings) = scope.launch { app.prefs.saveSettings(s) }

    if (editingProfile) {
        val id = session.profileId
        if (id != null) {
            ProfilePicker(editOnly = Profile(id = id, name = session.profileName, hasAvatar = session.profileHasAvatar, avatarKey = session.profileAvatarKey), onDone = { editingProfile = false })
            return
        }
        editingProfile = false
    }

    var advanced by rememberSaveable { mutableStateOf(false) }
    var langPick by remember { mutableStateOf<String?>(null) } // "audio" | "sub"
    var confirmLogout by remember { mutableStateOf(false) }

    Column(modifier = Modifier.fillMaxSize().verticalScroll(rememberScrollState()).statusBarsPadding().padding(bottom = bottomInset + 24.dp)) {
        Text("Account", fontSize = 24.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(horizontal = PhonePad, vertical = 10.dp))

        // Profile header card: avatar, profile name, the login and server underneath.
        Row(
            modifier = Modifier.padding(horizontal = PhonePad).fillMaxWidth().clip(RoundedCornerShape(16.dp)).background(RoxyColors.surface).clickable { editingProfile = true }.padding(16.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            val avatarUrl = session.profileId?.takeIf { session.profileHasAvatar }?.let { app.api.profileAvatarUrl(it, widthPx(60.dp) * 2, session.profileAvatarKey.ifEmpty { session.profileAvatarVersion.toString() }) }
            AvatarCircle(name = session.profileName.ifEmpty { session.userName }, imageUrl = avatarUrl, size = 60.dp)
            Column(modifier = Modifier.weight(1f).padding(start = 14.dp)) {
                Text(session.profileName.ifEmpty { session.userName }, fontSize = 20.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground)
                Text(session.userName + "  ·  " + session.serverUrl.removePrefix("http://").removePrefix("https://"), fontSize = 12.sp, color = RoxyColors.muted, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text("Edit profile", fontSize = 13.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.accent, modifier = Modifier.padding(top = 4.dp))
            }
            Icon(Icons.Filled.ChevronRight, contentDescription = null, tint = RoxyColors.muted)
        }
        GroupCard(title = null) {
            SettingRow(Icons.Outlined.SwitchAccount, "Switch profile", "Back to \"Who's watching?\"") { scope.launch { app.prefs.clearProfile() } }
        }

        GroupCard("Playback") {
            SettingRow(Icons.Outlined.Translate, "Audio language", Languages.name(settings.audioLangs.firstOrNull() ?: "eng")) { langPick = "audio" }
            Divider()
            SettingRow(Icons.Outlined.Subtitles, "Subtitle language", Languages.name(settings.subLang)) { langPick = "sub" }
            Divider()
            SettingRow(Icons.Outlined.ClosedCaption, "Subtitles", PlayerSettings.subModeLabel(settings.subMode) + "  ·  " + PlayerSettings.subModeHint(settings.subMode)) {
                pick = Pick("Subtitles", PlayerSettings.subModeChoices.map { PlayerSettings.subModeLabel(it) }, PlayerSettings.subModeLabel(settings.subMode)) { picked ->
                    PlayerSettings.subModeChoices.firstOrNull { PlayerSettings.subModeLabel(it) == picked }?.let { save(settings.copy(subMode = it)) }
                }
            }
            Divider()
            SwitchRow(Icons.Outlined.FastForward, "Auto-skip intro & outro", settings.autoSkip) { save(settings.copy(autoSkip = it)) }
            if (!settings.autoSkip) {
                Divider()
                SettingRow(Icons.Outlined.Timer, "Skip button stays for", PlayerSettings.skipWindowLabel(settings.skipWindowSec)) {
                    pick = Pick("Skip button stays for", PlayerSettings.skipWindowChoices.map { PlayerSettings.skipWindowLabel(it) }, PlayerSettings.skipWindowLabel(settings.skipWindowSec)) { picked ->
                        PlayerSettings.skipWindowChoices.firstOrNull { PlayerSettings.skipWindowLabel(it) == picked }?.let { save(settings.copy(skipWindowSec = it)) }
                    }
                }
            }
            Divider()
            SwitchRow(Icons.Outlined.PlaylistPlay, "Autoplay next episode", settings.autoplay) { save(settings.copy(autoplay = it)) }
            Divider()
            SwitchRow(Icons.Outlined.SkipNext, "Next-episode countdown card", settings.nextCard) { save(settings.copy(nextCard = it)) }
            Divider()
            SettingRow(Icons.Outlined.Tune, "Advanced", if (advanced) "Hide decoder and debug options" else "Decoder, video output, stats overlay", trailing = if (advanced) Icons.Filled.ExpandLess else Icons.Filled.ExpandMore) { advanced = !advanced }
            if (advanced) {
                Divider()
                SettingRow(null, "Hardware decoding", settings.hwdec) {
                    pick = Pick("Hardware decoding", PlayerSettings.hwdecChoices, settings.hwdec) { save(settings.copy(hwdec = it)) }
                }
                Divider()
                SettingRow(null, "Video output", settings.vo) {
                    pick = Pick("Video output", PlayerSettings.voChoices, settings.vo) { save(settings.copy(vo = it)) }
                }
                Divider()
                SwitchRow(null, "Stats overlay", settings.showStats) { save(settings.copy(showStats = it)) }
            }
        }

        GroupCard("Downloads") {
            val dlDone = downloadIndex.entries.values.count { it.isDone }
            val dlActive = downloadIndex.entries.values.count { it.state.isActive }
            val dlLine = buildList {
                add(if (dlDone == 0) "Nothing downloaded yet" else "$dlDone ${if (dlDone == 1) "item" else "items"}  ·  ${formatBytes(app.downloads.usedBytes())}")
                if (dlActive > 0) add("$dlActive downloading")
            }.joinToString("  ·  ")
            SettingRow(Icons.Outlined.Download, "Downloads", dlLine, onClick = onOpenDownloads)
            Divider()
            SwitchRow(Icons.Outlined.Wifi, "Download over Wi-Fi only", wifiOnly) { scope.launch { app.prefs.setDownloadsWifiOnly(it) } }
        }

        if (session.isAdmin) {
            GroupCard("Server") {
                SettingRow(Icons.Outlined.Dns, "Sonarr & Radarr", "Where Discover requests go", onClick = onOpenArrSettings)
                Divider()
                SettingRow(Icons.Outlined.Webhook, "Webhooks", "How imports reach the library without a rescan", onClick = onOpenWebhooks)
            }
        }

        GroupCard("About") {
            SettingRow(Icons.Outlined.Info, "Version", "${BuildConfig.VERSION_NAME}  ·  ${BuildConfig.APPLICATION_ID}", trailing = null) {}
            Divider()
            SettingRow(Icons.Outlined.Description, "Terms of Use", if (session.acceptedTerms.isNotEmpty()) "Accepted  ·  version ${session.acceptedTerms}" else "Read the terms") { onOpenLegal("terms") }
            Divider()
            SettingRow(Icons.Outlined.Shield, "Privacy", "What stays on your server and your device") { onOpenLegal("privacy") }
        }

        // Log out stands alone at the end, in red, with a confirmation.
        Row(
            modifier = Modifier.padding(horizontal = PhonePad, vertical = 12.dp).fillMaxWidth().clip(RoundedCornerShape(16.dp)).background(RoxyColors.surface).clickable { confirmLogout = true }.padding(horizontal = 16.dp, vertical = 15.dp),
            verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.Center,
        ) {
            Icon(Icons.AutoMirrored.Outlined.Logout, contentDescription = null, tint = RoxyColors.error, modifier = Modifier.size(20.dp))
            Text("Log out", fontSize = 16.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.error, modifier = Modifier.padding(start = 8.dp))
        }
        Text("Signed in as ${session.userName}", fontSize = 12.sp, color = RoxyColors.muted, textAlign = TextAlign.Center, modifier = Modifier.fillMaxWidth())
    }

    langPick?.let { which ->
        LanguagePickerDialog(
            title = if (which == "audio") "Audio language" else "Subtitle language",
            selected = if (which == "audio") settings.audioLangs.firstOrNull() ?: "eng" else settings.subLang,
            onDismiss = { langPick = null },
            onPick = { code ->
                if (which == "audio") save(settings.copy(audioLangs = listOf(code))) else save(settings.copy(subLang = code))
                langPick = null
            },
        )
    }

    if (confirmLogout) {
        AlertDialog(
            onDismissRequest = { confirmLogout = false },
            containerColor = RoxyColors.surfaceHigh, titleContentColor = RoxyColors.onBackground, textContentColor = RoxyColors.onBackground,
            title = { Text("Log out?") },
            text = { Text("Every profile on this login signs out of this device. Downloads stay until you remove them.") },
            confirmButton = {
                TextButton(onClick = {
                    confirmLogout = false
                    scope.launch {
                        runCatching { app.api.logout() }.onFailure { Timber.w("logout: %s", it.message) }
                        app.prefs.clearSession() // AppRoot switches to the login screen
                    }
                }) { Text("Log out", color = RoxyColors.error) }
            },
            dismissButton = { TextButton(onClick = { confirmLogout = false }) { Text("Cancel", color = Color.White) } },
        )
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

/** A rounded card holding related rows, with an optional small caps-style title above it. */
@Composable
private fun GroupCard(title: String?, content: @Composable ColumnScope.() -> Unit) {
    if (title != null) Text(title.uppercase(), fontSize = 11.sp, fontWeight = FontWeight.SemiBold, letterSpacing = 1.2.sp, color = RoxyColors.muted, modifier = Modifier.padding(start = PhonePad + 4.dp, top = 22.dp, bottom = 8.dp))
    else Spacer(Modifier.height(10.dp))
    Column(modifier = Modifier.padding(horizontal = PhonePad).fillMaxWidth().clip(RoundedCornerShape(16.dp)).background(RoxyColors.surface), content = content)
}

@Composable
private fun Divider() {
    Box(modifier = Modifier.fillMaxWidth().padding(start = 52.dp).height(1.dp).background(Color.White.copy(alpha = 0.06f)))
}

@Composable
private fun SettingRow(icon: ImageVector?, label: String, value: String, trailing: ImageVector? = Icons.Filled.ChevronRight, onClick: () -> Unit) {
    Row(
        modifier = Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 13.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(modifier = Modifier.width(36.dp), contentAlignment = Alignment.CenterStart) {
            if (icon != null) Icon(icon, contentDescription = null, tint = RoxyColors.accent, modifier = Modifier.size(22.dp))
        }
        Column(modifier = Modifier.weight(1f)) {
            Text(label, fontSize = 16.sp, color = RoxyColors.onBackground)
            if (value.isNotEmpty()) Text(value, fontSize = 13.sp, color = RoxyColors.muted, maxLines = 2, overflow = TextOverflow.Ellipsis)
        }
        if (trailing != null) Icon(trailing, contentDescription = null, tint = RoxyColors.muted)
    }
}

@Composable
private fun SwitchRow(icon: ImageVector?, label: String, checked: Boolean, onChange: (Boolean) -> Unit) {
    Row(
        modifier = Modifier.fillMaxWidth().clickable { onChange(!checked) }.padding(start = 16.dp, end = 12.dp, top = 6.dp, bottom = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(modifier = Modifier.width(36.dp), contentAlignment = Alignment.CenterStart) {
            if (icon != null) Icon(icon, contentDescription = null, tint = RoxyColors.accent, modifier = Modifier.size(22.dp))
        }
        Text(label, fontSize = 16.sp, color = RoxyColors.onBackground, modifier = Modifier.weight(1f))
        Switch(
            checked = checked, onCheckedChange = onChange,
            colors = SwitchDefaults.colors(checkedThumbColor = Color.White, checkedTrackColor = RoxyColors.accent, uncheckedThumbColor = RoxyColors.muted, uncheckedTrackColor = ChipGrey),
        )
    }
}

/** Searchable list of every language, common ones first; the current choice ticked. */
@Composable
fun LanguagePickerDialog(title: String, selected: String, onDismiss: () -> Unit, onPick: (String) -> Unit) {
    var query by remember { mutableStateOf("") }
    val shown = remember(query) {
        val q = query.trim()
        if (q.isEmpty()) Languages.all else Languages.all.filter { it.name.contains(q, ignoreCase = true) || it.iso3.startsWith(q.lowercase()) || it.iso2 == q.lowercase() }
    }
    AlertDialog(
        onDismissRequest = onDismiss,
        containerColor = RoxyColors.surfaceHigh, titleContentColor = RoxyColors.onBackground, textContentColor = RoxyColors.onBackground,
        title = { Text(title) },
        text = {
            Column {
                Row(modifier = Modifier.fillMaxWidth().height(40.dp).clip(RoundedCornerShape(20.dp)).background(RoxyColors.surface).padding(horizontal = 12.dp), verticalAlignment = Alignment.CenterVertically) {
                    Icon(Icons.Filled.Search, contentDescription = null, tint = RoxyColors.muted, modifier = Modifier.size(18.dp))
                    Box(modifier = Modifier.weight(1f).padding(start = 8.dp)) {
                        if (query.isEmpty()) Text("Search languages", color = RoxyColors.muted, fontSize = 14.sp)
                        BasicTextField(value = query, onValueChange = { query = it }, singleLine = true, textStyle = TextStyle(color = RoxyColors.onBackground, fontSize = 14.sp), cursorBrush = SolidColor(RoxyColors.accent), modifier = Modifier.fillMaxWidth())
                    }
                }
                LazyColumn(modifier = Modifier.fillMaxWidth().heightIn(max = 420.dp).padding(top = 8.dp)) {
                    items(shown, key = { it.iso3 }) { l ->
                        Row(modifier = Modifier.fillMaxWidth().clickable { onPick(l.iso3) }.padding(vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
                            RadioButton(selected = l.iso3 == Languages.canonical(selected), onClick = null, colors = RadioButtonDefaults.colors(selectedColor = RoxyColors.accent, unselectedColor = RoxyColors.muted))
                            Text(l.name, fontSize = 15.sp, color = RoxyColors.onBackground, modifier = Modifier.weight(1f).padding(start = 10.dp))
                            Text(l.iso3, fontSize = 12.sp, color = RoxyColors.muted)
                        }
                    }
                }
            }
        },
        confirmButton = { TextButton(onClick = onDismiss) { Text("Cancel", color = Color.White) } },
    )
}

