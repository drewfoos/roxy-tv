package tv.roxy.tv.ui.phone

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.ChevronRight
import androidx.compose.material.icons.filled.RadioButtonChecked
import androidx.compose.material.icons.filled.RadioButtonUnchecked
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material.icons.outlined.Folder
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Icon
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
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
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.launch
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.FolderEntry
import tv.roxy.tv.api.MediaFolder
import tv.roxy.tv.ui.components.RoxyTextField
import tv.roxy.tv.ui.theme.RoxyColors

private fun kindLabel(kind: String, anime: Boolean) =
    (if (kind == "movies") "Movies only" else "Shows & movies") + if (anime) "  ·  anime" else ""

/**
 * Library settings' Media folders: every folder the server reads, what is in it, and
 * whether the server can still see it. Tap one to rename it, change its type or remove
 * it (the files stay); "Add folder" opens the server's folder browser.
 */
@Composable
fun MediaFoldersCard(reloadKey: Any?, onAdd: () -> Unit, onChanged: () -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    val scope = rememberCoroutineScope()
    var folders by remember { mutableStateOf<List<MediaFolder>?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var editing by remember { mutableStateOf<MediaFolder?>(null) }
    var tick by remember { mutableStateOf(0) }
    LaunchedEffect(reloadKey, tick) {
        runCatching { app.api.mediaFolders() }.onSuccess { folders = it; error = null }.onFailure { error = it.message }
    }
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text("Media folders", fontSize = 15.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(top = 6.dp))
        Column(modifier = Modifier.fillMaxWidth().clip(RoundedCornerShape(16.dp)).background(RoxyColors.surface)) {
            val list = folders
            when {
                list == null && error == null -> repeat(2) { BoneLine(width = 300.dp, height = 40.dp, modifier = Modifier.padding(14.dp)) }
                list != null -> list.forEachIndexed { i, f ->
                    if (i > 0) Box(modifier = Modifier.fillMaxWidth().padding(start = 52.dp).height(1.dp).background(Color.White.copy(alpha = 0.06f)))
                    Row(
                        modifier = Modifier.fillMaxWidth().clickable { editing = f }.padding(horizontal = 16.dp, vertical = 12.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Icon(
                            if (f.reachable) Icons.Outlined.Folder else Icons.Outlined.ErrorOutline, null,
                            tint = if (f.reachable) RoxyColors.onBackground else RoxyColors.error, modifier = Modifier.size(22.dp),
                        )
                        Column(modifier = Modifier.weight(1f).padding(start = 14.dp)) {
                            Text(f.name, fontSize = 15.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground, maxLines = 1, overflow = TextOverflow.Ellipsis)
                            Text(f.root, fontSize = 12.sp, color = RoxyColors.muted, maxLines = 1, overflow = TextOverflow.Ellipsis)
                            val counts = listOfNotNull(
                                f.series.takeIf { it > 0 }?.let { "$it ${if (it == 1) "show" else "shows"}" },
                                f.movies.takeIf { it > 0 }?.let { "$it ${if (it == 1) "movie" else "movies"}" },
                            ).joinToString(", ").ifEmpty { "empty" }
                            Text(
                                if (f.reachable) kindLabel(f.kind, f.animeMode) + "  ·  " + counts else "The server can't see this folder right now",
                                fontSize = 12.sp, color = if (f.reachable) RoxyColors.muted else RoxyColors.error,
                            )
                        }
                        Icon(Icons.Filled.ChevronRight, null, tint = RoxyColors.muted)
                    }
                }
            }
            Box(modifier = Modifier.fillMaxWidth().padding(start = 52.dp).height(1.dp).background(Color.White.copy(alpha = 0.06f)))
            Row(modifier = Modifier.fillMaxWidth().clickable(onClick = onAdd).padding(horizontal = 16.dp, vertical = 14.dp), verticalAlignment = Alignment.CenterVertically) {
                Icon(Icons.Filled.Add, null, tint = RoxyColors.accent, modifier = Modifier.size(22.dp))
                Text("Add folder", fontSize = 15.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.accent, modifier = Modifier.padding(start = 14.dp))
            }
        }
        error?.let { Text(it, fontSize = 12.sp, color = RoxyColors.error) }
    }
    editing?.let { f ->
        FolderEditDialog(
            folder = f,
            onDismiss = { editing = null },
            onSave = { name, kind, anime ->
                editing = null
                scope.launch { runCatching { app.api.updateMediaFolder(f.id, name, kind, anime) }.onFailure { error = it.message }; tick++; onChanged() }
            },
            onRemove = {
                editing = null
                scope.launch { runCatching { app.api.removeMediaFolder(f.id) }.onFailure { error = it.message }; tick++; app.items.userDataChanged(); onChanged() }
            },
        )
    }
}

@Composable
private fun KindChooser(kind: String, anime: Boolean, onKind: (String) -> Unit, onAnime: (Boolean) -> Unit) {
    Column {
        listOf(
            "series" to ("Shows & movies" to "Finds shows and movies in the folder by itself (what Roxy has always done)"),
            "movies" to ("Movies only" to "Every video is a movie"),
        ).forEach { (k, t) ->
            Row(modifier = Modifier.fillMaxWidth().clip(RoundedCornerShape(8.dp)).clickable { onKind(k) }.padding(vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                Icon(if (kind == k) Icons.Filled.RadioButtonChecked else Icons.Filled.RadioButtonUnchecked, null, tint = if (kind == k) RoxyColors.accent else RoxyColors.muted, modifier = Modifier.size(22.dp))
                Column(modifier = Modifier.padding(start = 12.dp)) {
                    Text(t.first, fontSize = 15.sp, color = RoxyColors.onBackground)
                    Text(t.second, fontSize = 12.sp, lineHeight = 16.sp, color = RoxyColors.muted)
                }
            }
        }
        Row(modifier = Modifier.fillMaxWidth().clip(RoundedCornerShape(8.dp)).clickable { onAnime(!anime) }.padding(vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
            Column(modifier = Modifier.weight(1f)) {
                Text("Anime", fontSize = 15.sp, color = RoxyColors.onBackground)
                Text("Look titles up on AniList first (Dub | Sub, anime episode numbering)", fontSize = 12.sp, lineHeight = 16.sp, color = RoxyColors.muted)
            }
            Switch(checked = anime, onCheckedChange = onAnime, colors = SwitchDefaults.colors(checkedTrackColor = RoxyColors.accent, checkedThumbColor = Color.White))
        }
    }
}

@Composable
private fun FolderEditDialog(folder: MediaFolder, onDismiss: () -> Unit, onSave: (String, String, Boolean) -> Unit, onRemove: () -> Unit) {
    var name by remember { mutableStateOf(folder.name) }
    var kind by remember { mutableStateOf(folder.kind) }
    var anime by remember { mutableStateOf(folder.animeMode) }
    var confirmRemove by remember { mutableStateOf(false) }
    if (confirmRemove) {
        ConfirmDialog(
            title = "Remove ${folder.name}?",
            text = "Roxy stops reading ${folder.root} and its titles leave the app. The files stay on the disk; add the folder again to bring everything back, watch history included.",
            confirm = "Remove",
            onConfirm = { confirmRemove = false; onRemove() },
            onDismiss = { confirmRemove = false },
        )
        return
    }
    AlertDialog(
        onDismissRequest = onDismiss,
        containerColor = RoxyColors.surfaceHigh, titleContentColor = RoxyColors.onBackground, textContentColor = RoxyColors.onBackground,
        title = { Text(folder.name) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text(folder.root, fontSize = 12.sp, color = RoxyColors.muted)
                RoxyTextField(name, { name = it }, label = "Name")
                KindChooser(kind, anime, onKind = { kind = it }, onAnime = { anime = it })
                if (kind != folder.kind || anime != folder.animeMode) {
                    Text("Changing the type or anime setting looks every title up again.", fontSize = 12.sp, color = RoxyColors.muted)
                }
                Text("Remove folder", fontSize = 14.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.error, modifier = Modifier.clickable { confirmRemove = true }.padding(vertical = 6.dp))
            }
        },
        confirmButton = { TextButton(onClick = { onSave(name.trim(), kind, anime) }) { Text("Save", color = RoxyColors.accent, fontWeight = FontWeight.SemiBold) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel", color = RoxyColors.muted) } },
    )
}

/**
 * Add folder: browse the server's disk (folders only, with how many videos each holds),
 * then name the library and pick its type. In Docker the server sees only mounted folders.
 */
@Composable
fun PhoneAddFolder(onBack: () -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    val scope = rememberCoroutineScope()
    var path by remember { mutableStateOf("") }
    var parent by remember { mutableStateOf<String?>(null) }
    var dirs by remember { mutableStateOf<List<FolderEntry>?>(null) }
    var videosHere by remember { mutableStateOf(0) }
    var error by remember { mutableStateOf<String?>(null) }
    var choosing by remember { mutableStateOf(false) }
    var name by remember { mutableStateOf("") }
    var kind by remember { mutableStateOf("series") }
    var anime by remember { mutableStateOf(false) }
    var saving by remember { mutableStateOf(false) }

    fun open(p: String) {
        dirs = null; error = null
        scope.launch {
            runCatching { app.api.browseFolders(p) }
                .onSuccess { r -> path = r.path; parent = r.parent?.takeIf { it.isNotEmpty() }; dirs = r.dirs; videosHere = r.videos }
                .onFailure { t -> error = t.message; dirs = emptyList() }
        }
    }
    LaunchedEffect(Unit) { open("") }

    Column(modifier = Modifier.fillMaxSize().statusBarsPadding()) {
        Row(modifier = Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
            BackCircle(onBack)
            Column(modifier = Modifier.padding(start = 12.dp)) {
                Text("Add folder", fontSize = 20.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground)
                Text(path.ifEmpty { "On the server" }, fontSize = 12.sp, color = RoxyColors.muted, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }
        Text(
            "These are the server's folders. Running in Docker, the server only sees folders mounted into its container.",
            fontSize = 12.sp, lineHeight = 16.sp, color = RoxyColors.muted, modifier = Modifier.padding(horizontal = PhonePad),
        )
        error?.let { Text(it, fontSize = 13.sp, color = RoxyColors.error, modifier = Modifier.padding(horizontal = PhonePad, vertical = 6.dp)) }
        val list = dirs
        LazyColumn(modifier = Modifier.fillMaxWidth().weight(1f), contentPadding = PaddingValues(start = PhonePad, end = PhonePad, top = 10.dp, bottom = 12.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            if (path.isNotEmpty()) item(key = "up") {
                FolderRow(name = parent?.let { "Up" } ?: "Starting places", sub = parent ?: "", icon = Icons.AutoMirrored.Filled.ArrowBack) { open(parent ?: "") }
            }
            when {
                list == null -> items(5) { Bone(Modifier.fillMaxWidth().height(56.dp)) }
                list.isEmpty() && error == null -> item { Text("No folders in here.", fontSize = 13.sp, color = RoxyColors.muted) }
                else -> items(list, key = { it.path }) { d ->
                    FolderRow(name = d.name, sub = when (d.videos) { 0 -> d.path; in 1..999 -> "${d.videos} videos  ·  ${d.path}"; else -> "1000+ videos  ·  ${d.path}" }, icon = Icons.Outlined.Folder) { open(d.path) }
                }
            }
        }
        if (path.isNotEmpty()) {
            Box(modifier = Modifier.fillMaxWidth().background(RoxyColors.background).navigationBarsPadding().padding(horizontal = PhonePad, vertical = 10.dp)) {
                PlayButton(
                    "Use this folder", icon = Icons.Filled.Check, pill = true, container = RoxyColors.accent, content = OnBrand, modifier = Modifier.fillMaxWidth(),
                    onClick = { name = path.substringAfterLast('/').ifEmpty { path }; kind = if (name.contains("movie", true) || name.contains("film", true)) "movies" else "series"; anime = name.contains("anime", true); choosing = true },
                )
            }
        }
    }
    if (choosing) {
        AlertDialog(
            onDismissRequest = { choosing = false },
            containerColor = RoxyColors.surfaceHigh, titleContentColor = RoxyColors.onBackground, textContentColor = RoxyColors.onBackground,
            title = { Text("New library") },
            text = {
                Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    Text(path, fontSize = 12.sp, color = RoxyColors.muted)
                    if (videosHere == 0 && dirs.isNullOrEmpty()) Text("This folder looks empty.", fontSize = 12.sp, color = RoxyColors.error)
                    RoxyTextField(name, { name = it }, label = "Name")
                    KindChooser(kind, anime, onKind = { kind = it }, onAnime = { anime = it })
                    error?.let { Text(it, fontSize = 12.sp, color = RoxyColors.error) }
                }
            },
            confirmButton = {
                TextButton(onClick = {
                    if (saving) return@TextButton
                    saving = true
                    scope.launch {
                        runCatching { app.api.addMediaFolder(path, name.trim(), kind, anime) }
                            .onSuccess { choosing = false; app.items.userDataChanged(); onBack() }
                            .onFailure { t -> error = t.message?.substringAfter(": ")?.trim('{', '}', '"')?.removePrefix("error\":\"") }
                        saving = false
                    }
                }) { Text(if (saving) "Adding…" else "Add and scan", color = RoxyColors.accent, fontWeight = FontWeight.SemiBold) }
            },
            dismissButton = { TextButton(onClick = { choosing = false }) { Text("Cancel", color = RoxyColors.muted) } },
        )
    }
}

@Composable
private fun FolderRow(name: String, sub: String, icon: androidx.compose.ui.graphics.vector.ImageVector, onClick: () -> Unit) {
    Row(
        modifier = Modifier.fillMaxWidth().clip(RoundedCornerShape(12.dp)).background(RoxyColors.surface).clickable(onClick = onClick).padding(horizontal = 14.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(icon, null, tint = RoxyColors.onBackground, modifier = Modifier.size(22.dp))
        Column(modifier = Modifier.weight(1f).padding(start = 12.dp)) {
            Text(name, fontSize = 15.sp, color = RoxyColors.onBackground, maxLines = 1, overflow = TextOverflow.Ellipsis)
            if (sub.isNotEmpty()) Text(sub, fontSize = 12.sp, color = RoxyColors.muted, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        Spacer(Modifier.size(8.dp))
        Icon(Icons.Filled.ChevronRight, null, tint = RoxyColors.muted)
    }
}
