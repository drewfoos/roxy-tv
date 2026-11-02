package tv.roxy.tv.ui.phone

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
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
import tv.roxy.tv.api.TmdbSettings
import tv.roxy.tv.ui.components.RoxyTextField
import tv.roxy.tv.ui.theme.RoxyColors

/**
 * Admin: TMDB & fanart.tv, the services that name titles and dress them with art (the
 * web app's page of the same name). TMDB is where titles, posters and Discover come from;
 * fanart.tv adds logos and backgrounds to the art picker. Keys are never shown back.
 */
@Composable
fun PhoneFanartSettings(onBack: () -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    val scope = rememberCoroutineScope()
    var tmdb by remember { mutableStateOf<TmdbSettings?>(null) }
    var tmdbKey by remember { mutableStateOf("") }
    var tmdbError by remember { mutableStateOf<String?>(null) }
    var tmdbSaved by remember { mutableStateOf(false) }
    var fanartKey by remember { mutableStateOf("") }
    var fanartOn by remember { mutableStateOf<Boolean?>(null) }
    var fanartError by remember { mutableStateOf<String?>(null) }
    var fanartSaved by remember { mutableStateOf(false) }
    LaunchedEffect(Unit) {
        runCatching { app.api.tmdbSettings() }.onSuccess { tmdb = it }.onFailure { t -> Timber.w(t, "tmdb settings"); tmdbError = t.message }
        runCatching { app.api.fanartSettings() }.onSuccess { fanartOn = it.enabled }.onFailure { t -> Timber.w(t, "fanart settings"); fanartError = t.message }
    }
    Column(modifier = Modifier.fillMaxSize().statusBarsPadding()) {
        Row(modifier = Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
            BackCircle(onBack)
            Text("TMDB & fanart.tv", fontSize = 20.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(start = 12.dp))
        }
        Column(
            modifier = Modifier.fillMaxSize().verticalScroll(rememberScrollState()).imePadding().navigationBarsPadding().padding(horizontal = PhonePad, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Text("Keys are stored on the server and never shown again.", fontSize = 13.sp, lineHeight = 18.sp, color = RoxyColors.muted)

            Text("TMDB", fontSize = 17.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(top = 8.dp))
            val t = tmdb
            Text(
                when {
                    t == null -> "…"
                    t.set -> "A TMDB key is saved" + (if (t.source == "server") " on the server" else "") + ". Paste a new one to replace it."
                    else -> "No TMDB key yet. Discover and metadata lookups need one."
                },
                fontSize = 13.sp, lineHeight = 18.sp, color = if (t?.set == true) RoxyColors.accent else RoxyColors.muted,
            )
            RoxyTextField(tmdbKey, { tmdbKey = it; tmdbSaved = false }, label = "TMDB API key", password = true, imeAction = ImeAction.Done)
            Text("Free at themoviedb.org: Settings → API → Create. Checked with TMDB when you save.", fontSize = 12.sp, lineHeight = 16.sp, color = RoxyColors.muted)
            tmdbError?.let { Text(it, fontSize = 13.sp, color = RoxyColors.error) }
            PlayButton(if (tmdbSaved) "Saved" else "Save TMDB key", icon = Icons.Filled.Check, pill = true, container = RoxyColors.accent, content = OnBrand, modifier = Modifier.fillMaxWidth(), onClick = {
                val k = tmdbKey.trim()
                if (k.isEmpty()) return@PlayButton
                scope.launch {
                    runCatching { app.api.saveTmdbKey(k) }
                        .onSuccess { tmdb = it; tmdbKey = ""; tmdbSaved = true; tmdbError = null }
                        .onFailure { e -> tmdbError = e.message }
                }
            })

            Text("fanart.tv", fontSize = 17.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(top = 16.dp))
            Text(
                when (fanartOn) {
                    null -> "…"
                    true -> "fanart.tv is on: logos and backgrounds come from it when TMDB has none."
                    false -> "Off. Add a project key to get logos and backgrounds for the art picker."
                },
                fontSize = 13.sp, lineHeight = 18.sp, color = if (fanartOn == true) RoxyColors.accent else RoxyColors.muted,
            )
            RoxyTextField(fanartKey, { fanartKey = it; fanartSaved = false }, label = "Project API key", password = true, imeAction = ImeAction.Done)
            Text("Free at fanart.tv under your account's API page. An empty key turns fanart.tv off; picks already made stay.", fontSize = 12.sp, lineHeight = 16.sp, color = RoxyColors.muted)
            fanartError?.let { Text(it, fontSize = 13.sp, color = RoxyColors.error) }
            PlayButton(
                if (fanartSaved) "Saved" else if (fanartKey.isBlank()) "Turn fanart.tv off" else "Save fanart.tv key",
                icon = Icons.Filled.Check, pill = true, container = RoxyColors.accent, content = OnBrand, modifier = Modifier.fillMaxWidth(),
                onClick = {
                    scope.launch {
                        runCatching { app.api.saveFanartSettings(fanartKey.trim()) }
                            .onSuccess { fanartOn = it.enabled; fanartKey = ""; fanartSaved = true; fanartError = null }
                            .onFailure { e -> fanartError = e.message }
                    }
                },
            )
        }
    }
}
