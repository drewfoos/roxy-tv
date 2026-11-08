package tv.roxy.tv.ui.phone

import android.Manifest
import android.content.pm.PackageManager
import android.os.Build
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ChevronRight
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.OfflinePin
import androidx.compose.material.icons.filled.Pause
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Stop
import androidx.compose.material.icons.filled.Wifi
import androidx.compose.material.icons.outlined.Download
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
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
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.core.content.ContextCompat
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.Item
import tv.roxy.tv.data.downloads.DownloadEntry
import tv.roxy.tv.data.downloads.DownloadState
import tv.roxy.tv.data.downloads.formatBytes
import tv.roxy.tv.ui.components.ProgressBar
import tv.roxy.tv.ui.components.RoxyImage
import tv.roxy.tv.ui.components.widthPx
import tv.roxy.tv.ui.playItem
import tv.roxy.tv.ui.theme.RoxyColors
import androidx.compose.ui.graphics.vector.path
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.SolidColor

/*
 * Downloads, Netflix-style: a download control on every episode row, "Download Season N"
 * under the season dropdown, a Download button on movies, and a Downloads page (grouped by
 * show, storage bar) reachable from Account and from Home when the server is away.
 */

/**
 * Hands back a function that queues items for download, asking for the notification
 * permission first on Android 13+ (the progress notification is the only visible sign a
 * download is running once you leave the page; the download runs either way).
 */
@Composable
fun rememberDownloadStarter(): (items: List<Item>, title: Item, seasonName: String?) -> Unit {
    val context = LocalContext.current
    val app = RoxyApp.get(context)
    val ask = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { }
    var short by remember { mutableStateOf<Pair<Long, Long>?>(null) } // (needed, free) when there is not enough room
    short?.let { (needed, free) ->
        AlertDialog(
            onDismissRequest = { short = null },
            containerColor = RoxyColors.surfaceHigh, titleContentColor = RoxyColors.onBackground, textContentColor = RoxyColors.onBackground,
            title = { Text("Not enough space") },
            text = { Text("This needs ${formatBytes(needed)} but only ${formatBytes(free)} is free on this phone. Delete some downloads or free up storage, then try again.") },
            confirmButton = { TextButton(onClick = { short = null }) { Text("OK", color = Color.White) } },
        )
    }
    return remember {
        { all, title, seasonName ->
            // Episodes already here (or on their way) are skipped, so they don't count toward the space check.
            val items = all.filter { app.downloads.entry(it.id).let { e -> e == null || e.state == DownloadState.Failed } }
            // Room for the whole batch plus a margin, before anything is queued; a season
            // that would stop two-thirds through is worse than one that never starts.
            val needed = items.sumOf { it.primarySource?.size ?: 0L }
            val free = app.downloads.freeBytes()
            if (needed > 0 && free < needed + (200L shl 20)) {
                short = needed to free
            } else {
                if (Build.VERSION.SDK_INT >= 33 && ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) {
                    ask.launch(Manifest.permission.POST_NOTIFICATIONS)
                }
                app.downloads.enqueue(items, title, seasonName)
                Timber.i("queued %d download(s) for %s (%s, %s free)", items.size, title.title, formatBytes(needed), formatBytes(free))
            }
        }
    }
}

/**
 * Crunchyroll's download glyph: a thin arrow down onto a line (Material's are a filled
 * arrow, or an arrow in a circle).
 */
val DownloadArrow: ImageVector by lazy {
    ImageVector.Builder(name = "DownloadArrow", defaultWidth = 24.dp, defaultHeight = 24.dp, viewportWidth = 24f, viewportHeight = 24f).apply {
        path(stroke = SolidColor(Color.White), strokeLineWidth = 1.8f, strokeLineCap = StrokeCap.Round, strokeLineJoin = StrokeJoin.Round) {
            moveTo(12f, 3.5f); lineTo(12f, 15.5f) // the shaft
            moveTo(6.5f, 10f); lineTo(12f, 15.5f); lineTo(17.5f, 10f) // the head
            moveTo(5f, 20f); lineTo(19f, 20f) // the line it lands on
        }
    }.build()
}

/**
 * The per-item download control (episode rows, movie page, Downloads page): an arrow to
 * start, a ring while it runs (tap for Pause / Delete), a pin when done (tap for Delete),
 * a warning when it failed (tap for Retry / Delete).
 */
@Composable
fun DownloadButton(item: Item, onDownload: () -> Unit, modifier: Modifier = Modifier, size: Dp = 26.dp) {
    val app = RoxyApp.get(LocalContext.current)
    val index by app.downloads.index.collectAsStateWithLifecycle()
    val entry = index.entries[item.id]
    var open by remember { mutableStateOf(false) }
    var confirmDelete by remember { mutableStateOf(false) }

    Box(modifier = modifier.size(size + 12.dp).clickable(enabled = item.isPlayable || entry != null) { if (entry == null) onDownload() else open = true }, contentAlignment = Alignment.Center) {
        when (entry?.state) {
            null -> Icon(DownloadArrow, contentDescription = "Download", tint = if (item.isPlayable) RoxyColors.onBackground.copy(alpha = 0.85f) else RoxyColors.muted, modifier = Modifier.size(size))
            DownloadState.Done -> Icon(Icons.Filled.OfflinePin, contentDescription = "Downloaded", tint = RoxyColors.accent, modifier = Modifier.size(size))
            DownloadState.Failed -> Icon(Icons.Outlined.ErrorOutline, contentDescription = "Download failed", tint = RoxyColors.error, modifier = Modifier.size(size))
            DownloadState.Queued, DownloadState.Downloading, DownloadState.Paused, DownloadState.WaitingForWifi -> {
                val running = entry.state == DownloadState.Downloading
                val fraction = entry.fraction
                if (entry.state == DownloadState.Queued && fraction == 0f) {
                    CircularProgressIndicator(modifier = Modifier.size(size), color = RoxyColors.accent, trackColor = ChipGrey, strokeWidth = 2.5.dp)
                } else {
                    CircularProgressIndicator(progress = { fraction }, modifier = Modifier.size(size), color = RoxyColors.accent, trackColor = ChipGrey, strokeWidth = 2.5.dp)
                }
                val glyph = when (entry.state) {
                    DownloadState.Paused -> Icons.Filled.PlayArrow
                    DownloadState.WaitingForWifi -> Icons.Filled.Wifi
                    else -> Icons.Filled.Stop
                }
                Icon(glyph, contentDescription = null, tint = if (running || entry.state == DownloadState.Queued) RoxyColors.accent else RoxyColors.muted, modifier = Modifier.size(size * 0.5f))
            }
        }
        DropdownMenu(expanded = open, onDismissRequest = { open = false }, containerColor = RoxyColors.surfaceHigh) {
            when (entry?.state) {
                DownloadState.Done -> {
                    DropdownMenuItem(text = { Text(entry.sizeLine(), color = RoxyColors.muted, fontSize = 12.sp) }, onClick = { open = false }, enabled = false)
                    DropdownMenuItem(text = { Text("Delete download", color = Color.White) }, leadingIcon = { Icon(Icons.Filled.Delete, null, tint = Color.White) }, onClick = { open = false; confirmDelete = true })
                }
                DownloadState.Failed -> {
                    DropdownMenuItem(text = { Text(entry.error ?: "Download failed", color = RoxyColors.error, fontSize = 12.sp) }, onClick = { open = false }, enabled = false)
                    DropdownMenuItem(text = { Text("Retry", color = Color.White) }, leadingIcon = { Icon(Icons.Filled.Refresh, null, tint = Color.White) }, onClick = { open = false; app.downloads.resume(item.id) })
                    DropdownMenuItem(text = { Text("Delete", color = Color.White) }, leadingIcon = { Icon(Icons.Filled.Delete, null, tint = Color.White) }, onClick = { open = false; app.downloads.delete(item.id) })
                }
                DownloadState.Paused -> {
                    DropdownMenuItem(text = { Text("Resume download", color = Color.White) }, leadingIcon = { Icon(Icons.Filled.PlayArrow, null, tint = Color.White) }, onClick = { open = false; app.downloads.resume(item.id) })
                    DropdownMenuItem(text = { Text("Delete", color = Color.White) }, leadingIcon = { Icon(Icons.Filled.Delete, null, tint = Color.White) }, onClick = { open = false; app.downloads.delete(item.id) })
                }
                null -> {}
                else -> {
                    if (entry.state == DownloadState.WaitingForWifi) DropdownMenuItem(text = { Text("Waiting for Wi-Fi", color = RoxyColors.muted, fontSize = 12.sp) }, onClick = { open = false }, enabled = false)
                    DropdownMenuItem(text = { Text("Pause download", color = Color.White) }, leadingIcon = { Icon(Icons.Filled.Pause, null, tint = Color.White) }, onClick = { open = false; app.downloads.pause(item.id) })
                    DropdownMenuItem(text = { Text("Cancel download", color = Color.White) }, leadingIcon = { Icon(Icons.Filled.Delete, null, tint = Color.White) }, onClick = { open = false; app.downloads.delete(item.id) })
                }
            }
        }
    }
    if (confirmDelete) {
        ConfirmDialog(
            title = "Delete download?",
            text = "${item.displayLabel} will be removed from this device.",
            confirm = "Delete",
            onConfirm = { confirmDelete = false; app.downloads.delete(item.id) },
            onDismiss = { confirmDelete = false },
        )
    }
}

private fun DownloadEntry.sizeLine(): String = "Downloaded  ·  ${formatBytes(bytesTotal)}"

/**
 * Netflix's "Download Season N" row under the season dropdown. Shows the size of what is
 * left, progress while the season downloads (tap for Pause all / Cancel), and a pin once
 * every episode is on the device (tap to delete the season's downloads).
 */
@Composable
fun SeasonDownloadRow(episodes: List<Item>, seasonLabel: String, onDownloadAll: (List<Item>) -> Unit, compact: Boolean = false) {
    val app = RoxyApp.get(LocalContext.current)
    val index by app.downloads.index.collectAsStateWithLifecycle()
    val playable = episodes.filter { it.isPlayable && it.primarySource != null }
    if (playable.isEmpty()) return
    val entries = playable.mapNotNull { index.entries[it.id] }
    val active = entries.filter { it.state.isActive }
    val paused = entries.filter { it.state == DownloadState.Paused }
    val done = entries.count { it.isDone }
    val remaining = playable.filter { ep -> index.entries[ep.id].let { it == null || it.state == DownloadState.Failed } }
    val remainingBytes = remaining.sumOf { it.primarySource?.size ?: 0L }
    var open by remember { mutableStateOf(false) }
    var confirm by remember { mutableStateOf(false) }

    val (icon, label, tint) = when {
        active.isNotEmpty() -> {
            val bytesDone = entries.sumOf { if (it.isDone) it.bytesTotal else it.bytesDone }
            val bytesAll = entries.sumOf { it.bytesTotal }
            val pct = if (bytesAll > 0) (bytesDone * 100 / bytesAll).toInt() else 0
            Triple(Icons.Outlined.Download, "Downloading $seasonLabel  ·  $done of ${playable.size}  ·  $pct%", RoxyColors.accent)
        }
        paused.isNotEmpty() && remaining.isEmpty() -> Triple(Icons.Filled.Pause, "Season download paused  ·  $done of ${playable.size}", RoxyColors.muted)
        remaining.isEmpty() -> Triple(Icons.Filled.OfflinePin, "$seasonLabel downloaded  ·  ${formatBytes(entries.sumOf { it.bytesTotal })}", RoxyColors.accent)
        done > 0 || paused.isNotEmpty() -> Triple(Icons.Outlined.Download, "Download ${remaining.size} remaining  ·  ${formatBytes(remainingBytes)}", RoxyColors.onBackground)
        else -> Triple(Icons.Outlined.Download, "Download $seasonLabel  ·  ${formatBytes(remainingBytes)}", RoxyColors.onBackground)
    }

    val onTap = {
        when {
            active.isNotEmpty() || (paused.isNotEmpty() && remaining.isEmpty()) -> open = true
            remaining.isEmpty() -> open = true
            else -> onDownloadAll(remaining)
        }
    }
    Box {
        if (compact) {
            // The end of the season row, Crunchyroll's way: the icon with a short white
            // word beside it ("Download all" to start, the state otherwise). The 36 dp box
            // and 24 dp glyph match DownloadButton, so the icons line up with the rows below.
            val short = when {
                active.isNotEmpty() -> "Downloading"
                paused.isNotEmpty() && remaining.isEmpty() -> "Paused"
                remaining.isEmpty() -> "Downloaded"
                done > 0 || paused.isNotEmpty() -> "Download ${remaining.size} more"
                else -> "Download all"
            }
            Row(
                modifier = Modifier.clip(RoundedCornerShape(18.dp)).clickable(onClick = onTap).padding(start = 10.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(short, fontSize = 13.sp, fontWeight = FontWeight.SemiBold, color = if (tint == RoxyColors.onBackground) Color.White else tint)
                Icon(icon, contentDescription = label, tint = tint, modifier = Modifier.size(36.dp).padding(6.dp))
            }
        } else Row(
            modifier = Modifier.fillMaxWidth().clickable(onClick = onTap).padding(horizontal = PhonePad, vertical = 10.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Icon(icon, contentDescription = null, tint = tint, modifier = Modifier.size(22.dp))
            Text(label, fontSize = 14.sp, fontWeight = FontWeight.SemiBold, color = tint, modifier = Modifier.padding(start = 10.dp), maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        DropdownMenu(expanded = open, onDismissRequest = { open = false }, containerColor = RoxyColors.surfaceHigh) {
            if (active.isNotEmpty()) {
                DropdownMenuItem(text = { Text("Pause season download", color = Color.White) }, leadingIcon = { Icon(Icons.Filled.Pause, null, tint = Color.White) }, onClick = { open = false; app.downloads.pauseAll(active.map { it.itemId }) })
                DropdownMenuItem(text = { Text("Cancel remaining", color = Color.White) }, leadingIcon = { Icon(Icons.Filled.Delete, null, tint = Color.White) }, onClick = { open = false; active.forEach { app.downloads.delete(it.itemId) } })
            }
            if (paused.isNotEmpty()) {
                DropdownMenuItem(text = { Text("Resume season download", color = Color.White) }, leadingIcon = { Icon(Icons.Filled.PlayArrow, null, tint = Color.White) }, onClick = { open = false; paused.forEach { app.downloads.resume(it.itemId) } })
            }
            if (done > 0) {
                DropdownMenuItem(text = { Text("Delete season downloads", color = Color.White) }, leadingIcon = { Icon(Icons.Filled.Delete, null, tint = Color.White) }, onClick = { open = false; confirm = true })
            }
        }
    }
    if (confirm) {
        ConfirmDialog(
            title = "Delete $seasonLabel downloads?",
            text = "$done episode${if (done == 1) "" else "s"} will be removed from this device.",
            confirm = "Delete",
            onConfirm = { confirm = false; entries.forEach { app.downloads.delete(it.itemId) } },
            onDismiss = { confirm = false },
        )
    }
}

/** Movie page: a grey Download button that turns into progress, then "Downloaded". */
@Composable
fun MovieDownloadButton(movie: Item, modifier: Modifier = Modifier, onDownload: () -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    val index by app.downloads.index.collectAsStateWithLifecycle()
    val entry = index.entries[movie.id]
    val label = when (entry?.state) {
        null -> "Download" + (movie.primarySource?.size?.takeIf { it > 0 }?.let { "  ·  ${formatBytes(it)}" } ?: "")
        DownloadState.Done -> "Downloaded"
        DownloadState.Failed -> "Download failed  ·  Retry"
        DownloadState.Paused -> "Download paused  ·  ${(entry.fraction * 100).toInt()}%"
        DownloadState.WaitingForWifi -> "Waiting for Wi-Fi"
        DownloadState.Queued if entry.error != null -> "${queuedWord(entry)}  ·  ${(entry.fraction * 100).toInt()}%"
        else -> "Downloading  ·  ${(entry.fraction * 100).toInt()}%"
    }
    Row(modifier = modifier.fillMaxWidth().height(42.dp).clip(RoundedCornerShape(4.dp)).background(ButtonGrey), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.Center) {
        DownloadButton(movie, onDownload = onDownload, size = 22.dp)
        Text(label, color = Color.White, fontSize = 15.sp, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(end = 14.dp))
    }
}

/**
 * The Downloads page: shows and movies on this device, newest first, with what is still
 * coming in; the storage bar at the bottom. Works entirely from the local index.
 */
@Composable
fun PhoneDownloads(onBack: (() -> Unit)?, onOpenTitle: (String) -> Unit, onBrowse: () -> Unit, bottomInset: Dp = 0.dp) {
    val context = LocalContext.current
    val app = RoxyApp.get(context)
    val index by app.downloads.index.collectAsStateWithLifecycle()
    val wifiOnly by app.prefs.downloadsWifiOnly.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    val titles = remember(index) { app.downloads.titles() }
    var confirmAll by remember { mutableStateOf(false) }

    // onBack == null: shown as My Lists' Downloads tab, under that page's header and tabs.
    Column(modifier = Modifier.fillMaxSize().then(if (onBack != null) Modifier.statusBarsPadding() else Modifier)) {
        if (onBack != null) PageHeader("Downloads", onBack = onBack, trailing = {
            if (titles.isNotEmpty()) Icon(Icons.Filled.Delete, contentDescription = "Delete all", tint = RoxyColors.muted, modifier = Modifier.size(24.dp).clickable { confirmAll = true })
        }) else if (titles.isNotEmpty()) Row(modifier = Modifier.fillMaxWidth().padding(start = PhonePad, end = PhonePad, top = 8.dp), horizontalArrangement = Arrangement.End) {
            Text("Delete all", fontSize = 13.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.muted, modifier = Modifier.clickable { confirmAll = true }.padding(4.dp))
        }
        if (titles.isEmpty()) {
            Column(modifier = Modifier.fillMaxWidth().weight(1f).padding(32.dp), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.Center) {
                Icon(Icons.Outlined.Download, contentDescription = null, tint = RoxyColors.muted, modifier = Modifier.size(56.dp))
                Text("Nothing downloaded yet", fontSize = 20.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(top = 16.dp))
                Text(
                    "Episodes and movies you download appear here and play even when your server is out of reach.",
                    fontSize = 14.sp, lineHeight = 20.sp, color = RoxyColors.muted, textAlign = TextAlign.Center, modifier = Modifier.padding(top = 12.dp, bottom = 20.dp),
                )
                PlayButton("Find something to download", icon = Icons.Outlined.Download, pill = true, container = RoxyColors.accent, content = OnBrand, onClick = onBrowse)
            }
        } else {
            LazyColumn(modifier = Modifier.fillMaxWidth().weight(1f), contentPadding = PaddingValues(bottom = 16.dp)) {
                items(titles, key = { it.id }) { t ->
                    val movie = t.entries.singleOrNull()?.takeIf { it.item.isMovie }
                    if (movie != null) {
                        MovieDownloadRow(movie, onPlay = { scope.launch { playItem(context, movie.item).onFailure { Timber.w(it, "play failed") } } })
                    } else {
                        TitleDownloadRow(t.title, t.entries, onClick = { onOpenTitle(t.id) })
                    }
                }
            }
        }
        StorageBar(used = app.downloads.usedBytes(), free = app.downloads.freeBytes(), wifiOnly = wifiOnly, onWifiOnly = { scope.launch { app.prefs.setDownloadsWifiOnly(it) } })
        Spacer(Modifier.height(bottomInset))
    }
    if (confirmAll) {
        ConfirmDialog(
            title = "Delete all downloads?",
            text = "Everything downloaded to this device (${formatBytes(app.downloads.usedBytes())}) will be removed.",
            confirm = "Delete all",
            onConfirm = { confirmAll = false; app.downloads.deleteAll() },
            onDismiss = { confirmAll = false },
        )
    }
}

/** A downloaded show: its episodes by season, each with a thumb, size and the download control. */
@Composable
fun PhoneDownloadedTitle(titleId: String, onBack: () -> Unit) {
    val context = LocalContext.current
    val app = RoxyApp.get(context)
    val index by app.downloads.index.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    val group = remember(index) { app.downloads.titles().firstOrNull { it.id == titleId } }
    if (group == null) {
        // The last episode was deleted from this page: nothing left to show.
        Column(modifier = Modifier.fillMaxSize().statusBarsPadding()) { PageHeader("Downloads", onBack = onBack) }
        return
    }
    val bySeason = group.entries.groupBy { it.seasonNumber ?: 0 }
    Column(modifier = Modifier.fillMaxSize().statusBarsPadding()) {
        PageHeader(group.title.title, onBack = onBack)
        LazyColumn(modifier = Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
            bySeason.toSortedMap().forEach { (season, entries) ->
                item(key = "season-$season") {
                    val name = entries.firstOrNull()?.seasonName?.takeIf { it.isNotBlank() } ?: if (season == 0) "Specials" else "Season $season"
                    Text(name, fontSize = 15.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(start = PhonePad, top = 16.dp, bottom = 4.dp))
                }
                items(entries, key = { it.itemId }) { e ->
                    DownloadedEpisodeRow(e, onPlay = { scope.launch { playItem(context, e.item).onFailure { Timber.w(it, "play failed") } } })
                }
            }
        }
    }
}

@Composable
private fun PageHeader(title: String, onBack: () -> Unit, trailing: @Composable () -> Unit = {}) {
    Row(modifier = Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
        BackCircle(onBack)
        Text(title, fontSize = 20.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f).padding(start = 12.dp))
        trailing()
        Spacer(Modifier.width(4.dp))
    }
}

/** A show on the Downloads page: poster, name, "3 episodes · 1.2 GB" (or progress), chevron. */
@Composable
private fun TitleDownloadRow(title: Item, entries: List<DownloadEntry>, onClick: () -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    val done = entries.count { it.isDone }
    val active = entries.count { it.state.isActive }
    val bytes = entries.sumOf { if (it.isDone) it.bytesTotal else it.bytesDone }
    val sub = buildList {
        if (done > 0) add("$done episode${if (done == 1) "" else "s"}  ·  ${formatBytes(bytes)}")
        if (active > 0) add("Downloading $active")
        val paused = entries.count { it.state == DownloadState.Paused }
        if (paused > 0) add("$paused paused")
        val failed = entries.count { it.state == DownloadState.Failed }
        if (failed > 0) add("$failed failed")
    }.joinToString("  ·  ")
    Row(modifier = Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = PhonePad, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
        Box(modifier = Modifier.width(64.dp).aspectRatio(2f / 3f).clip(RoundedCornerShape(4.dp)).background(RoxyColors.surfaceHigh)) {
            RoxyImage(app.downloads.image(app.api, title, "poster", widthPx(64.dp) * 2), title.title, modifier = Modifier.fillMaxSize(), placeholderTextSize = 10)
        }
        Column(modifier = Modifier.weight(1f).padding(start = 14.dp)) {
            Text(title.title, fontSize = 16.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground, maxLines = 2, overflow = TextOverflow.Ellipsis)
            Text(sub, fontSize = 12.sp, color = if (active > 0) RoxyColors.accent else RoxyColors.muted, modifier = Modifier.padding(top = 3.dp))
        }
        Icon(Icons.Filled.ChevronRight, contentDescription = null, tint = RoxyColors.muted)
    }
}

/** A downloaded movie: backdrop thumb, name, size, play on tap; the control gives Delete. */
@Composable
private fun MovieDownloadRow(e: DownloadEntry, onPlay: () -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    val item = e.item
    Row(modifier = Modifier.fillMaxWidth().clickable(enabled = e.isDone, onClick = onPlay).padding(horizontal = PhonePad, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
        Box(modifier = Modifier.width(120.dp).aspectRatio(16f / 9f).clip(RoundedCornerShape(4.dp)).background(RoxyColors.surfaceHigh)) {
            RoxyImage(app.downloads.image(app.api, item, "backdrop", widthPx(120.dp) * 2) ?: app.downloads.image(app.api, item, "poster", widthPx(120.dp) * 2), item.title, modifier = Modifier.fillMaxSize(), placeholderTextSize = 10)
            if (e.isDone) PlayBadge(size = 30.dp, modifier = Modifier.align(Alignment.Center))
            ProgressBar(item.progressFraction, modifier = Modifier.align(Alignment.BottomCenter))
        }
        Column(modifier = Modifier.weight(1f).padding(start = 12.dp)) {
            Text(item.title, fontSize = 15.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground, maxLines = 2, overflow = TextOverflow.Ellipsis)
            Text(entryLine(e), fontSize = 12.sp, color = if (e.state.isActive) RoxyColors.accent else RoxyColors.muted, modifier = Modifier.padding(top = 3.dp))
        }
        DownloadButton(item, onDownload = {})
    }
}

/** One episode under a downloaded show: thumb, "N. Title", runtime · size (or progress), the control. */
@Composable
private fun DownloadedEpisodeRow(e: DownloadEntry, onPlay: () -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    val ep = e.item
    val watched = ep.isWatched
    Row(modifier = Modifier.fillMaxWidth().clickable(enabled = e.isDone, onClick = onPlay).padding(horizontal = PhonePad, vertical = 8.dp).alpha(if (e.isDone) 1f else 0.8f), verticalAlignment = Alignment.CenterVertically) {
        Box(modifier = Modifier.width(120.dp).aspectRatio(16f / 9f).clip(RoundedCornerShape(4.dp)).background(RoxyColors.surfaceHigh)) {
            RoxyImage(app.downloads.image(app.api, ep, "thumb", widthPx(120.dp) * 2), ep.episodeCode, modifier = Modifier.fillMaxSize().alpha(if (watched) 0.6f else 1f), placeholderTextSize = 10)
            if (e.isDone) PlayBadge(size = 30.dp, modifier = Modifier.align(Alignment.Center))
            ProgressBar(ep.progressFraction, modifier = Modifier.align(Alignment.BottomCenter))
        }
        Column(modifier = Modifier.weight(1f).padding(start = 12.dp)) {
            val n = (ep.indexAired ?: ep.indexAbsolute)?.let { "$it. " } ?: ""
            Text(n + ep.title, fontSize = 14.sp, fontWeight = FontWeight.Medium, color = RoxyColors.onBackground, maxLines = 2, overflow = TextOverflow.Ellipsis)
            Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.padding(top = 3.dp)) {
                Text(listOfNotNull(ep.runtimeMinutes?.let { "${it}m" }, entryLine(e)).joinToString("  ·  "), fontSize = 12.sp, color = if (e.state.isActive) RoxyColors.accent else RoxyColors.muted)
                if (watched) { Spacer(Modifier.width(8.dp)); WatchedCheck(size = 14.dp) }
            }
        }
        DownloadButton(ep, onDownload = {})
    }
}

/** A queued entry says why it waits: the server is away, or it is retrying after a dropped connection. */
private fun queuedWord(e: DownloadEntry): String = when {
    e.error == tv.roxy.tv.data.downloads.DownloadService.WAITING_FOR_SERVER -> "Waiting for Roxy"
    e.error != null -> "Retrying"
    else -> "Queued"
}

private fun entryLine(e: DownloadEntry): String = when (e.state) {
    DownloadState.Done -> formatBytes(e.bytesTotal)
    DownloadState.Downloading -> "Downloading  ·  ${(e.fraction * 100).toInt()}%"
    DownloadState.Queued -> queuedWord(e) + if (e.bytesDone > 0) "  ·  ${(e.fraction * 100).toInt()}%" else ""
    DownloadState.Paused -> "Paused  ·  ${(e.fraction * 100).toInt()}%"
    DownloadState.WaitingForWifi -> "Waiting for Wi-Fi"
    DownloadState.Failed -> "Failed"
}

/** Netflix's storage line: a thin bar (this app's share of the disk) and the numbers, plus the Wi-Fi-only switch. */
@Composable
private fun StorageBar(used: Long, free: Long, wifiOnly: Boolean, onWifiOnly: (Boolean) -> Unit) {
    val total = (used + free).coerceAtLeast(1)
    Column(modifier = Modifier.fillMaxWidth().background(RoxyColors.surface).navigationBarsPadding().padding(horizontal = PhonePad, vertical = 12.dp)) {
        Box(modifier = Modifier.fillMaxWidth().height(6.dp).clip(RoundedCornerShape(3.dp)).background(ChipGrey)) {
            Box(modifier = Modifier.fillMaxHeight().fillMaxWidth((used.toFloat() / total).coerceIn(0.01f, 1f)).background(RoxyColors.accent))
        }
        Row(modifier = Modifier.fillMaxWidth().padding(top = 8.dp), verticalAlignment = Alignment.CenterVertically) {
            Box(Modifier.size(8.dp).clip(CircleShape).background(RoxyColors.accent))
            Text("Roxy ${formatBytes(used)}", fontSize = 12.sp, color = RoxyColors.onBackground, modifier = Modifier.padding(start = 6.dp))
            Spacer(Modifier.weight(1f))
            Text("${formatBytes(free)} free", fontSize = 12.sp, color = RoxyColors.muted)
        }
        Row(modifier = Modifier.fillMaxWidth().clickable { onWifiOnly(!wifiOnly) }.padding(top = 6.dp), verticalAlignment = Alignment.CenterVertically) {
            Icon(Icons.Filled.Wifi, contentDescription = null, tint = RoxyColors.muted, modifier = Modifier.size(16.dp))
            Text("Download over Wi-Fi only", fontSize = 13.sp, color = RoxyColors.onBackground, modifier = Modifier.weight(1f).padding(start = 8.dp))
            Text(if (wifiOnly) "On" else "Off", fontSize = 13.sp, fontWeight = FontWeight.SemiBold, color = if (wifiOnly) RoxyColors.accent else RoxyColors.muted)
        }
    }
}

@Composable
internal fun ConfirmDialog(title: String, text: String, confirm: String, onConfirm: () -> Unit, onDismiss: () -> Unit) {
    AlertDialog(
        onDismissRequest = onDismiss,
        containerColor = RoxyColors.surfaceHigh,
        titleContentColor = RoxyColors.onBackground,
        textContentColor = RoxyColors.onBackground,
        title = { Text(title) },
        text = { Text(text) },
        confirmButton = { TextButton(onClick = onConfirm) { Text(confirm, color = RoxyColors.error) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel", color = Color.White) } },
    )
}
