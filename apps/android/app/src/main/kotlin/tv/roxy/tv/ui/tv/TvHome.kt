package tv.roxy.tv.ui.tv

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.tv.material3.Text
import coil3.compose.AsyncImage
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.HomeRow
import tv.roxy.tv.api.Item
import tv.roxy.tv.api.RoxyApi
import tv.roxy.tv.ui.components.RoxyImage
import tv.roxy.tv.ui.components.widthPx
import tv.roxy.tv.ui.home.HeroPick
import tv.roxy.tv.ui.home.HomeUiState
import tv.roxy.tv.ui.home.HomeViewModel
import tv.roxy.tv.ui.phone.OnUserDataChanged
import tv.roxy.tv.ui.playItem
import tv.roxy.tv.ui.theme.RoxyColors

/** The hero block's height; the first row's title shows under it, as on Crunchyroll. */
private val HeroHeight = 455.dp

/** A focused row is scrolled to sit this far down the page: the row above keeps its captions in view. */
private val RowPin = 105.dp

/**
 * Home: the hero (what you are in the middle of, else the newest arrival: key art, logo,
 * facts, synopsis, START WATCHING / MORE INFO) over the rows from GET /v1/home. Continue
 * Watching is 16:9 cards, the other rows posters.
 */
@Composable
fun TvHome(onOpenSeries: (Item) -> Unit, onOpenMovie: (Item) -> Unit, onUnauthorized: () -> Unit) {
    val context = LocalContext.current
    val app = RoxyApp.get(context)
    val vm: HomeViewModel = viewModel { HomeViewModel(app.api) }
    val state by vm.state.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    val memory = rememberFocusMemory()
    val listState = rememberLazyListState()

    // (Re)load on every return to the foreground so progress made in the player shows up.
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { vm.load(silent = state is HomeUiState.Ready) }
    OnUserDataChanged { vm.load(silent = true) }

    fun open(item: Item) {
        when {
            item.isSeries -> onOpenSeries(item)
            item.isMovie -> onOpenMovie(item)
            item.isEpisode -> scope.launch { playItem(context, item).onFailure { Timber.w(it, "play failed") } }
            item.isSeason -> item.seriesId?.let { onOpenSeries(Item(id = it, type = "series", title = item.seriesTitle ?: item.title)) }
        }
    }

    when (val s = state) {
        HomeUiState.Loading -> HomeSkeleton()
        is HomeUiState.Error -> Column(modifier = Modifier.fillMaxSize().padding(start = TvDims.ContentStart, top = 160.dp)) {
            Text(if (s.unauthorized) "Your session has expired." else "Could not load the library", color = RoxyColors.error, fontSize = 20.sp)
            Text(s.message, color = RoxyColors.muted, fontSize = 14.sp, modifier = Modifier.padding(top = 6.dp, bottom = 20.dp))
            Row {
                if (s.unauthorized) TvButton("Sign in again", onClick = onUnauthorized, modifier = Modifier.focusKey(memory, "retry"))
                else TvButton("Retry", onClick = { vm.load() }, modifier = Modifier.focusKey(memory, "retry"))
            }
            LaunchedEffect(Unit) { memory.restore("retry") }
        }
        is HomeUiState.Ready -> {
            val hero = s.heroes.firstOrNull() ?: s.hero?.let { HeroPick(it, it) }
            val pin = with(LocalDensity.current) { RowPin.roundToPx() }
            var focusedRow by remember { mutableStateOf(-1) }
            LaunchedEffect(focusedRow) { if (focusedRow >= 0) listState.animateScrollToItem(focusedRow, -pin) }
            val rows = remember(s.rows) { tvRows(s.rows) }
            RestoreFocus(memory, listState, rows, pin, fallback = "hero/play")
            LazyColumn(state = listState, modifier = Modifier.fillMaxSize()) {
                item(key = "hero") {
                    Hero(
                        pick = hero, api = app.api, memory = memory,
                        onPlay = { play -> if (play.isEpisode || play.isMovie) scope.launch { playItem(context, play).onFailure { Timber.w(it, "play failed") } } else open(play) },
                        onInfo = { title -> open(title) },
                        onFocused = { focusedRow = 0 },
                    )
                }
                itemsIndexed(rows, key = { _, r -> r.key }) { i, row ->
                    TvRow(
                        title = row.title, rowKey = row.key, items = row.items, api = app.api, memory = memory,
                        onClick = ::open, onRowFocused = { focusedRow = i + 1 }, thumbs = row.key == "continue",
                    )
                }
                item(key = "footer") {
                    if (rows.isEmpty()) PageMessage("The library is empty. Scan a library on the server first.")
                    Spacer(Modifier.height(48.dp))
                }
            }
        }
    }
}

/**
 * Puts focus back where it was when the page is (re)created: the hero's button at first,
 * the last focused card after a title page. A card in a row that is not composed yet is
 * reached by scrolling its row into place first.
 */
@Composable
private fun RestoreFocus(memory: FocusMemory, listState: LazyListState, rows: List<HomeRow>, pin: Int, fallback: String) {
    var done by remember { mutableStateOf(false) }
    LaunchedEffect(rows.isNotEmpty()) {
        if (done || rows.isEmpty()) return@LaunchedEffect
        val key = memory.key
        val rowIndex = key?.let { k -> rows.indexOfFirst { k.startsWith(it.key + "/") } } ?: -1
        if (rowIndex >= 0) listState.scrollToItem(rowIndex + 1, -pin)
        repeat(6) {
            withFrameNanos { }
            if (memory.restore(fallback)) { done = true; return@LaunchedEffect }
        }
    }
}

/**
 * The same rows as the phone's Home, from the server's: Recently added (shows and movies
 * together, newest first), Continue Watching, then Series and Movies. The server's
 * new-episodes row is not shown.
 */
private fun tvRows(rows: List<HomeRow>): List<HomeRow> {
    fun row(key: String) = rows.firstOrNull { it.key == key }?.items.orEmpty()
    val recent = (row("series") + row("recentMovies")).sortedByDescending { it.dateCreated.orEmpty() }.take(20)
    return buildList {
        if (recent.isNotEmpty()) add(HomeRow("recent", "Recently added", recent))
        row("continue").takeIf { it.isNotEmpty() }?.let { add(HomeRow("continue", "Continue Watching", it)) }
        row("series").takeIf { it.isNotEmpty() }?.let { add(HomeRow("series", "Series", it)) }
        row("movies").takeIf { it.isNotEmpty() }?.let { add(HomeRow("movies", "Movies", it)) }
    }
}

/** Grey blocks where the hero and the first row will be. */
@Composable
private fun HomeSkeleton() {
    Column(modifier = Modifier.fillMaxSize()) {
        Spacer(Modifier.height(HeroHeight))
        Row(modifier = Modifier.padding(start = TvDims.ContentStart, top = 38.dp)) {
            repeat(6) { Box(Modifier.width(TvDims.PosterCardWidth).height(260.dp).background(RoxyColors.surface)); Spacer(Modifier.width(8.dp)) }
        }
    }
}

/**
 * Crunchyroll's hero: full-bleed key art fading into the page at the bottom and under the
 * copy at the left; the logo (or the name), the facts line, two lines of synopsis, then
 * START WATCHING / CONTINUE and MORE INFO.
 */
@Composable
private fun Hero(pick: HeroPick?, api: RoxyApi, memory: FocusMemory, onPlay: (Item) -> Unit, onInfo: (Item) -> Unit, onFocused: () -> Unit) {
    Box(modifier = Modifier.fillMaxWidth().height(HeroHeight)) {
        if (pick == null) {
            Column(modifier = Modifier.padding(start = TvDims.ContentStart, top = 160.dp)) {
                Text("Nothing to show yet", fontSize = 30.sp, fontWeight = FontWeight.Bold)
                Text("Add media on the server and scan the library.", color = RoxyColors.muted, fontSize = 16.sp)
            }
            return
        }
        val title = pick.title
        val play = pick.play
        val art = api.imageUrl(title, "backdrop", widthPx(960.dp)) ?: api.imageUrl(play, "thumb", widthPx(960.dp))
        RoxyImage(art, "", modifier = Modifier.fillMaxSize(), placeholderTextSize = 0)
        // Dark under the copy (left) and into the page (bottom); near-black under the rail.
        Box(modifier = Modifier.fillMaxSize().background(Brush.horizontalGradient(0f to RoxyColors.background.copy(alpha = 0.95f), 0.08f to RoxyColors.background.copy(alpha = 0.85f), 0.55f to Color.Transparent)))
        Box(modifier = Modifier.fillMaxSize().background(Brush.verticalGradient(0.55f to Color.Transparent, 1f to RoxyColors.background)))

        Column(modifier = Modifier.padding(start = TvDims.ContentStart, top = 112.dp).widthIn(max = 460.dp)) {
            val tag = if ((play.userData?.positionMs ?: 0L) > 0) "Continue Watching" else "Newly Added"
            Text(tag, color = RoxyColors.onBackground, fontSize = 14.sp, fontWeight = FontWeight.Bold)
            Spacer(Modifier.height(10.dp))
            Box(modifier = Modifier.height(90.dp).fillMaxWidth(), contentAlignment = Alignment.BottomStart) {
                val logo = api.imageUrl(title, "logo", widthPx(420.dp))
                if (logo != null) {
                    AsyncImage(model = logo, contentDescription = title.title, contentScale = ContentScale.Fit, alignment = Alignment.BottomStart, modifier = Modifier.fillMaxWidth(0.5f).heightIn(max = 90.dp))
                } else {
                    Text(title.title, fontSize = 32.sp, lineHeight = 36.sp, fontWeight = FontWeight.Bold, maxLines = 2, overflow = TextOverflow.Ellipsis)
                }
            }
            Spacer(Modifier.height(14.dp))
            FactsLine(title, play)
            Spacer(Modifier.height(10.dp))
            val synopsis = title.overview?.takeIf { it.isNotBlank() } ?: play.overview.orEmpty()
            Text(synopsis, color = RoxyColors.onBackground, fontSize = 17.sp, lineHeight = 20.sp, minLines = 2, maxLines = 2, overflow = TextOverflow.Ellipsis)
            Spacer(Modifier.height(24.dp))
            Row {
                val started = (play.userData?.positionMs ?: 0L) > 0
                val label = if (play.isEpisode && (started || (play.indexAired ?: 1) > 1)) "Continue: ${episodeShort(play)}" else if (started) "Continue" else "Start watching"
                TvButton(label, play = true, onClick = { onPlay(play) }, modifier = Modifier.focusKey(memory, "hero/play"))
                Spacer(Modifier.width(18.dp))
                TvButton("More info", onClick = { onInfo(title) }, modifier = Modifier.focusKey(memory, "hero/info"))
            }
        }
        // The buttons sit at the bottom of the block; focus on either pins the page to the top.
        LaunchedEffect(memory.key) { if (memory.key?.startsWith("hero/") == true) onFocused() }
    }
}
