package tv.roxy.tv.ui.phone

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
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Info
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.produceState
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.blur
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
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
import tv.roxy.tv.ui.home.HomeUiState
import tv.roxy.tv.ui.home.HomeViewModel
import tv.roxy.tv.ui.playItem
import tv.roxy.tv.ui.theme.RoxyColors

/**
 * Netflix's phone Home: wordmark, a big portrait hero card with Play / Info, then rows.
 * Same [HomeViewModel] and rows as the TV screen.
 */
@Composable
fun PhoneHome(
    onOpenSeries: (Item) -> Unit,
    onOpenMovie: (Item) -> Unit,
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
    fun openSeriesOf(ep: Item) = (ep.seriesId ?: ep.parentId)?.let { onOpenSeries(Item(id = it, type = "series", title = ep.seriesTitle ?: "")) }

    when (val s = state) {
        HomeUiState.Loading -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { Text("Loading your library...", color = RoxyColors.muted) }
        is HomeUiState.Error -> Column(modifier = Modifier.fillMaxSize().padding(PhonePad), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.Center) {
            Text(if (s.unauthorized) "Your session has expired." else "Could not load the library", color = RoxyColors.error, fontSize = 16.sp)
            Text(s.message, color = RoxyColors.muted, fontSize = 13.sp, textAlign = TextAlign.Center, modifier = Modifier.padding(top = 6.dp, bottom = 16.dp))
            if (s.unauthorized) GreyButton("Sign in again", onClick = onUnauthorized) else GreyButton("Retry", onClick = { vm.load() })
        }
        is HomeUiState.Ready -> {
            val hero = s.hero
            // The hero card wants a portrait poster; an episode has only a thumb, so fetch its series.
            val heroTitle by produceState<Item?>(initialValue = null, hero?.id) {
                value = when {
                    hero == null -> null
                    hero.isEpisode -> hero.seriesId?.let { id -> runCatching { app.api.item(id) }.getOrNull() }
                    else -> hero
                }
            }
            LazyColumn(modifier = Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = bottomInset + 16.dp)) {
                item(key = "hero") {
                    HeroCard(
                        hero = hero, title = heroTitle, api = app.api,
                        onPlay = { h -> if (h.isEpisode || h.isMovie) play(h) else open(h) },
                        onInfo = { h -> if (h.isEpisode) openSeriesOf(h) else open(h) },
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
        }
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
 * The top of Netflix's Home: wordmark over a soft glow (the backdrop, blurred), then a
 * rounded 2:3 poster card with the title, genres and Play / Info over its bottom edge.
 */
@Composable
private fun HeroCard(hero: Item?, title: Item?, api: RoxyApi, onPlay: (Item) -> Unit, onInfo: (Item) -> Unit) {
    Box(modifier = Modifier.fillMaxWidth()) {
        val glow = title?.let { api.imageUrl(it, "backdrop", widthPx(400.dp)) } ?: hero?.let { api.imageUrl(it, "thumb", widthPx(400.dp)) }
        if (glow != null) {
            AsyncImage(model = glow, contentDescription = null, contentScale = ContentScale.Crop, modifier = Modifier.matchParentSize().blur(60.dp).alpha(0.5f))
        }
        Box(modifier = Modifier.matchParentSize().background(Brush.verticalGradient(0f to Color.Transparent, 0.6f to RoxyColors.background.copy(alpha = 0.5f), 1f to RoxyColors.background)))

        Column(modifier = Modifier.fillMaxWidth().statusBarsPadding()) {
            Row(modifier = Modifier.fillMaxWidth().padding(horizontal = PhonePad, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
                Text("Roxy", fontSize = 26.sp, fontWeight = FontWeight.Black, color = RoxyColors.accent, letterSpacing = (-0.5).sp)
            }
            Box(
                modifier = Modifier
                    .padding(horizontal = PhonePad)
                    .fillMaxWidth()
                    .aspectRatio(2f / 3f)
                    .clip(RoundedCornerShape(12.dp))
                    .background(RoxyColors.surfaceHigh)
                    .clickable(enabled = hero != null) { hero?.let(onInfo) },
            ) {
                if (hero == null) {
                    Column(modifier = Modifier.fillMaxSize().padding(24.dp), verticalArrangement = Arrangement.Center, horizontalAlignment = Alignment.CenterHorizontally) {
                        Text("Nothing to show yet", fontSize = 20.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground)
                        Text("Add media on the server and scan the library.", color = RoxyColors.muted, textAlign = TextAlign.Center)
                    }
                    return@Box
                }
                val poster = title?.let { api.imageUrl(it, "poster", widthPx(420.dp)) }
                val fallback = api.imageUrl(hero, "thumb", widthPx(420.dp))
                RoxyImage(poster ?: fallback, title?.title ?: hero.title, modifier = Modifier.fillMaxSize(), placeholderTextSize = 18)
                Box(modifier = Modifier.matchParentSize().background(Brush.verticalGradient(0.45f to Color.Transparent, 1f to Color(0xF2000000))))
                Column(
                    modifier = Modifier.align(Alignment.BottomCenter).fillMaxWidth().padding(PhonePad),
                    horizontalAlignment = Alignment.CenterHorizontally,
                ) {
                    val name = title?.title ?: hero.seriesTitle ?: hero.title
                    Text(name, fontSize = 24.sp, lineHeight = 28.sp, fontWeight = FontWeight.ExtraBold, color = Color.White, textAlign = TextAlign.Center, maxLines = 2, overflow = TextOverflow.Ellipsis)
                    if (hero.isEpisode) {
                        Text(listOf(hero.episodeCode, hero.title).filter { it.isNotEmpty() }.joinToString("  "), fontSize = 13.sp, color = Color.White.copy(alpha = 0.8f), textAlign = TextAlign.Center, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 4.dp))
                    }
                    val genres = (title ?: hero).genres.take(3).joinToString("  ·  ")
                    if (genres.isNotEmpty()) Text(genres, fontSize = 13.sp, color = Color.White.copy(alpha = 0.85f), textAlign = TextAlign.Center, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 6.dp))
                    val resumable = (hero.userData?.positionMs ?: 0) > 0
                    Row(modifier = Modifier.fillMaxWidth().padding(top = 14.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        PlayButton(if (resumable) "Resume" else "Play", modifier = Modifier.weight(1f), onClick = { onPlay(hero) })
                        GreyButton("Info", modifier = Modifier.weight(1f), icon = Icons.Outlined.Info, onClick = { onInfo(hero) })
                    }
                }
            }
        }
    }
}
