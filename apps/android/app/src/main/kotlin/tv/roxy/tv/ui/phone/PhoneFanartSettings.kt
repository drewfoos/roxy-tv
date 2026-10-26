package tv.roxy.tv.ui.phone

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
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
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.ui.components.RoxyTextField
import tv.roxy.tv.ui.theme.RoxyColors

/** Admin: the fanart.tv project key. With it, the art picker offers fanart.tv's logos and backgrounds. */
@Composable
fun PhoneFanartSettings(onBack: () -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    val scope = rememberCoroutineScope()
    var key by remember { mutableStateOf<String?>(null) }
    var enabled by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var saved by remember { mutableStateOf(false) }
    LaunchedEffect(Unit) {
        runCatching { app.api.fanartSettings() }
            .onSuccess { key = it.apiKey; enabled = it.enabled }
            .onFailure { t -> Timber.w(t, "fanart settings"); error = t.message }
    }
    Column(modifier = Modifier.fillMaxSize().statusBarsPadding()) {
        Row(modifier = Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
            BackCircle(onBack)
            Text("fanart.tv", fontSize = 20.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(start = 12.dp))
        }
        Column(modifier = Modifier.padding(horizontal = PhonePad), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text(
                "fanart.tv has the best logos and textless backgrounds for most anime. A free project key from fanart.tv/get-an-api-key adds its art to the picker under a title's three-dot menu.",
                fontSize = 13.sp, lineHeight = 18.sp, color = RoxyColors.muted,
            )
            val k = key
            if (k == null && error == null) BoneLine(width = 300.dp, height = 40.dp)
            else RoxyTextField(k.orEmpty(), { key = it; saved = false }, label = if (enabled) "Project API key (saved)" else "Project API key", password = true, imeAction = ImeAction.Done)
            PlayButton(if (saved) "Saved" else "Save", icon = Icons.Filled.Check, pill = true, container = RoxyColors.accent, content = OnBrand, modifier = Modifier.fillMaxWidth(), onClick = {
                scope.launch {
                    runCatching { app.api.saveFanartSettings(key.orEmpty()) }
                        .onSuccess { key = it.apiKey; enabled = it.enabled; saved = true; error = null }
                        .onFailure { t -> error = t.message }
                }
            })
            error?.let { Text(it, fontSize = 13.sp, color = RoxyColors.error) }
            Text("Leave the key empty and save to stop using fanart.tv. Picks already made stay.", fontSize = 12.sp, lineHeight = 16.sp, color = RoxyColors.muted)
        }
    }
}
