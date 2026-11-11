package tv.roxy.tv.ui.components

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.tv.material3.Text
import kotlinx.coroutines.flow.drop
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.isTelevision
import tv.roxy.tv.ui.phone.GreyButton
import tv.roxy.tv.ui.phone.OfflinePage
import tv.roxy.tv.ui.theme.RoxyColors
import tv.roxy.tv.ui.tv.TvButton
import tv.roxy.tv.ui.tv.TvDims

/**
 * The one "Can't reach Roxy" notice for the whole app: a small pill at the top while the
 * server is away (restart, Wi-Fi drop, away from home). Not focusable, so the TV's D-pad
 * never lands on it; each screen keeps its own Retry. Hidden while the phone's player is
 * up (it says "reconnecting" itself) and before anyone is signed in.
 */
@Composable
fun ServerBanner(tv: Boolean, modifier: Modifier = Modifier) {
    val app = RoxyApp.get(LocalContext.current)
    val online by app.server.online.collectAsStateWithLifecycle()
    val playing by app.server.playerActive.collectAsStateWithLifecycle()
    val session by app.prefs.session.collectAsStateWithLifecycle()
    val show = !online && !playing && session.isLoggedIn && !session.setupInProgress
    Box(modifier = modifier.fillMaxWidth().then(if (tv) Modifier.padding(top = 18.dp) else Modifier.statusBarsPadding().padding(top = 6.dp)), contentAlignment = Alignment.TopCenter) {
        AnimatedVisibility(show, enter = fadeIn(), exit = fadeOut()) {
            Text(
                "Can't reach Roxy. Retrying…",
                color = Color.White, fontSize = if (tv) 15.sp else 13.sp, fontWeight = FontWeight.SemiBold,
                modifier = Modifier
                    .background(Color(0xE6B3261E), RoundedCornerShape(50))
                    .padding(horizontal = if (tv) 20.dp else 14.dp, vertical = if (tv) 8.dp else 6.dp),
            )
        }
    }
}

/**
 * Runs [block] each time the server answers again after being away: a screen that failed
 * to load puts this beside its error so it fills in by itself once Roxy is back.
 */
@Composable
fun OnServerBack(block: () -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    val latest by rememberUpdatedState(block)
    LaunchedEffect(Unit) { app.server.backOnline.drop(1).collect { latest() } }
}

/**
 * A page that could not load: what went wrong (see friendlyError) and a Retry, instead of
 * a blank page or a dead-end line. Reloads by itself when the server comes back. On the TV
 * the Retry button takes focus, so the remote can reach it at once.
 */
@Composable
fun LoadError(message: String, onRetry: () -> Unit, modifier: Modifier = Modifier) {
    val context = LocalContext.current
    val tv = remember { isTelevision(context) }
    val online by RoxyApp.get(context).server.online.collectAsStateWithLifecycle()
    OnServerBack(onRetry)
    if (tv) {
        val focus = remember { FocusRequester() }
        Column(modifier = modifier.fillMaxWidth().padding(start = TvDims.ContentStart, end = TvDims.ContentStart, top = 120.dp)) {
            Text(message, color = RoxyColors.onBackground, fontSize = 18.sp)
            Box(Modifier.padding(top = 20.dp)) { TvButton("Retry", onClick = onRetry, modifier = Modifier.focusRequester(focus)) }
        }
        LaunchedEffect(Unit) { runCatching { focus.requestFocus() } }
    } else if (!online) {
        // The server is away, whatever this call's own error said: the offline page.
        OfflinePage(onRetry = onRetry, modifier = modifier)
    } else {
        Column(
            modifier = modifier.fillMaxSize().padding(24.dp),
            horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.Center,
        ) {
            Text(message, color = RoxyColors.onBackground, fontSize = 15.sp, textAlign = TextAlign.Center)
            GreyButton("Retry", modifier = Modifier.padding(top = 16.dp), onClick = onRetry)
        }
    }
}
