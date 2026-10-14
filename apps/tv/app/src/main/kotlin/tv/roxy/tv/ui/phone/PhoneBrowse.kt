package tv.roxy.tv.ui.phone

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.ViewModel
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

data class BrowseUiState(val items: List<Item>? = null, val error: String? = null)

class BrowseViewModel(private val api: RoxyApi, private val type: String) : ViewModel() {
    private val _state = MutableStateFlow(BrowseUiState())
    val state: StateFlow<BrowseUiState> = _state

    fun load() {
        viewModelScope.launch {
            runCatching { api.items(type = type, recursive = type == "movie", sort = "title", limit = 500).items }
                .onSuccess { _state.value = BrowseUiState(items = it) }
                .onFailure { t -> Timber.e(t, "browse %s failed", type); _state.value = _state.value.copy(error = t.message) }
        }
    }
}

/** The "see all" view for a library: a three-column poster grid under a title. */
@Composable
fun PhoneBrowse(type: String, title: String, onOpen: (Item) -> Unit, bottomInset: Dp) {
    val app = RoxyApp.get(LocalContext.current)
    val vm: BrowseViewModel = viewModel(key = "browse:$type") { BrowseViewModel(app.api, type) }
    val state by vm.state.collectAsStateWithLifecycle()
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { vm.load() }

    val items = state.items
    LazyVerticalGrid(
        columns = GridCells.Fixed(3),
        modifier = Modifier.fillMaxSize().statusBarsPadding(),
        contentPadding = PaddingValues(start = PhonePad, end = PhonePad, top = 8.dp, bottom = bottomInset + 16.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        item(span = { GridItemSpan(maxLineSpan) }) {
            Text(title, fontSize = 24.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(vertical = 12.dp))
        }
        when {
            items == null && state.error != null -> item(span = { GridItemSpan(maxLineSpan) }) { Text(state.error ?: "", color = RoxyColors.error) }
            items == null -> item(span = { GridItemSpan(maxLineSpan) }) { Text("Loading...", color = RoxyColors.muted) }
            items.isEmpty() -> item(span = { GridItemSpan(maxLineSpan) }) {
                Box(Modifier.fillMaxWidth().padding(32.dp), contentAlignment = Alignment.Center) { Text("Nothing here yet.", color = RoxyColors.muted, textAlign = TextAlign.Center) }
            }
            else -> items(items, key = { it.id }) { PosterTile(it, app.api, modifier = Modifier.fillMaxWidth(), onClick = { onOpen(it) }) }
        }
    }
}
