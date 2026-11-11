package tv.roxy.tv.ui.phone

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.expandVertically
import androidx.compose.animation.shrinkVertically
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
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
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.ChevronRight
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.outlined.CloudOff
import androidx.compose.material.icons.outlined.Download
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.drop
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.data.OfflineWords
import tv.roxy.tv.data.downloads.DownloadEntry
import tv.roxy.tv.data.downloads.shortEpisode
import tv.roxy.tv.data.offlineWords
import tv.roxy.tv.ui.components.ProgressBar
import tv.roxy.tv.ui.components.RoxyImage
import tv.roxy.tv.ui.components.widthPx
import tv.roxy.tv.ui.theme.RoxyColors

/*
 * The phone with the server out of reach, the way the big streaming apps do it: a quiet
 * strip over the bottom bar instead of a red alarm, pages that say "You're offline" and
 * point at the downloads, and the downloads themselves standing in wherever they can.
 */

/** Where an offline page sends people: the Downloads page, or one downloaded title. */
class OfflineNav(val downloads: () -> Unit, val title: (String) -> Unit)

/** Set by PhoneRoot; null in previews and tests (the buttons that need it are left out). */
val LocalOfflineNav = staticCompositionLocalOf<OfflineNav?> { null }

/** Height of [OfflineStrip]; the tab pages add it to their bottom padding while it shows. */
val OfflineStripHeight = 26.dp

/** The offline notices' words for the phone as it is now (see [offlineWords]). */
@Composable
fun rememberOfflineWords(): OfflineWords {
    val app = RoxyApp.get(LocalContext.current)
    val hasNetwork by app.server.hasNetwork.collectAsStateWithLifecycle()
    val index by app.downloads.index.collectAsStateWithLifecycle()
    return remember(hasNetwork, index) { offlineWords(hasNetwork, index.entries.values.count { it.isDone }) }
}

/**
 * The strip over the bottom bar: grey "No network connection" (or "Can't reach your Roxy
 * server") while the server is away, then green "Back online" for a moment when it answers.
 */
@Composable
fun OfflineStrip() {
    val app = RoxyApp.get(LocalContext.current)
    val online by app.server.online.collectAsStateWithLifecycle()
    val words = rememberOfflineWords()
    var back by remember { mutableStateOf(false) }
    LaunchedEffect(Unit) { app.server.backOnline.drop(1).collect { back = true; delay(2500); back = false } }
    AnimatedVisibility(!online || back, enter = expandVertically(), exit = shrinkVertically()) {
        Row(
            modifier = Modifier.fillMaxWidth().height(OfflineStripHeight).background(if (online) Color(0xFF1E8E3E) else Color(0xFF26262B)),
            verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.Center,
        ) {
            Icon(if (online) Icons.Filled.Check else Icons.Outlined.CloudOff, contentDescription = null, tint = Color.White.copy(alpha = if (online) 1f else 0.8f), modifier = Modifier.size(14.dp))
            Text(if (online) "Back online" else words.strip, fontSize = 12.sp, fontWeight = FontWeight.Medium, color = Color.White.copy(alpha = if (online) 1f else 0.9f), maxLines = 1, modifier = Modifier.padding(start = 6.dp))
        }
    }
}

/** "Try again" that says it is looking for a moment: the answer takes up to a few seconds either way. */
@Composable
fun rememberRetry(onRetry: () -> Unit): Pair<String, () -> Unit> {
    val app = RoxyApp.get(LocalContext.current)
    var checking by remember { mutableStateOf(0) }
    LaunchedEffect(checking) { if (checking > 0) { delay(2500); checking = 0 } }
    return (if (checking > 0) "Checking…" else "Try again") to { checking++; app.server.checkSoon(); onRetry() }
}

/** "Try again" as quiet text with the turning arrow: the second choice under a page's main button, or a notice's only one. */
@Composable
fun RetryText(onRetry: () -> Unit, modifier: Modifier = Modifier) {
    val (word, retry) = rememberRetry(onRetry)
    Row(modifier = modifier.clip(RoundedCornerShape(50)).clickable(onClick = retry).padding(horizontal = 14.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
        Icon(Icons.Filled.Refresh, contentDescription = null, tint = Color.White, modifier = Modifier.size(18.dp))
        Text(word, fontSize = 14.sp, fontWeight = FontWeight.SemiBold, color = Color.White, maxLines = 1, modifier = Modifier.padding(start = 6.dp))
    }
}

/**
 * A page that cannot load because the server is away: what is wrong, the white button the
 * rest of the app uses for its main action (to the downloads; Try again when the page has
 * no way there) and Try again as quiet text under it. The page reloads by itself when the
 * server is back (its caller's OnServerBack).
 */
@Composable
fun OfflinePage(onRetry: () -> Unit, modifier: Modifier = Modifier) {
    val words = rememberOfflineWords()
    val nav = LocalOfflineNav.current
    Column(modifier = modifier.fillMaxSize().padding(horizontal = 40.dp, vertical = 24.dp), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.Center) {
        Icon(Icons.Outlined.CloudOff, contentDescription = null, tint = RoxyColors.muted, modifier = Modifier.size(56.dp))
        Text(words.title, fontSize = 22.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, textAlign = TextAlign.Center, modifier = Modifier.padding(top = 16.dp))
        Text(words.body, fontSize = 14.sp, lineHeight = 21.sp, color = RoxyColors.muted, textAlign = TextAlign.Center, modifier = Modifier.padding(top = 8.dp, bottom = 24.dp))
        if (nav != null) {
            PlayButton("Go to Downloads", icon = Icons.Outlined.Download, pill = true, onClick = nav.downloads)
            RetryText(onRetry, modifier = Modifier.padding(top = 6.dp))
        } else {
            val (word, retry) = rememberRetry(onRetry)
            PlayButton(word, icon = Icons.Filled.Refresh, pill = true, onClick = retry)
        }
    }
}

/**
 * The notice at the top of a Home made of downloads: one line (what is wrong, that the
 * page shows downloads) with Try again at its end, so the rows below stay the page's subject.
 */
@Composable
fun OfflineNote(onRetry: () -> Unit, modifier: Modifier = Modifier) {
    val words = rememberOfflineWords()
    Row(modifier = modifier.fillMaxWidth().padding(start = PhonePad, end = PhonePad - 14.dp), verticalAlignment = Alignment.CenterVertically) {
        Column(modifier = Modifier.weight(1f).padding(end = 8.dp)) {
            Text(words.title, fontSize = 22.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(words.short, fontSize = 13.sp, lineHeight = 18.sp, color = RoxyColors.muted, modifier = Modifier.padding(top = 2.dp))
        }
        RetryText(onRetry)
    }
}

/** A row's heading with "See all" at its end. */
@Composable
fun RowHeading(title: String, modifier: Modifier = Modifier, onSeeAll: (() -> Unit)? = null) {
    Row(modifier = modifier.fillMaxWidth().padding(start = PhonePad, end = PhonePad - 8.dp, bottom = 4.dp), verticalAlignment = Alignment.CenterVertically) {
        Text(title, fontSize = 18.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.weight(1f))
        if (onSeeAll != null) Row(modifier = Modifier.clip(RoundedCornerShape(50)).clickable(onClick = onSeeAll).padding(start = 8.dp, top = 6.dp, bottom = 6.dp), verticalAlignment = Alignment.CenterVertically) {
            Text("See all", fontSize = 13.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.muted)
            Icon(Icons.Filled.ChevronRight, contentDescription = null, tint = RoxyColors.muted, modifier = Modifier.size(20.dp))
        }
    }
}
