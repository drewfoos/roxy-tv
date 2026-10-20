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
import tv.roxy.tv.data.Languages
import tv.roxy.tv.diagnostics.DeviceReport
import tv.roxy.tv.player.AudioPassthrough
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
        // The TV cycles through the common languages with the D-pad; the phone has the full searchable list.
        val tvLangs = Languages.all.take(12)
        ChoiceRow("Audio language", tvLangs.map { it.name }, Languages.name(settings.audioLangs.firstOrNull() ?: "eng")) { picked ->
            tvLangs.firstOrNull { it.name == picked }?.let { save(settings.copy(audioLangs = listOf(it.iso3))) }
        }
        ChoiceRow("Subtitle language", tvLangs.map { it.name }, Languages.name(settings.subLang)) { picked ->
            tvLangs.firstOrNull { it.name == picked }?.let { save(settings.copy(subLang = it.iso3)) }
        }
        ChoiceRow("Subtitles", PlayerSettings.subModeChoices.map { PlayerSettings.subModeLabel(it) }, PlayerSettings.subModeLabel(settings.subMode)) { picked ->
            PlayerSettings.subModeChoices.firstOrNull { PlayerSettings.subModeLabel(it) == picked }?.let { save(settings.copy(subMode = it)) }
        }
        val passthroughFound = remember { AudioPassthrough.detect(context) }
        val passLabels = listOf("auto" to AudioPassthrough.autoLabel(passthroughFound), "off" to "Off (decode on the TV)")
        ChoiceRow("Soundbar passthrough", passLabels.map { it.second }, passLabels.first { it.first == settings.passthrough }.second) { picked ->
            passLabels.firstOrNull { it.second == picked }?.let { save(settings.copy(passthrough = it.first)) }
        }
        ChoiceRow("Stats overlay", listOf("off", "on"), if (settings.showStats) "on" else "off") { save(settings.copy(showStats = it == "on")) }
        Text(
            "Subtitles: signs & songs pairs an anime dub with its signs track; foreign audio only turns them off for a dub; " +
                "always full dialogue keeps the dialogue track up whatever the audio. Soundbar passthrough sends Dolby and DTS audio untouched " +
                "to a soundbar or receiver on HDMI ARC, so it plays the surround; the soundbar then controls the volume. The stats overlay can also be toggled in the player with Up or Menu.",
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


