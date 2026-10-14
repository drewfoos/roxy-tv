package tv.roxy.tv.ui.phone

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Search
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelStoreOwner
import androidx.lifecycle.compose.LifecycleEventEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.Item
import tv.roxy.tv.api.RoxyApi
import tv.roxy.tv.ui.theme.RoxyColors

data class LibraryUiState(val items: List<Item>? = null, val error: String? = null)

/**
 * The whole library's series and movies in one list (sorted by title), shared by Browse,
 * Search and Watchlist so the three tabs cost one request between them. Reloaded on
 * resume only when it has been more than a minute, not on every tab switch.
 */
class LibraryViewModel(private val api: RoxyApi, private val favoritesOnly: Boolean = false) : ViewModel() {
    private val _state = MutableStateFlow(LibraryUiState())
    val state: StateFlow<LibraryUiState> = _state
    private var loadedAt = 0L

    fun load(force: Boolean = false) {
        val now = System.currentTimeMillis()
        if (!force && _state.value.items != null && now - loadedAt < 60_000) return
        viewModelScope.launch {
            runCatching {
                if (favoritesOnly) api.favorites().items
                else api.items(type = "series,movie", recursive = true, sort = "title", limit = 1000).items
            }
                .onSuccess { loadedAt = System.currentTimeMillis(); _state.value = LibraryUiState(items = it) }
                .onFailure { t -> Timber.e(t, "library load failed"); _state.value = _state.value.copy(error = t.message) }
        }
    }
}

/** Browse tab: Series / Movies toggle over a three-column poster grid, search in the corner. */
@Composable
fun PhoneBrowse(onOpenSeries: (Item) -> Unit, onOpenMovie: (Item) -> Unit, onSearch: () -> Unit, bottomInset: Dp) {
    val vm = libraryViewModel()
    val state by vm.state.collectAsStateWithLifecycle()
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { vm.load() }
    var movies by rememberSaveable { mutableStateOf(false) }

    val all = state.items
    val shown = all?.filter { if (movies) it.isMovie else it.isSeries }
    Column(modifier = Modifier.fillMaxSize().statusBarsPadding()) {
        Row(modifier = Modifier.fillMaxWidth().padding(horizontal = PhonePad, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
            Text("Browse", fontSize = 24.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground)
            Spacer(Modifier.weight(1f))
            Icon(Icons.Filled.Search, contentDescription = "Search", tint = Color.White, modifier = Modifier.size(26.dp).clickable(onClick = onSearch))
        }
        Row(modifier = Modifier.padding(horizontal = PhonePad, vertical = 4.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Chip("Series", selected = !movies, onClick = { movies = false })
            Chip("Movies", selected = movies, onClick = { movies = true })
        }
        PosterGrid(items = shown, error = state.error, emptyText = "Nothing here yet.", bottomInset = bottomInset, onOpen = { if (it.isMovie) onOpenMovie(it) else onOpenSeries(it) })
    }
}

/** Watchlist tab: the user's favourites. */
@Composable
fun PhoneWatchlist(onOpenSeries: (Item) -> Unit, onOpenMovie: (Item) -> Unit, bottomInset: Dp) {
    val app = RoxyApp.get(LocalContext.current)
    val vm: LibraryViewModel = viewModel(key = "watchlist") { LibraryViewModel(app.api, favoritesOnly = true) }
    val state by vm.state.collectAsStateWithLifecycle()
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { vm.load(force = true) } // a + on a title page changes it
    Column(modifier = Modifier.fillMaxSize().statusBarsPadding()) {
        Text("Watchlist", fontSize = 24.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(horizontal = PhonePad, vertical = 10.dp))
        PosterGrid(
            items = state.items, error = state.error,
            emptyText = "Titles you add with \"My List\" on their page show up here.",
            bottomInset = bottomInset, onOpen = { if (it.isMovie) onOpenMovie(it) else onOpenSeries(it) },
        )
    }
}

/** Search: type-to-filter over the library's titles. */
@Composable
fun PhoneSearch(onBack: () -> Unit, onOpenSeries: (Item) -> Unit, onOpenMovie: (Item) -> Unit) {
    val vm = libraryViewModel()
    val state by vm.state.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { vm.load() }
    var query by rememberSaveable { mutableStateOf("") }
    val focus = remember { FocusRequester() }
    LaunchedEffect(Unit) { focus.requestFocus() }

    val q = query.trim()
    val results = state.items?.let { all ->
        if (q.isEmpty()) emptyList() else all.filter { it.title.contains(q, ignoreCase = true) || it.genres.any { g -> g.contains(q, ignoreCase = true) } }
    }
    Column(modifier = Modifier.fillMaxSize().statusBarsPadding().imePadding()) {
        Row(modifier = Modifier.fillMaxWidth().padding(horizontal = PhonePad, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
            BackCircle(onBack)
            Box(
                modifier = Modifier.weight(1f).padding(start = 12.dp).clip(RoundedCornerShape(8.dp)).background(ChipGrey).padding(horizontal = 12.dp, vertical = 10.dp),
                contentAlignment = Alignment.CenterStart,
            ) {
                if (query.isEmpty()) Text("Search series, movies, genres", color = RoxyColors.muted, fontSize = 15.sp)
                BasicTextField(
                    value = query, onValueChange = { query = it }, singleLine = true,
                    textStyle = TextStyle(color = Color.White, fontSize = 15.sp), cursorBrush = SolidColor(Color.White),
                    keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
                    modifier = Modifier.fillMaxWidth().focusRequester(focus),
                )
            }
        }
        when {
            state.items == null && state.error == null -> GridSkeleton(modifier = Modifier.padding(top = 8.dp), rows = 2)
            q.isEmpty() -> Text("Start typing to search your library.", color = RoxyColors.muted, modifier = Modifier.padding(PhonePad))
            else -> PosterGrid(items = results, error = state.error, emptyText = "No matches for \"$q\".", bottomInset = 16.dp, onOpen = { if (it.isMovie) onOpenMovie(it) else onOpenSeries(it) })
        }
    }
}

/** Three-column poster grid with skeleton, error and empty states. */
@Composable
private fun PosterGrid(items: List<Item>?, error: String?, emptyText: String, bottomInset: Dp, onOpen: (Item) -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    when {
        items == null && error != null -> Text(error, color = RoxyColors.error, modifier = Modifier.padding(PhonePad))
        items == null -> GridSkeleton(modifier = Modifier.padding(top = 8.dp))
        items.isEmpty() -> Box(Modifier.fillMaxWidth().padding(32.dp), contentAlignment = Alignment.Center) {
            Text(emptyText, color = RoxyColors.muted, textAlign = TextAlign.Center, lineHeight = 20.sp)
        }
        else -> LazyVerticalGrid(
            columns = GridCells.Fixed(3),
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(start = PhonePad, end = PhonePad, top = 8.dp, bottom = bottomInset + 16.dp),
            horizontalArrangement = Arrangement.spacedBy(8.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            items(items, key = { it.id }) { PosterTile(it, app.api, modifier = Modifier.fillMaxWidth(), onClick = { onOpen(it) }) }
        }
    }
}

/** The library list is one per app (activity-scoped), so Browse and Search share a single request. */
@Composable
private fun libraryViewModel(): LibraryViewModel {
    val context = LocalContext.current
    val app = RoxyApp.get(context)
    val owner = context as ViewModelStoreOwner
    return viewModel(viewModelStoreOwner = owner, key = "library") { LibraryViewModel(app.api) }
}
