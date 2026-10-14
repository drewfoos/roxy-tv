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
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.unit.Dp
import androidx.compose.foundation.layout.navigationBarsPadding
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
import androidx.compose.runtime.derivedStateOf
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
    val playable = state.episodes.filter { it.isPlayable }
    val next = playable.firstOrNull { (it.userData?.positionMs ?: 0) > 0 && it.userData?.played != true }
        ?: playable.firstOrNull { it.userData?.played != true }
        ?: playable.firstOrNull()
    val resumable = next != null && (next.userData?.positionMs ?: 0) > 0
    val label = when {
        next == null -> "Nothing to play yet"
        resumable -> "Resume ${next.episodeCode}"
        else -> "Start watching ${next.episodeCode}".trim()
    }
    val listState = rememberLazyListState()

    TitlePageScaffold(
        item = series, api = app.api, listState = listState, onBack = onBack,
        playLabel = label, onPlay = { next?.let { play(it) } }, onSaved = { vm.load() },
    ) { bottomInset ->
        LazyColumn(state = listState, modifier = Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = bottomInset)) {
            item(key = "banner") { TitleBanner(series, app.api, listState) }
            item(key = "info") {
                TitleInfo(
                    item = series,
                    badge = audioBadge(playable.firstOrNull()),
                    seasons = state.seasons.size,
                )
            }
        if (state.seasons.isNotEmpty() || state.related.isNotEmpty()) {
            item(key = "seasons") {
                val label = when {
                    state.showRelated -> "Movies & OVAs"
                    state.seasons.isEmpty() -> "Episodes"
                    else -> state.seasons[state.selectedSeason].let { it.title.ifEmpty { "Season ${it.indexAired ?: state.selectedSeason + 1}" } }
                }
                Box(modifier = Modifier.padding(start = PhonePad, top = 22.dp, bottom = 6.dp)) {
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
}

/** Height of the banner at the top of a title page. */
private val BannerHeight = 400.dp

/**
 * Crunchyroll's title page frame: the scrolling content, a pinned back button over the
 * banner, and the Play / Resume pill with the bookmark always sitting at the bottom.
 * [content] receives the bottom inset the list must leave for that bar.
 */
@Composable
fun TitlePageScaffold(
    item: Item,
    api: RoxyApi,
    listState: LazyListState,
    onBack: () -> Unit,
    playLabel: String,
    onPlay: () -> Unit,
    onSaved: () -> Unit,
    content: @Composable (bottomInset: Dp) -> Unit,
) {
    val navInset = WindowInsets.navigationBars.asPaddingValues().calculateBottomPadding()
    val barHeight = 44.dp + 24.dp
    // The header strip goes from transparent over the banner to the page colour once the
    // banner has scrolled away, so the back button never floats over the text.
    val fadePx = with(LocalDensity.current) { (BannerHeight - 80.dp).toPx() }
    val headerAlpha by remember {
        derivedStateOf {
            if (listState.firstVisibleItemIndex > 0) 1f else (listState.firstVisibleItemScrollOffset / fadePx).coerceIn(0f, 1f)
        }
    }
    Box(modifier = Modifier.fillMaxSize()) {
        content(barHeight + navInset + 8.dp)
        Box(modifier = Modifier.fillMaxWidth().background(RoxyColors.background.copy(alpha = headerAlpha)).statusBarsPadding().padding(12.dp)) {
            BackCircle(onBack)
        }
        // Sticky actions: the same pill + bookmark as the hero, on a dark pane over the list.
        Box(
            modifier = Modifier.align(Alignment.BottomCenter).fillMaxWidth()
                .background(Brush.verticalGradient(0f to Color.Transparent, 0.35f to RoxyColors.background.copy(alpha = 0.96f), 1f to RoxyColors.background))
                .navigationBarsPadding().padding(start = PhonePad, end = PhonePad, top = 24.dp, bottom = 12.dp),
        ) {
            PlayAndSaveRow(playLabel, item, api, onPlay = onPlay, onSaved = onSaved)
        }
    }
}

/**
 * Full-bleed banner (the backdrop, cropped tall) that fades and slides up as the page
 * scrolls, melting into the page colour at its foot.
 */
@Composable
fun TitleBanner(item: Item, api: RoxyApi, listState: LazyListState) {
    val heightPx = with(LocalDensity.current) { BannerHeight.toPx() }
    Box(
        modifier = Modifier.fillMaxWidth().height(BannerHeight).graphicsLayer {
            val off = if (listState.firstVisibleItemIndex == 0) listState.firstVisibleItemScrollOffset.toFloat() else heightPx
            alpha = (1f - off / (heightPx * 0.8f)).coerceIn(0f, 1f)
            translationY = off * 0.45f
        },
    ) {
        RoxyImage(api.imageUrl(item, "backdrop", widthPx(420.dp) * 2) ?: api.imageUrl(item, "poster", widthPx(420.dp) * 2), item.title, modifier = Modifier.fillMaxSize(), placeholderTextSize = 16)
        Box(modifier = Modifier.matchParentSize().background(Brush.verticalGradient(0f to Color(0x66000000), 0.25f to Color.Transparent)))
        Box(modifier = Modifier.matchParentSize().background(Brush.verticalGradient(0.45f to Color.Transparent, 1f to RoxyColors.background)))
    }
}

/**
 * Centred title block under the banner: name, "Dub | Sub • genres", the rating, then the
 * description (tap to expand) and the year / season count line.
 */
@Composable
fun TitleInfo(item: Item, badge: String?, seasons: Int = 0, extra: String? = null) {
    var expanded by rememberSaveable { mutableStateOf(false) }
    Column(modifier = Modifier.fillMaxWidth().padding(horizontal = PhonePad), horizontalAlignment = Alignment.CenterHorizontally) {
        Text(item.title, fontSize = 24.sp, lineHeight = 28.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, textAlign = TextAlign.Center)
        val tags = listOf(badge.orEmpty(), item.genres.take(3).joinToString(", ")).filter { it.isNotEmpty() }.joinToString("  •  ")
        if (tags.isNotEmpty()) Text(tags, fontSize = 13.sp, color = Color.White.copy(alpha = 0.85f), textAlign = TextAlign.Center, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 10.dp))
        item.communityRating?.let {
            Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.padding(top = 8.dp)) {
                Text("★ %.1f".format(it), fontSize = 15.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.accent)
                Text("  TMDB", fontSize = 12.sp, color = RoxyColors.muted)
            }
        }
        val facts = listOfNotNull(
            (item.year ?: item.premiered?.take(4)?.toIntOrNull())?.toString(),
            seasons.takeIf { it > 0 }?.let { if (it == 1) "1 Season" else "$it Seasons" },
            item.officialRating,
            extra,
        ).joinToString("  ·  ")
        if (facts.isNotEmpty()) Text(facts, fontSize = 12.sp, color = RoxyColors.muted, textAlign = TextAlign.Center, modifier = Modifier.padding(top = 6.dp))
        item.overview?.takeIf { it.isNotBlank() }?.let {
            Text(
                it, fontSize = 14.sp, lineHeight = 20.sp, color = RoxyColors.onBackground.copy(alpha = 0.9f),
                maxLines = if (expanded) Int.MAX_VALUE else 4, overflow = TextOverflow.Ellipsis,
                modifier = Modifier.fillMaxWidth().padding(top = 16.dp).clickable { expanded = !expanded },
            )
        }
    }
}

/** Title page while it loads: banner, centred title lines and three episode rows as shimmer. */
@Composable
fun DetailSkeleton(onBack: () -> Unit) {
    Column(modifier = Modifier.fillMaxSize()) {
        Box(modifier = Modifier.fillMaxWidth()) {
            Bone(Modifier.fillMaxWidth().height(BannerHeight), shape = RoundedCornerShape(0.dp))
            BackCircle(onBack, modifier = Modifier.statusBarsPadding().padding(12.dp))
        }
        Column(modifier = Modifier.fillMaxWidth().padding(horizontal = PhonePad), horizontalAlignment = Alignment.CenterHorizontally) {
            BoneLine(width = 240.dp, height = 22.dp, modifier = Modifier.padding(top = 4.dp))
            BoneLine(width = 200.dp, height = 12.dp, modifier = Modifier.padding(top = 12.dp))
            BoneLine(width = 80.dp, height = 12.dp, modifier = Modifier.padding(top = 10.dp))
            BoneLine(width = 320.dp, height = 12.dp, modifier = Modifier.padding(top = 18.dp))
            BoneLine(width = 300.dp, height = 12.dp, modifier = Modifier.padding(top = 6.dp))
            BoneLine(width = 220.dp, height = 12.dp, modifier = Modifier.padding(top = 6.dp))
        }
        Spacer(Modifier.height(24.dp))
        repeat(2) { EpisodeRowSkeleton() }
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
