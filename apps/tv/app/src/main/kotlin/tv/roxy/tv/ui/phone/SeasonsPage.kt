package tv.roxy.tv.ui.phone

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import tv.roxy.tv.ui.theme.RoxyColors

/** One row of the Seasons page: its name, the count on the right, a note under the name. */
data class SeasonEntry(val key: String, val label: String, val count: String?, val selected: Boolean, val note: String? = null)

/**
 * Crunchyroll's season picker: a full page with an X and "Seasons", then one row per
 * season (name left, "24 Episodes" right), the open one in the brand colour.
 */
@Composable
fun SeasonsPage(entries: List<SeasonEntry>, onPick: (SeasonEntry) -> Unit, onDismiss: () -> Unit) {
    Dialog(onDismissRequest = onDismiss, properties = DialogProperties(usePlatformDefaultWidth = false, decorFitsSystemWindows = false)) {
        Column(modifier = Modifier.fillMaxSize().background(RoxyColors.background).statusBarsPadding().navigationBarsPadding()) {
            Row(modifier = Modifier.fillMaxWidth().padding(horizontal = 6.dp, vertical = 4.dp), verticalAlignment = Alignment.CenterVertically) {
                HeaderIcon(Icons.Filled.Close, "Close", onClick = onDismiss)
                Text("Seasons", fontSize = 20.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(start = 14.dp))
            }
            Box(modifier = Modifier.fillMaxWidth().height(1.dp).background(RoxyColors.surfaceHigh))
            LazyColumn(modifier = Modifier.fillMaxSize(), contentPadding = PaddingValues(vertical = 6.dp)) {
                items(entries, key = { it.key }) { e ->
                    Row(
                        modifier = Modifier.fillMaxWidth().clickable { onPick(e) }.padding(horizontal = PhonePad, vertical = 16.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Column(modifier = Modifier.weight(1f).padding(end = 12.dp)) {
                            Text(
                                e.label, fontSize = 17.sp, maxLines = 1, overflow = TextOverflow.Ellipsis,
                                fontWeight = if (e.selected) FontWeight.SemiBold else FontWeight.Normal,
                                color = if (e.selected) RoxyColors.accent else RoxyColors.onBackground,
                            )
                            e.note?.let { Text(it, fontSize = 12.sp, color = RoxyColors.muted) }
                        }
                        e.count?.let { Text(it, fontSize = 14.sp, color = RoxyColors.muted) }
                    }
                }
            }
        }
    }
}

/**
 * Under a season the library lacks: not out yet ("Coming Apr 2027", or no date known),
 * else whether it was requested. A season already airing counts as out.
 */
internal fun seasonNote(airDate: String?, requested: Boolean): String {
    val date = airDate?.takeIf { it.length >= 10 }?.let { runCatching { java.time.LocalDate.parse(it.take(10)) }.getOrNull() }
    return when {
        date == null -> if (requested) "Requested  ·  not released yet" else "Not released yet"
        date.isAfter(java.time.LocalDate.now()) -> "Coming " + date.format(java.time.format.DateTimeFormatter.ofPattern("MMM yyyy")) + if (requested) "  ·  requested" else ""
        requested -> "Requested"
        else -> "Not in your library"
    }
}

/** "Nov 5" this year, "Nov 5, 2027" otherwise; null for an unreadable date. */
internal fun friendlyDate(iso: String): String? {
    val d = iso.takeIf { it.length >= 10 }?.let { runCatching { java.time.LocalDate.parse(it.take(10)) }.getOrNull() } ?: return null
    val pattern = if (d.year == java.time.LocalDate.now().year) "MMM d" else "MMM d, yyyy"
    return d.format(java.time.format.DateTimeFormatter.ofPattern(pattern))
}

internal fun episodesLabel(n: Int): String? = when {
    n <= 0 -> null
    n == 1 -> "1 Episode"
    else -> "$n Episodes"
}
