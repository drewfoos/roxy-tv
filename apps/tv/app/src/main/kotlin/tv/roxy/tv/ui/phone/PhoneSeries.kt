package tv.roxy.tv.ui.phone

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.asPaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.ArrowDropDown
import androidx.compose.material.icons.filled.Check
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.Item
import tv.roxy.tv.api.RelatedEntry
import tv.roxy.tv.api.RoxyApi
import tv.roxy.tv.ui.components.ProgressBar
import tv.roxy.tv.ui.components.RoxyImage
import tv.roxy.tv.ui.components.widthPx
import tv.roxy.tv.ui.detail.SeriesViewModel
import tv.roxy.tv.ui.playItem
import tv.roxy.tv.ui.theme.RoxyColors

/**
 * Netflix's phone title page: 16:9 backdrop, title and meta, full-width Play, My List,
 * synopsis, a season **dropdown** (One Piece has 40), then the episode list with watched
 * ticks. Same [SeriesViewModel] as the TV screen; shimmer skeleton while loading.
 */
@Composable
fun PhoneSeries(
    seriesId: String,
    fallbackTitle: String,
    onBack: () -> Unit,
    onOpenMovie: (Item) -> Unit,
    onOpenSeries: (Item) -> Unit,
) {
    val context = LocalContext.current
    val app = RoxyApp.get(context)
    val vm: SeriesViewModel = viewModel(key = "series:$seriesId") { SeriesViewModel(app.api, seriesId) }
    val state by vm.state.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { vm.load() }

    val series = state.series
    if (series == null) {
        if (state.loading) DetailSkeleton(onBack) else Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { Text(state.error ?: "Not found", color = RoxyColors.error) }
        return
    }

    fun play(item: Item) = scope.launch { playItem(context, item).onFailure { Timber.w(it, "play failed") } }
    var expanded by rememberSaveable { mutableStateOf(false) }
    val bottom = WindowInsets.navigationBars.asPaddingValues().calculateBottomPadding() + 24.dp

    LazyColumn(modifier = Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = bottom)) {
        item(key = "backdrop") { Backdrop(series, app.api, onBack) }
        item(key = "info") {
            Column(modifier = Modifier.padding(horizontal = PhonePad)) {
                Text(series.title, fontSize = 22.sp, lineHeight = 26.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground)
                val meta = listOfNotNull(
                    (series.year ?: series.premiered?.take(4)?.toIntOrNull())?.toString(),
                    state.seasons.size.takeIf { it > 0 }?.let { if (it == 1) "1 Season" else "$it Seasons" },
                    series.officialRating,
                    series.communityRating?.let { "★ %.1f".format(it) },
                ).joinToString("   ")
                if (meta.isNotEmpty()) Text(meta, fontSize = 13.sp, color = RoxyColors.muted, modifier = Modifier.padding(top = 6.dp))

                val playable = state.episodes.filter { it.isPlayable }
                val next = playable.firstOrNull { (it.userData?.positionMs ?: 0) > 0 && it.userData?.played != true }
                    ?: playable.firstOrNull { it.userData?.played != true }
                    ?: playable.firstOrNull()
                if (next != null) {
                    val resumable = (next.userData?.positionMs ?: 0) > 0
                    PlayButton(if (resumable) "Resume ${next.episodeCode}" else "Play ${next.episodeCode}".trim(), modifier = Modifier.fillMaxWidth().padding(top = 14.dp), onClick = { play(next) })
                }
                MyListButton(series, app.api, modifier = Modifier.fillMaxWidth().padding(top = 8.dp), onChanged = { vm.load() })
                series.overview?.takeIf { it.isNotBlank() }?.let {
                    Text(
                        it, fontSize = 14.sp, lineHeight = 20.sp, color = RoxyColors.onBackground.copy(alpha = 0.9f),
                        maxLines = if (expanded) Int.MAX_VALUE else 3, overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.fillMaxWidth().padding(top = 14.dp).clickable { expanded = !expanded },
                    )
                }
                val genres = series.genres.joinToString(", ")
                if (genres.isNotEmpty()) Text("Genres: $genres", fontSize = 12.sp, color = RoxyColors.muted, modifier = Modifier.padding(top = 8.dp))
            }
        }
        if (state.seasons.isNotEmpty() || state.related.isNotEmpty()) {
            item(key = "seasons") {
                val label = when {
                    state.showRelated -> "Movies & OVAs"
                    state.seasons.isEmpty() -> "Episodes"
                    else -> state.seasons[state.selectedSeason].let { it.title.ifEmpty { "Season ${it.indexAired ?: state.selectedSeason + 1}" } }
                }
                Box(modifier = Modifier.padding(start = PhonePad, top = 18.dp, bottom = 6.dp)) {
                    var open by remember { mutableStateOf(false) }
                    Chip(text = label, selected = false, trailing = Icons.Filled.ArrowDropDown, onClick = { open = true })
                    DropdownMenu(expanded = open, onDismissRequest = { open = false }, containerColor = RoxyColors.surfaceHigh) {
                        state.seasons.forEachIndexed { i, season ->
                            val name = season.title.ifEmpty { "Season ${season.indexAired ?: i + 1}" }
                            DropdownMenuItem(
                                text = { Text(name, color = Color.White, fontWeight = if (!state.showRelated && i == state.selectedSeason) FontWeight.Bold else FontWeight.Normal) },
                                onClick = { open = false; vm.selectSeason(i) },
                            )
                        }
                        if (state.related.isNotEmpty()) {
                            DropdownMenuItem(
                                text = { Text("Movies & OVAs", color = Color.White, fontWeight = if (state.showRelated) FontWeight.Bold else FontWeight.Normal) },
                                onClick = { open = false; vm.selectRelated() },
                            )
                        }
                    }
                }
            }
        }
        if (state.showRelated) {
            items(state.related.chunked(3)) { row ->
                RelatedGridRow(row, app.api, onOpen = { rel ->
                    val item = rel.item ?: return@RelatedGridRow
                    if (item.isMovie) onOpenMovie(item) else onOpenSeries(item)
                })
            }
        } else {
            if (state.episodesLoading && state.episodes.isEmpty()) {
                items(4) { EpisodeRowSkeleton() }
            } else if (state.episodes.isEmpty()) {
                item(key = "eps-empty") { Text("No episodes in this season.", color = RoxyColors.muted, modifier = Modifier.padding(PhonePad)) }
            }
            items(state.episodes, key = { it.id }) { ep ->
                PhoneEpisodeRow(ep, app.api, onClick = { if (ep.isPlayable) play(ep) })
            }
        }
    }
}

/** "+ My List" / "✓ My List": toggles the favourite flag that feeds the Watchlist tab. */
@Composable
fun MyListButton(item: Item, api: RoxyApi, modifier: Modifier = Modifier, onChanged: () -> Unit) {
    val scope = rememberCoroutineScope()
    var on by remember(item.id, item.userData?.isFavorite) { mutableStateOf(item.userData?.isFavorite == true) }
    GreyButton(if (on) "In My List" else "My List", modifier = modifier, icon = if (on) Icons.Filled.Check else Icons.Filled.Add, onClick = {
        val want = !on
        on = want
        scope.launch {
            runCatching { api.setFavorite(item.id, want) }
                .onFailure { Timber.w(it, "favorite failed"); on = !want }
                .onSuccess { onChanged() }
        }
    })
}

/** 16:9 backdrop fading into the page, with the round back button over it. */
@Composable
fun Backdrop(item: Item, api: RoxyApi, onBack: () -> Unit) {
    Box(modifier = Modifier.fillMaxWidth()) {
        Box(modifier = Modifier.fillMaxWidth().aspectRatio(16f / 9f)) {
            RoxyImage(api.imageUrl(item, "backdrop", widthPx(420.dp) * 2), item.title, modifier = Modifier.fillMaxSize(), placeholderTextSize = 16)
            Box(modifier = Modifier.matchParentSize().background(Brush.verticalGradient(0.6f to Color.Transparent, 1f to RoxyColors.background)))
        }
        BackCircle(onBack, modifier = Modifier.statusBarsPadding().padding(12.dp))
    }
}

/** Title page while it loads: backdrop, title lines, button and three episode rows as shimmer. */
@Composable
fun DetailSkeleton(onBack: () -> Unit) {
    Column(modifier = Modifier.fillMaxSize()) {
        Box(modifier = Modifier.fillMaxWidth()) {
            Bone(Modifier.fillMaxWidth().aspectRatio(16f / 9f), shape = RoundedCornerShape(0.dp))
            BackCircle(onBack, modifier = Modifier.statusBarsPadding().padding(12.dp))
        }
        Column(modifier = Modifier.padding(horizontal = PhonePad)) {
            BoneLine(width = 240.dp, height = 22.dp, modifier = Modifier.padding(top = 4.dp))
            BoneLine(width = 160.dp, height = 12.dp, modifier = Modifier.padding(top = 10.dp))
            Bone(Modifier.fillMaxWidth().height(42.dp).padding(top = 0.dp), shape = RoundedCornerShape(4.dp))
            BoneLine(width = 320.dp, height = 12.dp, modifier = Modifier.padding(top = 14.dp))
            BoneLine(width = 280.dp, height = 12.dp, modifier = Modifier.padding(top = 6.dp))
            BoneLine(width = 200.dp, height = 12.dp, modifier = Modifier.padding(top = 6.dp))
        }
        Spacer(Modifier.height(16.dp))
        repeat(3) { EpisodeRowSkeleton() }
    }
}

@Composable
private fun EpisodeRowSkeleton() {
    Column(modifier = Modifier.fillMaxWidth().padding(horizontal = PhonePad, vertical = 10.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            Bone(Modifier.width(130.dp).aspectRatio(16f / 9f), shape = RoundedCornerShape(4.dp))
            Column { BoneLine(width = 180.dp, height = 14.dp); BoneLine(width = 40.dp, height = 11.dp, modifier = Modifier.padding(top = 8.dp)) }
        }
        BoneLine(width = 300.dp, height = 11.dp, modifier = Modifier.padding(top = 10.dp))
        BoneLine(width = 220.dp, height = 11.dp, modifier = Modifier.padding(top = 5.dp))
    }
}

/** Netflix's episode row: thumb with a play badge, "N. Title", runtime and a watched tick, then the synopsis. */
@Composable
private fun PhoneEpisodeRow(ep: Item, api: RoxyApi, onClick: () -> Unit) {
    val thumbWidth = 130.dp
    val placeholder = !ep.isPlayable
    val watched = ep.userData?.played == true
    val badge = when (ep.availability) {
        "missing" -> "Missing"
        "unaired" -> ep.airDate?.takeIf { it.isNotBlank() }?.let { "Airs $it" } ?: "Unaired"
        else -> null
    }
    Column(
        modifier = Modifier.fillMaxWidth().clickable(enabled = !placeholder, onClick = onClick).padding(horizontal = PhonePad, vertical = 10.dp).alpha(if (placeholder) 0.45f else 1f),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            Box(modifier = Modifier.width(thumbWidth).aspectRatio(16f / 9f).clip(RoundedCornerShape(4.dp)).background(RoxyColors.surfaceHigh)) {
                RoxyImage(api.imageUrl(ep, "thumb", widthPx(thumbWidth) * 2), ep.episodeCode, modifier = Modifier.fillMaxSize().alpha(if (watched) 0.6f else 1f), placeholderTextSize = 12)
                if (!placeholder) {
                    PlayBadge(size = 34.dp, modifier = Modifier.align(Alignment.Center))
                    ProgressBar(ep.progressFraction, modifier = Modifier.align(Alignment.BottomCenter))
                }
            }
            Column(modifier = Modifier.weight(1f)) {
                val n = (ep.indexAired ?: ep.indexAbsolute)?.let { "$it. " } ?: ""
                Text(n + ep.title, fontSize = 14.sp, lineHeight = 18.sp, fontWeight = FontWeight.Medium, color = RoxyColors.onBackground, maxLines = 2, overflow = TextOverflow.Ellipsis)
                Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.padding(top = 3.dp)) {
                    val sub = listOfNotNull(
                        ep.runtimeMinutes?.let { "${it}m" },
                        badge,
                        if (placeholder && ep.availability == "missing" && !ep.airDate.isNullOrBlank()) "Aired ${ep.airDate}" else null,
                    ).joinToString("  ·  ")
                    if (sub.isNotEmpty()) Text(sub, fontSize = 12.sp, color = RoxyColors.muted)
                    if (watched) {
                        if (sub.isNotEmpty()) Spacer(Modifier.width(8.dp))
                        WatchedCheck(size = 15.dp)
                        Text("Watched", fontSize = 12.sp, color = RoxyColors.muted, modifier = Modifier.padding(start = 4.dp))
                    }
                }
            }
        }
        ep.overview?.takeIf { it.isNotBlank() }?.let {
            Text(it, fontSize = 13.sp, lineHeight = 17.sp, color = RoxyColors.muted, maxLines = 3, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 8.dp))
        }
    }
}

/** One grid row (up to three) of the franchise's related titles. */
@Composable
private fun RelatedGridRow(row: List<RelatedEntry>, api: RoxyApi, onOpen: (RelatedEntry) -> Unit) {
    Row(modifier = Modifier.fillMaxWidth().padding(horizontal = PhonePad, vertical = 4.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        row.forEach { rel ->
            val item = rel.item
            Column(modifier = Modifier.weight(1f)) {
                if (rel.inLibrary && item != null) {
                    PosterTile(item, api, modifier = Modifier.fillMaxWidth(), onClick = { onOpen(rel) })
                    Text(rel.label + (rel.year?.let { " · $it" } ?: ""), fontSize = 11.sp, color = RoxyColors.muted, maxLines = 2, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 4.dp))
                } else {
                    Box(
                        modifier = Modifier.fillMaxWidth().aspectRatio(2f / 3f).clip(RoundedCornerShape(6.dp)).background(RoxyColors.surface).alpha(0.7f).padding(8.dp),
                        contentAlignment = Alignment.Center,
                    ) {
                        Text(rel.title, fontSize = 12.sp, color = RoxyColors.muted, textAlign = TextAlign.Center, maxLines = 5, overflow = TextOverflow.Ellipsis)
                    }
                    Text(rel.label + (rel.year?.let { " · $it" } ?: "") + " · Not in library", fontSize = 11.sp, color = RoxyColors.muted, maxLines = 2, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 4.dp))
                }
            }
        }
        repeat(3 - row.size) { Spacer(Modifier.weight(1f)) }
    }
}
