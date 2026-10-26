package tv.roxy.tv.ui.phone

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material3.CircularProgressIndicator
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
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import coil3.compose.AsyncImage
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.ArtCandidate
import tv.roxy.tv.api.Item
import tv.roxy.tv.ui.theme.RoxyColors

/**
 * Admin's art picker (Jellyfin's "Images" tab, on a phone): every backdrop, poster or
 * logo TMDB and fanart.tv hold for the title, the one in use ticked, tap to switch.
 * The choice is stored and locked on the server, so a metadata refresh keeps it.
 */
@Composable
fun ArtPickerDialog(item: Item, type: String, onDismiss: () -> Unit, onChanged: () -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    val scope = rememberCoroutineScope()
    var candidates by remember { mutableStateOf<List<ArtCandidate>?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf<String?>(null) } // URL being applied
    var source by remember { mutableStateOf("all") }
    LaunchedEffect(item.id, type) {
        runCatching { app.api.imageCandidates(item.id, type).candidates }
            .onSuccess { candidates = it }
            .onFailure { t -> Timber.w(t, "candidates"); error = t.message }
    }
    val title = when (type) { "backdrop" -> "Banner"; "poster" -> "Poster"; "hero" -> "Home banner"; else -> "Logo" }
    val aspect = when (type) { "backdrop" -> 16f / 9f; "poster", "hero" -> 2f / 3f; else -> 2.5f }
    val columns = if (type == "poster" || type == "hero") 3 else 2

    Dialog(onDismissRequest = onDismiss, properties = DialogProperties(usePlatformDefaultWidth = false, decorFitsSystemWindows = false)) {
        Column(modifier = Modifier.fillMaxSize().background(RoxyColors.background).statusBarsPadding().navigationBarsPadding()) {
            Row(modifier = Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
                BackCircle(onDismiss)
                Column(modifier = Modifier.padding(start = 12.dp).weight(1f)) {
                    Text("$title for ${item.title}", fontSize = 18.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, maxLines = 1)
                    Text("Tap one to use it. Your pick survives metadata refreshes.", fontSize = 12.sp, color = RoxyColors.muted)
                }
            }
            val all = candidates
            val sources = all?.map { it.source }?.distinct().orEmpty()
            if (sources.size > 1) {
                Row(modifier = Modifier.padding(horizontal = PhonePad, vertical = 4.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    Chip("All", selected = source == "all", onClick = { source = "all" })
                    sources.forEach { s -> Chip(if (s == "fanart") "fanart.tv" else "TMDB", selected = source == s, onClick = { source = s }) }
                }
            }
            when {
                all == null && error != null -> Text(error ?: "", color = RoxyColors.error, modifier = Modifier.padding(PhonePad))
                all == null -> Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { CircularProgressIndicator(color = RoxyColors.accent, strokeWidth = 3.dp, strokeCap = StrokeCap.Round, modifier = Modifier.size(36.dp)) }
                all.isEmpty() && type == "hero" -> Text("No textless posters for this title. The Home banner falls back to the poster.", color = RoxyColors.muted, modifier = Modifier.padding(PhonePad))
                all.isEmpty() -> Text("No alternatives for this title. A fanart.tv key (Account → Server) adds more for most shows.", color = RoxyColors.muted, modifier = Modifier.padding(PhonePad))
                else -> {
                    val shown = if (source == "all") all else all.filter { it.source == source }
                    LazyVerticalGrid(
                        columns = GridCells.Fixed(columns), modifier = Modifier.fillMaxSize(), contentPadding = PaddingValues(PhonePad),
                        horizontalArrangement = Arrangement.spacedBy(10.dp), verticalArrangement = Arrangement.spacedBy(10.dp),
                    ) {
                        items(shown, key = { it.url }) { c ->
                            Column {
                                Box(
                                    modifier = Modifier.fillMaxWidth().aspectRatio(aspect).clip(RoundedCornerShape(6.dp))
                                        .background(if (type == "logo") Color(0xFF2A2A32) else RoxyColors.surfaceHigh)
                                        .border(2.dp, if (c.selected) RoxyColors.accent else Color.Transparent, RoundedCornerShape(6.dp))
                                        .clickable(enabled = busy == null && !c.selected) {
                                            busy = c.url
                                            scope.launch {
                                                runCatching { app.api.setImage(item.id, type, c.url) }
                                                    .onSuccess { candidates = all.map { it.copy(selected = it.url == c.url) }; onChanged() }
                                                    .onFailure { t -> error = t.message }
                                                busy = null
                                            }
                                        },
                                ) {
                                    AsyncImage(model = c.preview, contentDescription = null, contentScale = if (type == "logo") ContentScale.Fit else ContentScale.Crop, modifier = Modifier.fillMaxSize().padding(if (type == "logo") 10.dp else 0.dp))
                                    if (c.selected) Icon(Icons.Filled.Check, contentDescription = "In use", tint = Color.White, modifier = Modifier.align(Alignment.TopEnd).padding(6.dp).size(22.dp).background(RoxyColors.accent, RoundedCornerShape(11.dp)).padding(3.dp))
                                    if (busy == c.url) Box(modifier = Modifier.fillMaxSize().background(Color(0x80000000)), contentAlignment = Alignment.Center) { CircularProgressIndicator(color = Color.White, strokeWidth = 2.dp, modifier = Modifier.size(24.dp)) }
                                }
                                Text(
                                    listOfNotNull(if (c.source == "fanart") "fanart.tv" else "TMDB", c.lang.takeIf { it.isNotEmpty() }?.uppercase(), if (c.width > 0) "${c.width}×${c.height}" else null).joinToString("  ·  "),
                                    fontSize = 10.sp, color = RoxyColors.muted, maxLines = 1, modifier = Modifier.padding(top = 4.dp),
                                )
                            }
                        }
                    }
                }
            }
            error?.takeIf { all != null }?.let { Text(it, color = RoxyColors.error, fontSize = 12.sp, modifier = Modifier.padding(PhonePad)) }
        }
    }
}
