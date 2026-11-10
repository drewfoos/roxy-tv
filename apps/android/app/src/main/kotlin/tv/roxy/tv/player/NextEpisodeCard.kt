package tv.roxy.tv.player

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.focusable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.TextUnit
import androidx.compose.ui.unit.Dp
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.filled.Close
import androidx.compose.ui.unit.sp
import coil3.compose.AsyncImage
import tv.roxy.tv.ui.components.tapClick
import tv.roxy.tv.ui.phone.OnBrand
import tv.roxy.tv.ui.theme.RoxyColors

/** What the next-episode card shows: the episode, its thumb, and the countdown (null when autoplay is off). */
data class NextCard(
    val title: String,          // "S01E03  A Panty Treasure in This Right Hand!"
    val thumbUrl: String?,
    val secondsLeft: Int?,      // counts down to 0; null = no countdown, wait for the user
)

/**
 * Netflix's end-of-episode card, bottom-right over the video: the next episode's thumb and
 * name, "Next episode in 8 s", then Play now and Watch credits. Shows when the outro
 * starts (or in the last 15 s without a marker) and stays until it fires or is dismissed.
 * D-pad: Play now has focus on arrival; Right moves to Watch credits.
 */
@Composable
fun NextEpisodeCard(card: NextCard, controlsVisible: Boolean, onPlayNow: () -> Unit, onDismiss: () -> Unit, compact: Boolean = false, pageHost: Boolean = false) {
    val playFocus = remember { FocusRequester() }
    LaunchedEffect(Unit) { runCatching { playFocus.requestFocus() } }
    // In the page's 16:9 box the card is one slim row: countdown and name, Play, dismiss.
    if (compact) {
        Box(modifier = Modifier.fillMaxSize().padding(end = 10.dp, bottom = 44.dp), contentAlignment = Alignment.BottomEnd) {
            Row(
                modifier = Modifier.widthIn(max = 300.dp).clip(RoundedCornerShape(8.dp)).background(Color(0xF2141418)).border(BorderStroke(1.dp, Color(0x33FFFFFF)), RoundedCornerShape(8.dp)).padding(start = 10.dp, top = 6.dp, bottom = 6.dp, end = 6.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Column(modifier = Modifier.weight(1f, fill = false)) {
                    Text(
                        when (val s = card.secondsLeft) { null -> "Next episode"; 0 -> "Starting..."; else -> "Next in $s s" },
                        color = RoxyColors.accent, fontSize = 10.sp, fontWeight = FontWeight.SemiBold,
                    )
                    Text(card.title, color = Color.White, fontSize = 11.sp, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
                Spacer(Modifier.width(8.dp))
                CardButton("Play", primary = true, focusRequester = playFocus, height = 28.dp, fontSize = 12.sp, onClick = onPlayNow)
                Spacer(Modifier.width(6.dp))
                Icon(Icons.Filled.Close, contentDescription = "Watch credits", tint = Color.White, modifier = Modifier.size(28.dp).clip(CircleShape).tapClick(onDismiss).padding(6.dp))
            }
        }
        return
    }
    Box(modifier = Modifier.fillMaxSize().padding(end = 32.dp, bottom = if (pageHost) 56.dp else if (controlsVisible) 118.dp else 40.dp), contentAlignment = Alignment.BottomEnd) {
        Column(
            modifier = Modifier.width(340.dp).clip(RoundedCornerShape(8.dp)).background(Color(0xF2141418)).border(BorderStroke(1.dp, Color(0x33FFFFFF)), RoundedCornerShape(8.dp)).padding(12.dp),
        ) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Box(modifier = Modifier.width(112.dp).aspectRatio(16f / 9f).clip(RoundedCornerShape(4.dp)).background(RoxyColors.surfaceHigh)) {
                    if (card.thumbUrl != null) AsyncImage(model = card.thumbUrl, contentDescription = null, contentScale = ContentScale.Crop, modifier = Modifier.fillMaxSize())
                }
                Column(modifier = Modifier.padding(start = 12.dp)) {
                    Text(
                        when (val s = card.secondsLeft) { null -> "Next episode"; 0 -> "Starting…"; else -> "Next episode in $s s" },
                        color = RoxyColors.accent, fontSize = 12.sp, fontWeight = FontWeight.SemiBold,
                    )
                    Text(card.title, color = Color.White, fontSize = 14.sp, fontWeight = FontWeight.SemiBold, maxLines = 2, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 2.dp))
                }
            }
            Row(modifier = Modifier.padding(top = 12.dp), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                CardButton("Play now", primary = true, focusRequester = playFocus, modifier = Modifier.weight(1f), onClick = onPlayNow)
                CardButton("Watch credits", primary = false, modifier = Modifier.weight(1f), onClick = onDismiss)
            }
        }
    }
}

@Composable
private fun CardButton(label: String, primary: Boolean, modifier: Modifier = Modifier, focusRequester: FocusRequester? = null, height: Dp = 38.dp, fontSize: TextUnit = 14.sp, onClick: () -> Unit) {
    var focused by remember { mutableStateOf(false) }
    val bg = when { focused -> Color.White; primary -> RoxyColors.accent; else -> Color(0xFF2E2E36) }
    val fg = when { focused -> Color.Black; primary -> OnBrand; else -> Color.White }
    Row(
        modifier = modifier.height(height)
            .then(if (focusRequester != null) Modifier.focusRequester(focusRequester) else Modifier)
            .onFocusChanged { focused = it.isFocused }
            .focusable()
            .onOkPress { onClick() }
            .clip(RoundedCornerShape(19.dp)).background(bg).tapClick(onClick).padding(horizontal = if (height < 38.dp) 10.dp else 0.dp),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.Center,
    ) {
        if (primary) { Icon(Icons.Filled.PlayArrow, contentDescription = null, tint = fg, modifier = Modifier.size(if (height < 38.dp) 16.dp else 20.dp)); Spacer(Modifier.width(4.dp)) }
        Text(label, color = fg, fontSize = fontSize, fontWeight = FontWeight.Bold, maxLines = 1)
    }
}
