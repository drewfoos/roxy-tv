package tv.roxy.tv.ui.phone

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.StartOffset
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.ui.graphics.drawscope.Stroke
import tv.roxy.tv.data.downloads.DownloadedTitle
import tv.roxy.tv.data.downloads.continueFromDownloads
import androidx.compose.foundation.background
import androidx.compose.foundation.interaction.collectIsDraggedAsState
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
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
import androidx.compose.foundation.layout.width
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
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.outlined.CloudOff
import androidx.compose.material.icons.outlined.Download
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.material3.CircularProgressIndicator
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
import androidx.compose.material.icons.filled.Explore
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
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.RoxyApp
import androidx.compose.ui.platform.LocalConfiguration
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
    onDownloads: () -> Unit,
    onOpenDownloadedTitle: (String) -> Unit,
    onUnauthorized: () -> Unit,
    bottomInset: Dp,
    onOpenDiscover: () -> Unit = {},
    onOpenLibrarySettings: () -> Unit = {},
    onAddFolder: () -> Unit = {},
) {
    val context = LocalContext.current
    val app = RoxyApp.get(context)
    val vm: HomeViewModel = viewModel { HomeViewModel(app.api) }
    val state by vm.state.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()

    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { vm.load(silent = state is HomeUiState.Ready) }
    OnUserDataChanged { vm.load(silent = true) }

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
        HomeUiState.Loading -> Box(Modifier.fillMaxSize().padding(bottom = bottomInset), contentAlignment = Alignment.Center) {
            CircularProgressIndicator(color = RoxyColors.accent, strokeWidth = 3.dp, strokeCap = StrokeCap.Round, modifier = Modifier.size(40.dp))
        }
        is HomeUiState.Error -> when {
            // The server restarted or the Wi-Fi dropped: reload once it answers again.
            s.offline -> {
                tv.roxy.tv.ui.components.OnServerBack { vm.load() }
                val index by app.downloads.index.collectAsStateWithLifecycle()
                val titles = remember(index) { app.downloads.titles().filter { it.doneCount > 0 } }
                OfflineState(serverUrl = app.api.baseUrl, titles = titles, bottomInset = bottomInset, onRetry = { vm.load() }, onDownloads = onDownloads, onOpenTitle = onOpenDownloadedTitle)
            }
            else -> Column(modifier = Modifier.fillMaxSize().padding(PhonePad), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.Center) {
                if (!s.unauthorized) tv.roxy.tv.ui.components.OnServerBack { vm.load() }
                Text(if (s.unauthorized) "Your session has expired." else "Could not load the library", color = RoxyColors.error, fontSize = 16.sp)
                Text(s.message, color = RoxyColors.muted, fontSize = 13.sp, textAlign = TextAlign.Center, modifier = Modifier.padding(top = 6.dp, bottom = 16.dp))
                if (s.unauthorized) GreyButton("Sign in again", onClick = onUnauthorized) else GreyButton("Retry", onClick = { vm.load() })
            }
        }
        // Nothing in the library yet: a proper welcome instead of an empty banner.
        is HomeUiState.Ready if s.heroes.isEmpty() && s.rows.all { it.items.isEmpty() } -> Box(modifier = Modifier.fillMaxSize()) {
            EmptyLibrary(isAdmin = app.prefs.session.value.isAdmin, bottomInset = bottomInset, onOpenDiscover = onOpenDiscover, onOpenLibrarySettings = onOpenLibrarySettings, onAddFolder = onAddFolder, onRescanned = { vm.load(silent = true) })
            HomeHeader(alpha = 0f, onSearch = onSearch, modifier = Modifier.align(Alignment.TopCenter))
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
                    // Recently added leads: shows and movies together, newest first, as big
                    // title cards. Continue Watching follows, then the Series and Movies rows.
                    val recent = (s.rows.firstOrNull { it.key == "series" }?.items.orEmpty() + s.rows.firstOrNull { it.key == "recentMovies" }?.items.orEmpty())
                        .sortedByDescending { it.dateCreated.orEmpty() }.take(20)
                    if (recent.isNotEmpty()) {
                        item(key = "recent") { PhoneRow("Recently added", recent, app.api, onClick = ::open, modifier = Modifier.padding(top = 22.dp)) }
                    }
                    s.rows.firstOrNull { it.key == "continue" }?.let { row ->
                        item(key = "continue") { ContinueRow("Continue Watching", row.items, app.api, onClick = ::open, modifier = Modifier.padding(top = 26.dp)) }
                    }
                    items(s.rows.filter { it.key in setOf("series", "movies") }, key = { it.key }) { row ->
                        PhoneRow(row.title.ifEmpty { rowTitle(row.key) }, row.items, app.api, onClick = ::open, modifier = Modifier.padding(top = 26.dp))
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

/**
 * Home with an empty library. For the admin it says what is actually wrong, because a
 * folder being set up is not the same as videos being in it:
 *  - no media folders yet: add one;
 *  - a folder the server can't see (not mounted in Docker, a drive unplugged): fix it;
 *  - folders but no videos found in them: they're empty, or a scan hasn't run;
 *  - videos found but nothing to show yet: the first scan is still sorting them.
 * Everyone else is told the server has nothing yet, and pointed at Discover.
 */
@Composable
private fun EmptyLibrary(isAdmin: Boolean, bottomInset: Dp, onOpenDiscover: () -> Unit, onOpenLibrarySettings: () -> Unit, onAddFolder: () -> Unit, onRescanned: () -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    val scope = rememberCoroutineScope()
    var folders by remember { mutableStateOf<List<tv.roxy.tv.api.MediaFolder>?>(null) }
    var scanning by remember { mutableStateOf(false) }
    LaunchedEffect(isAdmin) { if (isAdmin) folders = runCatching { app.api.mediaFolders() }.getOrNull() }
    val f = folders
    val unreachable = f?.filter { !it.reachable }.orEmpty()
    val (title, body) = when {
        !isAdmin -> "Nothing to watch yet" to "The server doesn't have any shows or movies yet. Request some in Discover; they show up here once they're in."
        f == null -> "Nothing to watch yet" to ""
        f.isEmpty() -> "Add your media" to "Roxy hasn't been told where your shows and movies are. Pick the folders that hold them on the server."
        unreachable.isNotEmpty() -> "Roxy can't see a media folder" to "The server can't open ${unreachable.joinToString { it.root }}. If it runs in Docker, check the folder is mounted into the container; if it's a drive, check it's connected."
        f.sumOf { it.files } == 0 -> "No videos found yet" to "Roxy reads ${f.joinToString { it.root }}, but found no videos there. Make sure the files are in that folder (or a folder inside it), then scan again."
        else -> "Almost there" to "Roxy found ${f.sumOf { it.files }} videos and is still sorting them into shows and movies. This page fills in as it goes."
    }
    Column(
        modifier = Modifier.fillMaxSize().padding(start = PhonePad, end = PhonePad, bottom = bottomInset),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        WitchHat(size = 72.dp)
        Text(title, fontSize = 22.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, textAlign = TextAlign.Center, modifier = Modifier.padding(top = 20.dp))
        if (body.isNotEmpty()) Text(body, fontSize = 14.sp, lineHeight = 20.sp, color = RoxyColors.muted, textAlign = TextAlign.Center, modifier = Modifier.padding(top = 10.dp, bottom = 24.dp))
        when {
            isAdmin && f != null && f.isEmpty() -> {
                PlayButton("Add media folder", icon = Icons.Filled.Add, pill = true, container = RoxyColors.accent, content = OnBrand, modifier = Modifier.fillMaxWidth(0.8f), onClick = onAddFolder)
                GreyButton("Discover something", modifier = Modifier.padding(top = 12.dp).fillMaxWidth(0.8f), onClick = onOpenDiscover)
            }
            isAdmin && f != null && (unreachable.isNotEmpty() || f.sumOf { it.files } == 0) -> {
                PlayButton(if (scanning) "Scanning…" else "Scan again", icon = Icons.Filled.Refresh, pill = true, container = RoxyColors.accent, content = OnBrand, modifier = Modifier.fillMaxWidth(0.8f), onClick = {
                    if (scanning) return@PlayButton
                    scanning = true
                    scope.launch {
                        runCatching { app.api.libraryScan("files") }
                        kotlinx.coroutines.delay(4000)
                        folders = runCatching { app.api.mediaFolders() }.getOrNull()
                        scanning = false
                        onRescanned()
                    }
                })
                GreyButton("Media folders", modifier = Modifier.padding(top = 12.dp).fillMaxWidth(0.8f), onClick = onOpenLibrarySettings)
            }
            else -> {
                PlayButton("Discover something", icon = Icons.Filled.Explore, pill = true, container = RoxyColors.accent, content = OnBrand, modifier = Modifier.fillMaxWidth(0.8f), onClick = onOpenDiscover)
                if (isAdmin) GreyButton("Library settings", modifier = Modifier.padding(top = 12.dp).fillMaxWidth(0.8f), onClick = onOpenLibrarySettings)
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
        CastIcon()
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

/**
 * Home with the server away. With downloads on the phone it is a small Home made of them:
 * a line saying why, Continue Watching (from offline progress) and the downloaded titles.
 * With none, the hat sends out sonar rings while we look for the server.
 */
@Composable
internal fun OfflineState(serverUrl: String, titles: List<DownloadedTitle>, bottomInset: Dp, onRetry: () -> Unit, onDownloads: () -> Unit, onOpenTitle: (String) -> Unit) {
    val context = LocalContext.current
    val app = RoxyApp.get(context)
    val scope = rememberCoroutineScope()
    val words = rememberOfflineWords()
    val resume = remember(titles) { continueFromDownloads(titles) }
    Column(modifier = Modifier.fillMaxSize().verticalScroll(rememberScrollState()).statusBarsPadding().padding(bottom = bottomInset + 16.dp), horizontalAlignment = Alignment.CenterHorizontally) {
        if (titles.isEmpty()) {
            val (retryWord, retry) = rememberRetry(onRetry)
            Sonar(modifier = Modifier.padding(top = 56.dp)) { WitchHat(size = 64.dp) }
            Text(words.title, fontSize = 22.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(top = 8.dp))
            Text(serverUrl.removePrefix("http://").removePrefix("https://"), fontSize = 13.sp, color = RoxyColors.muted, modifier = Modifier.padding(top = 4.dp))
            Text(words.body, fontSize = 14.sp, lineHeight = 21.sp, color = RoxyColors.muted, textAlign = TextAlign.Center, modifier = Modifier.padding(start = 40.dp, end = 40.dp, top = 12.dp, bottom = 24.dp))
            PlayButton(retryWord, icon = Icons.Filled.Refresh, pill = true, onClick = retry)
            return@Column
        }
        // Home's own top (the hat) and Home's own rows and cards, filled from the downloads.
        Row(modifier = Modifier.fillMaxWidth().padding(horizontal = PhonePad, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) { WitchHat(size = 32.dp) }
        OfflineNote(onRetry = onRetry, modifier = Modifier.padding(top = 10.dp))
        val saved: (Item, String, Int) -> String? = { item, type, px -> app.downloads.image(app.api, item, type, px) }
        if (resume.isNotEmpty()) {
            ContinueRow(
                "Continue Watching", resume.map { it.item }, app.api, image = saved, menu = false, modifier = Modifier.padding(top = 24.dp),
                onClick = { item -> scope.launch { playItem(context, item).onFailure { Timber.w(it, "play failed") } } },
            )
        }
        val cardWidth = (LocalConfiguration.current.screenWidthDp.dp - PhonePad - 10.dp * 2) / 2.5f
        Column(modifier = Modifier.fillMaxWidth().padding(top = 26.dp)) {
            RowHeading("Downloads", onSeeAll = onDownloads, modifier = Modifier.padding(bottom = 6.dp))
            LazyRow(contentPadding = PaddingValues(horizontal = PhonePad), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                items(titles, key = { it.id }) { t ->
                    TitleCard(
                        t.title, app.api, width = cardWidth, menu = false,
                        image = saved(t.title, "poster", widthPx(cardWidth) * 2),
                        caption = if (t.title.isMovie) "Movie" else "${t.doneCount} episode${if (t.doneCount == 1) "" else "s"}",
                        onClick = { onOpenTitle(t.id) },
                    )
                }
            }
        }
    }
}

/**
 * Three rings in the brand blue swelling out from the centre and fading, like a sonar
 * ping, with [content] (the hat) sitting still in the middle.
 */
@Composable
private fun Sonar(modifier: Modifier = Modifier, content: @Composable () -> Unit) {
    val transition = rememberInfiniteTransition(label = "sonar")
    val phases = (0 until 3).map { i ->
        transition.animateFloat(
            initialValue = 0f, targetValue = 1f,
            animationSpec = infiniteRepeatable(tween(2700, easing = LinearEasing), initialStartOffset = StartOffset(i * 900)),
            label = "ring$i",
        )
    }
    Box(modifier = modifier.size(220.dp), contentAlignment = Alignment.Center) {
        Canvas(modifier = Modifier.fillMaxSize()) {
            val maxR = size.minDimension / 2
            for (p in phases) {
                val t = p.value
                val r = 36.dp.toPx() + (maxR - 36.dp.toPx()) * t
                val alpha = (1f - t) * 0.55f
                drawCircle(color = RoxyColors.accent.copy(alpha = alpha), radius = r, style = Stroke(width = 2.dp.toPx()))
                drawCircle(color = RoxyColors.accent.copy(alpha = alpha * 0.12f), radius = r)
            }
        }
        Box(modifier = Modifier.size(88.dp).clip(CircleShape).background(RoxyColors.surface), contentAlignment = Alignment.Center) { content() }
    }
}

/** Height of the full-bleed hero (image under the status bar, down to the timer bars). */
private val HeroHeight = 640.dp

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

    // Timer: fill the current bar over the dwell, then move on. A finger on the pager
    // restarts it. The effect must not key on isScrollInProgress: its own scroll to the
    // next page would cancel it mid-animation and the pager would settle back where it was.
    val dragged by pager.interactionSource.collectIsDraggedAsState()
    val scope = rememberCoroutineScope()
    LaunchedEffect(count, pager.currentPage, dragged) {
        if (count < 2 || dragged) return@LaunchedEffect
        progress.snapTo(0f)
        progress.animateTo(1f, tween(HERO_DWELL_MS, easing = LinearEasing))
        val next = (pager.currentPage + 1) % count
        scope.launch { pager.animateScrollToPage(next) }
    }

    BoxWithConstraints(modifier = Modifier.fillMaxWidth().height(HeroHeight)) {
        if (count == 0) {
            // Rows arrived before the carousel resolved (or the library is empty): hold the space.
            Bone(Modifier.fillMaxSize(), shape = RoundedCornerShape(0.dp))
        } else {
            HorizontalPager(state = pager, modifier = Modifier.fillMaxSize()) { page ->
                HeroSlide(heroes[page], api, onOpen = { onOpen(heroes[page]) }, onPlay = { onPlay(heroes[page]) })
            }
            // 90 % of the Start watching button (the row minus the save circle and its gap).
            val buttonWidth = maxWidth - PhonePad * 2 - 44.dp - 14.dp
            if (count > 1) TimerBars(count = count, current = pager.currentPage, progress = progress.value, width = buttonWidth * 0.9f, modifier = Modifier.align(Alignment.BottomStart))
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
        // Textless key art when the server has one (the logo is drawn over it, so a poster
        // with the title on it would show the name twice), else the poster.
        val art = api.imageUrl(title, "hero", widthPx(480.dp)) ?: api.imageUrl(title, "poster", widthPx(480.dp)) ?: api.imageUrl(hero.play, "thumb", widthPx(480.dp))
        RoxyImage(art, title.title, modifier = Modifier.fillMaxSize(), placeholderTextSize = 18)
        // Top scrim under the header; a long bottom fade so the picture shows through behind the text.
        Box(modifier = Modifier.matchParentSize().background(Brush.verticalGradient(0f to Color(0x80000000), 0.2f to Color.Transparent)))
        Box(modifier = Modifier.matchParentSize().background(Brush.verticalGradient(0.3f to Color.Transparent, 0.62f to Color(0xB30B0B0F), 0.9f to Color(0xF20B0B0F), 1f to RoxyColors.background)))

        Column(
            // Everything flush left: the logo, then the facts line close under it, the
            // synopsis close under that, and a clear gap before the button; 34 dp from the
            // button down to the timer bars (bars are 9 dp + 10 dp padding).
            modifier = Modifier.align(Alignment.BottomCenter).fillMaxWidth().padding(start = PhonePad, end = PhonePad, bottom = 34.dp),
            horizontalAlignment = Alignment.Start,
        ) {
            val logo = api.imageUrl(title, "logo", widthPx(320.dp))
            if (logo != null) {
                AsyncImage(model = logo, contentDescription = title.title, contentScale = ContentScale.Fit, alignment = Alignment.CenterStart, modifier = Modifier.fillMaxWidth(0.62f).heightIn(max = 64.dp))
            } else {
                Text(title.title, fontSize = 26.sp, lineHeight = 30.sp, fontWeight = FontWeight.ExtraBold, color = Color.White, maxLines = 2, overflow = TextOverflow.Ellipsis, modifier = Modifier.fillMaxWidth())
            }
            val badges = listOfNotNull(title.communityRating?.let { "★ %.1f".format(it) }, audioBadge(hero.play)).joinToString("  ")
            val genres = title.genres.take(3).joinToString(", ") // three at most, so the line never ellipsises
            val meta = listOf(badges, genres).filter { it.isNotEmpty() }.joinToString("  •  ")
            if (meta.isNotEmpty()) {
                Text(meta, fontSize = 12.sp, color = Color.White.copy(alpha = 0.85f), maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.fillMaxWidth().padding(top = 12.dp))
            }
            title.overview?.takeIf { it.isNotBlank() }?.let {
                Text(it, fontSize = 13.sp, lineHeight = 18.sp, color = Color.White.copy(alpha = 0.92f), maxLines = 3, overflow = TextOverflow.Ellipsis, modifier = Modifier.fillMaxWidth().padding(top = 8.dp))
            }
            Spacer(Modifier.height(20.dp))
            val resumable = (hero.play.userData?.positionMs ?: 0) > 0
            val label = when {
                resumable && hero.play.isEpisode -> "Resume ${hero.play.episodeCode}"
                resumable -> "Resume"
                hero.play.isEpisode && hero.play.episodeCode.isNotEmpty() -> "Start watching ${hero.play.episodeCode}"
                else -> "Start watching"
            }
            PlayAndSaveRow(label, title, api, onPlay = onPlay)
        }
    }
}

/**
 * Story-style segmented pills under the button, about 80 % of its width: done slides
 * full, the current one filling in the brand blue, the rest dim.
 */
@Composable
private fun TimerBars(count: Int, current: Int, progress: Float, width: Dp, modifier: Modifier = Modifier) {
    Row(modifier = modifier.padding(start = PhonePad, bottom = 8.dp).width(width), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
        repeat(count) { i ->
            val fill = when { i < current -> 1f; i == current -> progress; else -> 0f }
            Box(modifier = Modifier.weight(1f).height(7.dp).clip(RoundedCornerShape(3.5.dp)).background(Color.White.copy(alpha = 0.55f))) {
                Box(modifier = Modifier.fillMaxWidth(fill).height(7.dp).clip(RoundedCornerShape(3.5.dp)).background(RoxyColors.accent))
            }
        }
    }
}
