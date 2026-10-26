package tv.roxy.tv.ui.phone

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
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
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.EncoderOption
import tv.roxy.tv.api.TranscodingSettings
import tv.roxy.tv.ui.theme.RoxyColors

/**
 * Admin: which encoder the server transcodes with (the player's lower qualities). Auto
 * names what it picks on this server; encoders that failed the server's test encode are
 * shown but cannot be chosen. Nothing changes until Save.
 */
@Composable
fun PhoneTranscodingSettings(onBack: () -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    val scope = rememberCoroutineScope()
    var current by remember { mutableStateOf<TranscodingSettings?>(null) }
    var selected by remember { mutableStateOf<String?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var saving by remember { mutableStateOf(false) }
    var saved by remember { mutableStateOf(false) }
    LaunchedEffect(Unit) {
        runCatching { app.api.transcodingSettings() }
            .onSuccess { current = it; selected = it.hwAccel }
            .onFailure { t -> Timber.w(t, "transcoding settings"); error = t.message }
    }
    Column(modifier = Modifier.fillMaxSize().statusBarsPadding().verticalScroll(rememberScrollState())) {
        Row(modifier = Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
            BackCircle(onBack)
            Text("Transcoding", fontSize = 20.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(start = 12.dp))
        }
        Column(modifier = Modifier.padding(horizontal = PhonePad), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text(
                "When a player picks a lower quality, the server converts the video on the fly. A graphics card does this far faster than the CPU. Subtitles, fonts and audio tracks are kept either way.",
                fontSize = 13.sp, lineHeight = 18.sp, color = RoxyColors.muted,
            )
            val cur = current
            if (cur == null) {
                if (error == null) {
                    Text("Testing which encoders work on the server…", fontSize = 13.sp, color = RoxyColors.muted)
                    repeat(4) { BoneLine(width = 320.dp, height = 48.dp) }
                }
            } else {
                Text("In use now: " + (cur.options.firstOrNull { it.id == cur.encoder }?.label ?: cur.encoder), fontSize = 14.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground)
                Column(modifier = Modifier.fillMaxWidth().clip(RoundedCornerShape(16.dp)).background(RoxyColors.surface)) {
                    cur.options.forEachIndexed { i, o ->
                        if (i > 0) Box(modifier = Modifier.fillMaxWidth().padding(start = 52.dp).height(1.dp).background(Color.White.copy(alpha = 0.06f)))
                        EncoderRow(o, checked = o.id == selected, onClick = { selected = o.id; saved = false; error = null })
                    }
                }
                val dirty = selected != null && selected != cur.hwAccel
                PlayButton(
                    when { saving -> "Saving…"; saved && !dirty -> "Saved"; else -> "Save" },
                    icon = Icons.Filled.Check, pill = true,
                    container = if (dirty) RoxyColors.accent else RoxyColors.surface,
                    content = if (dirty) OnBrand else RoxyColors.muted,
                    modifier = Modifier.fillMaxWidth(),
                    onClick = {
                        val choice = selected
                        if (!dirty || saving || choice == null) return@PlayButton
                        saving = true
                        scope.launch {
                            runCatching { app.api.saveTranscodingSettings(choice) }
                                .onSuccess { current = it; selected = it.hwAccel; saved = true; error = null }
                                .onFailure { t -> error = t.message }
                            saving = false
                        }
                    },
                )
                Text("Videos already converting keep their encoder; the next one uses the new choice.", fontSize = 12.sp, lineHeight = 16.sp, color = RoxyColors.muted)
            }
            error?.let { Text(it, fontSize = 13.sp, color = RoxyColors.error) }
        }
    }
}

@Composable
private fun EncoderRow(o: EncoderOption, checked: Boolean, onClick: () -> Unit) {
    Row(
        modifier = Modifier.fillMaxWidth()
            .then(if (o.available) Modifier.clickable(onClick = onClick) else Modifier)
            .alpha(if (o.available) 1f else 0.45f)
            .padding(horizontal = 16.dp, vertical = 13.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(
            if (checked) Icons.Filled.RadioButtonChecked else Icons.Filled.RadioButtonUnchecked,
            contentDescription = null,
            tint = if (checked) RoxyColors.accent else RoxyColors.muted,
            modifier = Modifier.size(22.dp),
        )
        Column(modifier = Modifier.padding(start = 14.dp)) {
            Text(o.label, fontSize = 15.sp, fontWeight = if (checked) FontWeight.SemiBold else FontWeight.Normal, color = RoxyColors.onBackground)
            val hint = when {
                o.id == "auto" -> "Recommended: uses the best encoder this server has"
                !o.available -> "Not found on this server"
                o.id == "software" -> "Works everywhere; slower and uses more power"
                else -> null
            }
            hint?.let { Text(it, fontSize = 12.sp, color = RoxyColors.muted) }
        }
    }
}
