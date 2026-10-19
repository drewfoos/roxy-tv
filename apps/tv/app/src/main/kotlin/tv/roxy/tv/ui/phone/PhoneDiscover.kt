package tv.roxy.tv.ui.phone

import androidx.compose.foundation.background
import androidx.compose.foundation.border
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
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.outlined.AddCircleOutline
import androidx.compose.material.icons.outlined.Schedule
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.LifecycleEventEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import coil3.compose.AsyncImage
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.DiscoverDetail
import tv.roxy.tv.api.DiscoverItem
import tv.roxy.tv.api.DiscoverRow
import tv.roxy.tv.api.RoxyApi
import tv.roxy.tv.ui.theme.RoxyColors

/*
 * Discover: Roxy's own request app. TMDB rows (trending, new anime this season, popular and
 * top-rated anime, anime movies, popular and upcoming movies, popular series) and a
 * search box, every poster marked when the library already has it or someone asked for
 * it; a title page with Watch (in the library) or Request. Requests go to Sonarr /
 * Radarr when the server has them configured and are kept as pending until then.
 */

data class DiscoverUiState(
    val rows: List<DiscoverRow>? = null,
    val requests: List<DiscoverItem> = emptyList(),
    val requestsEnabled: Map<String, Boolean> = emptyMap(),
    val error: String? = null,
    val query: String = "",
    val results: List<DiscoverItem>? = null, // null = not searching
    val searching: Boolean = false,
)

class DiscoverViewModel(private val api: RoxyApi) : ViewModel() {
    private val _state = MutableStateFlow(DiscoverUiState())
    val state: StateFlow<DiscoverUiState> = _state
    private var searchJob: Job? = null

    fun load() {
        viewModelScope.launch {
            runCatching { api.discover() }
                .onSuccess { d -> _state.value = _state.value.copy(rows = d.rows, requestsEnabled = d.requestsEnabled, error = null) }
                .onFailure { t -> Timber.w(t, "discover failed"); _state.value = _state.value.copy(error = t.message) }
            runCatching { api.discoverRequests() }.onSuccess { r -> _state.value = _state.value.copy(requests = r) }
        }
    }

    /** Type-to-search with a short debounce; an empty query returns to the rows. */
    fun search(q: String) {
        _state.value = _state.value.copy(query = q)
        searchJob?.cancel()
        if (q.isBlank()) { _state.value = _state.value.copy(results = null, searching = false); return }
        searchJob = viewModelScope.launch {
            delay(350)
            _state.value = _state.value.copy(searching = true)
            runCatching { api.discoverSearch(q) }
                .onSuccess { r -> if (_state.value.query == q) _state.value = _state.value.copy(results = r, searching = false) }
                .onFailure { t -> Timber.w(t, "discover search"); _state.value = _state.value.copy(searching = false) }
        }
    }
}

/** The Discover tab: search box, then "Your requests" and the TMDB rows (or search results as a grid). */
@Composable
fun PhoneDiscover(onOpen: (DiscoverItem) -> Unit, bottomInset: Dp) {
    val app = RoxyApp.get(LocalContext.current)
    val vm: DiscoverViewModel = viewModel { DiscoverViewModel(app.api) }
    val state by vm.state.collectAsStateWithLifecycle()
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { vm.load() }

    Column(modifier = Modifier.fillMaxSize().statusBarsPadding()) {
        Row(modifier = Modifier.fillMaxWidth().padding(start = PhonePad, end = PhonePad, top = 10.dp, bottom = 6.dp), verticalAlignment = Alignment.CenterVertically) {
            Text("Discover", fontSize = 24.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground)
        }
        SearchBox(query = state.query, onChange = vm::search, modifier = Modifier.padding(horizontal = PhonePad, vertical = 6.dp))
        when {
            state.results != null || state.searching -> {
                val results = state.results
                when {
                    results == null -> GridSkeleton(modifier = Modifier.padding(top = 8.dp))
                    results.isEmpty() -> Box(Modifier.fillMaxWidth().padding(32.dp), contentAlignment = Alignment.Center) { Text("Nothing on TMDB for \"${state.query}\".", color = RoxyColors.muted, textAlign = TextAlign.Center) }
                    else -> LazyVerticalGrid(
                        columns = GridCells.Fixed(3), modifier = Modifier.fillMaxSize(),
                        contentPadding = PaddingValues(start = PhonePad, end = PhonePad, top = 8.dp, bottom = bottomInset + 16.dp),
                        horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(12.dp),
                    ) {
                        items(results, key = { "${it.type}:${it.tmdbId}" }) { DiscoverCard(it, modifier = Modifier.fillMaxWidth(), onClick = { onOpen(it) }) }
                    }
                }
            }
            state.rows == null && state.error != null -> Column(modifier = Modifier.fillMaxWidth().padding(32.dp), horizontalAlignment = Alignment.CenterHorizontally) {
                Text("Discover is unavailable", fontSize = 18.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground)
                Text(state.error ?: "", color = RoxyColors.muted, fontSize = 13.sp, textAlign = TextAlign.Center, modifier = Modifier.padding(top = 8.dp, bottom = 16.dp))
                GreyButton("Try again", onClick = { vm.load() })
            }
            state.rows == null -> Column(modifier = Modifier.fillMaxSize().padding(bottom = bottomInset)) {
                RowSkeleton(modifier = Modifier.padding(top = 16.dp)); RowSkeleton(modifier = Modifier.padding(top = 22.dp)); RowSkeleton(modifier = Modifier.padding(top = 22.dp))
            }
            else -> LazyColumn(modifier = Modifier.fillMaxSize(), state = rememberLazyListState(), contentPadding = PaddingValues(top = 8.dp, bottom = bottomInset + 16.dp)) {
                if (state.requests.isNotEmpty()) {
                    item(key = "requests") { DiscoverRowView("Your requests", state.requests, onOpen = onOpen, modifier = Modifier.padding(top = 10.dp)) }
                }
                items(state.rows.orEmpty(), key = { it.key }) { row ->
                    DiscoverRowView(row.title, row.items, onOpen = onOpen, modifier = Modifier.padding(top = 22.dp))
                }
            }
        }
    }
}

/** Rounded dark search field with a clear button; TMDB search as you type. */
@Composable
private fun SearchBox(query: String, onChange: (String) -> Unit, modifier: Modifier = Modifier) {
    Row(
        modifier = modifier.fillMaxWidth().height(42.dp).clip(RoundedCornerShape(21.dp)).background(RoxyColors.surfaceHigh).padding(horizontal = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(Icons.Filled.Search, contentDescription = null, tint = RoxyColors.muted, modifier = Modifier.size(20.dp))
        Box(modifier = Modifier.weight(1f).padding(start = 10.dp)) {
            if (query.isEmpty()) Text("Search shows and movies on TMDB", color = RoxyColors.muted, fontSize = 14.sp)
            BasicTextField(
                value = query, onValueChange = onChange, singleLine = true,
                textStyle = TextStyle(color = RoxyColors.onBackground, fontSize = 14.sp),
                cursorBrush = SolidColor(RoxyColors.accent),
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
                modifier = Modifier.fillMaxWidth(),
            )
        }
        if (query.isNotEmpty()) Icon(Icons.Filled.Close, contentDescription = "Clear", tint = RoxyColors.muted, modifier = Modifier.size(20.dp).clickable { onChange("") })
    }
}

/** A titled row of Discover posters, three and a half across. */
@Composable
private fun DiscoverRowView(title: String, items: List<DiscoverItem>, onOpen: (DiscoverItem) -> Unit, modifier: Modifier = Modifier) {
    val screen = LocalConfiguration.current.screenWidthDp.dp
    val cardWidth = (screen - PhonePad - 8.dp * 3) / 3.5f
    Column(modifier = modifier.fillMaxWidth()) {
        Text(title, fontSize = 18.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(start = PhonePad, bottom = 10.dp))
        LazyRow(contentPadding = PaddingValues(horizontal = PhonePad), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            items(items, key = { "${it.type}:${it.tmdbId}" }) { DiscoverCard(it, modifier = Modifier.width(cardWidth), onClick = { onOpen(it) }) }
        }
    }
}

/** Poster with a corner badge (check = in your library, clock = requested), name and year beneath. */
@Composable
private fun DiscoverCard(item: DiscoverItem, modifier: Modifier = Modifier, onClick: () -> Unit) {
    Column(modifier = modifier) {
        Box(modifier = Modifier.fillMaxWidth().aspectRatio(2f / 3f).clip(RoundedCornerShape(6.dp)).background(RoxyColors.surfaceHigh).clickable(onClick = onClick)) {
            if (item.poster != null) AsyncImage(model = item.poster, contentDescription = item.title, contentScale = ContentScale.Crop, modifier = Modifier.fillMaxSize())
            else Text(item.title, color = RoxyColors.muted, fontSize = 11.sp, textAlign = TextAlign.Center, modifier = Modifier.align(Alignment.Center).padding(6.dp))
            StatusBadge(item.inLibrary, item.requestStatus, modifier = Modifier.align(Alignment.TopEnd).padding(5.dp))
        }
        Text(item.title, fontSize = 12.sp, lineHeight = 15.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground, maxLines = 2, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 6.dp))
        Text(listOfNotNull(item.year?.toString(), if (item.isAnime) "Anime" else if (item.isMovie) "Movie" else null).joinToString("  ·  "), fontSize = 11.sp, color = RoxyColors.muted, maxLines = 1)
    }
}

/** Small round badge: brand check when the library has it, clock while requested. */
@Composable
private fun StatusBadge(inLibrary: Boolean, requestStatus: String?, modifier: Modifier = Modifier) {
    val (icon, bg) = when {
        inLibrary -> Icons.Filled.Check to RoxyColors.accent
        requestStatus == "failed" -> Icons.Filled.Close to RoxyColors.error
        requestStatus != null -> Icons.Outlined.Schedule to Color(0xFF4B4B57)
        else -> return
    }
    Box(modifier = modifier.size(22.dp).clip(CircleShape).background(bg).border(1.5.dp, Color(0x66000000), CircleShape), contentAlignment = Alignment.Center) {
        Icon(icon, contentDescription = null, tint = if (inLibrary) OnBrand else Color.White, modifier = Modifier.size(14.dp))
    }
}

/**
 * The Discover title page: backdrop, name and facts from TMDB, then Watch when the
 * library has it, Request otherwise. A request shows its state (pending until Sonarr /
 * Radarr are connected, then sent, or failed with the reason) and can be withdrawn.
 */
@Composable
fun PhoneDiscoverDetail(type: String, tmdbId: Int, onBack: () -> Unit, onOpenSeries: (String, String) -> Unit, onOpenMovie: (String, String) -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    val scope = rememberCoroutineScope()
    var detail by remember { mutableStateOf<DiscoverDetail?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    var expanded by rememberSaveable { mutableStateOf(false) }

    suspend fun reload() {
        runCatching { app.api.discoverDetail(type, tmdbId) }
            .onSuccess { detail = it; error = null }
            .onFailure { t -> Timber.w(t, "discover detail"); error = t.message }
    }
    LaunchedEffect(type, tmdbId) { reload() }

    val d = detail
    if (d == null) {
        if (error != null) Column(modifier = Modifier.fillMaxSize().statusBarsPadding().padding(PhonePad)) { BackCircle(onBack); Text(error ?: "", color = RoxyColors.error, modifier = Modifier.padding(top = 16.dp)) }
        else DetailSkeleton(onBack)
        return
    }

    Box(modifier = Modifier.fillMaxSize()) {
        LazyColumn(modifier = Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 120.dp)) {
            item(key = "banner") {
                Box(modifier = Modifier.fillMaxWidth().height(300.dp)) {
                    AsyncImage(model = d.backdrop ?: d.poster, contentDescription = d.title, contentScale = ContentScale.Crop, modifier = Modifier.fillMaxSize().background(RoxyColors.surfaceHigh))
                    Box(modifier = Modifier.fillMaxSize().background(Brush.verticalGradient(0f to Color(0x66000000), 0.3f to Color.Transparent)))
                    Box(modifier = Modifier.fillMaxSize().background(Brush.verticalGradient(0.5f to Color.Transparent, 1f to RoxyColors.background)))
                }
            }
            item(key = "info") {
                Column(modifier = Modifier.fillMaxWidth().padding(horizontal = PhonePad), horizontalAlignment = Alignment.CenterHorizontally) {
                    Text(d.title, fontSize = 24.sp, lineHeight = 28.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, textAlign = TextAlign.Center)
                    d.tagline?.takeIf { it.isNotBlank() }?.let { Text(it, fontSize = 13.sp, color = RoxyColors.muted, textAlign = TextAlign.Center, modifier = Modifier.padding(top = 4.dp)) }
                    val tags = listOf(if (d.isAnime) "Anime" else if (d.isMovie) "Movie" else "Series", d.genres.take(3).joinToString(", ")).filter { it.isNotEmpty() }.joinToString("  •  ")
                    Text(tags, fontSize = 13.sp, color = Color.White.copy(alpha = 0.85f), textAlign = TextAlign.Center, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 10.dp))
                    d.rating?.takeIf { it > 0 }?.let {
                        Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.padding(top = 8.dp)) {
                            Text("★ %.1f".format(it), fontSize = 15.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.accent)
                            Text("  TMDB", fontSize = 12.sp, color = RoxyColors.muted)
                        }
                    }
                    val facts = listOfNotNull(
                        d.year?.toString(),
                        d.seasons.takeIf { it > 0 }?.let { if (it == 1) "1 Season" else "$it Seasons" },
                        d.episodes.takeIf { it > 0 && !d.isMovie }?.let { "$it Episodes" },
                        d.runtimeMinutes.takeIf { it > 0 }?.let { if (d.isMovie) "${it / 60}h ${it % 60}m".removePrefix("0h ") else "${it}m" },
                        d.status?.takeIf { it.isNotBlank() },
                        d.networks.firstOrNull(),
                    ).joinToString("  ·  ")
                    if (facts.isNotEmpty()) Text(facts, fontSize = 12.sp, color = RoxyColors.muted, textAlign = TextAlign.Center, modifier = Modifier.padding(top = 6.dp))
                    d.overview?.takeIf { it.isNotBlank() }?.let {
                        Text(
                            it, fontSize = 14.sp, lineHeight = 20.sp, color = RoxyColors.onBackground.copy(alpha = 0.9f),
                            maxLines = if (expanded) Int.MAX_VALUE else 5, overflow = TextOverflow.Ellipsis,
                            modifier = Modifier.fillMaxWidth().padding(top = 16.dp).clickable { expanded = !expanded },
                        )
                    }
                    d.request?.let { r ->
                        val line = when (r.status) {
                            "sent" -> "Requested by ${r.userName.ifEmpty { "someone" }}  ·  sent to ${if (d.isMovie) "Radarr" else "Sonarr"}"
                            "failed" -> "Request failed  ·  ${r.error ?: "unknown error"}"
                            else -> "Requested by ${r.userName.ifEmpty { "someone" }}  ·  waiting for ${if (d.isMovie) "Radarr" else "Sonarr"}"
                        }
                        Text(line, fontSize = 12.sp, color = if (r.status == "failed") RoxyColors.error else RoxyColors.muted, textAlign = TextAlign.Center, modifier = Modifier.padding(top = 16.dp))
                    }
                    if (!d.inLibrary && !d.requestsEnabled) {
                        Text(
                            "${if (d.isMovie) "Radarr" else "Sonarr"} isn't connected to Roxy Server yet. Requests are kept here and sent once it is.",
                            fontSize = 12.sp, lineHeight = 16.sp, color = RoxyColors.muted, textAlign = TextAlign.Center, modifier = Modifier.padding(top = 12.dp),
                        )
                    }
                }
            }
        }
        Box(modifier = Modifier.fillMaxWidth().statusBarsPadding().padding(12.dp)) { BackCircle(onBack) }
        Box(
            modifier = Modifier.align(Alignment.BottomCenter).fillMaxWidth()
                .background(Brush.verticalGradient(0f to Color.Transparent, 0.35f to RoxyColors.background.copy(alpha = 0.96f), 1f to RoxyColors.background))
                .navigationBarsPadding().padding(start = PhonePad, end = PhonePad, top = 24.dp, bottom = 12.dp),
        ) {
            when {
                d.inLibrary -> PlayButton("Watch", modifier = Modifier.fillMaxWidth(), height = 44.dp, pill = true, container = RoxyColors.accent, content = OnBrand, onClick = {
                    val id = d.libraryItemId ?: return@PlayButton
                    if (d.isMovie) onOpenMovie(id, d.title) else onOpenSeries(id, d.title)
                })
                d.request != null && d.request.status != "failed" -> Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                    PlayButton("Requested", modifier = Modifier.weight(1f), icon = Icons.Filled.Check, height = 44.dp, pill = true, container = ButtonGrey, content = Color.White, onClick = {})
                    GreyButton("Withdraw", modifier = Modifier.height(44.dp), onClick = {
                        if (busy) return@GreyButton
                        busy = true
                        scope.launch { runCatching { app.api.deleteRequest(d.request.id) }.onFailure { Timber.w(it, "withdraw") }; reload(); busy = false }
                    })
                }
                else -> PlayButton(
                    if (busy) "Requesting…" else if (d.request?.status == "failed") "Request again" else "Request",
                    modifier = Modifier.fillMaxWidth(), icon = Icons.Outlined.AddCircleOutline, height = 44.dp, pill = true, container = RoxyColors.accent, content = OnBrand,
                    onClick = {
                        if (busy) return@PlayButton
                        busy = true
                        scope.launch {
                            runCatching { app.api.discoverRequest(d.type, d.tmdbId) }.onFailure { t -> Timber.w(t, "request failed"); error = t.message }
                            reload(); busy = false
                        }
                    },
                )
            }
        }
    }
}

@Suppress("unused")
private val playIcon = Icons.Filled.PlayArrow
