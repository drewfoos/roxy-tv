package tv.roxy.tv.ui.phone

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.ApiException
import tv.roxy.tv.ui.components.RoxyTextField
import tv.roxy.tv.ui.theme.RoxyColors

/**
 * Change password: the current one, then the new one twice. The server checks the
 * current password (wrong ones count toward the sign-in limit) and signs every other
 * device out; this one stays signed in.
 */
@Composable
fun PhoneChangePassword(onBack: () -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    val scope = rememberCoroutineScope()
    var current by remember { mutableStateOf("") }
    var next by remember { mutableStateOf("") }
    var again by remember { mutableStateOf("") }
    var saving by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var done by remember { mutableStateOf(false) }

    Column(modifier = Modifier.fillMaxSize().statusBarsPadding().imePadding()) {
        Row(modifier = Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
            BackCircle(onBack)
            Text("Change password", fontSize = 20.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(start = 12.dp))
        }
        Column(
            modifier = Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(horizontal = PhonePad),
            verticalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            Text(
                "Signed in as ${app.prefs.session.value.userName}. Changing the password signs out every other device on this account; they sign in again with the new one.",
                fontSize = 13.sp, lineHeight = 18.sp, color = RoxyColors.muted,
            )
            RoxyTextField(current, { current = it; error = null }, label = "Current password", password = true)
            RoxyTextField(next, { next = it; error = null }, label = "New password (at least 8 characters)", password = true)
            RoxyTextField(again, { again = it; error = null }, label = "New password again", password = true, imeAction = ImeAction.Done)
            val ready = current.isNotEmpty() && next.length >= 8 && next == again && !saving && !done
            PlayButton(
                when { done -> "Password changed"; saving -> "Saving…"; else -> "Change password" },
                icon = Icons.Filled.Check, pill = true,
                container = if (ready || done) RoxyColors.accent else RoxyColors.surface, content = if (ready || done) OnBrand else RoxyColors.muted,
                modifier = Modifier.fillMaxWidth(),
                onClick = {
                    when {
                        done || saving -> {}
                        next.length < 8 -> error = "The new password must be at least 8 characters."
                        next != again -> error = "The two new passwords don't match."
                        else -> {
                            saving = true
                            scope.launch {
                                runCatching { app.api.changePassword(current, next) }
                                    .onSuccess { done = true; delay(1200); onBack() }
                                    .onFailure { t ->
                                        error = when ((t as? ApiException)?.code) {
                                            403 -> "That's not your current password."
                                            429 -> "Too many wrong passwords. Wait a little and try again."
                                            else -> t.message ?: "Could not change the password."
                                        }
                                    }
                                saving = false
                            }
                        }
                    }
                },
            )
            error?.let { Text(it, fontSize = 13.sp, color = RoxyColors.error) }
        }
    }
}
