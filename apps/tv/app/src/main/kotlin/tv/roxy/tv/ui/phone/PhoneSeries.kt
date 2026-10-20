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
import androidx.compose.foundation.layout.statusBars
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
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.material.icons.outlined.Schedule
import androidx.compose.material.icons.outlined.AddCircleOutline
import androidx.compose.material.icons.filled.ArrowDropDown
import androidx.compose.material.icons.filled.Check
import tv.roxy.tv.data.downloads.formatBytes
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.material3.TextButton
import androidx.compose.material3.AlertDialog
import androidx.compose.material.icons.outlined.Info
import androidx.compose.material.icons.filled.Replay
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.outlined.RemoveDone
import androidx.compose.material.icons.outlined.Image
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Cast
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import coil3.compose.AsyncImage
import androidx.compose.ui.layout.ContentScale
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.layout.layout
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
import tv.roxy.tv.api.DiscoverItem
import tv.roxy.tv.api.DiscoverSeason
import tv.roxy.tv.api.RelatedEntry
import tv.roxy.tv.api.RoxyApi
import tv.roxy.tv.ui.components.ProgressBar
import tv.roxy.tv.ui.components.RoxyImage
import tv.roxy.tv.ui.components.ImageMemo
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
    onOpenDetails: (Item) -> Unit = {},
    onOpenEpisode: (Item) -> Unit = {},
    onOpenDiscover: (DiscoverItem) -> Unit = {},
) {
    val context = LocalContext.current
    val app = RoxyApp.get(context)
    val vm: SeriesViewModel = viewModel(key = "series:$seriesId") { SeriesViewModel(app.api, seriesId, initial = app.items[seriesId]) }
    val state by vm.state.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { vm.load() }

    val series = state.series
    if (series == null) {
        if (state.loading) DetailSkeleton(onBack) else Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { Text(state.error ?: "Not found", color = RoxyColors.error) }
        return
    }

    fun play(item: Item) = scope.launch { playItem(context, item).onFailure { Timber.w(it, "play failed") } }
    val startDownload = rememberDownloadStarter()
    // Seasons TMDB knows that the library lacks (Discover's list), so the dropdown shows
    // the whole show and a missing season can be requested from here.
    val tmdbId = series.providerIds["tmdb"]?.toIntOrNull()
    var extraSeasons by remember(series.id) { mutableStateOf<List<DiscoverSeason>>(emptyList()) }
    var selectedExtra by rememberSaveable(series.id) { mutableStateOf<Int?>(null) }
    LaunchedEffect(series.id, state.seasons.size) {
        if (tmdbId == null) return@LaunchedEffect
        val have = state.seasons.mapNotNull { it.indexAired }.toSet()
        runCatching { app.api.discoverDetail("tv", tmdbId) }
            .onSuccess { d -> extraSeasons = d.seasons.filter { it.number !in have && it.number != 0 } }
    }
    val seasonLabel = when {
        selectedExtra != null -> "Season $selectedExtra"
        state.showRelated -> "Movies & OVAs"
        state.seasons.isEmpty() -> "Episodes"
        else -> state.seasons[state.selectedSeason].let { it.title.ifEmpty { "Season ${it.indexAired ?: state.selectedSeason + 1}" } }
    }
    fun download(items: List<Item>) = startDownload(items, series, seasonLabel.takeIf { it != "Episodes" })
    val playable = state.episodes.filter { it.isPlayable }
    val next = playable.firstOrNull { (it.userData?.positionMs ?: 0) > 0 && it.userData?.played != true }
        ?: playable.firstOrNull { it.userData?.played != true }
        ?: playable.firstOrNull()
    val resumable = next != null && (next.userData?.positionMs ?: 0) > 0
    val label = when {
        next == null && (state.loading || state.episodesLoading) -> "Play" // the list is still on its way; don't claim there is nothing
        next == null -> "Nothing to play yet"
        resumable -> "Resume ${next.episodeCode}"
        else -> "Start watching ${next.episodeCode}".trim()
    }
    val listState = rememberLazyListState()

    TitlePageScaffold(
        item = series, api = app.api, listState = listState, onBack = onBack,
        playLabel = label, onPlay = { next?.let { play(it) } }, onSaved = { vm.load() },
        onMarkWatched = { on -> scope.launch { runCatching { app.api.setPlayed(series.id, on) }.onFailure { Timber.w(it, "mark watched") }; vm.load() } },
        onArtChanged = { vm.load() },
    ) { bottomInset ->
        // Crunchyroll's order: key art with the logo, the facts and synopsis centred, a tab
        // strip (Episodes / Movies & OVAs), the season row, then the episodes. Nothing in
        // the list is a skeleton: the header is drawn from the card that opened the page
        // and the episodes simply appear under the season row when they land.
        // Two tabs; the films and OVAs are an entry in the season dropdown, not a tab.
        val hasRelated = series.relatedCount > 0 || state.related.isNotEmpty()
        val tabs = listOf("Episodes", "More like this")
        // "More like this": TMDB's recommendations, fetched the first time the tab is opened.
        var moreLike by rememberSaveable(seriesId) { mutableStateOf(false) }
        var similar by remember(seriesId) { mutableStateOf<List<DiscoverItem>?>(null) }
        LaunchedEffect(moreLike, series.id) { if (moreLike && similar == null) similar = runCatching { app.api.similar(series.id) }.getOrDefault(emptyList()) }
        val selectedTab = if (moreLike) 1 else 0
        LazyColumn(state = listState, modifier = Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = bottomInset)) {
            item(key = "banner") { TitleBanner(series, app.api, listState) }
            item(key = "info") {
                TitleInfo(
                    item = series, api = app.api,
                    badge = audioBadge(series) ?: audioBadge(playable.firstOrNull()),
                    // The provider's count (3 for KonoSuba) even when the library holds fewer; known from the list item, so it never pops in.
                    seasons = series.seasonCount ?: state.seasons.size,
                    onMoreDetails = { onOpenDetails(series) },
                )
            }
            item(key = "tabs") {
                // The strip paints its own gap, or the pinned banner shows through it.
                SectionTabs(tabs, selectedTab, modifier = Modifier.background(RoxyColors.background).padding(top = 22.dp)) { i -> moreLike = i == 1 }
            }
            if (moreLike) {
                val list = similar
                when {
                    list == null -> item(key = "similar-wait") { Row(modifier = Modifier.fillMaxWidth().padding(start = PhonePad, end = PhonePad, top = 18.dp, bottom = 8.dp), horizontalArrangement = Arrangement.spacedBy(12.dp)) { repeat(2) { Bone(Modifier.weight(1f).aspectRatio(2f / 3f)) } } }
                    list.isEmpty() -> item(key = "similar-none") { Text("Nothing to suggest for this title yet.", color = RoxyColors.muted, fontSize = 13.sp, modifier = Modifier.padding(PhonePad)) }
                    else -> { item(key = "similar-top") { Spacer(Modifier.height(10.dp)) }; items(list.chunked(2), key = { row -> "sim:" + row.joinToString("|") { it.tmdbId.toString() } }) { row ->
                        DiscoverGridRow(row) { c ->
                            val id = c.libraryItemId
                            if (id != null) { if (c.isMovie) onOpenMovie(Item(id = id, type = "movie", title = c.title)) else onOpenSeries(Item(id = id, type = "series", title = c.title)) }
                            else onOpenDiscover(c)
                        }
                    } }
                }
            } else {
                if (state.seasons.isNotEmpty() || extraSeasons.isNotEmpty() || hasRelated) {
                    item(key = "seasons") {
                        SeasonRow(
                            label = seasonLabel,
                            menu = { close ->
                                state.seasons.forEachIndexed { i, season ->
                                    val name = season.title.ifEmpty { "Season ${season.indexAired ?: i + 1}" }
                                    DropdownMenuItem(
                                        text = { Text(name, color = Color.White, fontWeight = if (selectedExtra == null && i == state.selectedSeason) FontWeight.Bold else FontWeight.Normal) },
                                        onClick = { close(); selectedExtra = null; vm.selectSeason(i) },
                                    )
                                }
                                extraSeasons.forEach { sn ->
                                    DropdownMenuItem(
                                        text = {
                                            Row(verticalAlignment = Alignment.CenterVertically) {
                                                Text(sn.name.ifEmpty { "Season ${sn.number}" }, color = Color.White, fontWeight = if (selectedExtra == sn.number) FontWeight.Bold else FontWeight.Normal)
                                                Text(if (sn.requested) "  ·  requested" else "  ·  not in library", color = RoxyColors.muted, fontSize = 12.sp)
                                            }
                                        },
                                        onClick = { close(); selectedExtra = sn.number },
                                    )
                                }
                                if (hasRelated) {
                                    DropdownMenuItem(
                                        text = { Text("Movies & OVAs", color = Color.White, fontWeight = if (state.showRelated) FontWeight.Bold else FontWeight.Normal) },
                                        onClick = { close(); selectedExtra = null; vm.selectRelated() },
                                    )
                                }
                            },
                            trailing = {
                                // Season download lives at the end of the season row as an icon, not a row of its own.
                                if (selectedExtra == null && !state.showRelated && state.episodes.any { it.isPlayable }) SeasonDownloadRow(state.episodes, seasonLabel, onDownloadAll = ::download, compact = true)
                            },
                        )
                    }
                }
                val extra = extraSeasons.firstOrNull { it.number == selectedExtra }
                if (state.showRelated && selectedExtra == null) {
                    item(key = "related-top") { Spacer(Modifier.height(10.dp)) }
                    items(state.related.chunked(2)) { row ->
                        RelatedGridRow(row, app.api, onOpen = { rel ->
                            val item = rel.item ?: return@RelatedGridRow
                            if (item.isMovie) onOpenMovie(item) else onOpenSeries(item)
                        })
                    }
                } else if (extra != null && tmdbId != null) {
                    item(key = "extra-season") {
                        Column {
                            Text(
                                listOfNotNull(extra.episodes.takeIf { it > 0 }?.let { "$it episodes" }, extra.airDate?.take(4)).joinToString("  ·  ").ifEmpty { "Season ${extra.number}" },
                                fontSize = 13.sp, color = RoxyColors.muted, modifier = Modifier.padding(horizontal = PhonePad, vertical = 4.dp),
                            )
                            RequestSeasonRow(tmdbId, extra.number, "Season ${extra.number}")
                        }
                    }
                } else {
                    if (!state.episodesLoading && !state.loading && state.episodes.isEmpty()) {
                        item(key = "eps-empty") { Text("No episodes in this season.", color = RoxyColors.muted, modifier = Modifier.padding(PhonePad)) }
                    } else if (state.episodes.isNotEmpty() && state.episodes.none { it.isPlayable }) {
                        // Nothing on disk for this season (provider placeholders only): offer to request it.
                        val seasonNumber = state.seasons.getOrNull(state.selectedSeason)?.indexAired
                        if (tmdbId != null && seasonNumber != null) item(key = "season-request") { RequestSeasonRow(tmdbId, seasonNumber, seasonLabel) }
                    }
                    items(state.episodes, key = { it.id }) { ep ->
                        PhoneEpisodeRow(
                            ep, app.api,
                            onClick = { if (ep.isPlayable) onOpenEpisode(ep) },
                            onDownload = { download(listOf(ep)) },
                            onMarkWatched = { on -> scope.launch { runCatching { app.api.setPlayed(ep.id, on) }.onFailure { Timber.w(it, "mark watched") }; vm.load() } },
                        )
                    }
                }
            }
        }
    }
}

/** Crunchyroll's tab strip: bold labels, a brand-colour underline under the open one, a hairline across. */
@Composable
fun SectionTabs(tabs: List<String>, selected: Int, modifier: Modifier = Modifier, onSelect: (Int) -> Unit) {
    Column(modifier = modifier.fillMaxWidth()) {
        Row(modifier = Modifier.padding(horizontal = PhonePad)) {
            tabs.forEachIndexed { i, tab ->
                val on = i == selected
                Column(modifier = Modifier.width(IntrinsicSize.Max).clickable(interactionSource = remember { MutableInteractionSource() }, indication = null) { onSelect(i) }.padding(end = 28.dp)) {
                    Text(tab, fontSize = 15.sp, fontWeight = FontWeight.Bold, color = if (on) RoxyColors.onBackground else RoxyColors.muted, modifier = Modifier.padding(vertical = 10.dp))
                    Box(modifier = Modifier.fillMaxWidth().height(2.dp).background(if (on) RoxyColors.accent else Color.Transparent))
                }
            }
        }
        Box(modifier = Modifier.fillMaxWidth().height(1.dp).background(RoxyColors.surfaceHigh))
    }
}

/** Full-width season selector under the tabs: the name with a caret opens [menu]; [trailing] sits at the right end. */
@Composable
private fun SeasonRow(label: String, menu: @Composable ColumnScope.(close: () -> Unit) -> Unit, trailing: @Composable () -> Unit) {
    var open by remember { mutableStateOf(false) }
    Column {
        Row(modifier = Modifier.fillMaxWidth().padding(horizontal = PhonePad), verticalAlignment = Alignment.CenterVertically) {
            Box {
                Row(
                    modifier = Modifier.clip(RoundedCornerShape(8.dp)).clickable { open = true }.padding(vertical = 12.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(label, fontSize = 16.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground)
                    Icon(Icons.Filled.ArrowDropDown, contentDescription = "Choose season", tint = RoxyColors.onBackground)
                }
                DropdownMenu(expanded = open, onDismissRequest = { open = false }, containerColor = RoxyColors.surfaceHigh) { menu { open = false } }
            }
            Spacer(Modifier.weight(1f))
            trailing()
        }
        Box(modifier = Modifier.fillMaxWidth().height(1.dp).background(RoxyColors.surfaceHigh))
    }
}

/** Height of the banner at the top of a title page. */
private val BannerHeight = 520.dp

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
    /** The three-dot menu's "Mark … as watched / unwatched"; null hides the menu. */
    onMarkWatched: ((Boolean) -> Unit)? = null,
    markAll: Boolean = true,
    /** After the admin picked new art: reload so the page shows it. */
    onArtChanged: () -> Unit = {},
    content: @Composable (bottomInset: Dp) -> Unit,
) {
    val isAdmin = RoxyApp.get(LocalContext.current).prefs.session.collectAsStateWithLifecycle().value.isAdmin
    var pickArt by remember { mutableStateOf<String?>(null) } // "backdrop" | "poster" | "logo"
    val cache = RoxyApp.get(LocalContext.current).items
    // New art: every list that showed the old URL refetches on its next resume.
    pickArt?.let { type -> ArtPickerDialog(item, type, onDismiss = { pickArt = null }, onChanged = { cache.touch(); onArtChanged() }) }
    val navInset = WindowInsets.navigationBars.asPaddingValues().calculateBottomPadding()
    val barHeight = 44.dp + 24.dp
    // The strip is clear over the banner and only goes solid as the picture itself goes
    // black (it fades into it); the name fades in once the logo block has scrolled under it.
    val density = LocalDensity.current
    val stripPx = with(density) { (BannerHeight * 0.55f).toPx() }
    val headerAlpha by remember {
        derivedStateOf {
            if (listState.firstVisibleItemIndex > 0) 1f else (listState.firstVisibleItemScrollOffset / stripPx).coerceIn(0f, 1f)
        }
    }
    val titleFromPx = with(density) { (BannerHeight - LogoOverlap + 30.dp).toPx() }
    val titleOverPx = with(density) { 70.dp.toPx() }
    val titleAlpha by remember {
        derivedStateOf {
            if (listState.firstVisibleItemIndex > 0) 1f else ((listState.firstVisibleItemScrollOffset - titleFromPx) / titleOverPx).coerceIn(0f, 1f)
        }
    }
    Box(modifier = Modifier.fillMaxSize()) {
        content(barHeight + navInset + 8.dp)
        // Crunchyroll's header: a plain white X at the left, Cast and the three-dot menu
        // at the right, over the banner's top fade; the strip turns solid once scrolled.
        Row(
            modifier = Modifier.fillMaxWidth().background(RoxyColors.background.copy(alpha = headerAlpha)).statusBarsPadding().padding(horizontal = 6.dp, vertical = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            HeaderIcon(Icons.Filled.Close, "Close", onClick = onBack)
            // The name takes the strip over once the banner (which carries the logo) is gone.
            Text(
                item.title, fontSize = 17.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, maxLines = 1, overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f).padding(horizontal = 6.dp).alpha(titleAlpha),
            )
            Box(modifier = Modifier.size(44.dp), contentAlignment = Alignment.Center) { CastIcon() }
            if (onMarkWatched != null) {
                Box {
                    var open by remember { mutableStateOf(false) }
                    HeaderIcon(Icons.Filled.MoreVert, "More", onClick = { open = true })
                    DropdownMenu(expanded = open, onDismissRequest = { open = false }, containerColor = RoxyColors.surfaceHigh) {
                        val what = if (markAll) "all" else "movie"
                        DropdownMenuItem(text = { Text("Mark $what as watched", color = Color.White) }, leadingIcon = { Icon(Icons.Filled.Check, null, tint = Color.White) }, onClick = { open = false; onMarkWatched(true) })
                        DropdownMenuItem(text = { Text("Mark $what as unwatched", color = Color.White) }, leadingIcon = { Icon(Icons.Outlined.RemoveDone, null, tint = Color.White) }, onClick = { open = false; onMarkWatched(false) })
                        if (isAdmin) {
                            // Admin only: swap the art for any the providers hold (TMDB, fanart.tv).
                            DropdownMenuItem(text = { Text("Change banner", color = Color.White) }, leadingIcon = { Icon(Icons.Outlined.Image, null, tint = Color.White) }, onClick = { open = false; pickArt = "backdrop" })
                            DropdownMenuItem(text = { Text("Change poster", color = Color.White) }, leadingIcon = { Icon(Icons.Outlined.Image, null, tint = Color.White) }, onClick = { open = false; pickArt = "poster" })
                            DropdownMenuItem(text = { Text("Change logo", color = Color.White) }, leadingIcon = { Icon(Icons.Outlined.Image, null, tint = Color.White) }, onClick = { open = false; pickArt = "logo" })
                            DropdownMenuItem(text = { Text("Change Home banner", color = Color.White) }, leadingIcon = { Icon(Icons.Outlined.Image, null, tint = Color.White) }, onClick = { open = false; pickArt = "hero" })
                        }
                    }
                }
            }
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

/** The quiet white the facts, genres and year share; the star rating is a shade brighter. */
val FactsGrey = Color.White.copy(alpha = 0.72f)

/** Crunchyroll's age badge: short label on a grey block. */
@Composable
internal fun AgePill(text: String, small: Boolean = false) {
    Box(modifier = Modifier.clip(RoundedCornerShape(3.dp)).background(Color(0xFF4A4A52)).padding(horizontal = if (small) 5.dp else 6.dp, vertical = if (small) 1.dp else 2.dp)) {
        Text(text, fontSize = if (small) 10.sp else 11.sp, fontWeight = FontWeight.Bold, color = Color.White, letterSpacing = 0.5.sp)
    }
}

/** TV and MPAA ratings folded into Crunchyroll's short labels; anything unknown shows as is. */
internal fun ageLabel(rating: String): String = when (rating.uppercase().removePrefix("US-")) {
    "TV-Y", "TV-Y7", "TV-G", "G" -> "ALL"
    "TV-PG", "PG" -> "PG"
    "TV-14", "PG-13" -> "14+"
    "TV-MA", "R", "NC-17" -> "18+"
    else -> rating
}

/** A 44 dp white glyph for the title page header (no circle behind it, like Crunchyroll's). */
@Composable
internal fun HeaderIcon(icon: ImageVector, description: String, tint: Color = Color.White, onClick: (() -> Unit)? = null) {
    Icon(
        icon, contentDescription = description, tint = tint,
        modifier = Modifier.size(44.dp).clip(CircleShape).then(if (onClick != null) Modifier.clickable(onClick = onClick) else Modifier).padding(10.dp),
    )
}

/** The header strip's height below the status bar: the X, Cast and menu row. */
internal val HeaderRow = 52.dp

/**
 * Full-bleed key art (the backdrop, cropped tall) starting just above the X, with the
 * status bar black above it and a short fade at its top edge. Pinned where it is while
 * the page scrolls over it, dimming to black on the way, like Crunchyroll's: the content
 * brings its own fade (see [TitleInfo]), so nothing bright is ever under the text. The
 * logo is not here; it scrolls with the content.
 */
@Composable
fun TitleBanner(item: Item, api: RoxyApi, listState: LazyListState) {
    val heightPx = with(LocalDensity.current) { BannerHeight.toPx() }
    val top = (WindowInsets.statusBars.asPaddingValues().calculateTopPadding() - 6.dp).coerceAtLeast(0.dp)
    Box(
        modifier = Modifier.fillMaxWidth().padding(top = top).height(BannerHeight).graphicsLayer {
            val off = if (listState.firstVisibleItemIndex == 0) listState.firstVisibleItemScrollOffset.toFloat() else heightPx
            translationY = off
            alpha = (1f - off / (heightPx * 0.55f)).coerceIn(0f, 1f)
        },
    ) {
        val backdrop = api.imageUrl(item, "backdrop", widthPx(420.dp) * 2)
        val poster = api.imageUrl(item, "poster", widthPx(420.dp) * 2)
        // Start from whichever rendition of this art a list already loaded (the card's
        // poster, the hero's backdrop), so the banner is never a grey box mid-slide.
        val warm = ImageMemo.lastFor(backdrop) ?: ImageMemo.lastFor(poster)
        RoxyImage(backdrop ?: poster, item.title, modifier = Modifier.fillMaxSize(), placeholderTextSize = 16, placeholderKey = warm)
        // A short fade from the black status bar into the art, which the X and icons sit on.
        val fadePx = with(LocalDensity.current) { (HeaderRow + 40.dp).toPx() }
        Box(modifier = Modifier.matchParentSize().background(Brush.verticalGradient(colorStops = arrayOf(0f to Color.Black, 0.35f to Color(0x99000000), 1f to Color.Transparent), startY = 0f, endY = fadePx)))
    }
}

/** How far the logo block (and its fade) reaches up over the banner. */
private val LogoOverlap = 230.dp

/**
 * Reports a shorter height than the content and places it that much higher, so a list
 * item can sit over the foot of the one above it (the logo over the banner's fade).
 */
private fun Modifier.pullUp(by: Dp): Modifier = layout { measurable, constraints ->
    val placeable = measurable.measure(constraints)
    val up = by.roundToPx()
    layout(placeable.width, (placeable.height - up).coerceAtLeast(0)) { placeable.placeRelative(0, -up) }
}

/**
 * The title block under (and reaching into) the banner, Crunchyroll's order: the logo
 * (or name) on the banner's faded foot with "14+  Dub | Sub" under it; then the star
 * rating with year and season count, the genres, the synopsis (tap to expand) and
 * "More details".
 */
@Composable
fun TitleInfo(item: Item, api: RoxyApi, badge: String?, seasons: Int = 0, extra: String? = null, onMoreDetails: (() -> Unit)? = null) {
    var expanded by rememberSaveable { mutableStateOf(false) }
    Column(modifier = Modifier.fillMaxWidth().pullUp(LogoOverlap), horizontalAlignment = Alignment.CenterHorizontally) {
        // The fade belongs to the content, not the picture: transparent at the top, black
        // by the logo's foot, so it rides up with the logo and the "14+" line below it is
        // always on solid black.
        Column(
            modifier = Modifier.fillMaxWidth()
                .background(Brush.verticalGradient(0f to Color.Transparent, 0.42f to RoxyColors.background.copy(alpha = 0.75f), 0.62f to RoxyColors.background, 1f to RoxyColors.background))
                .padding(horizontal = PhonePad).padding(top = 120.dp, bottom = 6.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            val logo = api.imageUrl(item, "logo", widthPx(320.dp))
            if (logo != null) {
                AsyncImage(model = logo, contentDescription = item.title, contentScale = ContentScale.Fit, modifier = Modifier.fillMaxWidth(0.7f).heightIn(max = 84.dp))
            } else {
                Text(item.title, fontSize = 24.sp, lineHeight = 28.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, textAlign = TextAlign.Center)
            }
            val age = item.officialRating?.let { ageLabel(it) }
            if (age != null || badge != null) {
                Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.padding(top = 14.dp)) {
                    if (age != null) AgePill(age)
                    if (age != null && badge != null) Spacer(Modifier.width(10.dp))
                    if (badge != null) Text(badge, fontSize = 13.sp, fontWeight = FontWeight.Medium, color = Color.White.copy(alpha = 0.9f))
                }
            }
        }
        // Solid from here down: the pinned picture is still fading behind the page, and
        // only the gradient above the logo is meant to show it.
        Column(modifier = Modifier.fillMaxWidth().background(RoxyColors.background).padding(horizontal = PhonePad), horizontalAlignment = Alignment.CenterHorizontally) {
            FactsLine(
                rating = item.communityRating,
                facts = listOfNotNull(
                    (item.year ?: item.premiered?.take(4)?.toIntOrNull())?.toString(),
                    seasons.takeIf { it > 0 }?.let { if (it == 1) "1 Season" else "$it Seasons" },
                    extra,
                ),
            )
            val genres = item.genres.take(3).joinToString(", ")
            if (genres.isNotEmpty()) Text(genres, fontSize = 13.sp, color = FactsGrey, textAlign = TextAlign.Center, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 8.dp))
            item.overview?.takeIf { it.isNotBlank() }?.let {
                Text(
                    it, fontSize = 14.sp, lineHeight = 20.sp, color = RoxyColors.onBackground.copy(alpha = 0.9f), textAlign = TextAlign.Center,
                    maxLines = if (expanded) Int.MAX_VALUE else 3, overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.fillMaxWidth().padding(top = 16.dp).clickable { expanded = !expanded },
                )
            }
            // Crunchyroll's "More details": a centred link in the brand colour, not a button.
            if (onMoreDetails != null) {
                Text(
                    "More details", fontSize = 13.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.accent,
                    modifier = Modifier.padding(top = 10.dp).clickable(onClick = onMoreDetails).padding(vertical = 4.dp, horizontal = 8.dp),
                )
            }
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

/** Netflix's episode row: thumb with a play badge, "N. Title", runtime and a watched tick, the download control, then the synopsis. */
@Composable
internal fun PhoneEpisodeRow(ep: Item, api: RoxyApi, onClick: () -> Unit, onDownload: () -> Unit, onMarkWatched: (Boolean) -> Unit) {
    val thumbWidth = 150.dp
    val placeholder = !ep.isPlayable
    val watched = ep.userData?.played == true
    val positionMs = ep.userData?.positionMs ?: 0L
    val runtimeMs = ep.runtimeMs ?: 0L
    val badge = when (ep.availability) {
        "missing" -> "Missing"
        "unaired" -> ep.airDate?.takeIf { it.isNotBlank() }?.let { "Airs $it" } ?: "Unaired"
        else -> null
    }
    // Crunchyroll's thumbnail tag: the length, "7m left" once started, "Watched" when done.
    val tag = when {
        watched -> "Watched"
        positionMs > 0 && runtimeMs > positionMs -> "${((runtimeMs - positionMs) / 60000L + 1).coerceAtLeast(1)}m left"
        else -> ep.runtimeMinutes?.let { "${it}m" }
    }
    Column(
        modifier = Modifier.fillMaxWidth().clickable(enabled = !placeholder, onClick = onClick).padding(horizontal = PhonePad, vertical = 10.dp).alpha(if (placeholder) 0.45f else 1f),
    ) {
        // Crunchyroll's row: the name sits a little below the thumbnail's top edge, with
        // the download and menu under it at the right; no synopsis in the list.
        Row(verticalAlignment = Alignment.Top, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            Box(modifier = Modifier.width(thumbWidth).aspectRatio(16f / 9f).clip(RoundedCornerShape(4.dp)).background(RoxyColors.surfaceHigh)) {
                RoxyImage(api.imageUrl(ep, "thumb", widthPx(thumbWidth) * 2) ?: api.imageUrl(ep, "backdrop", widthPx(thumbWidth) * 2), ep.episodeCode.ifEmpty { ep.title }, modifier = Modifier.fillMaxSize().alpha(if (watched) 0.45f else 1f), placeholderTextSize = 12)
                if (!placeholder) {
                    if (watched) ReplayBadge(size = 34.dp, modifier = Modifier.align(Alignment.Center)) else PlayBadge(size = 34.dp, modifier = Modifier.align(Alignment.Center))
                    if (!watched) ProgressBar(ep.progressFraction, modifier = Modifier.align(Alignment.BottomCenter))
                    if (tag != null) ThumbTag(tag, modifier = Modifier.align(Alignment.BottomEnd).padding(4.dp))
                }
            }
            Column(modifier = Modifier.weight(1f).heightIn(min = thumbWidth * 9f / 16f)) {
                val n = (ep.indexAired ?: ep.indexAbsolute)?.let { "$it. " } ?: ""
                Text(n + ep.title, fontSize = 14.sp, lineHeight = 18.sp, fontWeight = FontWeight.Medium, color = RoxyColors.onBackground, maxLines = 2, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 4.dp))
                val sub = listOfNotNull(
                    badge,
                    if (placeholder && ep.availability == "missing" && !ep.airDate.isNullOrBlank()) "Aired ${ep.airDate}" else null,
                ).joinToString("  ·  ")
                if (sub.isNotEmpty()) Text(sub, fontSize = 12.sp, color = RoxyColors.muted, modifier = Modifier.padding(top = 3.dp))
                if (!placeholder) {
                    Spacer(Modifier.weight(1f))
                    Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End, verticalAlignment = Alignment.CenterVertically) {
                        DownloadButton(ep, onDownload = onDownload, size = 24.dp)
                        EpisodeMenu(ep, api, onMarkWatched = onMarkWatched)
                    }
                }
            }
        }
    }
}

/** Small translucent label in a thumbnail's corner ("24m", "7m left", "Watched"). */
@Composable
private fun ThumbTag(text: String, modifier: Modifier = Modifier) {
    Box(modifier = modifier.clip(RoundedCornerShape(3.dp)).background(Color(0xB3000000)).padding(horizontal = 5.dp, vertical = 2.dp)) {
        Text(text, color = Color.White, fontSize = 10.sp, fontWeight = FontWeight.SemiBold)
    }
}

/** The play badge's twin for a watched episode: a replay arrow. */
@Composable
private fun ReplayBadge(size: Dp, modifier: Modifier = Modifier) {
    Box(modifier = modifier.size(size).clip(CircleShape).background(Color(0x99000000)), contentAlignment = Alignment.Center) {
        Icon(Icons.Filled.Replay, contentDescription = "Watch again", tint = Color.White, modifier = Modifier.size(size * 0.6f))
    }
}

/**
 * The three dots on an episode: mark it watched or unwatched, and the file's facts
 * (codec, resolution, tracks, size), since episodes come from different releases.
 */
@Composable
internal fun EpisodeMenu(ep: Item, api: RoxyApi, onMarkWatched: (Boolean) -> Unit, modifier: Modifier = Modifier) {
    var open by remember { mutableStateOf(false) }
    var info by remember { mutableStateOf(false) }
    Box(modifier) {
        Icon(Icons.Filled.MoreVert, contentDescription = "More", tint = RoxyColors.muted, modifier = Modifier.size(36.dp).clip(CircleShape).clickable { open = true }.padding(6.dp))
        DropdownMenu(expanded = open, onDismissRequest = { open = false }, containerColor = RoxyColors.surfaceHigh) {
            val watched = ep.userData?.played == true
            DropdownMenuItem(
                text = { Text(if (watched) "Mark as unwatched" else "Mark as watched", color = Color.White) },
                leadingIcon = { Icon(if (watched) Icons.Outlined.RemoveDone else Icons.Filled.Check, null, tint = Color.White) },
                onClick = { open = false; onMarkWatched(!watched) },
            )
            DropdownMenuItem(text = { Text("File info", color = Color.White) }, leadingIcon = { Icon(Icons.Outlined.Info, null, tint = Color.White) }, onClick = { open = false; info = true })
        }
    }
    if (info) EpisodeInfoDialog(ep, api) { info = false }
}

@Composable
private fun EpisodeInfoDialog(ep: Item, api: RoxyApi, onDismiss: () -> Unit) {
    val e = with(api) { ep.toEpisode() }
    val src = ep.primarySource
    AlertDialog(
        onDismissRequest = onDismiss,
        containerColor = RoxyColors.surfaceHigh, titleContentColor = RoxyColors.onBackground, textContentColor = RoxyColors.onBackground,
        title = { Text(listOfNotNull(ep.episodeCode.takeIf { it.isNotEmpty() }, ep.title.takeIf { it.isNotBlank() }).joinToString("  ·  "), fontSize = 17.sp) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                InfoLine("Video", e.videoLine)
                InfoLine("Tracks", e.tracksLine)
                InfoLine("Size", src?.size?.takeIf { it > 0 }?.let { formatBytes(it) })
                InfoLine("Container", src?.container)
                val flags = listOfNotNull(if (e.isHi10p) "Hi10P" else null, if (e.hasAss) "ASS subtitles" else null).joinToString(", ")
                if (flags.isNotEmpty()) InfoLine("Notes", flags)
            }
        },
        confirmButton = { TextButton(onClick = onDismiss) { Text("Close", color = Color.White) } },
    )
}

@Composable
private fun InfoLine(label: String, value: String?) {
    if (value.isNullOrBlank()) return
    Column {
        Text(label, fontSize = 11.sp, color = RoxyColors.muted)
        Text(value, fontSize = 13.sp, lineHeight = 18.sp, color = RoxyColors.onBackground, fontFamily = FontFamily.Monospace)
    }
}

/**
 * The library's twin of Discover's season request: a season the server knows about but
 * has no files for gets a Request row here, so nobody has to leave the show's page.
 * State comes from the same request list Discover shows.
 */
@Composable
private fun RequestSeasonRow(tmdbId: Int, season: Int, label: String) {
    val app = RoxyApp.get(LocalContext.current)
    val scope = rememberCoroutineScope()
    var requested by remember(tmdbId, season) { mutableStateOf<Boolean?>(null) }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(tmdbId, season) {
        runCatching { app.api.discoverDetail("tv", tmdbId) }
            .onSuccess { d -> requested = d.seasons.firstOrNull { it.number == season }?.requested ?: false }
            .onFailure { requested = false }
    }
    Row(
        modifier = Modifier.fillMaxWidth().clickable(enabled = requested == false && !busy) {
            busy = true
            scope.launch {
                runCatching { app.api.discoverRequest("tv", tmdbId, listOf(season)) }
                    .onSuccess { requested = true }
                    .onFailure { t -> error = t.message }
                busy = false
            }
        }.padding(horizontal = PhonePad, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        val on = requested == true
        Icon(if (on) Icons.Outlined.Schedule else Icons.Outlined.AddCircleOutline, contentDescription = null, tint = if (on) RoxyColors.muted else RoxyColors.accent, modifier = Modifier.size(22.dp))
        Column(modifier = Modifier.padding(start = 10.dp)) {
            Text(
                when { requested == null -> "Checking…"; on -> "$label requested"; busy -> "Requesting…"; else -> "Request $label" },
                fontSize = 14.sp, fontWeight = FontWeight.SemiBold, color = if (on) RoxyColors.muted else RoxyColors.accent,
            )
            Text(error ?: if (on) "Sonarr will fetch it once connected" else "Not in your library yet", fontSize = 12.sp, color = if (error != null) RoxyColors.error else RoxyColors.muted)
        }
    }
}

/** One grid row (up to three) of the franchise's related titles. */
@Composable
internal fun RelatedGridRow(row: List<RelatedEntry>, api: RoxyApi, onOpen: (RelatedEntry) -> Unit) {
    // The same card as Discover and the person page: poster (the provider's for a title
    // the library lacks), the library tick, the name, then "OVA · 2016" on one line.
    Row(modifier = Modifier.fillMaxWidth().padding(horizontal = PhonePad, vertical = 8.dp), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
        row.forEach { rel ->
            val item = rel.item
            Column(modifier = Modifier.weight(1f)) {
                Box(modifier = Modifier.fillMaxWidth().aspectRatio(2f / 3f).clip(RoundedCornerShape(6.dp)).background(RoxyColors.surfaceHigh).clickable { onOpen(rel) }) {
                    val url = if (rel.inLibrary && item != null) api.imageUrl(item, "poster", widthPx(PosterW) * 2) else rel.posterUrl
                    if (url != null) AsyncImage(model = url, contentDescription = rel.title, contentScale = ContentScale.Crop, modifier = Modifier.fillMaxSize())
                    else Text(rel.title, fontSize = 11.sp, color = RoxyColors.muted, textAlign = TextAlign.Center, maxLines = 4, overflow = TextOverflow.Ellipsis, modifier = Modifier.align(Alignment.Center).padding(6.dp))
                    StatusBadge(rel.inLibrary, null, modifier = Modifier.align(Alignment.TopEnd).padding(5.dp))
                }
                Text(rel.title, fontSize = 14.sp, lineHeight = 18.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground, maxLines = 2, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 8.dp))
                Text(listOfNotNull(rel.label, rel.year?.toString()).joinToString("  ·  "), fontSize = 12.sp, color = RoxyColors.muted, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }
        repeat(2 - row.size) { Spacer(Modifier.weight(1f)) }
    }
}
