package tv.roxy.tv.ui.profiles

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Edit
import androidx.compose.material.icons.filled.Search
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
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import coil3.compose.AsyncImage
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.Character
import tv.roxy.tv.api.Profile
import tv.roxy.tv.api.RoxyApi
import tv.roxy.tv.ui.components.widthPx
import tv.roxy.tv.ui.phone.ChipGrey
import tv.roxy.tv.ui.phone.OnBrand
import tv.roxy.tv.ui.phone.PhoneTheme
import tv.roxy.tv.ui.phone.PlayButton
import tv.roxy.tv.ui.phone.WitchHat
import tv.roxy.tv.ui.theme.RoxyColors

/**
 * "Who's watching?" (Netflix): the account's profiles as big avatars, an Add tile, and a
 * Manage toggle that turns taps into edits. Picking one stores it in the session and the
 * app continues; everything watched from then on is that profile's.
 *
 * [editOnly] opens the editor for one profile straight away (Account â†’ Edit profile).
 */
@Composable
fun ProfilePicker(editOnly: Profile? = null, onDone: () -> Unit = {}) {
    val app = RoxyApp.get(LocalContext.current)
    val scope = rememberCoroutineScope()
    var profiles by remember { mutableStateOf<List<Profile>?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var managing by rememberSaveable { mutableStateOf(false) }
    var editing by remember { mutableStateOf<Profile?>(editOnly) }
    var creating by remember { mutableStateOf(false) }

    suspend fun reload() {
        runCatching { app.api.profiles().profiles }
            .onSuccess { profiles = it; error = null }
            .onFailure { t -> Timber.w(t, "profiles failed"); error = t.message }
    }
    LaunchedEffect(Unit) { reload() }

    PhoneTheme {
        Box(modifier = Modifier.fillMaxSize().background(RoxyColors.background)) {
            when {
                creating || editing != null -> ProfileEditor(
                    api = app.api,
                    profile = editing,
                    onSaved = { saved ->
                        scope.launch {
                            // Keep the session in step when the active profile was edited.
                            if (app.prefs.session.value.profileId == saved.id) app.prefs.selectProfile(saved.id, saved.name, saved.hasAvatar)
                            reload()
                            creating = false; editing = null
                            if (editOnly != null) onDone()
                        }
                    },
                    onDeleted = {
                        scope.launch {
                            if (app.prefs.session.value.profileId == editing?.id) app.prefs.clearProfile()
                            reload(); creating = false; editing = null
                            if (editOnly != null) onDone()
                        }
                    },
                    onCancel = { creating = false; editing = null; if (editOnly != null) onDone() },
                )
                else -> PickerGrid(
                    api = app.api, profiles = profiles, error = error, managing = managing,
                    onToggleManage = { managing = !managing },
                    onPick = { p ->
                        if (managing) editing = p
                        else scope.launch { app.prefs.selectProfile(p.id, p.name, p.hasAvatar); onDone() }
                    },
                    onAdd = { creating = true },
                    onRetry = { scope.launch { reload() } },
                )
            }
        }
    }
}

@Composable
private fun PickerGrid(
    api: RoxyApi,
    profiles: List<Profile>?,
    error: String?,
    managing: Boolean,
    onToggleManage: () -> Unit,
    onPick: (Profile) -> Unit,
    onAdd: () -> Unit,
    onRetry: () -> Unit,
) {
    Column(modifier = Modifier.fillMaxSize().statusBarsPadding().navigationBarsPadding().padding(24.dp), horizontalAlignment = Alignment.CenterHorizontally) {
        WitchHat(size = 44.dp, modifier = Modifier.padding(top = 24.dp))
        Text("Who's watching?", fontSize = 26.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(top = 20.dp, bottom = 28.dp))
        when {
            profiles == null && error != null -> {
                Text(error, color = RoxyColors.error, textAlign = TextAlign.Center)
                PlayButton("Try again", icon = Icons.Filled.Search, modifier = Modifier.padding(top = 16.dp), onClick = onRetry)
            }
            profiles == null -> Row(horizontalArrangement = Arrangement.spacedBy(20.dp)) {
                repeat(2) { Box(Modifier.size(96.dp).clip(CircleShape).background(RoxyColors.surfaceHigh)) }
            }
            else -> {
                LazyVerticalGrid(
                    columns = GridCells.Fixed(3),
                    modifier = Modifier.widthIn(max = 360.dp).weight(1f, fill = false),
                    horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.CenterHorizontally),
                    verticalArrangement = Arrangement.spacedBy(16.dp),
                    contentPadding = PaddingValues(bottom = 16.dp),
                ) {
                    items(profiles, key = { it.id }) { p ->
                        Column(horizontalAlignment = Alignment.CenterHorizontally, modifier = Modifier.clickable { onPick(p) }.padding(4.dp)) {
                            Box {
                                ProfileAvatar(p, api, size = 88.dp)
                                if (managing) {
                                    Box(modifier = Modifier.matchParentSize().clip(CircleShape).background(Color(0x88000000)), contentAlignment = Alignment.Center) {
                                        Icon(Icons.Filled.Edit, contentDescription = "Edit", tint = Color.White, modifier = Modifier.size(32.dp))
                                    }
                                }
                            }
                            Text(p.name, color = RoxyColors.onBackground, fontSize = 15.sp, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 10.dp))
                        }
                    }
                    if (profiles.size < 6) {
                        item(key = "add") {
                            Column(horizontalAlignment = Alignment.CenterHorizontally, modifier = Modifier.clickable(onClick = onAdd).padding(4.dp)) {
                                Box(modifier = Modifier.size(88.dp).clip(CircleShape).border(2.dp, RoxyColors.muted, CircleShape), contentAlignment = Alignment.Center) {
                                    Icon(Icons.Filled.Add, contentDescription = "Add profile", tint = RoxyColors.muted, modifier = Modifier.size(40.dp))
                                }
                                Text("Add profile", color = RoxyColors.muted, fontSize = 15.sp, modifier = Modifier.padding(top = 10.dp))
                            }
                        }
                    }
                }
                Box(
                    modifier = Modifier.padding(top = 20.dp).clip(RoundedCornerShape(4.dp)).border(1.dp, RoxyColors.muted, RoundedCornerShape(4.dp)).clickable(onClick = onToggleManage).padding(horizontal = 22.dp, vertical = 10.dp),
                ) {
                    Text(if (managing) "Done" else "Manage profiles", color = if (managing) Color.White else RoxyColors.muted, fontSize = 14.sp, fontWeight = FontWeight.SemiBold)
                }
            }
        }
    }
}

/** Round avatar: the cached character picture, or the initial on the brand blue. */
@Composable
fun ProfileAvatar(profile: Profile, api: RoxyApi, size: Dp, version: Int = 0, modifier: Modifier = Modifier) {
    AvatarCircle(name = profile.name, imageUrl = if (profile.hasAvatar) api.profileAvatarUrl(profile.id, widthPx(size) * 2, version) else null, size = size, modifier = modifier)
}

@Composable
fun AvatarCircle(name: String, imageUrl: String?, size: Dp, modifier: Modifier = Modifier) {
    Box(modifier = modifier.size(size).clip(CircleShape).background(RoxyColors.accent), contentAlignment = Alignment.Center) {
        Text(name.firstOrNull()?.uppercaseChar()?.toString() ?: "?", color = OnBrand, fontSize = (size.value * 0.45f).sp, fontWeight = FontWeight.Bold)
        if (imageUrl != null) {
            AsyncImage(model = imageUrl, contentDescription = name, contentScale = ContentScale.Crop, modifier = Modifier.fillMaxSize())
        }
    }
}

/**
 * Create or edit one profile: name, then the avatar from "From your library" (the main
 * cast of the shows on the server) or a character search on AniList.
 */
@Composable
private fun ProfileEditor(api: RoxyApi, profile: Profile?, onSaved: (Profile) -> Unit, onDeleted: () -> Unit, onCancel: () -> Unit) {
    val scope = rememberCoroutineScope()
    var name by rememberSaveable { mutableStateOf(profile?.name ?: "") }
    var picked by remember { mutableStateOf<Character?>(null) } // new choice; null = keep
    var suggested by remember { mutableStateOf<List<Character>?>(null) }
    var query by rememberSaveable { mutableStateOf("") }
    var results by remember { mutableStateOf<List<Character>>(emptyList()) }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    BackHandler(onBack = onCancel)

    LaunchedEffect(Unit) { suggested = runCatching { api.avatarSuggested() }.getOrElse { emptyList() } }
    LaunchedEffect(query) {
        val q = query.trim()
        if (q.length < 2) { results = emptyList(); return@LaunchedEffect }
        delay(350) // type-ahead debounce so AniList is not hit per keystroke
        results = runCatching { api.avatarSearch(q) }.getOrElse { emptyList() }
    }

    fun save() {
        if (name.isBlank() || busy) return
        busy = true
        scope.launch {
            runCatching {
                if (profile == null) api.createProfile(name.trim(), picked?.image, picked?.name)
                else api.updateProfile(profile.id, name.trim().takeIf { it != profile.name }, picked?.image, picked?.name)
            }.onSuccess { onSaved(it) }.onFailure { t -> error = t.message; busy = false }
        }
    }

    Column(modifier = Modifier.fillMaxSize().statusBarsPadding().navigationBarsPadding().imePadding().verticalScroll(rememberScrollState()).padding(horizontal = 20.dp)) {
        Row(modifier = Modifier.fillMaxWidth().padding(vertical = 14.dp), verticalAlignment = Alignment.CenterVertically) {
            Text("Cancel", color = RoxyColors.muted, fontSize = 15.sp, modifier = Modifier.clickable(onClick = onCancel))
            Spacer(Modifier.weight(1f))
            Text(if (profile == null) "New profile" else "Edit profile", color = RoxyColors.onBackground, fontSize = 17.sp, fontWeight = FontWeight.Bold)
            Spacer(Modifier.weight(1f))
            Text(if (busy) "Savingâ€¦" else "Save", color = if (name.isBlank()) RoxyColors.muted else RoxyColors.accent, fontSize = 15.sp, fontWeight = FontWeight.SemiBold, modifier = Modifier.clickable { save() })
        }
        // Preview and name.
        Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.padding(vertical = 12.dp)) {
            val preview = picked?.image ?: profile?.takeIf { it.hasAvatar }?.let { api.profileAvatarUrl(it.id, widthPx(72.dp) * 2) }
            AvatarCircle(name = name.ifBlank { "?" }, imageUrl = preview, size = 72.dp)
            Box(modifier = Modifier.padding(start = 16.dp).weight(1f).clip(RoundedCornerShape(8.dp)).background(ChipGrey).padding(horizontal = 14.dp, vertical = 12.dp)) {
                if (name.isEmpty()) Text("Name", color = RoxyColors.muted, fontSize = 16.sp)
                BasicTextField(
                    value = name, onValueChange = { name = it.take(24) }, singleLine = true,
                    textStyle = TextStyle(color = Color.White, fontSize = 16.sp), cursorBrush = SolidColor(Color.White),
                    keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
                    modifier = Modifier.fillMaxWidth(),
                )
            }
        }
        error?.let { Text(it, color = RoxyColors.error, fontSize = 13.sp, modifier = Modifier.padding(bottom = 8.dp)) }
        picked?.let { Text("Picked: ${it.name}" + if (it.series.isNotEmpty()) " Â· ${it.series}" else "", color = RoxyColors.muted, fontSize = 13.sp, modifier = Modifier.padding(bottom = 8.dp)) }

        Text("From your library", fontSize = 15.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(top = 8.dp, bottom = 10.dp))
        val s = suggested
        if (s == null) {
            Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) { repeat(4) { Box(Modifier.size(72.dp).clip(CircleShape).background(RoxyColors.surfaceHigh)) } }
        } else if (s.isEmpty()) {
            Text("No matched anime in the library yet.", color = RoxyColors.muted, fontSize = 13.sp)
        } else {
            LazyRow(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                items(s, key = { it.id }) { c -> CharacterTile(c, selected = picked?.id == c.id, onClick = { picked = c }) }
            }
        }

        Text("Search characters", fontSize = 15.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(top = 22.dp, bottom = 10.dp))
        Row(modifier = Modifier.fillMaxWidth().clip(RoundedCornerShape(8.dp)).background(ChipGrey).padding(horizontal = 12.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
            Icon(Icons.Filled.Search, contentDescription = null, tint = RoxyColors.muted, modifier = Modifier.size(20.dp))
            Box(modifier = Modifier.weight(1f).padding(start = 8.dp)) {
                if (query.isEmpty()) Text("Any anime character, e.g. Roxy Migurdia", color = RoxyColors.muted, fontSize = 15.sp)
                BasicTextField(
                    value = query, onValueChange = { query = it }, singleLine = true,
                    textStyle = TextStyle(color = Color.White, fontSize = 15.sp), cursorBrush = SolidColor(Color.White),
                    keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
                    modifier = Modifier.fillMaxWidth(),
                )
            }
        }
        if (results.isNotEmpty()) {
            // A plain wrapping grid (inside the scrolling column, so no nested lazy grid).
            results.chunked(4).forEach { row ->
                Row(modifier = Modifier.fillMaxWidth().padding(top = 12.dp), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                    row.forEach { c -> Box(Modifier.weight(1f)) { CharacterTile(c, selected = picked?.id == c.id, onClick = { picked = c }) } }
                    repeat(4 - row.size) { Spacer(Modifier.weight(1f)) }
                }
            }
        }

        if (profile != null && !profile.isDefault) {
            Spacer(Modifier.height(28.dp))
            Text(
                "Delete profile", color = RoxyColors.error, fontSize = 15.sp, fontWeight = FontWeight.SemiBold,
                modifier = Modifier.align(Alignment.CenterHorizontally).clickable {
                    scope.launch { runCatching { api.deleteProfile(profile.id) }.onSuccess { onDeleted() }.onFailure { t -> error = t.message } }
                }.padding(8.dp),
            )
        }
        Spacer(Modifier.height(24.dp))
    }
}

@Composable
private fun CharacterTile(c: Character, selected: Boolean, onClick: () -> Unit) {
    Column(horizontalAlignment = Alignment.CenterHorizontally, modifier = Modifier.width(72.dp).clickable(onClick = onClick)) {
        Box(modifier = Modifier.size(72.dp).clip(CircleShape).background(RoxyColors.surfaceHigh).border(if (selected) 3.dp else 0.dp, if (selected) RoxyColors.accent else Color.Transparent, CircleShape)) {
            AsyncImage(model = c.image, contentDescription = c.name, contentScale = ContentScale.Crop, modifier = Modifier.fillMaxSize())
            if (selected) {
                Box(modifier = Modifier.align(Alignment.BottomEnd).size(22.dp).clip(CircleShape).background(RoxyColors.accent), contentAlignment = Alignment.Center) {
                    Icon(Icons.Filled.Check, contentDescription = null, tint = OnBrand, modifier = Modifier.size(14.dp))
                }
            }
        }
        Text(c.name, color = RoxyColors.onBackground, fontSize = 11.sp, maxLines = 2, textAlign = TextAlign.Center, overflow = TextOverflow.Ellipsis, lineHeight = 13.sp, modifier = Modifier.padding(top = 5.dp))
    }
}
