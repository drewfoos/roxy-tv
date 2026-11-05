package tv.roxy.tv.ui.tv

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ArrowBack
import androidx.compose.material.icons.filled.Replay
import androidx.compose.material.icons.filled.SkipPrevious
import androidx.compose.material.icons.outlined.Check
import androidx.compose.material.icons.outlined.Info
import androidx.compose.material.icons.outlined.Movie
import androidx.compose.material.icons.outlined.Tv
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.window.Dialog
import androidx.tv.material3.Text
import kotlinx.coroutines.launch
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.Item
import tv.roxy.tv.data.downloads.formatBytes
import tv.roxy.tv.ui.phone.markWatched
import tv.roxy.tv.ui.theme.RoxyColors

/**
 * Hold OK on an episode or film (Home's rows, a title page): Play from start, Go to show /
 * movie ([onGoTo], where the card is not already that page), Mark as watched / unwatched,
 * the whole [season] too on a show's page, and File info (codec, resolution, tracks, size),
 * since episodes come from different releases. Marking goes through markWatched, so the
 * page and every list reload, as on the phone.
 */
@Composable
fun TvItemMenu(
    item: Item,
    onDismiss: () -> Unit,
    season: Item? = null,
    onPlayFromStart: (() -> Unit)? = null,
    onGoTo: (() -> Unit)? = null,
) {
    val app = RoxyApp.get(LocalContext.current)
    val scope = rememberCoroutineScope()
    var info by remember { mutableStateOf(false) }
    val watched = item.isWatched // a rewatch under way is in progress, not watched
    val started = (item.userData?.positionMs ?: 0L) > 0 || item.userData?.played == true
    fun then(action: () -> Unit) { onDismiss(); action() }
    fun mark(id: String, on: Boolean) = then { scope.launch { app.markWatched(id, on) } }
    Dialog(onDismissRequest = onDismiss) {
        Column(modifier = Modifier.width(520.dp).background(RoxyColors.surfaceHigh).padding(vertical = 18.dp)) {
            val code = if (item.isEpisode) episodeShort(item) else ""
            val heading = if (item.isEpisode) listOfNotNull(item.seriesTitle, code.ifEmpty { null }).joinToString("  ") else item.title
            Text(heading, color = RoxyColors.onBackground, fontSize = 20.sp, fontWeight = FontWeight.Medium, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(horizontal = 24.dp))
            if (item.isEpisode && item.title.isNotBlank()) Text(item.title, color = TvSubtle, fontSize = 16.sp, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(horizontal = 24.dp))
            Column(modifier = Modifier.padding(top = 12.dp)) {
                if (info) {
                    FileInfo(item, onBack = { info = false })
                } else {
                    val first = remember { FocusRequester() }
                    val firstMod = Modifier.fillMaxWidth().focusRequester(first)
                    var firstUsed = false
                    fun mod(): Modifier = if (!firstUsed) { firstUsed = true; firstMod } else Modifier.fillMaxWidth()
                    if (onPlayFromStart != null && started) TvListRow(label = "Play from start", icon = Icons.Filled.SkipPrevious, selected = true, onClick = { then(onPlayFromStart) }, modifier = mod())
                    if (onGoTo != null) TvListRow(label = if (item.isMovie) "Go to movie" else "Go to show", icon = if (item.isMovie) Icons.Outlined.Movie else Icons.Outlined.Tv, selected = true, onClick = { then(onGoTo) }, modifier = mod())
                    TvListRow(
                        label = if (watched) "Mark as unwatched" else "Mark as watched",
                        icon = if (watched) Icons.Filled.Replay else Icons.Outlined.Check, selected = true,
                        onClick = { mark(item.id, !watched) }, modifier = mod(),
                    )
                    if (season != null) {
                        val name = season.title.ifEmpty { "this season" }
                        TvListRow(label = "Mark $name as watched", icon = Icons.Outlined.Check, selected = true, onClick = { mark(season.id, true) }, modifier = mod())
                        TvListRow(label = "Mark $name as unwatched", icon = Icons.Filled.Replay, selected = true, onClick = { mark(season.id, false) }, modifier = mod())
                    }
                    TvListRow(label = "File info", icon = Icons.Outlined.Info, selected = true, onClick = { info = true }, modifier = mod())
                    LaunchedEffect(Unit) { repeat(10) { withFrameNanos { }; if (runCatching { first.requestFocus() }.getOrDefault(false)) return@LaunchedEffect } }
                }
            }
        }
    }
}

/** The file's facts, as on the phone's File info: video, tracks, size, container, notes. Back returns to the menu. */
@Composable
private fun FileInfo(given: Item, onBack: () -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    // Episode lists carry the file; Home's cards and a film's page item may not.
    var item by remember(given.id) { mutableStateOf(given) }
    var failed by remember(given.id) { mutableStateOf(false) }
    LaunchedEffect(given.id) {
        if (given.mediaSources.isEmpty()) runCatching { app.api.item(given.id) }.onSuccess { item = it }.onFailure { failed = true }
    }
    val src = item.primarySource
    Column(modifier = Modifier.padding(horizontal = 24.dp).padding(bottom = 12.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        when {
            item.mediaSources.isNotEmpty() -> {
                val e = with(app.api) { item.toEpisode() }
                InfoLine("Video", e.videoLine)
                InfoLine("Tracks", e.tracksLine)
                InfoLine("Size", src?.size?.takeIf { it > 0 }?.let { formatBytes(it) })
                InfoLine("Container", src?.container)
                val flags = listOfNotNull(if (e.isHi10p) "Hi10P" else null, if (e.hasAss) "ASS subtitles" else null).joinToString(", ")
                if (flags.isNotEmpty()) InfoLine("Notes", flags)
            }
            failed -> Text("Couldn't load the file's details.", color = TvSubtle, fontSize = 16.sp)
            else -> Text("Loading…", color = TvSubtle, fontSize = 16.sp)
        }
    }
    val back = remember { FocusRequester() }
    TvListRow(label = "Back", icon = Icons.Filled.ArrowBack, selected = true, onClick = onBack, modifier = Modifier.fillMaxWidth().focusRequester(back))
    LaunchedEffect(Unit) { repeat(10) { withFrameNanos { }; if (runCatching { back.requestFocus() }.getOrDefault(false)) return@LaunchedEffect } }
}

@Composable
private fun InfoLine(label: String, value: String?) {
    if (value.isNullOrBlank()) return
    Column {
        Text(label, fontSize = 14.sp, color = TvSubtle)
        Text(value, fontSize = 17.sp, lineHeight = 23.sp, color = RoxyColors.onBackground, fontFamily = FontFamily.Monospace)
    }
}
