package tv.roxy.tv.ui.phone

import androidx.compose.foundation.background
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Download
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

internal fun stateWord(state: String): String = when (state) {
    "downloading" -> "Downloading"
    "importing" -> "Adding to library"
    "queued" -> "Queued"
    "searching" -> "Searching"
    "notFound" -> "Not found yet" // a download, not the show: "not out yet" is "waiting"
    "waiting" -> "Not out yet"
    "stalled" -> "Stalled"
    "attention" -> "Needs attention"
    "failed" -> "Failed"
    "complete" -> "Downloaded"
    else -> state
}

private fun pct(p: Double) = "${(p * 100).toInt()}%"

/** "Downloading S1 E5 · 45%", "Downloading 3 episodes · 45%", "Downloading 45%" for a poster. */
internal fun DownloadStatus.shortLine(isMovie: Boolean): String = when (state) {
    "downloading", "stalled", "importing" -> {
        val active = activeEpisodes
        val what = when {
            isMovie || active.isEmpty() -> null
            active.size == 1 -> "S${active[0].season} E${active[0].episode}"
            else -> "${active.size} eps"
        }
        // Short: the poster's ring already says "downloading". "S1 E5 · 45%", "3 eps · 40%",
        // "Stalled · 37%", "Adding to library".
        when {
            state == "importing" -> stateWord(state)
            state == "stalled" -> "Stalled · ${pct(progress)}"
            what != null -> "$what · ${pct(progress)}"
            else -> "Downloading · ${pct(progress)}"
        }
    }
    else -> stateWord(state)
}

/**
 * The poster's corner box while something downloads: which episodes and how far,
 * "S1 E5 · 45%", "S1 E1–E9 · 41%", or "45%" for a movie. Amber when stalled.
 */
@Composable
internal fun ProgressBadge(d: DownloadStatus, isMovie: Boolean, modifier: Modifier = Modifier) {
    val active = d.activeEpisodes.sortedWith(compareBy({ it.season }, { it.episode }))
    val which = when {
        isMovie || active.isEmpty() -> null
        active.size == 1 -> "S${active[0].season} E${active[0].episode}"
        else -> "${active.size} eps" // the exact episodes are in the list a tap opens
    }
    val how = if (d.state == "importing") "Adding" else pct(d.progress)
    Box(modifier = modifier.widthIn(max = 96.dp).clip(RoundedCornerShape(4.dp)).background(Color(0xE6000000)).padding(horizontal = 6.dp, vertical = 3.dp)) {
        Text(listOfNotNull(which, how).joinToString(" · "), fontSize = 10.sp, lineHeight = 12.sp, fontWeight = FontWeight.Bold, color = stateColor(d.state), maxLines = 1)
    }
}

/** The corner box for a request that is not downloading: "Searching", "Not out yet", "Failed"... */
@Composable
internal fun StateBox(d: DownloadStatus, modifier: Modifier = Modifier) {
    Box(modifier = modifier.clip(RoundedCornerShape(4.dp)).background(Color(0xE6000000)).padding(horizontal = 6.dp, vertical = 3.dp)) {
        Text(if (d.state == "waiting" && d.waitingFor == "digital") "In cinemas" else stateWord(d.state), fontSize = 10.sp, lineHeight = 12.sp, fontWeight = FontWeight.Bold, color = if (d.state in setOf("searching", "notFound", "waiting", "queued")) Color.White else stateColor(d.state), maxLines = 1)
    }
}

/** What a long-press on a request poster shows: each episode in the download client with its bar. */
@Composable
internal fun ActiveDownloadsList(d: DownloadStatus, title: String, isMovie: Boolean, modifier: Modifier = Modifier) {
    val active = d.activeEpisodes
    // Moving ones get a bar each, stalled first, then the furthest along; queued and
    // importing fold into one line each, so a whole season stays a short list.
    val moving = active.filter { it.state == "downloading" || it.state == "stalled" }
        .sortedWith(compareBy<tv.roxy.tv.api.EpisodeDownload>({ if (it.state == "stalled") 0 else 1 }, { -it.progress }))
    val queued = active.filter { it.state == "queued" }
    val importing = active.filter { it.state == "importing" }
    Column(modifier = modifier.width(270.dp).padding(horizontal = 14.dp, vertical = 10.dp)) {
        Text(title, fontSize = 14.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, maxLines = 1, overflow = TextOverflow.Ellipsis)
        Text(
            listOfNotNull(
                if (isMovie || active.isEmpty()) null else "${active.size} episode${if (active.size == 1) "" else "s"}",
                pct(d.progress), timeLeft(d.timeLeftSec),
            ).joinToString(" · "),
            fontSize = 12.sp, color = stateColor(d.state), modifier = Modifier.padding(top = 2.dp, bottom = 10.dp),
        )
        Column(
            modifier = Modifier.heightIn(max = 300.dp).verticalScroll(rememberScrollState()),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            if (isMovie || active.isEmpty()) {
                BarRow(if (isMovie) "Movie" else "Download", d.progress, d.state, d.timeLeftSec)
            } else {
                moving.forEach { BarRow("S${it.season} E${it.episode}", it.progress, it.state, it.timeLeftSec) }
                if (importing.isNotEmpty()) FoldRow("Adding to library", importing, "importing")
                if (queued.isNotEmpty()) FoldRow("Queued", queued, "queued")
            }
        }
        if (!isMovie && d.wanted > 1) Text("${d.have} of ${d.wanted} episodes downloaded", fontSize = 12.sp, color = RoxyColors.muted, modifier = Modifier.padding(top = 10.dp))
    }
}

@Composable
private fun BarRow(label: String, progress: Double, state: String, left: Int) {
    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(label, fontSize = 13.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground, modifier = Modifier.weight(1f))
            Text(listOfNotNull(if (state == "stalled") "Stalled" else null, pct(progress), timeLeft(left)?.removeSuffix(" left")).joinToString(" · "), fontSize = 12.sp, color = stateColor(state))
        }
        DownloadBar(progress, state, modifier = Modifier.fillMaxWidth().clip(RoundedCornerShape(2.dp)))
    }
}

/** "Queued · S1 E10–E20 (11)": several episodes in one line. */
@Composable
private fun FoldRow(word: String, eps: List<tv.roxy.tv.api.EpisodeDownload>, state: String) {
    val sorted = eps.sortedWith(compareBy({ it.season }, { it.episode }))
    val first = sorted.first()
    val last = sorted.last()
    val range = when {
        sorted.size == 1 -> "S${first.season} E${first.episode}"
        first.season == last.season -> "S${first.season} E${first.episode}–E${last.episode}"
        else -> "S${first.season} E${first.episode} – S${last.season} E${last.episode}"
    }
    Row(verticalAlignment = Alignment.CenterVertically) {
        Text(word, fontSize = 13.sp, fontWeight = FontWeight.SemiBold, color = stateColor(state), modifier = Modifier.width(118.dp))
        Text(if (sorted.size == 1) range else "$range (${sorted.size})", fontSize = 12.sp, color = RoxyColors.onBackground, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

/** One episode's line under its title: "Downloading 45% · 2 min left", "Failed · reason", "Airs Nov 3". */
internal fun episodeLine(e: tv.roxy.tv.api.EpisodeDownload): String = when (e.state) {
    "downloading", "stalled" -> listOfNotNull("${stateWord(e.state)} ${pct(e.progress)}", timeLeft(e.timeLeftSec), e.message?.takeIf { e.state == "stalled" }).joinToString("  ·  ")
    "waiting" -> day(e.airDate)?.let { "Airs $it" } ?: "Not aired yet"
    "failed", "attention" -> listOfNotNull(stateWord(e.state), e.message).joinToString("  ·  ")
    else -> stateWord(e.state)
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
        "notFound" -> "No download found yet"
        "waiting" -> when { !isMovie -> "Not aired yet"; d.waitingFor == "digital" -> "In cinemas"; else -> "Not released yet" }
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
            "waiting" -> when {
                !isMovie -> day(d.nextAir)?.let { "First episode airs $it" }
                d.waitingFor == "digital" -> day(d.nextAir)?.let { "Radarr fetches it once it is out digitally ($it)" } ?: "Radarr fetches it once it is out digitally"
                else -> day(d.nextAir)?.let { "Releases $it" }
            }
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
        // The server decides: no search over a running search or download, none right after
        // the last one, no replacing a download that only just stalled.
        // Never "Search again" while it is searching (or downloading): that is what it is doing.
        val canSearch = d.canSearch && d.state !in setOf("searching", "downloading", "queued", "importing")
        val canReplace = admin && d.canRetry
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
