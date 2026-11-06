package tv.roxy.tv.ui.tv

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.itemsIndexed
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Done
import androidx.compose.material.icons.outlined.Person
import androidx.compose.material.icons.outlined.Search
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
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.scale
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusProperties
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import coil3.compose.AsyncImage
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import tv.roxy.tv.api.Character
import tv.roxy.tv.api.Profile
import tv.roxy.tv.api.RoxyApi
import tv.roxy.tv.ui.components.widthPx
import tv.roxy.tv.ui.profiles.AvatarCircle
import tv.roxy.tv.ui.theme.RoxyColors
import tv.roxy.tv.ui.components.tryFocus
import tv.roxy.tv.ui.components.retryEachFrame

/*
 * Edit or create a profile on the TV. The phone's editor (a scrolling column of small
 * touch targets) had no focus look at all on a remote. Here: the preview, the name and the
 * actions at the left; at the right a character search and a grid of big round avatars.
 * The focused avatar gets a thick white ring and grows; the chosen one an accent ring and
 * a tick. Every move between the two sides is wired, so focus never drops.
 */

private const val AvatarColumns = 4
private val AvatarSize = 96.dp
private val LeftWidth = 300.dp

@Composable
fun TvProfileEditor(api: RoxyApi, profile: Profile?, onSaved: (Profile) -> Unit, onDeleted: () -> Unit, onCancel: () -> Unit) {
    val scope = rememberCoroutineScope()
    var name by rememberSaveable { mutableStateOf(profile?.name ?: "") }
    var picked by remember { mutableStateOf<Character?>(null) }
    var suggested by remember { mutableStateOf<List<Character>?>(null) }
    var query by rememberSaveable { mutableStateOf("") }
    var results by remember { mutableStateOf<List<Character>?>(null) }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var confirmDelete by remember { mutableStateOf(false) }
    BackHandler(onBack = onCancel)

    LaunchedEffect(Unit) { suggested = runCatching { api.avatarSuggested() }.getOrElse { emptyList() } }
    LaunchedEffect(query) {
        val q = query.trim()
        if (q.length < 2) { results = null; return@LaunchedEffect }
        delay(400) // typing on a remote is slow: one AniList search per pause, not per letter
        results = runCatching { api.avatarSearch(q) }.getOrElse { emptyList() }
    }

    val nameFocus = remember { FocusRequester() }
    val saveFocus = remember { FocusRequester() }
    val searchFocus = remember { FocusRequester() }
    // One requester per avatar shown, so Down from the search field can aim at one that is on
    // screen (a fixed "first avatar" was a dead end once the grid had scrolled).
    val tileFocus = remember { HashMap<Int, FocusRequester>() }
    fun tile(id: Int) = tileFocus.getOrPut(id) { FocusRequester() }
    val gridState = androidx.compose.foundation.lazy.grid.rememberLazyGridState()
    fun firstVisibleTile(): FocusRequester? = (gridState.layoutInfo.visibleItemsInfo.firstOrNull()?.key as? Int)?.let { tile(it) }
    LaunchedEffect(Unit) { retryEachFrame("profile editor: name") { nameFocus.tryFocus() } }

    fun save() {
        if (name.isBlank() || busy) { if (name.isBlank()) error = "Give the profile a name."; return }
        busy = true
        error = null
        scope.launch {
            runCatching {
                if (profile == null) api.createProfile(name.trim(), picked?.image, picked?.name)
                else api.updateProfile(profile.id, name.trim().takeIf { it != profile.name }, picked?.image, picked?.name)
            }.onSuccess { onSaved(it) }.onFailure { t -> error = t.message ?: "Could not save"; busy = false }
        }
    }

    val shown = results ?: suggested
    Row(modifier = Modifier.fillMaxSize().background(RoxyColors.background).padding(start = 64.dp, top = 44.dp, end = 48.dp)) {
        // Left: what the profile will look like, its name, and the actions.
        Column(modifier = Modifier.width(LeftWidth).fillMaxHeight()) {
            Text(if (profile == null) "New profile" else "Edit profile", color = RoxyColors.onBackground, fontSize = 28.sp, fontWeight = FontWeight.Bold)
            Spacer(Modifier.height(24.dp))
            val preview = picked?.image ?: profile?.takeIf { it.hasAvatar }?.let { api.profileAvatarUrl(it.id, widthPx(140.dp) * 2, it.avatarKey) }
            AvatarCircle(name = name.ifBlank { "?" }, imageUrl = preview, size = 140.dp)
            Text(
                picked?.let { it.name + if (it.series.isNotEmpty()) "  ·  ${it.series}" else "" } ?: " ",
                color = RoxyColors.muted, fontSize = 13.sp, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 10.dp).width(LeftWidth - 20.dp),
            )
            Spacer(Modifier.height(18.dp))
            TvTextField(
                name, { name = it.take(24); error = null }, placeholder = "Name", icon = Icons.Outlined.Person, imeAction = ImeAction.Done,
                modifier = Modifier.width(LeftWidth - 20.dp).focusRequester(nameFocus).focusProperties { right = searchFocus; down = saveFocus },
            )
            Spacer(Modifier.height(22.dp))
            val right = Modifier.focusProperties { this.right = searchFocus }
            TvListRow(
                label = if (busy) "Saving…" else "Save", icon = Icons.Outlined.Done, selected = true, onClick = { save() },
                modifier = Modifier.width(LeftWidth - 20.dp).focusRequester(saveFocus).then(right).focusProperties { up = nameFocus },
            )
            Spacer(Modifier.height(4.dp))
            TvListRow(label = "Cancel", icon = Icons.Outlined.Close, onClick = onCancel, modifier = Modifier.width(LeftWidth - 20.dp).then(right))
            if (profile != null && !profile.isDefault) {
                Spacer(Modifier.height(4.dp))
                TvListRow(
                    label = if (confirmDelete) "Press again to delete" else "Delete profile", icon = Icons.Outlined.Delete,
                    onClick = {
                        if (!confirmDelete) confirmDelete = true
                        else scope.launch { runCatching { api.deleteProfile(profile.id) }.onSuccess { onDeleted() }.onFailure { t -> error = t.message } }
                    },
                    modifier = Modifier.width(LeftWidth - 20.dp).then(right),
                )
            }
            error?.let { Text(it, color = RoxyColors.error, fontSize = 14.sp, modifier = Modifier.padding(top = 12.dp)) }
        }
        // Right: search, then the avatars to pick from.
        Column(modifier = Modifier.weight(1f).fillMaxHeight().padding(start = 32.dp)) {
            TvTextField(
                query, { query = it }, placeholder = "Search any anime character, e.g. Roxy Migurdia", icon = Icons.Outlined.Search, imeAction = ImeAction.Search,
                modifier = Modifier.fillMaxWidth().padding(end = 16.dp).focusRequester(searchFocus).focusProperties { left = nameFocus; firstVisibleTile()?.let { down = it } },
            )
            Text(
                if (results != null) "RESULTS" else "FROM YOUR LIBRARY", color = RoxyColors.muted, fontSize = 13.sp, letterSpacing = 1.sp, fontWeight = FontWeight.Medium,
                modifier = Modifier.padding(top = 20.dp, bottom = 6.dp),
            )
            when {
                shown == null -> Row(horizontalArrangement = Arrangement.spacedBy(28.dp), modifier = Modifier.padding(top = 10.dp)) {
                    val pulse = rememberSkeletonPulse()
                    repeat(AvatarColumns) { SkeletonBlock(pulse, Modifier.size(AvatarSize), radius = AvatarSize / 2) }
                }
                shown.isEmpty() -> Text(
                    if (results != null) "Nobody by that name on AniList." else "No characters from the library yet. Search for one above.",
                    color = RoxyColors.muted, fontSize = 16.sp, modifier = Modifier.padding(top = 10.dp),
                )
                else -> LazyVerticalGrid(
                    columns = GridCells.Fixed(AvatarColumns),
                    state = gridState,
                    contentPadding = PaddingValues(top = 12.dp, bottom = 32.dp, end = 16.dp),
                    horizontalArrangement = Arrangement.spacedBy(20.dp),
                    verticalArrangement = Arrangement.spacedBy(20.dp),
                    modifier = Modifier.fillMaxSize(),
                ) {
                    itemsIndexed(shown, key = { _, c -> c.id }) { i, c ->
                        AvatarTile(
                            c, chosen = picked?.id == c.id, onClick = { picked = c },
                            modifier = Modifier
                                .focusRequester(tile(c.id))
                                .focusProperties {
                                    if (i < AvatarColumns) up = searchFocus
                                    if (i % AvatarColumns == 0) left = saveFocus
                                },
                        )
                    }
                }
            }
        }
    }
}

/** One character: a big round picture and the name. Focus: white ring and a little larger; chosen: accent ring and a tick. */
@Composable
private fun AvatarTile(c: Character, chosen: Boolean, onClick: () -> Unit, modifier: Modifier = Modifier) {
    TvFocusable(onClick = onClick, modifier = modifier) { focused ->
        Column(horizontalAlignment = Alignment.CenterHorizontally, modifier = Modifier.fillMaxWidth().padding(vertical = 6.dp)) {
            Box(modifier = Modifier.size(AvatarSize + 12.dp).scale(if (focused) 1.08f else 1f), contentAlignment = Alignment.Center) {
                // Focus is the blue ring (and a little larger); the chosen one keeps a thin white ring and the tick.
                when {
                    focused -> Box(modifier = Modifier.size(AvatarSize + 12.dp).border(4.dp, RoxyColors.accent, CircleShape))
                    chosen -> Box(modifier = Modifier.size(AvatarSize + 12.dp).border(2.dp, Color.White, CircleShape))
                }
                AsyncImage(
                    model = c.image, contentDescription = c.name, contentScale = ContentScale.Crop,
                    modifier = Modifier.size(AvatarSize).clip(CircleShape).background(RoxyColors.surfaceHigh),
                )
                if (chosen) {
                    Box(modifier = Modifier.align(Alignment.BottomEnd).size(30.dp).clip(CircleShape).background(RoxyColors.accent), contentAlignment = Alignment.Center) {
                        Icon(Icons.Filled.Check, contentDescription = "Chosen", tint = OnAccent, modifier = Modifier.size(20.dp))
                    }
                }
            }
            Text(
                c.name, color = if (focused || chosen) Color.White else RoxyColors.muted,
                fontSize = 14.sp, fontWeight = if (focused || chosen) FontWeight.SemiBold else FontWeight.Normal,
                maxLines = 2, lineHeight = 17.sp, textAlign = TextAlign.Center, overflow = TextOverflow.Ellipsis,
                modifier = Modifier.padding(top = 8.dp, start = 4.dp, end = 4.dp),
            )
        }
    }
}
