package tv.roxy.tv.ui.phone

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.ui.graphics.Color
import tv.roxy.tv.ui.components.RoxyTextField
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import coil3.compose.AsyncImage
import kotlinx.coroutines.launch
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.AdminRequest
import tv.roxy.tv.ui.theme.RoxyColors
import java.time.Duration
import java.time.Instant

/** "just now", "5 min ago", "3 h ago", "2 days ago". */
private fun ago(iso: String): String = runCatching {
    val d = Duration.between(Instant.parse(iso), Instant.now())
    when {
        d.toMinutes() < 1 -> "just now"
        d.toMinutes() < 60 -> "${d.toMinutes()} min ago"
        d.toHours() < 24 -> "${d.toHours()} h ago"
        d.toDays() == 1L -> "yesterday"
        else -> "${d.toDays()} days ago"
    }
}.getOrDefault("")

private fun what(r: AdminRequest): String? = when {
    r.type == "movie" -> null
    r.seasons == listOf(-1) || (r.seasons.isEmpty() && r.episodes.isNotEmpty()) -> "${r.episodes.size} episode${if (r.episodes.size == 1) "" else "s"}"
    r.seasons.isEmpty() -> "Whole series"
    else -> r.seasons.filter { it >= 0 }.sorted().joinToString(", ") { if (it == 0) "Specials" else "S$it" }
}

/**
 * Admin: every request in one place (request app's Requests page). Waiting ones first, with
 * Approve / Decline; then what is downloading, what failed, and the rest. A row opens the
 * title in Discover.
 */
@Composable
fun PhoneRequests(onBack: () -> Unit, onOpen: (type: String, tmdbId: Int) -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    val scope = rememberCoroutineScope()
    var list by remember { mutableStateOf<List<AdminRequest>?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var filter by rememberSaveable { mutableStateOf("all") }
    var busy by remember { mutableStateOf<String?>(null) }
    var declining by remember { mutableStateOf<AdminRequest?>(null) }
    var reason by remember { mutableStateOf("") }
    suspend fun reload() {
        runCatching { app.api.adminRequests() }.onSuccess { list = it; error = null }.onFailure { error = it.message }
    }
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { scope.launch { reload() } }
    // An approve / decline / retry the server refused: kept apart from [error], which reload() clears.
    var actionError by remember { mutableStateOf<String?>(null) }
    fun act(id: String, block: suspend () -> Unit) {
        if (busy != null) return
        busy = id
        actionError = null
        scope.launch { runCatching { block() }.onFailure { actionError = it.message ?: "Something went wrong" }; reload(); busy = null }
    }

    val all = list.orEmpty()
    val waiting = all.filter { it.status == "approval" }
    val problems = all.filter { it.status == "failed" || it.download?.isProblem == true }
    val active = all.filter { it.status == "sent" && it.download?.isActive == true }
    // Keep the list current while it shows: fast while something downloads.
    PollWhileResumed(if (active.isNotEmpty()) PollFastMs else PollSlowMs) { reload() }
    val tab = filter
    val shown = when (tab) {
        "waiting" -> waiting
        "active" -> active
        "problems" -> problems
        else -> all
    }

    Column(modifier = Modifier.fillMaxSize().statusBarsPadding()) {
        Row(modifier = Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
            BackCircle(onBack)
            Text("Requests", fontSize = 20.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(start = 12.dp))
        }
        Row(modifier = Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(horizontal = PhonePad), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Chip("All · ${all.size}", selected = tab == "all", onClick = { filter = "all" })
            Chip("Waiting · ${waiting.size}", selected = tab == "waiting", onClick = { filter = "waiting" })
            Chip("Downloading · ${active.size}", selected = tab == "active", onClick = { filter = "active" })
            Chip("Problems · ${problems.size}", selected = tab == "problems", onClick = { filter = "problems" })
        }
        // Requests that failed to reach Sonarr / Radarr are retried every 10 minutes; this sends them now.
        if (all.any { it.status == "failed" }) {
            GreyButton(if (busy == "retry") "Retrying…" else "Retry failed", modifier = Modifier.padding(start = PhonePad, top = 8.dp).height(38.dp), onClick = { act("retry") { app.api.retryRequests() } })
        }
        (actionError ?: error)?.let { Text(it, fontSize = 13.sp, color = RoxyColors.error, modifier = Modifier.padding(horizontal = PhonePad, vertical = 6.dp)) }
        LazyColumn(modifier = Modifier.fillMaxSize(), contentPadding = PaddingValues(start = PhonePad, end = PhonePad, top = 12.dp, bottom = 32.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            when {
                list == null -> items(4) { Bone(Modifier.fillMaxWidth().height(110.dp)) }
                shown.isEmpty() -> item {
                    Text(
                        when (tab) { "waiting" -> "Nothing is waiting for approval."; "active" -> "Nothing is downloading right now."; "problems" -> "No problems."; else -> "No requests yet." },
                        fontSize = 14.sp, color = RoxyColors.muted, modifier = Modifier.padding(top = 20.dp),
                    )
                }
                else -> items(shown, key = { it.id }) { r -> RequestCard(r, busy == r.id, onOpen = { onOpen(r.type, r.tmdbId) },
                    onApprove = { act(r.id) { app.api.approveRequest(r.id) } },
                    onDecline = { reason = ""; declining = r }) }
            }
        }
    }
    declining?.let { r ->
        AlertDialog(
            onDismissRequest = { declining = null },
            containerColor = RoxyColors.surfaceHigh, titleContentColor = RoxyColors.onBackground, textContentColor = RoxyColors.onBackground,
            title = { Text("Decline ${r.title}?") },
            text = { RoxyTextField(reason, { reason = it }, label = "Reason (optional, shown to them)") },
            confirmButton = {
                TextButton(onClick = { declining = null; act(r.id) { app.api.declineRequest(r.id, reason.trim()) } }) { Text("Decline", color = RoxyColors.error) }
            },
            dismissButton = { TextButton(onClick = { declining = null }) { Text("Cancel", color = Color.White) } },
        )
    }
}

@Composable
private fun RequestCard(r: AdminRequest, busy: Boolean, onOpen: () -> Unit, onApprove: () -> Unit, onDecline: () -> Unit) {
    Column(modifier = Modifier.fillMaxWidth().clip(RoundedCornerShape(12.dp)).background(RoxyColors.surface).clickable(onClick = onOpen).padding(10.dp)) {
        Row {
            Box(modifier = Modifier.width(58.dp).aspectRatio(2f / 3f).clip(RoundedCornerShape(6.dp)).background(RoxyColors.surfaceHigh)) {
                if (!r.poster.isNullOrBlank()) AsyncImage(model = r.poster, contentDescription = r.title, contentScale = ContentScale.Crop, modifier = Modifier.fillMaxSize())
            }
            Column(modifier = Modifier.weight(1f).padding(start = 12.dp)) {
                Text(r.title + (r.year?.let { " ($it)" } ?: ""), fontSize = 15.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground, maxLines = 2, overflow = TextOverflow.Ellipsis)
                Text(
                    listOfNotNull(if (r.type == "movie") "Movie" else "Series", what(r)).joinToString("  ·  "),
                    fontSize = 12.sp, color = RoxyColors.muted,
                )
                Text("${r.userName.ifEmpty { "Someone" }}  ·  ${ago(r.createdAt)}", fontSize = 12.sp, color = RoxyColors.muted)
                val dl = r.download
                val (line, color) = when {
                    r.status == "approval" -> "Waiting for your approval" to RoxyColors.accent
                    r.status == "declined" -> ("Declined" + (r.error?.takeIf { it.isNotBlank() }?.let { "  ·  $it" } ?: "")) to RoxyColors.error
                    r.status == "failed" -> ("Couldn't send  ·  ${r.error ?: "unknown error"}") to RoxyColors.error
                    r.status == "pending" -> "Waiting for ${if (r.type == "movie") "Radarr" else "Sonarr"}" to RoxyColors.muted
                    dl != null -> stateLine(dl) to stateColor(dl.state)
                    else -> "Sent to ${if (r.type == "movie") "Radarr" else "Sonarr"}" to RoxyColors.muted
                }
                Text(line, fontSize = 12.sp, color = color, maxLines = 2, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 4.dp))
            }
        }
        if (r.status == "approval") {
            Row(modifier = Modifier.fillMaxWidth().padding(top = 10.dp), horizontalArrangement = Arrangement.spacedBy(10.dp, Alignment.End)) {
                GreyButton(if (busy) "…" else "Decline", modifier = Modifier.height(38.dp), onClick = onDecline)
                PlayButton(if (busy) "…" else "Approve", icon = Icons.Filled.Check, height = 38.dp, pill = true, container = RoxyColors.accent, content = OnBrand, onClick = onApprove)
            }
        }
    }
}

/** One line for a download: "Downloading  ·  45%", "Stalled", "In the library". */
private fun stateLine(d: tv.roxy.tv.api.DownloadStatus): String = listOfNotNull(
    stateWord(d.state),
    (d.progress * 100).toInt().takeIf { d.state == "downloading" && it > 0 }?.let { "$it%" },
    d.message?.takeIf { d.isProblem && it.isNotBlank() },
).joinToString("  ·  ")
