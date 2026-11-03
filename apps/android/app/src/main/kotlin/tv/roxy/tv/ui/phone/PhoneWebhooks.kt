package tv.roxy.tv.ui.phone

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.os.PersistableBundle
import android.widget.Toast
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ContentCopy
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.outlined.Visibility
import androidx.compose.material.icons.outlined.VisibilityOff
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
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.ApiKeyRow
import tv.roxy.tv.api.WebhookEvent
import tv.roxy.tv.ui.theme.RoxyColors

/**
 * Admin page: how Sonarr and Radarr tell Roxy about imports, renames and deletes. Shows
 * the two URLs to paste into a Webhook connection, makes the API key they authenticate
 * with (shown once, with Copy), and lists the last notifications and what Roxy did.
 */
@Composable
fun PhoneWebhooks(onBack: () -> Unit) {
    val context = LocalContext.current
    val app = RoxyApp.get(context)
    val scope = rememberCoroutineScope()
    val base = app.api.baseUrl
    var keys by remember { mutableStateOf<List<ApiKeyRow>?>(null) }
    var events by remember { mutableStateOf<List<WebhookEvent>?>(null) }
    var newToken by remember { mutableStateOf<String?>(null) }
    var error by remember { mutableStateOf<String?>(null) }

    suspend fun reload() {
        runCatching { app.api.apiKeys() }.onSuccess { keys = it }.onFailure { error = it.message }
        runCatching { app.api.webhookEvents() }.onSuccess { events = it }.onFailure { error = it.message }
    }
    LaunchedEffect(Unit) { reload() }

    fun copy(label: String, text: String, sensitive: Boolean = false) {
        val cm = context.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
        val clip = ClipData.newPlainText(label, text)
        // A key: Android 13+ hides its preview in the clipboard pop-up (and keyboards' clipboard
        // strips). The extra's literal name works below API 33 too, where some keyboards honour it.
        if (sensitive) clip.description.extras = PersistableBundle().apply { putBoolean("android.content.extra.IS_SENSITIVE", true) }
        cm.setPrimaryClip(clip)
        Toast.makeText(context, "$label copied", Toast.LENGTH_SHORT).show()
    }
    // The new key is dotted out unless asked for: the owner streams this screen.
    var revealToken by remember { mutableStateOf(false) }

    Column(modifier = Modifier.fillMaxSize().statusBarsPadding().verticalScroll(rememberScrollState()).navigationBarsPadding().padding(bottom = 24.dp)) {
        Row(modifier = Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
            BackCircle(onBack)
            Text("Webhooks", fontSize = 20.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.weight(1f).padding(start = 12.dp))
            Icon(Icons.Filled.Refresh, contentDescription = "Refresh", tint = RoxyColors.muted, modifier = Modifier.size(24.dp).clickable { scope.launch { reload() } })
            Spacer(Modifier.width(4.dp))
        }
        Text(
            "In Sonarr and Radarr add a Webhook connection (Settings → Connect → +) with the URL below, method POST, and the API key as the header X-Api-Key (or ?api_key= on the URL). Tick On Import, On Upgrade, On Rename, On File Delete and On Series / Movie Delete. Roxy then updates the library in seconds instead of on a rescan.",
            fontSize = 13.sp, lineHeight = 18.sp, color = RoxyColors.muted, modifier = Modifier.padding(horizontal = PhonePad),
        )
        SectionTitle("URLs", modifier = Modifier.padding(top = 12.dp))
        CopyRow("Sonarr", "$base/v1/webhooks/sonarr") { copy("Sonarr webhook URL", it) }
        CopyRow("Radarr", "$base/v1/webhooks/radarr") { copy("Radarr webhook URL", it) }
        Text("Paths in the notifications are the arr app's own (a Docker mount); Roxy matches them onto its library folders by their tail, so the same folder layout on both sides needs no setup.", fontSize = 12.sp, lineHeight = 16.sp, color = RoxyColors.muted, modifier = Modifier.padding(horizontal = PhonePad, vertical = 6.dp))

        SectionTitle("API keys", modifier = Modifier.padding(top = 12.dp))
        newToken?.let { t ->
            Column(modifier = Modifier.padding(horizontal = PhonePad).fillMaxWidth().clip(RoundedCornerShape(8.dp)).background(RoxyColors.surfaceHigh).padding(12.dp)) {
                Text("New key (shown once, copy it now)", fontSize = 12.sp, color = RoxyColors.accent, fontWeight = FontWeight.SemiBold)
                Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.padding(top = 6.dp)) {
                    Text(if (revealToken) t else "•".repeat(minOf(t.length, 24)), fontSize = 13.sp, fontFamily = FontFamily.Monospace, color = RoxyColors.onBackground, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
                    Icon(
                        if (revealToken) Icons.Outlined.VisibilityOff else Icons.Outlined.Visibility, contentDescription = if (revealToken) "Hide key" else "Show key",
                        tint = RoxyColors.muted, modifier = Modifier.padding(start = 8.dp).size(22.dp).clickable { revealToken = !revealToken },
                    )
                    Icon(Icons.Filled.ContentCopy, contentDescription = "Copy", tint = Color.White, modifier = Modifier.padding(start = 12.dp).size(22.dp).clickable { copy("API key", t, sensitive = true) })
                }
            }
        }
        keys?.forEach { k ->
            Row(modifier = Modifier.fillMaxWidth().padding(horizontal = PhonePad, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
                Column(modifier = Modifier.weight(1f)) {
                    Text(k.name, fontSize = 15.sp, color = RoxyColors.onBackground)
                    Text("Created ${k.createdAt.take(10)}", fontSize = 12.sp, color = RoxyColors.muted)
                }
                Icon(Icons.Filled.Delete, contentDescription = "Delete key", tint = RoxyColors.muted, modifier = Modifier.size(22.dp).clickable {
                    scope.launch { runCatching { app.api.deleteApiKey(k.id) }.onFailure { error = it.message }; if (newToken != null) newToken = null; reload() }
                })
            }
        }
        if (keys?.isEmpty() == true) Text("No keys yet.", fontSize = 13.sp, color = RoxyColors.muted, modifier = Modifier.padding(horizontal = PhonePad, vertical = 6.dp))
        Row(modifier = Modifier.padding(horizontal = PhonePad, vertical = 8.dp), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            GreyButton("New key for Sonarr", onClick = { scope.launch { runCatching { app.api.createApiKey("Sonarr") }.onSuccess { newToken = it.token; reload() }.onFailure { error = it.message } } })
            GreyButton("For Radarr", onClick = { scope.launch { runCatching { app.api.createApiKey("Radarr") }.onSuccess { newToken = it.token; reload() }.onFailure { error = it.message } } })
        }
        error?.let { Text(it, fontSize = 13.sp, color = RoxyColors.error, modifier = Modifier.padding(horizontal = PhonePad)) }

        SectionTitle("Recent notifications", modifier = Modifier.padding(top = 12.dp))
        when {
            events == null -> Text("Loading…".replace("Loading…", ""), fontSize = 1.sp)
            events!!.isEmpty() -> Text("Nothing received yet. Use the Test button on the connection in Sonarr or Radarr; it should show up here.", fontSize = 13.sp, lineHeight = 18.sp, color = RoxyColors.muted, modifier = Modifier.padding(horizontal = PhonePad))
            else -> events!!.forEach { e ->
                Column(modifier = Modifier.fillMaxWidth().padding(horizontal = PhonePad, vertical = 8.dp)) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Text("${e.source.replaceFirstChar { it.uppercase() }}  ·  ${e.eventType}", fontSize = 14.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground, modifier = Modifier.weight(1f))
                        Text(e.at.replace('T', ' ').take(16), fontSize = 11.sp, color = RoxyColors.muted)
                    }
                    if (e.title.isNotEmpty()) Text(e.title, fontSize = 13.sp, color = RoxyColors.onBackground.copy(alpha = 0.85f), maxLines = 1, overflow = TextOverflow.Ellipsis)
                    Text(e.result, fontSize = 12.sp, color = if (e.result.startsWith("error")) RoxyColors.error else RoxyColors.muted, maxLines = 2, overflow = TextOverflow.Ellipsis)
                }
            }
        }
    }
}

@Composable
private fun CopyRow(label: String, value: String, onCopy: (String) -> Unit) {
    Row(modifier = Modifier.fillMaxWidth().clickable { onCopy(value) }.padding(horizontal = PhonePad, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
        Column(modifier = Modifier.weight(1f)) {
            Text(label, fontSize = 15.sp, color = RoxyColors.onBackground)
            Text(value, fontSize = 12.sp, fontFamily = FontFamily.Monospace, color = RoxyColors.muted, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        Icon(Icons.Filled.ContentCopy, contentDescription = "Copy", tint = RoxyColors.muted, modifier = Modifier.size(22.dp))
    }
}
