package tv.roxy.tv.ui.phone

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
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ArrowDropDown
import androidx.compose.material.icons.filled.Check
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.foundation.layout.offset
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.ArrConfig
import tv.roxy.tv.api.ArrNamed
import tv.roxy.tv.api.ArrProbe
import tv.roxy.tv.api.ArrSettings
import tv.roxy.tv.data.downloads.formatBytes
import tv.roxy.tv.ui.components.RoxyTextField
import tv.roxy.tv.ui.theme.RoxyColors

/**
 * Admin page: the Sonarr and Radarr servers Discover requests go to, laid out like
 * request app's Services page. Enter the URL and API key, Test connection, then pick the root
 * folder and quality profile (and the anime ones for Sonarr) from what the server
 * reports. Save stores it on Roxy Server and pushes any pending requests.
 */
@Composable
fun PhoneArrSettings(onBack: () -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    val scope = rememberCoroutineScope()
    var settings by remember { mutableStateOf<ArrSettings?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var saved by remember { mutableStateOf(false) }
    var tab by rememberSaveable { mutableStateOf("sonarr") }

    LaunchedEffect(Unit) {
        runCatching { app.api.arrSettings() }
            .onSuccess { settings = ArrSettings(it.sonarr, it.radarr) }
            .onFailure { t -> Timber.w(t, "arr settings"); error = t.message }
    }

    Column(modifier = Modifier.fillMaxSize().statusBarsPadding()) {
        Row(modifier = Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
            BackCircle(onBack)
            Text("Sonarr & Radarr", fontSize = 20.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(start = 12.dp))
        }
        Row(modifier = Modifier.padding(horizontal = PhonePad, vertical = 4.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Chip("Sonarr (series)", selected = tab == "sonarr", onClick = { tab = "sonarr" })
            Chip("Radarr (movies)", selected = tab == "radarr", onClick = { tab = "radarr" })
        }
        val s = settings
        when {
            s == null && error != null -> Text(error ?: "", color = RoxyColors.error, modifier = Modifier.padding(PhonePad))
            s == null -> Column(modifier = Modifier.padding(PhonePad)) { BoneLine(width = 300.dp, height = 40.dp); BoneLine(width = 300.dp, height = 40.dp, modifier = Modifier.padding(top = 12.dp)) }
            else -> {
                val cfg = if (tab == "sonarr") s.sonarr else s.radarr
                ArrForm(
                    kind = tab, cfg = cfg,
                    onChange = { c -> settings = if (tab == "sonarr") s.copy(sonarr = c) else s.copy(radarr = c); saved = false },
                    onSave = {
                        scope.launch {
                            runCatching { app.api.saveArrSettings(settings ?: return@launch) }
                                .onSuccess { r -> settings = ArrSettings(r.sonarr, r.radarr); saved = true; error = null }
                                .onFailure { t -> error = t.message }
                        }
                    },
                    saved = saved, error = error,
                )
            }
        }
    }
}

private val availabilityChoices = listOf("announced" to "Announced", "inCinemas" to "In cinemas", "released" to "Released")

@Composable
private fun ArrForm(kind: String, cfg: ArrConfig, onChange: (ArrConfig) -> Unit, onSave: () -> Unit, saved: Boolean, error: String?) {
    val app = RoxyApp.get(LocalContext.current)
    val scope = rememberCoroutineScope()
    var probe by remember(kind) { mutableStateOf<ArrProbe?>(null) }
    var testing by remember { mutableStateOf(false) }
    var testError by remember(kind) { mutableStateOf<String?>(null) }
    val isSonarr = kind == "sonarr"
    val name = if (isSonarr) "Sonarr" else "Radarr"

    Column(modifier = Modifier.fillMaxSize().verticalScroll(rememberScrollState()).imePadding().navigationBarsPadding().padding(horizontal = PhonePad, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(14.dp)) {
        Text(
            if (isSonarr) "Series requests are added to Sonarr, monitored, and searched season by season. Anime (Animation from Japan) uses the anime root folder and profile when set. A show Sonarr has but doesn't monitor is left alone."
            else "Movie requests are added to Radarr, monitored, with a search. Anime films use the anime root folder and profile when set.",
            fontSize = 13.sp, lineHeight = 18.sp, color = RoxyColors.muted,
        )
        Text(
            if (isSonarr) "Works with Sonarr v4 (tested with 4.0). A whole-series request also follows seasons that air later; a single-season request does not."
            else "Works with Radarr v5 and v6 (tested with 6.4).",
            fontSize = 12.sp, lineHeight = 16.sp, color = RoxyColors.muted,
        )
        RoxyTextField(cfg.url, { onChange(cfg.copy(url = it)) }, label = "$name URL (http://host:port, with any URL base)", keyboardType = KeyboardType.Uri)
        RoxyTextField(cfg.apiKey, { onChange(cfg.copy(apiKey = it)) }, label = "API key", password = true, imeAction = ImeAction.Done)
        // The key is Sonarr's / Radarr's own: link to the page that shows it.
        val base = cfg.externalUrl.trim().ifEmpty { cfg.url.trim() }.trimEnd('/')
        val keyPage = base.takeIf { Regex("^https?://[^/]", RegexOption.IGNORE_CASE).containsMatchIn(it) }?.let { "$it/settings/general" }
        val uri = LocalUriHandler.current
        Text("$name's own key: in $name, Settings → General → Security → API Key. A saved key stays unless you type a new one.", fontSize = 12.sp, lineHeight = 16.sp, color = RoxyColors.muted)
        if (keyPage != null) {
            Text("Open $name's General settings", fontSize = 13.sp, fontWeight = FontWeight.Bold, color = RoxyColors.accent, modifier = Modifier.clickable { runCatching { uri.openUri(keyPage) } })
        }
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            GreyButton(if (testing) "Testing…" else "Test connection", onClick = {
                if (testing) return@GreyButton
                testing = true; testError = null
                scope.launch {
                    runCatching { app.api.testArr(kind, cfg.url.trim(), cfg.apiKey) }
                        .onSuccess { p -> probe = p }
                        .onFailure { t -> testError = t.message ?: "failed" }
                    testing = false
                }
            })
            probe?.let { Text("${it.appName.ifEmpty { name }} ${it.version}", fontSize = 13.sp, color = if (it.supported) RoxyColors.accent else RoxyColors.error) }
        }
        probe?.note?.let { Text(it, fontSize = 12.sp, lineHeight = 16.sp, color = RoxyColors.error) }
        testError?.let { Text(it, fontSize = 13.sp, color = RoxyColors.error) }
        val p = probe
        if (p != null) {
            Picker("Root folder (where $name saves downloads)", listOf("" to "Choose…") + p.rootFolders.map { it.path to it.path + (it.freeSpace.takeIf { f -> f > 0 }?.let { f -> "  ·  ${formatBytes(f)} free" } ?: "") }, cfg.rootFolder) { onChange(cfg.copy(rootFolder = it)) }
            if (cfg.rootFolder.isNotEmpty()) RootFolderCheck(p.rootFolders.firstOrNull { it.path == cfg.rootFolder }, name)
            Picker("Quality profile", listOf("0" to "Choose…") + p.qualityProfiles.map { it.id.toString() to it.name }, cfg.qualityProfileId.toString()) { onChange(cfg.copy(qualityProfileId = it.toIntOrNull() ?: 0)) }
            // Anime goes to its own folder and profile on both (Radarr: an "Anime Movies" folder).
            Picker(if (isSonarr) "Anime root folder" else "Anime movies root folder", listOf("" to "Same as above") + p.rootFolders.map { it.path to it.path }, cfg.animeRootFolder) { onChange(cfg.copy(animeRootFolder = it)) }
            if (cfg.animeRootFolder.isNotEmpty()) RootFolderCheck(p.rootFolders.firstOrNull { it.path == cfg.animeRootFolder }, name)
            Picker(if (isSonarr) "Anime quality profile" else "Anime movies quality profile", listOf("0" to "Same as above") + p.qualityProfiles.map { it.id.toString() to it.name }, cfg.animeQualityProfileId.toString()) { onChange(cfg.copy(animeQualityProfileId = it.toIntOrNull() ?: 0)) }
            if (isSonarr) {
                Picker("Older anime quality profile (before 2010, ended)", listOf("0" to "Same as anime") + p.qualityProfiles.map { it.id.toString() to it.name }, cfg.oldAnimeQualityProfileId.toString()) { onChange(cfg.copy(oldAnimeQualityProfileId = it.toIntOrNull() ?: 0)) }
                Picker("Anime series type", listOf("standard" to "Standard (recommended)", "anime" to "Anime (absolute numbering)"), if (cfg.animeSeriesType == "anime") "anime" else "standard") { onChange(cfg.copy(animeSeriesType = it)) }
                Hint(if (cfg.animeSeriesType == "anime") "Sonarr names and searches by absolute episode numbers (Episode 1047)." else "Sonarr names and searches by season and episode (S21E1047), like the rest of your library.")
                if (p.tags.isNotEmpty()) {
                    TagPicker("Tags", p.tags, cfg.tags) { onChange(cfg.copy(tags = it)) }
                    TagPicker("Anime tags", p.tags, cfg.animeTags) { onChange(cfg.copy(animeTags = it)) }
                }
            } else if (p.tags.isNotEmpty()) {
                TagPicker("Tags", p.tags, cfg.tags) { onChange(cfg.copy(tags = it)) }
            }
        } else {
            Text(
                if (cfg.rootFolder.isNotEmpty() || cfg.qualityProfileId != 0) "Saved: root folder ${cfg.rootFolder.ifEmpty { "(first reported)" }}, quality profile ${cfg.qualityProfileId.takeIf { it != 0 } ?: "(first reported)"}. Test the connection to change them."
                else "Test the connection to pick a root folder and quality profile. Without a choice the first ones $name reports are used.",
                fontSize = 12.sp, lineHeight = 16.sp, color = RoxyColors.muted,
            )
        }
        if (!isSonarr) {
            Picker("Minimum availability", availabilityChoices, cfg.minimumAvailability.ifEmpty { "released" }) { onChange(cfg.copy(minimumAvailability = it)) }
            Hint("How early Radarr starts looking for a movie.")
        }
        if (isSonarr) ToggleRow("Season folders", cfg.seasonFolders ?: true) { onChange(cfg.copy(seasonFolders = it)) }
        ToggleRow("Search when added", cfg.searchOnAdd ?: true) { onChange(cfg.copy(searchOnAdd = it)) }
        Hint(if (isSonarr) "Sonarr searches each requested season as soon as a request arrives (season packs first)." else "Radarr starts looking as soon as a request arrives.")
        RoxyTextField(cfg.externalUrl, { onChange(cfg.copy(externalUrl = it)) }, label = "External URL (optional, for links)", keyboardType = KeyboardType.Uri, imeAction = ImeAction.Done)

        Spacer(Modifier.height(4.dp))
        PlayButton(if (saved) "Saved" else "Save", icon = Icons.Filled.Check, pill = true, container = RoxyColors.accent, content = OnBrand, modifier = Modifier.fillMaxWidth(), onClick = onSave)
        error?.let { Text(it, fontSize = 13.sp, color = RoxyColors.error) }
        Text("Saving sends any requests made earlier. Leave the URL empty to disconnect; requesting is then turned off.", fontSize = 12.sp, lineHeight = 16.sp, color = RoxyColors.muted)
        Spacer(Modifier.height(24.dp))
    }
}

@Composable
private fun Hint(text: String) = Text(text, fontSize = 12.sp, lineHeight = 16.sp, color = RoxyColors.muted, modifier = Modifier.offset(y = (-8).dp))

/** Label + dropdown chip over (value, label) pairs. */
@Composable
private fun Picker(label: String, options: List<Pair<String, String>>, value: String, onPick: (String) -> Unit) {
    var open by remember { mutableStateOf(false) }
    Column {
        Text(label, fontSize = 12.sp, color = RoxyColors.muted, modifier = Modifier.padding(bottom = 4.dp))
        Box {
            Chip(options.firstOrNull { it.first == value }?.second ?: options.firstOrNull()?.second ?: "—", selected = false, trailing = Icons.Filled.ArrowDropDown, onClick = { open = true })
            DropdownMenu(expanded = open, onDismissRequest = { open = false }, containerColor = RoxyColors.surfaceHigh) {
                options.forEach { (v, l) ->
                    DropdownMenuItem(text = { Text(l, color = Color.White, maxLines = 1, overflow = TextOverflow.Ellipsis, fontWeight = if (v == value) FontWeight.Bold else FontWeight.Normal) }, onClick = { open = false; onPick(v) })
                }
            }
        }
    }
}

/** Multi-select chips over the server's tags. */
@Composable
private fun TagPicker(label: String, tags: List<ArrNamed>, selected: List<Int>, onChange: (List<Int>) -> Unit) {
    Column {
        Text(label, fontSize = 12.sp, color = RoxyColors.muted, modifier = Modifier.padding(bottom = 6.dp))
        androidx.compose.foundation.layout.FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            tags.forEach { t ->
                val on = t.id in selected
                Chip(t.label.ifEmpty { t.name }, selected = on, onClick = { onChange(if (on) selected - t.id else selected + t.id) })
            }
        }
    }
}

@Composable
private fun ToggleRow(label: String, checked: Boolean, onChange: (Boolean) -> Unit) {
    Row(modifier = Modifier.fillMaxWidth().clickable { onChange(!checked) }, verticalAlignment = Alignment.CenterVertically) {
        Text(label, fontSize = 15.sp, color = RoxyColors.onBackground, modifier = Modifier.weight(1f))
        Switch(checked = checked, onCheckedChange = onChange, colors = SwitchDefaults.colors(checkedThumbColor = Color.White, checkedTrackColor = RoxyColors.accent, uncheckedThumbColor = RoxyColors.muted, uncheckedTrackColor = ChipGrey))
        Spacer(Modifier.width(4.dp))
    }
}

/**
 * Whether Roxy reads the folder Sonarr / Radarr saves into: if not, requests download
 * fine but never appear in the library. Paths are compared as each app sees them; in
 * Docker the same disk can have different paths in each container ("maybe").
 */
@Composable
private fun RootFolderCheck(rf: tv.roxy.tv.api.ArrNamed?, app: String) {
    if (rf == null) return
    val (text, color) = when (rf.roxyMatch) {
        "yes" -> "Roxy reads this folder (library \"${rf.roxy}\"), so downloads show up by themselves." to RoxyColors.accent
        "maybe" -> "Probably the library \"${rf.roxy}\" (same folder name). If $app runs in its own container, make sure both see the same disk; downloads that don't show up mean they don't." to RoxyColors.muted
        else -> "Roxy doesn't read ${rf.path}: downloads saved there won't show up. Add it in Library > Media folders, or pick a folder Roxy reads. (In Docker, $app and Roxy may see the same folder under different paths.)" to RoxyColors.error
    }
    Text(text, fontSize = 12.sp, lineHeight = 16.sp, color = color)
}
