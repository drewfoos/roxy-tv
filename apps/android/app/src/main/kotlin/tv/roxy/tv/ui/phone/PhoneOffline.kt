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
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
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
 * The strip over the bottom bar: grey "No internet connection" (or "Can't reach your Roxy
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
        Box(modifier = Modifier.fillMaxWidth().height(OfflineStripHeight).background(if (online) Color(0xFF1E8E3E) else Color(0xFF2A2A2E)), contentAlignment = Alignment.Center) {
            Text(if (online) "Back online" else words.strip, fontSize = 12.sp, fontWeight = FontWeight.Medium, color = Color.White, maxLines = 1)
        }
    }
}

/**
 * A page that cannot load because the server is away: what is wrong, a way to the
 * downloads, and Try again (which also looks for the server at once). The page reloads by
 * itself when the server is back (its caller's OnServerBack).
 */
@Composable
fun OfflinePage(onRetry: () -> Unit, modifier: Modifier = Modifier) {
    val app = RoxyApp.get(LocalContext.current)
    val words = rememberOfflineWords()
    val nav = LocalOfflineNav.current
    // Try again answers within a few seconds either way; say that it is looking meanwhile.
    var checking by remember { mutableStateOf(0) }
    LaunchedEffect(checking) { if (checking > 0) { delay(2500); checking = 0 } }
    Column(modifier = modifier.fillMaxSize().padding(horizontal = 32.dp, vertical = 24.dp), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.Center) {
        Icon(Icons.Outlined.CloudOff, contentDescription = null, tint = RoxyColors.muted, modifier = Modifier.size(56.dp))
        Text(words.title, fontSize = 20.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, textAlign = TextAlign.Center, modifier = Modifier.padding(top = 16.dp))
        Text(words.body, fontSize = 14.sp, lineHeight = 20.sp, color = RoxyColors.muted, textAlign = TextAlign.Center, modifier = Modifier.padding(top = 10.dp, bottom = 20.dp))
        if (nav != null) {
            PlayButton("Go to Downloads", icon = Icons.Outlined.Download, pill = true, container = RoxyColors.accent, content = OnBrand, onClick = nav.downloads)
        }
        GreyButton(if (checking > 0) "Checking…" else "Try again", icon = Icons.Filled.Refresh, modifier = Modifier.padding(top = 12.dp), onClick = { checking++; app.server.checkSoon(); onRetry() })
    }
}

/** Continue Watching made from the downloads (see continueFromDownloads): wide tiles that play on tap. */
@Composable
fun DownloadsContinueRow(entries: List<DownloadEntry>, onPlay: (DownloadEntry) -> Unit, modifier: Modifier = Modifier) {
    val app = RoxyApp.get(LocalContext.current)
    val tileW: Dp = 220.dp
    Column(modifier = modifier.fillMaxWidth()) {
        Text("Continue watching", fontSize = 18.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(start = PhonePad, bottom = 10.dp))
        LazyRow(contentPadding = PaddingValues(horizontal = PhonePad), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            items(entries, key = { it.itemId }) { e ->
                val item = e.item
                Column(modifier = Modifier.width(tileW).clickable { onPlay(e) }) {
                    Box(modifier = Modifier.fillMaxWidth().aspectRatio(16f / 9f).clip(RoundedCornerShape(6.dp)).background(RoxyColors.surfaceHigh)) {
                        val px = widthPx(tileW)
                        val image = if (item.isEpisode) app.downloads.image(app.api, item, "thumb", px) else app.downloads.image(app.api, item, "backdrop", px) ?: app.downloads.image(app.api, item, "poster", px)
                        RoxyImage(image, item.title, modifier = Modifier.fillMaxSize(), placeholderTextSize = 12)
                        PlayBadge(size = 36.dp, modifier = Modifier.align(Alignment.Center))
                        ProgressBar(item.progressFraction, modifier = Modifier.align(Alignment.BottomCenter))
                    }
                    Text(item.seriesTitle?.takeIf { item.isEpisode && it.isNotEmpty() } ?: item.title, fontSize = 13.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 6.dp))
                    if (item.isEpisode) {
                        Text(listOf(shortEpisode(item), item.title).filter { it.isNotEmpty() }.joinToString("  ·  "), fontSize = 12.sp, color = RoxyColors.muted, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    }
                }
            }
        }
    }
}
