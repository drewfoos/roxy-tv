package tv.roxy.tv.ui.phone

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.ExpandLess
import androidx.compose.material.icons.filled.ExpandMore
import androidx.compose.material.icons.filled.RadioButtonChecked
import androidx.compose.material.icons.filled.RadioButtonUnchecked
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.launch
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.ServerUser
import tv.roxy.tv.ui.components.RoxyTextField
import tv.roxy.tv.ui.profiles.AvatarCircle
import tv.roxy.tv.ui.theme.RoxyColors

private val roles = listOf(
    "admin" to ("Admin" to "Everything, including these settings"),
    "requester" to ("Requester" to "Watches; requests go straight to Sonarr and Radarr"),
    "basic" to ("Basic" to "Watches; requests wait for an admin to approve them"),
)

private fun roleName(r: String) = roles.firstOrNull { it.first == r }?.second?.first ?: r

/** The server's message out of an API error ("HTTP 400 ...: {"error":"..."}"). */
private fun apiMessage(t: Throwable): String {
    val m = t.message ?: return "Something went wrong."
    return Regex(""""error":"([^"]+)"""").find(m)?.groupValues?.get(1)?.replaceFirstChar { it.uppercase() } ?: m
}

/**
 * Admin: Users. Every account has its own password and its own profiles. Three levels:
 * Admin, Requester (requests go straight to Sonarr / Radarr), Basic (requests wait for
 * approval). Share the server by making an account per person or household, never by
 * giving out the admin password. Adding and editing happen in forms on the page itself
 * (no popups), so the keyboard just scrolls the page.
 */
@Composable
fun PhoneUsers(onBack: () -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    var users by remember { mutableStateOf<List<ServerUser>?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var adding by remember { mutableStateOf(false) }
    var open by remember { mutableStateOf<String?>(null) } // the user whose form is open
    var tick by remember { mutableStateOf(0) }
    LaunchedEffect(tick) { runCatching { app.api.users() }.onSuccess { users = it; error = null }.onFailure { error = apiMessage(it) } }
    val done: () -> Unit = { adding = false; open = null; tick++ }

    Column(modifier = Modifier.fillMaxSize().statusBarsPadding().imePadding()) {
        Row(modifier = Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
            BackCircle(onBack)
            Text("Users", fontSize = 20.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(start = 12.dp))
        }
        LazyColumn(modifier = Modifier.fillMaxSize(), contentPadding = PaddingValues(start = PhonePad, end = PhonePad, bottom = 32.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            item {
                Text(
                    "Give each person or household their own account instead of sharing yours. Everyone gets their own profiles inside it.",
                    fontSize = 13.sp, lineHeight = 18.sp, color = RoxyColors.muted, modifier = Modifier.padding(bottom = 6.dp),
                )
            }
            error?.let { e -> item { Text(e, fontSize = 13.sp, color = RoxyColors.error) } }
            val list = users
            if (list == null) items(3) { Bone(Modifier.fillMaxWidth().height(60.dp)) }
            else items(list, key = { it.id }) { u ->
                val expanded = open == u.id
                Column(
                    modifier = Modifier.fillMaxWidth().clip(RoundedCornerShape(12.dp)).background(RoxyColors.surface)
                        .then(if (expanded) Modifier.border(1.dp, RoxyColors.surfaceHigh, RoundedCornerShape(12.dp)) else Modifier),
                ) {
                    Row(
                        modifier = Modifier.fillMaxWidth().clickable { open = if (expanded) null else u.id; adding = false }.padding(horizontal = 14.dp, vertical = 12.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        AvatarCircle(name = u.name, imageUrl = null, size = 36.dp)
                        Column(modifier = Modifier.weight(1f).padding(start = 12.dp)) {
                            Text(u.name + if (u.isYou) "  (you)" else "", fontSize = 15.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground)
                            Text(
                                roleName(u.role) + if (u.isDisabled) "  ·  disabled" else "",
                                fontSize = 12.sp, color = if (u.isDisabled) RoxyColors.error else if (u.role == "admin") RoxyColors.accent else RoxyColors.muted,
                            )
                        }
                        Icon(if (expanded) Icons.Filled.ExpandLess else Icons.Filled.ExpandMore, null, tint = RoxyColors.muted)
                    }
                    if (expanded) EditUserForm(u, onDone = done)
                }
            }
            item(key = "add") {
                Column(modifier = Modifier.fillMaxWidth().padding(top = 4.dp).clip(RoundedCornerShape(12.dp)).background(RoxyColors.surface)) {
                    Row(
                        modifier = Modifier.fillMaxWidth().clickable { adding = !adding; open = null }.padding(horizontal = 14.dp, vertical = 14.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Icon(Icons.Filled.Add, null, tint = RoxyColors.accent, modifier = Modifier.size(22.dp))
                        Text("Add user", fontSize = 15.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.accent, modifier = Modifier.padding(start = 12.dp).weight(1f))
                        if (adding) Icon(Icons.Filled.ExpandLess, null, tint = RoxyColors.muted)
                    }
                    if (adding) AddUserForm(onDone = done)
                }
            }
        }
    }
}

@Composable
private fun AddUserForm(onDone: () -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    val scope = rememberCoroutineScope()
    var name by remember { mutableStateOf("") }
    var password by remember { mutableStateOf("") }
    var role by remember { mutableStateOf("requester") }
    var saving by remember { mutableStateOf(false) }
    var err by remember { mutableStateOf<String?>(null) }
    Column(modifier = Modifier.fillMaxWidth().padding(start = 14.dp, end = 14.dp, bottom = 14.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        RoxyTextField(name, { name = it; err = null }, label = "Name (what they type to sign in)")
        RoxyTextField(password, { password = it; err = null }, label = "Password (at least 8 characters)", password = true, imeAction = ImeAction.Done)
        RolePicker(role) { role = it }
        Text("Tell them the name and password yourself; they can change the password under Account.", fontSize = 12.sp, lineHeight = 16.sp, color = RoxyColors.muted)
        err?.let { Text(it, fontSize = 13.sp, color = RoxyColors.error) }
        val ready = name.isNotBlank() && password.length >= 8 && !saving
        PlayButton(
            if (saving) "Adding…" else "Add user", icon = Icons.Filled.Check, pill = true,
            container = if (ready) RoxyColors.accent else RoxyColors.surfaceHigh, content = if (ready) OnBrand else RoxyColors.muted,
            modifier = Modifier.fillMaxWidth(),
            onClick = {
                when {
                    saving -> {}
                    name.isBlank() -> err = "Give them a name."
                    password.length < 8 -> err = "The password must be at least 8 characters."
                    else -> {
                        saving = true
                        scope.launch {
                            runCatching { app.api.createUser(name.trim(), password, role) }
                                .onSuccess { onDone() }
                                .onFailure { err = apiMessage(it) }
                            saving = false
                        }
                    }
                }
            },
        )
    }
}

@Composable
private fun EditUserForm(u: ServerUser, onDone: () -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    val scope = rememberCoroutineScope()
    var role by remember(u.id) { mutableStateOf(u.role) }
    var newPassword by remember(u.id) { mutableStateOf("") }
    var err by remember(u.id) { mutableStateOf<String?>(null) }
    var confirmDelete by remember(u.id) { mutableStateOf(false) }
    var saving by remember(u.id) { mutableStateOf(false) }
    fun run(block: suspend () -> Unit) {
        if (saving) return
        saving = true
        scope.launch { runCatching { block() }.onSuccess { onDone() }.onFailure { err = apiMessage(it) }; saving = false }
    }
    Column(modifier = Modifier.fillMaxWidth().padding(start = 14.dp, end = 14.dp, bottom = 14.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        if (u.isYou) {
            Text(
                "You can't change your own level, disable or delete yourself; another admin can. Change your password under Account > Change password.",
                fontSize = 13.sp, lineHeight = 18.sp, color = RoxyColors.muted,
            )
            return@Column
        }
        RolePicker(role) { role = it }
        RoxyTextField(newPassword, { newPassword = it; err = null }, label = "New password (leave empty to keep theirs)", password = true, imeAction = ImeAction.Done)
        err?.let { Text(it, fontSize = 13.sp, color = RoxyColors.error) }
        val changed = role != u.role || newPassword.isNotEmpty()
        PlayButton(
            if (saving) "Saving…" else "Save", icon = Icons.Filled.Check, pill = true,
            container = if (changed) RoxyColors.accent else RoxyColors.surfaceHigh, content = if (changed) OnBrand else RoxyColors.muted,
            modifier = Modifier.fillMaxWidth(),
            onClick = {
                when {
                    !changed -> {}
                    newPassword.isNotEmpty() && newPassword.length < 8 -> err = "The new password must be at least 8 characters."
                    else -> run { app.api.updateUser(u.id, role = role.takeIf { it != u.role }, password = newPassword.takeIf { it.isNotEmpty() }) }
                }
            },
        )
        Text("A new password or disabling signs them out everywhere.", fontSize = 12.sp, color = RoxyColors.muted)
        Row(horizontalArrangement = Arrangement.spacedBy(24.dp)) {
            Text(
                if (u.isDisabled) "Enable account" else "Disable account", fontSize = 14.sp, fontWeight = FontWeight.SemiBold,
                color = if (u.isDisabled) RoxyColors.accent else RoxyColors.error,
                modifier = Modifier.clickable { run { app.api.updateUser(u.id, disabled = !u.isDisabled) } }.padding(vertical = 6.dp),
            )
            Text(
                if (confirmDelete) "Tap again to delete" else "Delete account", fontSize = 14.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.error,
                modifier = Modifier.clickable { if (confirmDelete) run { app.api.deleteUser(u.id) } else confirmDelete = true }.padding(vertical = 6.dp),
            )
        }
        if (confirmDelete) Text(
            "Deleting removes their account, profiles and watch history and signs them out. Their requests stay. This can't be undone.",
            fontSize = 12.sp, lineHeight = 16.sp, color = RoxyColors.error,
        )
    }
}

@Composable
private fun RolePicker(role: String, onRole: (String) -> Unit) {
    Column {
        Text("Level", fontSize = 12.sp, color = RoxyColors.muted, modifier = Modifier.padding(bottom = 2.dp))
        roles.forEach { (key, t) ->
            Row(modifier = Modifier.fillMaxWidth().clip(RoundedCornerShape(8.dp)).clickable { onRole(key) }.padding(vertical = 7.dp), verticalAlignment = Alignment.CenterVertically) {
                Icon(if (role == key) Icons.Filled.RadioButtonChecked else Icons.Filled.RadioButtonUnchecked, null, tint = if (role == key) RoxyColors.accent else RoxyColors.muted, modifier = Modifier.size(22.dp))
                Column(modifier = Modifier.padding(start = 12.dp)) {
                    Text(t.first, fontSize = 15.sp, color = RoxyColors.onBackground)
                    Text(t.second, fontSize = 12.sp, lineHeight = 16.sp, color = RoxyColors.muted)
                }
            }
        }
    }
}
