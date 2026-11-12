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
import androidx.tv.material3.Icon
import androidx.tv.material3.Text
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.outlined.CloudOff
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import kotlinx.coroutines.flow.Flow
import androidx.compose.runtime.setValue
import kotlinx.coroutines.delay
import tv.roxy.tv.data.tvOfflineWords
import tv.roxy.tv.ui.tv.PageMessage
import kotlinx.coroutines.flow.drop
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.isTelevision
import tv.roxy.tv.ui.phone.GreyButton
import tv.roxy.tv.ui.phone.OfflinePage
import tv.roxy.tv.ui.theme.RoxyColors
import tv.roxy.tv.ui.tv.TvButton
import tv.roxy.tv.ui.tv.TvDims

/**
 * The TV's notice that the server is away (restart, Wi-Fi drop): a quiet dark pill at the
 * top, the way the streaming apps say it, not a red alarm; green "Back online" for a
 * moment when the server answers. Not focusable, so the D-pad never lands on it. Hidden
 * before anyone is signed in.
 */
@Composable
fun ServerBanner(tv: Boolean, modifier: Modifier = Modifier) {
    val app = RoxyApp.get(LocalContext.current)
    val online by app.server.online.collectAsStateWithLifecycle()
    val hasNetwork by app.server.hasNetwork.collectAsStateWithLifecycle()
    val playing by app.server.playerActive.collectAsStateWithLifecycle()
    val session by app.prefs.session.collectAsStateWithLifecycle()
    var back by remember { mutableStateOf(false) }
    LaunchedEffect(Unit) { app.server.backOnline.drop(1).collect { back = true; delay(2500); back = false } }
    val signedIn = session.isLoggedIn && !session.setupInProgress
    val show = signedIn && !playing && (!online || back)
    Box(modifier = modifier.fillMaxWidth().then(if (tv) Modifier.padding(top = 18.dp) else Modifier.statusBarsPadding().padding(top = 6.dp)), contentAlignment = Alignment.TopCenter) {
        AnimatedVisibility(show, enter = fadeIn(), exit = fadeOut()) {
            Row(
                modifier = Modifier.background(if (online) Color(0xF01E8E3E) else Color(0xF026262B), RoundedCornerShape(50)).padding(horizontal = if (tv) 18.dp else 14.dp, vertical = if (tv) 8.dp else 6.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Icon(if (online) Icons.Filled.Check else Icons.Outlined.CloudOff, contentDescription = null, tint = Color.White, modifier = Modifier.size(if (tv) 18.dp else 14.dp))
                Text(
                    if (online) "Back online" else tvOfflineWords(hasNetwork).pill,
                    color = Color.White, fontSize = if (tv) 14.sp else 13.sp, fontWeight = FontWeight.Medium, modifier = Modifier.padding(start = 8.dp),
                )
            }
        }
    }
}

/**
 * A TV page that cannot load because the server is away: the crossed-out cloud, what is
 * wrong and what to check, and Try again (which also looks for the server at once). The
 * page's caller reloads it when the server is back (OnServerBack). [takeFocus] puts the
 * remote on the button: for a whole page, not for one under tabs the remote is already on.
 */
@Composable
fun TvOfflinePage(onRetry: () -> Unit, modifier: Modifier = Modifier, takeFocus: Boolean = true, button: Modifier = Modifier) {
    val app = RoxyApp.get(LocalContext.current)
    val hasNetwork by app.server.hasNetwork.collectAsStateWithLifecycle()
    val words = tvOfflineWords(hasNetwork)
    var checking by remember { mutableStateOf(0) }
    LaunchedEffect(checking) { if (checking > 0) { delay(2500); checking = 0 } }
    val focus = remember { FocusRequester() }
    Column(modifier = modifier.fillMaxSize().padding(bottom = 40.dp), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.Center) {
        Icon(Icons.Outlined.CloudOff, contentDescription = null, tint = RoxyColors.muted, modifier = Modifier.size(64.dp))
        Text(words.title, color = RoxyColors.onBackground, fontSize = 26.sp, fontWeight = FontWeight.Bold, modifier = Modifier.padding(top = 18.dp))
        Text(words.body, color = RoxyColors.muted, fontSize = 15.sp, lineHeight = 22.sp, textAlign = TextAlign.Center, modifier = Modifier.padding(top = 8.dp, bottom = 26.dp).widthIn(max = 460.dp))
        TvButton(if (checking > 0) "Checking…" else "Try again", onClick = { checking++; app.server.checkSoon(); onRetry() }, modifier = button.focusRequester(focus))
    }
    if (takeFocus) LaunchedEffect(Unit) { runCatching { focus.requestFocus() } }
}

/**
 * A TV page's "could not load" under its own heading or tabs: the offline page while the
 * server is away, the error's own line otherwise.
 */
@Composable
fun TvPageError(message: String, onRetry: () -> Unit, modifier: Modifier = Modifier) {
    val online by RoxyApp.get(LocalContext.current).server.online.collectAsStateWithLifecycle()
    if (!online) TvOfflinePage(onRetry, modifier = modifier, takeFocus = false)
    else PageMessage(message, modifier = modifier, color = RoxyColors.error)
}

/**
 * Runs [block] each time the server answers again after being away: a screen that failed
 * to load puts this beside its error so it fills in by itself once Roxy is back.
 *
 * A page under another one (a title page under the player, a tab left for Downloads) is
 * not drawn, so it cannot hear the return when it happens. It remembers the last return it
 * saw instead, and catches up when it is shown again: before, it came back still saying
 * "Can't reach your server" under a strip that had already gone.
 */
@Composable
fun OnServerBack(block: () -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    val latest by rememberUpdatedState(block)
    var seen by rememberSaveable { mutableIntStateOf(app.server.reloads.value) }
    LaunchedEffect(Unit) { onReturns(app.server.reloads, seen = { seen }, mark = { seen = it }) { latest() } }
}

/**
 * [block] for each change of [returns] (ServerStatus.backOnline, a count) from the count
 * last [seen]: the returns that happen while collecting, and the one that happened while
 * nobody was (the count moved on from [seen]).
 */
internal suspend fun onReturns(returns: Flow<Int>, seen: () -> Int, mark: (Int) -> Unit, block: () -> Unit) {
    returns.collect { n -> if (n != seen()) { mark(n); block() } }
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
    if (tv && !online) {
        TvOfflinePage(onRetry = onRetry, modifier = modifier)
    } else if (tv) {
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
