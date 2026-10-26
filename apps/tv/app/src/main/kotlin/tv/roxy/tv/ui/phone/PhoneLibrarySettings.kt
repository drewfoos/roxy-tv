package tv.roxy.tv.ui.phone

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.material.icons.outlined.Checklist
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import androidx.compose.material.icons.filled.CheckBox
import androidx.compose.material.icons.filled.CheckBoxOutlineBlank
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.TextButton
import tv.roxy.tv.api.RefreshParts
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.collectIsFocusedAsState
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.outlined.Circle
import androidx.compose.material.icons.filled.Edit
import androidx.compose.material.icons.filled.Lock
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.filled.Sync
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material.icons.outlined.FolderOpen
import androidx.compose.material.icons.outlined.ManageSearch
import androidx.compose.material.icons.outlined.OpenInNew
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
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
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import coil3.compose.AsyncImage
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.LibraryManage
import tv.roxy.tv.api.LibraryTitle
import tv.roxy.tv.api.MatchCandidate
import tv.roxy.tv.api.MetadataEdit
import tv.roxy.tv.ui.components.RoxyTextField
import tv.roxy.tv.ui.theme.RoxyColors
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter

/**
 * Admin: Library. Scan for new files, look up what is missing, refresh every title or
 * only some (a separate picker), then just the titles that need a hand: unmatched ones,
 * each opening Identify, Edit details (custom shows like One Pace) or a refresh.
 * Matched titles are fixed from their own page (the admin ⋮ menu).
 */
@Composable
fun PhoneLibrarySettings(onBack: () -> Unit, onIdentify: (LibraryTitle) -> Unit, onEdit: (LibraryTitle) -> Unit, onOpen: (LibraryTitle) -> Unit, onPickTitles: () -> Unit, onAddFolder: () -> Unit = {}) {
    val app = RoxyApp.get(LocalContext.current)
    val scope = rememberCoroutineScope()
    var state by remember { mutableStateOf<LibraryManage?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var notice by remember { mutableStateOf<String?>(null) }
    // Refresh dialog: for every title, or (refreshOne) just one.
    var refreshAll by remember { mutableStateOf(false) }
    var refreshOne by remember { mutableStateOf<LibraryTitle?>(null) }
    var parts by remember { mutableStateOf(RefreshParts()) }

    suspend fun reload() {
        runCatching { app.api.libraryManage() }
            .onSuccess { state = it; error = null }
            .onFailure { t -> Timber.w(t, "library manage"); error = t.message }
    }
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { scope.launch { reload() } }
    // While something runs, follow it.
    val busy = state?.let { it.scanning || it.queue > 0 } == true
    LaunchedEffect(busy) {
        while (busy) { delay(3_000); reload() }
    }
    fun refresh(ids: List<String>, all: Boolean, p: RefreshParts, label: String) = scope.launch {
        runCatching { app.api.libraryRefresh(ids, all, p) }
            .onSuccess { notice = "Refreshing $label: ${p.describe()}…"; delay(1500); reload(); app.items.userDataChanged() }
            .onFailure { t -> notice = t.message }
    }
    fun run(mode: String, message: String) = scope.launch {
        runCatching { app.api.libraryScan(mode) }
            .onSuccess { notice = message; delay(800); reload(); app.items.userDataChanged() }
            .onFailure { t -> notice = t.message }
    }

    Column(modifier = Modifier.fillMaxSize().statusBarsPadding()) {
        Row(modifier = Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
            BackCircle(onBack)
            Text("Library", fontSize = 20.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(start = 12.dp))
        }
        val s = state
        LazyColumn(modifier = Modifier.fillMaxSize(), contentPadding = PaddingValues(start = PhonePad, end = PhonePad, bottom = 32.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            item(key = "status") {
                val line = when {
                    s == null -> "…"
                    s.scanning -> "Scanning folders for new and removed files…"
                    s.queue > 0 -> "Looking up details: ${s.queue} waiting"
                    s.lastScanAt != null -> "Last scan " + formatWhen(s.lastScanAt)
                    else -> "Up to date"
                }
                Row(verticalAlignment = Alignment.CenterVertically) {
                    if (busy) Icon(Icons.Filled.Sync, null, tint = RoxyColors.accent, modifier = Modifier.size(16.dp).padding(end = 2.dp))
                    Text(line, fontSize = 13.sp, color = if (busy) RoxyColors.accent else RoxyColors.muted, modifier = Modifier.padding(start = if (busy) 6.dp else 0.dp))
                }
            }
            item(key = "folders") { MediaFoldersCard(reloadKey = s, onAdd = onAddFolder, onChanged = { scope.launch { delay(800); reload() } }) }
            item(key = "actions") {
                Column(modifier = Modifier.fillMaxWidth().clip(RoundedCornerShape(16.dp)).background(RoxyColors.surface)) {
                    ActionRow(Icons.Outlined.FolderOpen, "Scan for new files", "Adds new episodes and movies, removes ones deleted from disk, reads new .nfo files and artwork") { run("files", "Scanning…") }
                    Divider()
                    ActionRow(Icons.Outlined.ManageSearch, "Find missing metadata", "Looks up titles that matched nothing yet") { run("missing", "Looking up unmatched titles…") }
                }
            }
            item(key = "refresh") {
                Column(modifier = Modifier.fillMaxWidth().clip(RoundedCornerShape(16.dp)).background(RoxyColors.surface)) {
                    ActionRow(Icons.Filled.Refresh, "Refresh all titles", "Metadata, images, trickplay or intro detection for the whole library") { refreshAll = true }
                    Divider()
                    ActionRow(Icons.Outlined.Checklist, "Refresh certain titles", "Pick the shows and movies to refresh") { onPickTitles() }
                }
            }
            notice?.let { n -> item(key = "notice") { Text(n, fontSize = 13.sp, color = RoxyColors.muted) } }
            error?.let { e -> item(key = "error") { Text(e, fontSize = 13.sp, color = RoxyColors.error) } }
            if (s == null) {
                if (error == null) items(4) { BoneLine(width = 320.dp, height = 44.dp) }
            } else {
                val unmatched = s.titles.filter { !it.matched && !it.locked }
                item(key = "h-unmatched") { SectionLabel(if (unmatched.isEmpty()) "Needs attention" else "Needs attention  ·  ${unmatched.size}") }
                if (unmatched.isEmpty()) {
                    item(key = "all-good") {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Icon(Icons.Filled.Check, null, tint = RoxyColors.accent, modifier = Modifier.size(18.dp))
                            Text("Every title matched. Fix one from its page: ⋮ › Identify or Edit details.", fontSize = 13.sp, color = RoxyColors.muted, modifier = Modifier.padding(start = 8.dp))
                        }
                    }
                } else {
                    item(key = "why") {
                        Text("These matched nothing on TMDB or AniList. Identify picks the right entry; Edit details types them in (custom shows like One Pace, or add a tvshow.nfo next to the files).", fontSize = 12.sp, lineHeight = 17.sp, color = RoxyColors.muted)
                    }
                    items(unmatched, key = { it.id }) { TitleRow(it, onIdentify, onEdit, onOpen, onRefresh = { t -> refreshOne = t }) }
                }
            }
        }
    }
    val one = refreshOne
    if (refreshAll || one != null) {
        RefreshDialog(
            heading = if (one != null) "Refresh ${one.title}" else "Refresh all titles",
            parts = parts, onParts = { parts = it },
            primary = "Refresh",
            onPrimary = {
                if (one != null) refresh(listOf(one.id), false, parts, one.title) else refresh(emptyList(), true, parts, "every title")
                refreshAll = false; refreshOne = null
            },
            secondary = null, onSecondary = {},
            onDismiss = { refreshAll = false; refreshOne = null },
        )
    }
}

private fun RefreshParts.describe() = listOfNotNull(
    "metadata".takeIf { metadata }, "images".takeIf { images }, "trickplay".takeIf { trickplay }, "intro & outro".takeIf { segments },
).joinToString(", ")

/**
 * Refresh certain titles: every show and movie with its poster, searchable, ticked one by
 * one; the bar at the bottom asks what to redo for them and runs it.
 */
@Composable
fun PhoneRefreshPicker(onBack: () -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    val scope = rememberCoroutineScope()
    var titles by remember { mutableStateOf<List<LibraryTitle>?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var query by remember { mutableStateOf("") }
    var kind by remember { mutableStateOf("all") } // all | series | movie
    var selected by remember { mutableStateOf(setOf<String>()) }
    var dialog by remember { mutableStateOf(false) }
    var parts by remember { mutableStateOf(RefreshParts()) }
    var sending by remember { mutableStateOf(false) }
    LaunchedEffect(Unit) {
        runCatching { app.api.libraryManage().titles.sortedBy { it.title.lowercase() } }
            .onSuccess { titles = it }
            .onFailure { t -> error = t.message }
    }
    Column(modifier = Modifier.fillMaxSize().statusBarsPadding()) {
        Row(modifier = Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
            BackCircle(onBack)
            Text("Refresh certain titles", fontSize = 20.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(start = 12.dp).weight(1f))
        }
        Column(modifier = Modifier.padding(horizontal = PhonePad), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Row(
                modifier = Modifier.fillMaxWidth().height(42.dp).clip(RoundedCornerShape(21.dp)).background(RoxyColors.surfaceHigh).padding(horizontal = 14.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Icon(Icons.Filled.Search, null, tint = RoxyColors.muted, modifier = Modifier.size(20.dp))
                Box(modifier = Modifier.weight(1f).padding(start = 10.dp)) {
                    if (query.isEmpty()) Text("Search your library", color = RoxyColors.muted, fontSize = 14.sp)
                    BasicTextField(query, { query = it }, singleLine = true, textStyle = TextStyle(color = RoxyColors.onBackground, fontSize = 14.sp), cursorBrush = SolidColor(RoxyColors.accent), modifier = Modifier.fillMaxWidth())
                }
            }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                Chip("All", selected = kind == "all", onClick = { kind = "all" })
                Chip("Series", selected = kind == "series", onClick = { kind = "series" })
                Chip("Movies", selected = kind == "movie", onClick = { kind = "movie" })
                Spacer(Modifier.weight(1f))
                if (selected.isNotEmpty()) Text("Clear", fontSize = 13.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.accent, modifier = Modifier.clickable { selected = emptySet() }.padding(6.dp))
            }
            error?.let { Text(it, fontSize = 13.sp, color = RoxyColors.error) }
        }
        val list = titles?.filter { (kind == "all" || it.type == kind) && (query.isBlank() || it.title.contains(query.trim(), ignoreCase = true)) }
        LazyColumn(modifier = Modifier.fillMaxWidth().weight(1f), contentPadding = PaddingValues(start = PhonePad, end = PhonePad, top = 12.dp, bottom = 16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            when {
                list == null -> if (error == null) items(6) { Bone(Modifier.fillMaxWidth().height(72.dp)) }
                list.isEmpty() -> item { Text("Nothing matches.", fontSize = 13.sp, color = RoxyColors.muted) }
                else -> items(list, key = { it.id }) { t ->
                    val on = t.id in selected
                    PickRow(t, on, poster = app.api.absoluteImage(t.poster, 120)) { selected = if (on) selected - t.id else selected + t.id }
                }
            }
        }
        Box(modifier = Modifier.fillMaxWidth().background(RoxyColors.background).navigationBarsPadding().padding(horizontal = PhonePad, vertical = 10.dp)) {
            val n = selected.size
            PlayButton(
                when { sending -> "Starting…"; n == 0 -> "Pick titles to refresh"; n == 1 -> "Refresh 1 title"; else -> "Refresh $n titles" },
                icon = Icons.Filled.Refresh, pill = true,
                container = if (n > 0) RoxyColors.accent else RoxyColors.surface, content = if (n > 0) OnBrand else RoxyColors.muted,
                modifier = Modifier.fillMaxWidth(),
                onClick = { if (n > 0 && !sending) dialog = true },
            )
        }
    }
    if (dialog) {
        val n = selected.size
        RefreshDialog(
            heading = "Refresh $n title${if (n == 1) "" else "s"}",
            parts = parts, onParts = { parts = it },
            primary = "Refresh",
            onPrimary = {
                dialog = false; sending = true
                val ids = selected.toList()
                scope.launch {
                    runCatching { app.api.libraryRefresh(ids, false, parts) }
                        .onSuccess { app.items.userDataChanged(); onBack() }
                        .onFailure { t -> error = t.message }
                    sending = false
                }
            },
            secondary = null, onSecondary = {},
            onDismiss = { dialog = false },
        )
    }
}

@Composable
private fun PickRow(t: LibraryTitle, checked: Boolean, poster: String?, onClick: () -> Unit) {
    Row(
        modifier = Modifier.fillMaxWidth().clip(RoundedCornerShape(12.dp)).background(RoxyColors.surface)
            .then(if (checked) Modifier.border(1.5.dp, RoxyColors.accent, RoundedCornerShape(12.dp)) else Modifier)
            .clickable(onClick = onClick).padding(8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(modifier = Modifier.width(40.dp).aspectRatio(2f / 3f).clip(RoundedCornerShape(4.dp)).background(RoxyColors.surfaceHigh)) {
            if (poster != null) AsyncImage(model = poster, contentDescription = t.title, contentScale = ContentScale.Crop, modifier = Modifier.fillMaxSize())
        }
        Column(modifier = Modifier.weight(1f).padding(horizontal = 12.dp)) {
            Text(t.title + (t.year?.let { " ($it)" } ?: ""), fontSize = 14.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground, maxLines = 1, overflow = TextOverflow.Ellipsis)
            val status = when {
                t.locked -> "Edited by hand"
                !t.matched -> "No match"
                t.providerIds["tmdb"] == null && t.providerIds["anilist"] == null -> "From .nfo"
                else -> "Matched"
            }
            Text("${if (t.type == "movie") "Movie" else "Series"}  ·  $status", fontSize = 12.sp, color = if (!t.matched && !t.locked) RoxyColors.error else RoxyColors.muted)
        }
        Icon(
            if (checked) Icons.Filled.CheckCircle else Icons.Outlined.Circle, contentDescription = if (checked) "Selected" else "Not selected",
            tint = if (checked) RoxyColors.accent else RoxyColors.muted, modifier = Modifier.size(24.dp),
        )
    }
}

/** Jellyfin's refresh dialog: tick what to redo, then run it. */
@Composable
private fun RefreshDialog(
    heading: String, parts: RefreshParts, onParts: (RefreshParts) -> Unit,
    primary: String, onPrimary: () -> Unit, secondary: String?, onSecondary: () -> Unit, onDismiss: () -> Unit,
) {
    AlertDialog(
        onDismissRequest = onDismiss,
        containerColor = RoxyColors.surfaceHigh, titleContentColor = RoxyColors.onBackground, textContentColor = RoxyColors.onBackground,
        title = { Text(heading, maxLines = 2, overflow = TextOverflow.Ellipsis) },
        text = {
            Column {
                PartRow("Metadata", "Details and episode names from TMDB / AniList (or the .nfo files)", parts.metadata) { onParts(parts.copy(metadata = it)) }
                PartRow("Replace images", "Download posters, banners, logos and thumbnails again. Art you picked stays.", parts.images) { onParts(parts.copy(images = it)) }
                PartRow("Trickplay", "Make the seek-bar preview thumbnails again", parts.trickplay) { onParts(parts.copy(trickplay = it)) }
                PartRow("Intro & outro", "Detect where intros and credits are again", parts.segments) { onParts(parts.copy(segments = it)) }
                Text("Titles you edited by hand keep their details.", fontSize = 12.sp, color = RoxyColors.muted, modifier = Modifier.padding(top = 8.dp))
            }
        },
        confirmButton = {
            TextButton(onClick = onPrimary, enabled = parts.any) { Text(primary, color = if (parts.any) RoxyColors.accent else RoxyColors.muted, fontWeight = FontWeight.SemiBold) }
        },
        dismissButton = {
            Row {
                if (secondary != null) TextButton(onClick = onSecondary, enabled = parts.any) { Text(secondary, color = if (parts.any) Color.White else RoxyColors.muted) }
                TextButton(onClick = onDismiss) { Text("Cancel", color = RoxyColors.muted) }
            }
        },
    )
}

@Composable
private fun PartRow(title: String, sub: String, checked: Boolean, onChange: (Boolean) -> Unit) {
    Row(modifier = Modifier.fillMaxWidth().clip(RoundedCornerShape(8.dp)).clickable { onChange(!checked) }.padding(vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
        Icon(
            if (checked) Icons.Filled.CheckBox else Icons.Filled.CheckBoxOutlineBlank, contentDescription = null,
            tint = if (checked) RoxyColors.accent else RoxyColors.muted, modifier = Modifier.size(24.dp),
        )
        Column(modifier = Modifier.padding(start = 12.dp)) {
            Text(title, fontSize = 15.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground)
            Text(sub, fontSize = 12.sp, lineHeight = 16.sp, color = RoxyColors.muted)
        }
    }
}

private fun formatWhen(iso: String): String = runCatching {
    val t = Instant.parse(iso).atZone(ZoneId.systemDefault())
    val today = java.time.LocalDate.now()
    if (t.toLocalDate() == today) "today at " + t.format(DateTimeFormatter.ofPattern("h:mm a"))
    else t.format(DateTimeFormatter.ofPattern("MMM d, h:mm a"))
}.getOrDefault(iso)

@Composable
private fun SectionLabel(text: String) {
    Text(text, fontSize = 15.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(top = 14.dp, bottom = 2.dp))
}

@Composable
private fun Divider() {
    Box(modifier = Modifier.fillMaxWidth().padding(start = 52.dp).height(1.dp).background(Color.White.copy(alpha = 0.06f)))
}

@Composable
private fun ActionRow(icon: ImageVector, title: String, sub: String, onClick: () -> Unit) {
    Row(modifier = Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 13.dp), verticalAlignment = Alignment.CenterVertically) {
        Icon(icon, null, tint = RoxyColors.onBackground, modifier = Modifier.size(22.dp))
        Column(modifier = Modifier.padding(start = 14.dp)) {
            Text(title, fontSize = 15.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground)
            Text(sub, fontSize = 12.sp, lineHeight = 16.sp, color = RoxyColors.muted)
        }
    }
}

@Composable
private fun TitleRow(
    t: LibraryTitle, onIdentify: (LibraryTitle) -> Unit, onEdit: (LibraryTitle) -> Unit, onOpen: (LibraryTitle) -> Unit, onRefresh: (LibraryTitle) -> Unit,
) {
    var open by remember { mutableStateOf(false) }
    Box {
        Row(
            modifier = Modifier.fillMaxWidth().clip(RoundedCornerShape(12.dp)).background(RoxyColors.surface).clickable { open = true }.padding(horizontal = 14.dp, vertical = 11.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(modifier = Modifier.weight(1f)) {
                Text(t.title + (t.year?.let { " ($it)" } ?: ""), fontSize = 14.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground, maxLines = 1, overflow = TextOverflow.Ellipsis)
                val kind = if (t.type == "movie") "Movie" else "Series"
                val match = when {
                    t.locked -> "Edited by hand"
                    !t.matched -> "No match"
                    t.providerIds["nfo"] != null && t.providerIds["tmdb"] == null && t.providerIds["anilist"] == null -> "From .nfo"
                    else -> listOfNotNull(t.providerIds["tmdb"]?.let { "TMDB $it" }, t.providerIds["anilist"]?.let { "AniList $it" }).joinToString("  ·  ")
                }
                Text("$kind  ·  $match", fontSize = 12.sp, color = if (!t.matched && !t.locked) RoxyColors.error else RoxyColors.muted, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            when {
                t.locked -> Icon(Icons.Filled.Lock, "Edited", tint = RoxyColors.muted, modifier = Modifier.size(18.dp))
                !t.matched -> Icon(Icons.Outlined.ErrorOutline, "No match", tint = RoxyColors.error, modifier = Modifier.size(20.dp))
                else -> Icon(Icons.Filled.Check, "Matched", tint = RoxyColors.accent, modifier = Modifier.size(20.dp))
            }
        }
        DropdownMenu(expanded = open, onDismissRequest = { open = false }, containerColor = RoxyColors.surfaceHigh) {
            DropdownMenuItem(text = { Text("Identify", color = Color.White) }, leadingIcon = { Icon(Icons.Outlined.ManageSearch, null, tint = Color.White) }, onClick = { open = false; onIdentify(t) })
            DropdownMenuItem(text = { Text("Edit details", color = Color.White) }, leadingIcon = { Icon(Icons.Filled.Edit, null, tint = Color.White) }, onClick = { open = false; onEdit(t) })
            DropdownMenuItem(text = { Text("Refresh…", color = Color.White) }, leadingIcon = { Icon(Icons.Filled.Refresh, null, tint = Color.White) }, onClick = { open = false; onRefresh(t) })
            DropdownMenuItem(text = { Text("Open", color = Color.White) }, leadingIcon = { Icon(Icons.Outlined.OpenInNew, null, tint = Color.White) }, onClick = { open = false; onOpen(t) })
        }
    }
}

/**
 * Identify: search TMDB or AniList and pick the right entry. The title's old ids are
 * dropped and it is looked up again from the chosen one (any hand edits are unlocked).
 */
@Composable
fun PhoneIdentify(itemId: String, title: String, type: String, onBack: () -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    val scope = rememberCoroutineScope()
    var provider by remember { mutableStateOf("tmdb") }
    var query by remember { mutableStateOf(title) }
    var results by remember { mutableStateOf<List<MatchCandidate>?>(null) }
    var searching by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var picked by remember { mutableStateOf<MatchCandidate?>(null) }
    var done by remember { mutableStateOf(false) }
    fun search() {
        searching = true; error = null
        scope.launch {
            runCatching { app.api.identifySearch(itemId, provider, query) }
                .onSuccess { results = it.candidates.orEmpty() }
                .onFailure { t -> error = t.message; results = emptyList() }
            searching = false
        }
    }
    LaunchedEffect(provider) { search() }
    Column(modifier = Modifier.fillMaxSize().statusBarsPadding().imePadding()) {
        Row(modifier = Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
            BackCircle(onBack)
            Column(modifier = Modifier.padding(start = 12.dp)) {
                Text("Identify", fontSize = 20.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground)
                Text(title, fontSize = 12.sp, color = RoxyColors.muted, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }
        Column(modifier = Modifier.padding(horizontal = PhonePad), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Chip("TMDB", selected = provider == "tmdb", onClick = { provider = "tmdb" })
                Chip("AniList", selected = provider == "anilist", onClick = { provider = "anilist" })
            }
            Row(
                modifier = Modifier.fillMaxWidth().height(44.dp).clip(RoundedCornerShape(22.dp)).background(RoxyColors.surfaceHigh).padding(horizontal = 14.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Icon(Icons.Filled.Search, null, tint = RoxyColors.muted, modifier = Modifier.size(20.dp))
                BasicTextField(
                    query, { query = it }, singleLine = true,
                    textStyle = TextStyle(color = RoxyColors.onBackground, fontSize = 15.sp), cursorBrush = SolidColor(RoxyColors.accent),
                    keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search), keyboardActions = KeyboardActions(onSearch = { search() }),
                    modifier = Modifier.weight(1f).padding(start = 10.dp),
                )
                Text("Search", fontSize = 13.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.accent, modifier = Modifier.clickable { search() }.padding(6.dp))
            }
            if (done) Text("Looking it up from the new match. The title page shows it in a moment.", fontSize = 13.sp, color = RoxyColors.accent)
            error?.let { Text(it, fontSize = 13.sp, color = RoxyColors.error) }
        }
        val list = results
        LazyColumn(modifier = Modifier.fillMaxSize(), contentPadding = PaddingValues(start = PhonePad, end = PhonePad, top = 12.dp, bottom = 32.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            when {
                list == null || searching -> items(4) { Bone(Modifier.fillMaxWidth().height(96.dp)) }
                list.isEmpty() -> item { Text("Nothing found. Try another name, or the other source.", fontSize = 13.sp, color = RoxyColors.muted) }
                else -> items(list, key = { it.provider + it.id }) { c -> CandidateRow(c, onClick = { picked = c }) }
            }
        }
    }
    picked?.let { c ->
        ConfirmDialog(
            title = "Use this match?",
            text = "${c.title}${c.year?.let { " ($it)" } ?: ""} on ${if (c.provider == "anilist") "AniList" else "TMDB"}. Its details, art and episode names replace what the title has now.",
            confirm = "Use it",
            onConfirm = {
                picked = null
                scope.launch {
                    runCatching { app.api.identify(itemId, c.provider, c.id) }
                        .onSuccess { done = true; delay(2500); app.items.userDataChanged(); onBack() }
                        .onFailure { t -> error = t.message }
                }
            },
            onDismiss = { picked = null },
        )
    }
}

@Composable
private fun CandidateRow(c: MatchCandidate, onClick: () -> Unit) {
    Row(modifier = Modifier.fillMaxWidth().clip(RoundedCornerShape(12.dp)).background(RoxyColors.surface).clickable(onClick = onClick).padding(10.dp)) {
        Box(modifier = Modifier.width(58.dp).aspectRatio(2f / 3f).clip(RoundedCornerShape(6.dp)).background(RoxyColors.surfaceHigh)) {
            if (c.poster != null) AsyncImage(model = c.poster, contentDescription = c.title, contentScale = ContentScale.Crop, modifier = Modifier.fillMaxSize())
        }
        Column(modifier = Modifier.weight(1f).padding(start = 12.dp)) {
            Text(c.title + (c.year?.let { " ($it)" } ?: ""), fontSize = 14.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground, maxLines = 2, overflow = TextOverflow.Ellipsis)
            Text("${if (c.provider == "anilist") "AniList" else "TMDB"} ${c.id}", fontSize = 11.sp, color = RoxyColors.muted)
            c.overview?.takeIf { it.isNotBlank() }?.let { Text(it, fontSize = 12.sp, lineHeight = 16.sp, color = RoxyColors.muted, maxLines = 3, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 4.dp)) }
        }
    }
}

/**
 * Edit details: type a title's name, year, synopsis, genres, studio and premiere date.
 * "Keep my edits" locks them so metadata refreshes leave them alone (custom shows).
 */
@Composable
fun PhoneEditMetadata(itemId: String, fallbackTitle: String, onBack: () -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    val scope = rememberCoroutineScope()
    var loaded by remember { mutableStateOf(false) }
    var title by remember { mutableStateOf(fallbackTitle) }
    var year by remember { mutableStateOf("") }
    var overview by remember { mutableStateOf("") }
    var genres by remember { mutableStateOf("") }
    var studio by remember { mutableStateOf("") }
    var premiered by remember { mutableStateOf("") }
    var lock by remember { mutableStateOf(true) }
    var saving by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(itemId) {
        runCatching { app.api.item(itemId) }
            .onSuccess { it ->
                title = it.title; year = it.year?.toString().orEmpty(); overview = it.overview.orEmpty()
                genres = it.genres.joinToString(", "); studio = it.studio.orEmpty(); premiered = it.premiered.orEmpty()
                loaded = true
            }
            .onFailure { t -> error = t.message; loaded = true }
    }
    Column(modifier = Modifier.fillMaxSize().statusBarsPadding().imePadding()) {
        Row(modifier = Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
            BackCircle(onBack)
            Text("Edit details", fontSize = 20.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(start = 12.dp))
        }
        Column(
            modifier = Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(horizontal = PhonePad).navigationBarsPadding(),
            verticalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            if (!loaded) {
                repeat(5) { BoneLine(width = 320.dp, height = 40.dp) }
                return@Column
            }
            RoxyTextField(title, { title = it }, label = "Title")
            RoxyTextField(year, { v -> year = v.filter { it.isDigit() }.take(4) }, label = "Year", keyboardType = KeyboardType.Number)
            MultiLineField(overview, { overview = it }, label = "Synopsis")
            RoxyTextField(genres, { genres = it }, label = "Genres (comma separated)")
            RoxyTextField(studio, { studio = it }, label = "Studio")
            RoxyTextField(premiered, { premiered = it }, label = "Premiered (YYYY-MM-DD)", imeAction = ImeAction.Done)
            Row(modifier = Modifier.fillMaxWidth().clip(RoundedCornerShape(12.dp)).background(RoxyColors.surface).clickable { lock = !lock }.padding(horizontal = 14.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
                Column(modifier = Modifier.weight(1f)) {
                    Text("Keep my edits", fontSize = 15.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground)
                    Text("Metadata refreshes leave this title alone. Identify undoes it.", fontSize = 12.sp, lineHeight = 16.sp, color = RoxyColors.muted)
                }
                Switch(checked = lock, onCheckedChange = { lock = it }, colors = SwitchDefaults.colors(checkedTrackColor = RoxyColors.accent, checkedThumbColor = Color.White))
            }
            PlayButton(
                if (saving) "Saving…" else "Save", icon = Icons.Filled.Check, pill = true,
                container = RoxyColors.accent, content = OnBrand, modifier = Modifier.fillMaxWidth(),
                onClick = {
                    if (saving) return@PlayButton
                    if (title.isBlank()) { error = "A title is required."; return@PlayButton }
                    saving = true; error = null
                    val edit = MetadataEdit(
                        title = title.trim(), year = year.toIntOrNull(), overview = overview.trim(),
                        genres = genres.split(",").map { it.trim() }.filter { it.isNotEmpty() },
                        studio = studio.trim(), premiered = premiered.trim(), locked = lock,
                    )
                    scope.launch {
                        runCatching { app.api.editMetadata(itemId, edit) }
                            .onSuccess { app.items.userDataChanged(); onBack() }
                            .onFailure { t -> error = t.message }
                        saving = false
                    }
                },
            )
            error?.let { Text(it, fontSize = 13.sp, color = RoxyColors.error) }
            Text(
                "Artwork: pick it from the title page's ⋮ menu (Change banner, poster, logo), or put poster.jpg, fanart.jpg and clearlogo.png in the title's folder.",
                fontSize = 12.sp, lineHeight = 16.sp, color = RoxyColors.muted,
            )
            Spacer(Modifier.height(24.dp))
        }
    }
}

@Composable
private fun MultiLineField(value: String, onValueChange: (String) -> Unit, label: String) {
    val interaction = remember { MutableInteractionSource() }
    val focused by interaction.collectIsFocusedAsState()
    Column {
        Text(label, fontSize = 12.sp, color = RoxyColors.muted, modifier = Modifier.padding(bottom = 4.dp))
        BasicTextField(
            value = value, onValueChange = onValueChange, interactionSource = interaction,
            textStyle = TextStyle(color = RoxyColors.onBackground, fontSize = 15.sp, lineHeight = 21.sp), cursorBrush = SolidColor(RoxyColors.accent),
            modifier = Modifier.fillMaxWidth().heightIn(min = 110.dp).clip(RoundedCornerShape(10.dp)).background(RoxyColors.surfaceHigh)
                .border(1.dp, if (focused) RoxyColors.accent else Color.Transparent, RoundedCornerShape(10.dp)).padding(12.dp),
        )
    }
}
