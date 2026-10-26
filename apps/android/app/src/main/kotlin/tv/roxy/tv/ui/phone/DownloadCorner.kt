package tv.roxy.tv.ui.phone

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.OfflinePin
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.Item
import tv.roxy.tv.data.downloads.DownloadState
import tv.roxy.tv.ui.theme.RoxyColors

/**
 * A card's download state in its corner, live: a progress ring while the episode or movie
 * downloads (or waits), a blue check once it is on the phone, a red mark if it failed.
 * Nothing when it isn't downloaded. Shows have no corner (their episodes carry it).
 */
@Composable
fun DownloadCorner(item: Item, modifier: Modifier = Modifier) {
    if (item.isSeries) return
    val index by RoxyApp.get(LocalContext.current).downloads.index.collectAsStateWithLifecycle()
    val entry = index.entries[item.id] ?: return
    Box(modifier = modifier.size(26.dp).clip(CircleShape).background(Color(0xB3000000)), contentAlignment = Alignment.Center) {
        when (entry.state) {
            DownloadState.Done -> Icon(Icons.Filled.OfflinePin, contentDescription = "Downloaded", tint = RoxyColors.accent, modifier = Modifier.size(22.dp))
            DownloadState.Failed -> Icon(Icons.Outlined.ErrorOutline, contentDescription = "Download failed", tint = RoxyColors.error, modifier = Modifier.size(20.dp))
            else -> {
                val f = entry.fraction
                if (f <= 0f) CircularProgressIndicator(modifier = Modifier.size(18.dp), color = RoxyColors.accent, trackColor = Color(0x55FFFFFF), strokeWidth = 2.5.dp)
                else CircularProgressIndicator(progress = { f }, modifier = Modifier.size(18.dp), color = RoxyColors.accent, trackColor = Color(0x55FFFFFF), strokeWidth = 2.5.dp)
            }
        }
    }
}
