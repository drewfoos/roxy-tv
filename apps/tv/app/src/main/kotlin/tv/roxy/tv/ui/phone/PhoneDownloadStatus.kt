package tv.roxy.tv.ui.phone

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Search
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import tv.roxy.tv.api.DownloadGroup
import tv.roxy.tv.api.DownloadStatus
import tv.roxy.tv.ui.theme.RoxyColors
import java.time.Duration
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale

/*
 * Download status of a request, as the server's tracker reports it from Sonarr / Radarr:
 * a card on the Discover title page and a one-line form for posters. Unlike request app it
 * says what went wrong (stalled, failed, cannot import, nothing found) in the arr app's
 * own words, and offers to search again or try another release.
 */

private val Amber = Color(0xFFF5A524)

internal fun stateColor(state: String): Color = when (state) {
    "failed", "attention" -> RoxyColors.error
    "stalled" -> Amber
    "downloading", "importing", "complete" -> RoxyColors.accent
    else -> RoxyColors.muted
}

private fun stateWord(state: String): String = when (state) {
    "downloading" -> "Downloading"
    "importing" -> "Adding to library"
    "queued" -> "Queued"
    "searching" -> "Searching"
    "notFound" -> "No release yet"
    "waiting" -> "Not out yet"
    "stalled" -> "Stalled"
    "attention" -> "Needs attention"
    "failed" -> "Failed"
    "complete" -> "Downloaded"
    else -> state
}

private fun pct(p: Double) = "${(p * 100).toInt()}%"

/** "Downloading 45%" for a poster. */
internal fun DownloadStatus.shortLine(isMovie: Boolean): String = when (state) {
    "downloading", "stalled" -> "${stateWord(state)} ${pct(progress)}"
    else -> stateWord(state)
}

/** "12 min left", "1 h 5 min left". */
private fun timeLeft(sec: Int): String? {
    if (sec <= 0) return null
    val m = (sec + 59) / 60
    return if (m < 60) "$m min left" else "${m / 60} h ${m % 60} min left"
}

private fun parse(iso: String?): Instant? = iso?.let { runCatching { Instant.parse(it) }.getOrNull() }

/** "Nov 3" (with the year when it is not this year). */
private fun day(iso: String?): String? {
    val t = parse(iso) ?: return null
    val z = t.atZone(ZoneId.systemDefault())
    val pattern = if (z.year == java.time.LocalDate.now().year) "MMM d" else "MMM d, yyyy"
    return z.format(DateTimeFormatter.ofPattern(pattern, Locale.getDefault()))
}

/** "12 min ago", "3 h ago", "2 days ago". */
private fun ago(iso: String?): String? {
    val t = parse(iso) ?: return null
    val m = Duration.between(t, Instant.now()).toMinutes().coerceAtLeast(0)
    return when {
        m < 1 -> "just now"
        m < 60 -> "$m min ago"
        m < 48 * 60 -> "${m / 60} h ago"
        else -> "${m / 1440} days ago"
    }
}

/**
 * The status card under a requested title's facts: headline and percentage, a bar, a
 * line of detail, the arr app's message when something is wrong, one row per state
 * when the request has several downloads, and the actions that fit.
 */
@Composable
internal fun DownloadStatusCard(
    d: DownloadStatus,
    isMovie: Boolean,
    admin: Boolean,
    busy: Boolean,
    onSearchAgain: () -> Unit,
    onTryAnother: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val arr = if (isMovie) "Radarr" else "Sonarr"
    val episodes = !isMovie && d.wanted > 1
    val headline = when (d.state) {
        "searching" -> "Looking for a release"
        "notFound" -> "No release found yet"
        "waiting" -> if (isMovie) "Not released yet" else "Not aired yet"
        "queued" -> "Queued"
        "downloading" -> "Downloading"
        "stalled" -> "Download stalled"
        "importing" -> "Adding to your library"
        "attention" -> "Needs attention"
        "failed" -> "Download failed"
        "complete" -> if (!isMovie && d.upcoming > 0) "Up to date" else "Downloaded"
        else -> d.state
    }
    val detail = listOfNotNull(
        when (d.state) {
            "searching" -> "$arr is searching"
            "notFound" -> ago(d.lastSearch)?.let { "Searched $it · $arr keeps checking" } ?: "$arr keeps checking for new releases"
            "waiting" -> day(d.nextAir)?.let { if (isMovie) "Releases $it" else "First episode airs $it" }
            "downloading", "stalled" -> timeLeft(d.timeLeftSec)
            "complete" -> if (!isMovie && d.upcoming > 0) day(d.nextAir)?.let { "Next episode $it" } else "Adding to your library"
            else -> null
        },
        if (episodes) "${d.have} of ${d.wanted} episodes" else null,
    ).joinToString("  ·  ")
    val color = stateColor(d.state)
    Column(
        modifier = modifier.fillMaxWidth().clip(RoundedCornerShape(12.dp)).background(RoxyColors.surface).padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Box(modifier = Modifier.size(8.dp).clip(CircleShape).background(color))
            Text(headline, fontSize = 15.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(start = 8.dp).weight(1f))
            if (d.state == "downloading" || d.state == "stalled") Text(pct(d.progress), fontSize = 15.sp, fontWeight = FontWeight.Bold, color = color)
        }
        if (d.state == "downloading" || d.state == "stalled" || d.state == "importing") {
            DownloadBar(if (d.state == "importing") 1.0 else d.progress, d.state, modifier = Modifier.fillMaxWidth().clip(RoundedCornerShape(2.dp)))
        }
        if (detail.isNotEmpty()) Text(detail, fontSize = 13.sp, color = RoxyColors.muted)
        d.message?.takeIf { it.isNotBlank() }?.let {
            Text(it, fontSize = 13.sp, lineHeight = 18.sp, color = if (d.isProblem) color else RoxyColors.muted, maxLines = 4, overflow = TextOverflow.Ellipsis)
        }
        // Several downloads (a season fetched episode by episode): one line per state.
        if (d.groups.size > 1 || (d.groups.size == 1 && !isMovie && d.groups[0].count > 1)) {
            Column(modifier = Modifier.padding(top = 2.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                d.groups.forEach { GroupRow(it) }
            }
        }
        val canSearch = d.state == "notFound" || d.state == "failed"
        val canReplace = admin && d.isProblem
        if (canSearch || canReplace) {
            Row(horizontalArrangement = Arrangement.spacedBy(10.dp), modifier = Modifier.padding(top = 4.dp)) {
                if (canReplace) PlayButton(if (busy) "…" else "Try another release", icon = Icons.Filled.Refresh, pill = true, height = 38.dp, container = RoxyColors.accent, content = OnBrand, onClick = { if (!busy) onTryAnother() })
                if (canSearch) PlayButton(if (busy && !canReplace) "…" else "Search again", icon = Icons.Filled.Search, pill = true, height = 38.dp, container = ChipGrey, content = Color.White, onClick = { if (!busy) onSearchAgain() })
            }
        }
    }
}

@Composable
private fun GroupRow(g: DownloadGroup) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        Text(stateWord(g.state), fontSize = 12.sp, fontWeight = FontWeight.SemiBold, color = stateColor(g.state), modifier = Modifier.width(108.dp), maxLines = 1)
        Text(g.label, fontSize = 12.sp, color = RoxyColors.onBackground, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
        if (g.state == "downloading" || g.state == "stalled") Text(pct(g.progress), fontSize = 12.sp, color = RoxyColors.muted, modifier = Modifier.padding(start = 8.dp))
    }
}

/** A thin bar: brand blue while downloading, amber when stalled. */
@Composable
internal fun DownloadBar(progress: Double, state: String, modifier: Modifier = Modifier) {
    Box(modifier = modifier.height(4.dp).background(Color(0x55FFFFFF))) {
        Box(modifier = Modifier.fillMaxWidth(progress.toFloat().coerceIn(0f, 1f)).height(4.dp).background(stateColor(state)))
    }
}
