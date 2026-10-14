package tv.roxy.tv.ui.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.tv.material3.MaterialTheme
import androidx.tv.material3.Text
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.BuildConfig
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.data.PlayerSettings
import tv.roxy.tv.diagnostics.DeviceReport
import tv.roxy.tv.ui.components.ChoiceRow
import tv.roxy.tv.ui.components.ScreenPadding
import tv.roxy.tv.ui.components.SecondaryButton
import tv.roxy.tv.ui.theme.RoxyColors

@Composable
fun SettingsScreen(onLoggedOut: () -> Unit) {
    val context = LocalContext.current
    val app = RoxyApp.get(context)
    val settings by app.prefs.settings.collectAsStateWithLifecycle()
    val session by app.prefs.session.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    val device = remember { DeviceReport.soc() + "\n" + DeviceReport.display(context) }

    fun save(s: PlayerSettings) = scope.launch { app.prefs.saveSettings(s) }

    Column(
        modifier = Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(horizontal = ScreenPadding, vertical = 28.dp),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        Text("Settings", style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
        Text("${session.userName} @ ${session.serverUrl}", color = RoxyColors.muted, fontSize = 13.sp, modifier = Modifier.padding(bottom = 12.dp))

        Text("Player", style = MaterialTheme.typography.titleMedium, modifier = Modifier.padding(top = 6.dp))
        ChoiceRow("Hardware decoding", PlayerSettings.hwdecChoices, settings.hwdec) { save(settings.copy(hwdec = it)) }
        ChoiceRow("Video output", PlayerSettings.voChoices, settings.vo) { save(settings.copy(vo = it)) }
        ChoiceRow(
            "Audio language",
            PlayerSettings.audioChoices.map { it.label() },
            settings.audioLangs.label(),
        ) { picked -> PlayerSettings.audioChoices.firstOrNull { it.label() == picked }?.let { save(settings.copy(audioLangs = it)) } }
        ChoiceRow("Subtitles", PlayerSettings.subModeChoices.map { it.subLabel() }, settings.subMode.subLabel()) { picked ->
            PlayerSettings.subModeChoices.firstOrNull { it.subLabel() == picked }?.let { save(settings.copy(subMode = it)) }
        }
        ChoiceRow("Stats overlay", listOf("off", "on"), if (settings.showStats) "on" else "off") { save(settings.copy(showStats = it == "on")) }
        Text(
            "Subtitles: \"full\" picks the dialogue track (for Japanese audio), \"signs\" the signs & songs track (for an English dub). " +
                "The stats overlay can also be toggled in the player with Up or Menu.",
            color = RoxyColors.muted, fontSize = 12.sp, modifier = Modifier.padding(top = 4.dp, bottom = 12.dp),
        )

        Text("Account", style = MaterialTheme.typography.titleMedium)
        SecondaryButton("Log out", onClick = {
            scope.launch {
                runCatching { app.api.logout() }.onFailure { Timber.w("logout: %s", it.message) }
                app.prefs.clearSession()
                onLoggedOut()
            }
        })

        Spacer(Modifier.height(16.dp))
        Text("About", style = MaterialTheme.typography.titleMedium)
        Text("Roxy TV ${BuildConfig.VERSION_NAME} (${BuildConfig.APPLICATION_ID})", color = RoxyColors.muted, fontSize = 12.sp)
        Text(device, color = RoxyColors.muted, fontSize = 11.sp, fontFamily = FontFamily.Monospace)
    }
}

private fun List<String>.label() = when (this) {
    listOf("eng", "jpn") -> "English dub, else Japanese"
    listOf("jpn", "eng") -> "Japanese, else English"
    else -> joinToString(",")
}

private fun String.subLabel() = when (this) {
    "full" -> "full dialogue"
    "signs" -> "signs & songs"
    "none" -> "off"
    else -> this
}
