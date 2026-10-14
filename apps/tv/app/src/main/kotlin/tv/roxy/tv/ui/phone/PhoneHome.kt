package tv.roxy.tv.ui.phone

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.border
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.pager.HorizontalPager
import androidx.compose.foundation.pager.rememberPagerState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Bookmark
import androidx.compose.material.icons.filled.Cast
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.outlined.BookmarkBorder
import androidx.compose.material.icons.outlined.CloudOff
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import coil3.compose.AsyncImage
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.Item
import tv.roxy.tv.api.RoxyApi
import tv.roxy.tv.ui.components.RoxyImage
import tv.roxy.tv.ui.components.widthPx
import tv.roxy.tv.ui.home.HeroPick
import tv.roxy.tv.ui.home.HomeUiState
import tv.roxy.tv.ui.home.HomeViewModel
import tv.roxy.tv.ui.playItem
import tv.roxy.tv.ui.theme.RoxyColors

/** How long each hero slide stays before the carousel moves on. */
private const val HERO_DWELL_MS = 8000

/**
 * Netflix's phone Home: a hero carousel (poster card with the app mark, search and cast
 * over it; rating, dub/sub, genres, Start watching and story-style timer bars under it),
 * then rows. Same [HomeViewModel] as the TV screen; shows skeletons while loading and a
 * dedicated state when the server cannot be reached.
 */
@Composable
fun PhoneHome(
    onOpenSeries: (Item) -> Unit,
    onOpenMovie: (Item) -> Unit,
    onSearch: () -> Unit,
    onUnauthorized: () -> Unit,
    bottomInset: Dp,
) {
    val context = LocalContext.current
    val app = RoxyApp.get(context)
    val vm: HomeViewModel = viewModel { HomeViewModel(app.api) }
    val state by vm.state.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()

    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { vm.load(silent = state is HomeUiState.Ready) }

    fun play(item: Item) = scope.launch { playItem(context, item).onFailure { Timber.w(it, "play failed") } }
    fun open(item: Item) {
        when {
            item.isSeries -> onOpenSeries(item)
            item.isMovie -> onOpenMovie(item)
            item.isEpisode -> play(item)
            item.isSeason -> item.seriesId?.let { onOpenSeries(Item(id = it, type = "series", title = item.seriesTitle ?: item.title)) }
        }
    }

    when (val s = state) {
        HomeUiState.Loading -> HomeSkeleton(bottomInset)
        is HomeUiState.Error -> when {
            s.offline -> OfflineState(serverUrl = app.api.baseUrl, onRetry = { vm.load() })
            else -> Column(modifier = Modifier.fillMaxSize().padding(PhonePad), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.Center) {
                Text(if (s.unauthorized) "Your session has expired." else "Could not load the library", color = RoxyColors.error, fontSize = 16.sp)
                Text(s.message, color = RoxyColors.muted, fontSize = 13.sp, textAlign = TextAlign.Center, modifier = Modifier.padding(top = 6.dp, bottom = 16.dp))
                if (s.unauthorized) GreyButton("Sign in again", onClick = onUnauthorized) else GreyButton("Retry", onClick = { vm.load() })
            }
        }
        is HomeUiState.Ready -> {
            val listState = rememberLazyListState()
            // The header fades to black over the first 160 dp of scroll and stays pinned.
            val fadePx = with(LocalDensity.current) { 160.dp.toPx() }
            val headerAlpha by remember {
                derivedStateOf {
                    if (listState.firstVisibleItemIndex > 0) 1f else (listState.firstVisibleItemScrollOffset / fadePx).coerceIn(0f, 1f)
                }
            }
            Box(modifier = Modifier.fillMaxSize()) {
                LazyColumn(state = listState, modifier = Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = bottomInset + 16.dp)) {
                    item(key = "hero") {
                        HeroCarousel(
                            heroes = s.heroes, api = app.api,
                            onPlay = { h -> if (h.play.isEpisode || h.play.isMovie) play(h.play) else open(h.play) },
                            onOpen = { h -> open(h.title) },
                        )
                    }
                    items(s.rows, key = { it.key }) { row ->
                        PhoneRow(row.title.ifEmpty { rowTitle(row.key) }, row.items, app.api, onClick = ::open, modifier = Modifier.padding(top = 22.dp))
                    }
                    item(key = "footer") {
                        if (s.rows.isEmpty()) Text("The library is empty. Scan a library on the server first.", color = RoxyColors.muted, textAlign = TextAlign.Center, modifier = Modifier.fillMaxWidth().padding(32.dp))
                        s.note?.let { Text(it, color = RoxyColors.muted.copy(alpha = 0.6f), fontSize = 11.sp, modifier = Modifier.padding(horizontal = PhonePad, vertical = 12.dp)) }
                        Spacer(Modifier.height(8.dp))
                    }
                }
                HomeHeader(alpha = headerAlpha, onSearch = onSearch, modifier = Modifier.align(Alignment.TopCenter))
            }
        }
    }
}

/** Pinned header: the witch-hat mark, Cast and Search; transparent over the hero, black once scrolled. */
@Composable
private fun HomeHeader(alpha: Float, onSearch: () -> Unit, modifier: Modifier = Modifier) {
    Row(
        modifier = modifier.fillMaxWidth().background(RoxyColors.background.copy(alpha = alpha)).statusBarsPadding().padding(horizontal = PhonePad, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        WitchHat(size = 32.dp)
        Spacer(Modifier.weight(1f))
        Icon(Icons.Filled.Cast, contentDescription = "Cast (coming soon)", tint = Color.White.copy(alpha = 0.6f), modifier = Modifier.size(24.dp))
        Spacer(Modifier.size(18.dp))
        Icon(Icons.Filled.Search, contentDescription = "Search", tint = Color.White, modifier = Modifier.size(26.dp).clickable(onClick = onSearch))
    }
}

private fun rowTitle(key: String) = when (key) {
    "continue" -> "Continue Watching"
    "recentEpisodes" -> "New Episodes"
    "recentMovies" -> "New Movies"
    "series" -> "Series"
    "movies" -> "Movies"
    else -> key
}

/** Home while the first load runs: the full-bleed banner and two rows as shimmer. */
@Composable
private fun HomeSkeleton(bottomInset: Dp) {
    Column(modifier = Modifier.fillMaxSize().padding(bottom = bottomInset)) {
        Bone(Modifier.fillMaxWidth().height(HeroHeight), shape = RoundedCornerShape(0.dp))
        RowSkeleton(modifier = Modifier.padding(top = 22.dp), thumbs = true)
        RowSkeleton(modifier = Modifier.padding(top = 22.dp))
    }
}

/** The server did not answer: likely off the home network. Downloads will live here. */
@Composable
private fun OfflineState(serverUrl: String, onRetry: () -> Unit) {
    Column(modifier = Modifier.fillMaxSize().padding(32.dp), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.Center) {
        Icon(Icons.Outlined.CloudOff, contentDescription = null, tint = RoxyColors.muted, modifier = Modifier.size(56.dp))
        Text("Can't reach your server", fontSize = 20.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(top = 16.dp))
        Text(serverUrl, fontSize = 13.sp, color = RoxyColors.muted, modifier = Modifier.padding(top = 4.dp))
        Text(
            "You may be away from your home network or Tailscale. Downloads for offline viewing are coming.",
            fontSize = 14.sp, lineHeight = 20.sp, color = RoxyColors.muted, textAlign = TextAlign.Center, modifier = Modifier.padding(top = 12.dp, bottom = 20.dp),
        )
        PlayButton("Try again", icon = Icons.Filled.Search, onClick = onRetry)
    }
}

/** Height of the full-bleed hero (image under the status bar, down to the timer bars). */
private val HeroHeight = 660.dp

/**
 * The carousel: a full-bleed banner (no card, no margins) that swipes between shows and
 * auto-advances after [HERO_DWELL_MS]; story-style bars along its bottom edge fill to
 * show when. Dragging resets the timer. The pinned header draws over the top edge.
 */
@Composable
private fun HeroCarousel(heroes: List<HeroPick>, api: RoxyApi, onPlay: (HeroPick) -> Unit, onOpen: (HeroPick) -> Unit) {
    val count = heroes.size
    val pager = rememberPagerState(pageCount = { count })
    val progress = remember { Animatable(0f) }

    // Timer: fill the current bar over the dwell, then move on. Any scroll restarts it.
    LaunchedEffect(count, pager.currentPage, pager.isScrollInProgress) {
        if (count < 2 || pager.isScrollInProgress) return@LaunchedEffect
        progress.snapTo(0f)
        progress.animateTo(1f, tween(HERO_DWELL_MS, easing = LinearEasing))
        pager.animateScrollToPage((pager.currentPage + 1) % count)
    }

    Box(modifier = Modifier.fillMaxWidth().height(HeroHeight)) {
        if (count == 0) {
            // Rows arrived before the carousel resolved (or the library is empty): hold the space.
            Bone(Modifier.fillMaxSize(), shape = RoundedCornerShape(0.dp))
        } else {
            HorizontalPager(state = pager, modifier = Modifier.fillMaxSize()) { page ->
                HeroSlide(heroes[page], api, onOpen = { onOpen(heroes[page]) }, onPlay = { onPlay(heroes[page]) })
            }
            if (count > 1) TimerBars(count = count, current = pager.currentPage, progress = progress.value, modifier = Modifier.align(Alignment.BottomStart))
        }
    }
}

/**
 * One banner: the poster cropped full-bleed and bleeding on under the text and the
 * button through a dark gradient; over its lower part the title treatment (logo) or
 * title, "★ 8.5  Dub | Sub • Action, Comedy", three lines of synopsis, then the Start
 * watching pill with the save circle beside it.
 */
@Composable
private fun HeroSlide(hero: HeroPick, api: RoxyApi, onOpen: () -> Unit, onPlay: () -> Unit) {
    val title = hero.title
    Box(modifier = Modifier.fillMaxSize().background(RoxyColors.surfaceHigh).clickable(onClick = onOpen)) {
        val poster = api.imageUrl(title, "poster", widthPx(480.dp)) ?: api.imageUrl(hero.play, "thumb", widthPx(480.dp))
        RoxyImage(poster, title.title, modifier = Modifier.fillMaxSize(), placeholderTextSize = 18)
        // Top scrim under the header; a long bottom fade so the picture shows through behind the text.
        Box(modifier = Modifier.matchParentSize().background(Brush.verticalGradient(0f to Color(0x80000000), 0.2f to Color.Transparent)))
        Box(modifier = Modifier.matchParentSize().background(Brush.verticalGradient(0.3f to Color.Transparent, 0.62f to Color(0xB30B0B0F), 0.9f to Color(0xF20B0B0F), 1f to RoxyColors.background)))

        Column(
            modifier = Modifier.align(Alignment.BottomCenter).fillMaxWidth().padding(start = PhonePad, end = PhonePad, bottom = 34.dp),
            horizontalAlignment = Alignment.Start,
            verticalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            val logo = api.imageUrl(title, "logo", widthPx(320.dp))
            if (logo != null) {
                Box(modifier = Modifier.fillMaxWidth(), contentAlignment = Alignment.Center) {
                    AsyncImage(model = logo, contentDescription = title.title, contentScale = ContentScale.Fit, modifier = Modifier.fillMaxWidth(0.72f).heightIn(max = 80.dp))
                }
            } else {
                Text(title.title, fontSize = 26.sp, lineHeight = 30.sp, fontWeight = FontWeight.ExtraBold, color = Color.White, textAlign = TextAlign.Center, maxLines = 2, overflow = TextOverflow.Ellipsis, modifier = Modifier.fillMaxWidth())
            }
            val badges = listOfNotNull(title.communityRating?.let { "★ %.1f".format(it) }, audioBadge(hero.play)).joinToString("  ")
            val genres = title.genres.take(3).joinToString(", ") // three at most, so the line never ellipsises
            val meta = listOf(badges, genres).filter { it.isNotEmpty() }.joinToString("  •  ")
            if (meta.isNotEmpty()) {
                Text(meta, fontSize = 12.sp, color = Color.White.copy(alpha = 0.85f), maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.fillMaxWidth())
            }
            title.overview?.takeIf { it.isNotBlank() }?.let {
                Text(it, fontSize = 13.sp, lineHeight = 18.sp, color = Color.White.copy(alpha = 0.92f), maxLines = 3, overflow = TextOverflow.Ellipsis, modifier = Modifier.fillMaxWidth())
            }
            val resumable = (hero.play.userData?.positionMs ?: 0) > 0
            val label = when {
                resumable && hero.play.isEpisode -> "Resume ${hero.play.episodeCode}"
                resumable -> "Resume"
                hero.play.isEpisode && hero.play.episodeCode.isNotEmpty() -> "Start watching ${hero.play.episodeCode}"
                else -> "Start watching"
            }
            Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(14.dp)) {
                PlayButton(label, modifier = Modifier.weight(1f), height = 46.dp, pill = true, container = RoxyColors.accent, content = OnBrand, onClick = onPlay)
                SaveButton(title, api)
            }
        }
    }
}

/** Round bookmark in the brand blue: adds the show to the Watchlist (favourite); filled once saved. */
@Composable
private fun SaveButton(item: Item, api: RoxyApi) {
    val scope = rememberCoroutineScope()
    var on by remember(item.id, item.userData?.isFavorite) { mutableStateOf(item.userData?.isFavorite == true) }
    Box(
        modifier = Modifier.size(46.dp).clip(CircleShape).border(2.dp, RoxyColors.accent, CircleShape).clickable {
            val want = !on
            on = want
            scope.launch { runCatching { api.setFavorite(item.id, want) }.onFailure { Timber.w(it, "favorite failed"); on = !want } }
        },
        contentAlignment = Alignment.Center,
    ) {
        Icon(if (on) Icons.Filled.Bookmark else Icons.Outlined.BookmarkBorder, contentDescription = if (on) "Saved" else "Save", tint = RoxyColors.accent, modifier = Modifier.size(24.dp))
    }
}

/**
 * "Dub | Sub" for anime from the playable item's audio tracks: both English and Japanese
 * audio = Dub | Sub, Japanese only = Sub, English only = Dub. Null when the streams are
 * unknown (series slides resolved from the library carry no sources yet).
 */
private fun audioBadge(play: Item): String? {
    val langs = play.primarySource?.streams.orEmpty().filter { it.type == "audio" }.mapNotNull { it.language?.lowercase() }.toSet()
    if (langs.isEmpty()) return null
    val eng = langs.any { it == "eng" || it == "en" }
    val jpn = langs.any { it == "jpn" || it == "ja" }
    return when {
        eng && jpn -> "Dub | Sub"
        jpn -> "Sub"
        eng && play.genres.any { it.equals("Animation", true) } -> "Dub"
        else -> null
    }
}

/**
 * Story-style segmented pills under the button, about 80 % of its width: done slides
 * full, the current one filling in the brand blue, the rest dim.
 */
@Composable
private fun TimerBars(count: Int, current: Int, progress: Float, modifier: Modifier = Modifier) {
    Row(modifier = modifier.fillMaxWidth(0.8f).padding(start = PhonePad, bottom = 10.dp), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
        repeat(count) { i ->
            val fill = when { i < current -> 1f; i == current -> progress; else -> 0f }
            Box(modifier = Modifier.weight(1f).height(5.dp).clip(RoundedCornerShape(3.dp)).background(Color.White.copy(alpha = 0.28f))) {
                Box(modifier = Modifier.fillMaxWidth(fill).height(5.dp).clip(RoundedCornerShape(3.dp)).background(RoxyColors.accent))
            }
        }
    }
}
