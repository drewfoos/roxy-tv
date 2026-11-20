package tv.roxy.tv.ui

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.outlined.Dns
import androidx.compose.material.icons.outlined.SystemUpdate
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.BuildConfig
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.data.Compat
import tv.roxy.tv.ui.components.LoadingCircle
import tv.roxy.tv.data.UpdateState
import tv.roxy.tv.data.Updates
import tv.roxy.tv.ui.phone.PlayButton
import tv.roxy.tv.ui.theme.RoxyColors
import tv.roxy.tv.ui.tv.TvButton

/** What the main button of the page does. */
enum class CompatAction { Install, LookForUpdate, TryAgain }

/**
 * The words of the page shown when the app and the server cannot work together.
 * [button] null: something is under way (looking for the update, downloading it) and
 * [progress] says what.
 */
data class CompatWords(val title: String, val body: String, val progress: String? = null, val button: String? = null, val action: CompatAction? = null)

/**
 * [appVersion] is this app's. The app being too old, the way out is the server's own
 * update: the words follow that update from "looking for it" to the installer.
 */
fun compatWords(verdict: Compat.Verdict, update: UpdateState, checked: Updates.Checked?, appVersion: String): CompatWords? = when (verdict) {
    Compat.Verdict.Ok -> null
    Compat.Verdict.ServerTooOld -> CompatWords(
        title = "The server needs updating",
        body = "This app (Roxy $appVersion) is newer than your Roxy Server can work with. Update the server, then try again.",
        button = "Try again", action = CompatAction.TryAgain,
    )
    Compat.Verdict.AppTooOld -> {
        val title = "Update needed"
        val old = "This version of the app (Roxy $appVersion) is too old for your server."
        when (update) {
            is UpdateState.Available -> CompatWords(title, "$old Roxy ${update.update.versionName} is ready to download from it.", button = "Update", action = CompatAction.Install)
            is UpdateState.Downloading -> CompatWords(title, "$old Android asks to confirm the install when the download is done.", progress = "Downloading Roxy ${update.update.versionName}  ·  ${(update.fraction.coerceIn(0f, 1f) * 100).toInt()}%")
            is UpdateState.Ready ->
                if (update.installing) CompatWords(title, "$old Android asks to confirm the install in a moment.", progress = "Installing Roxy ${update.update.versionName}")
                else CompatWords(title, "$old Roxy ${update.update.versionName} is downloaded.", button = "Install", action = CompatAction.Install)
            is UpdateState.Failed -> CompatWords(title, "$old The update didn't download: ${update.message.trimEnd('.')}.", button = "Try again", action = CompatAction.Install)
            UpdateState.None -> when (checked) {
                Updates.Checked.Current -> CompatWords(title, "$old The server has no newer version of the app to give. Whoever runs it needs to publish one.", button = "Look again", action = CompatAction.LookForUpdate)
                Updates.Checked.Unreachable -> CompatWords(title, "$old The update could not be looked for: the server did not answer.", button = "Look again", action = CompatAction.LookForUpdate)
                else -> CompatWords(title, old, progress = "Looking for the update…")
            }
        }
    }
}

/** The sign-in page's line under the address of a server this app cannot work with. */
fun compatSignInWarning(verdict: Compat.Verdict): String? = when (verdict) {
    Compat.Verdict.Ok -> null
    Compat.Verdict.AppTooOld -> "This app is too old for that server. Sign in to get the update from it."
    Compat.Verdict.ServerTooOld -> "That server is too old for this app. Update the server first."
}

/**
 * Stands in for the whole app ([content]) while the app and the server cannot work
 * together: nothing else would load, or worse, would half work. Only for someone signed
 * in; the sign-in page says it in its own line under the address.
 */
@Composable
fun CompatGate(tv: Boolean, content: @Composable () -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    val verdict by app.server.compat.collectAsStateWithLifecycle()
    val session by app.prefs.session.collectAsStateWithLifecycle()
    val update by app.updates.state.collectAsStateWithLifecycle()
    val checked by app.updates.checked.collectAsStateWithLifecycle()
    val blocked = verdict != Compat.Verdict.Ok && session.isLoggedIn && !session.setupInProgress
    if (!blocked) { content(); return }
    // An update offered and dismissed earlier is offered again; one already on its way is left alone.
    LaunchedEffect(verdict) { if (verdict == Compat.Verdict.AppTooOld && app.updates.state.value is UpdateState.None) app.updates.checkNow() }
    val scope = rememberCoroutineScope()
    val words = compatWords(verdict, update, checked, BuildConfig.VERSION_NAME) ?: return
    CompatPage(
        tv = tv, words = words,
        onAction = {
            when (it) {
                CompatAction.Install -> app.updates.install()
                CompatAction.LookForUpdate -> { app.updates.checkNow(); app.server.checkSoon() }
                CompatAction.TryAgain -> app.server.checkSoon()
            }
        },
        onSignOut = {
            scope.launch {
                runCatching { app.api.logout() }.onFailure { Timber.w("logout: %s", it.message) }
                app.prefs.clearSession()
            }
        },
    )
}

/** The page itself: what is wrong, the way out as the main button, and signing out (to use another server). */
@Composable
fun CompatPage(tv: Boolean, words: CompatWords, onAction: (CompatAction) -> Unit, onSignOut: () -> Unit, modifier: Modifier = Modifier) {
    val icon = if (words.action == CompatAction.TryAgain) Icons.Outlined.Dns else Icons.Outlined.SystemUpdate
    val main = remember { FocusRequester() }
    val out = remember { FocusRequester() }
    Column(
        modifier = modifier.fillMaxSize().padding(horizontal = 40.dp, vertical = 24.dp).testTag("compat-page"),
        horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.Center,
    ) {
        Icon(icon, contentDescription = null, tint = RoxyColors.muted, modifier = Modifier.size(if (tv) 64.dp else 56.dp))
        Text(words.title, color = RoxyColors.onBackground, fontSize = if (tv) 26.sp else 22.sp, fontWeight = FontWeight.Bold, textAlign = TextAlign.Center, modifier = Modifier.padding(top = if (tv) 18.dp else 16.dp))
        Text(
            words.body, color = RoxyColors.muted, fontSize = if (tv) 15.sp else 14.sp, lineHeight = if (tv) 22.sp else 21.sp, textAlign = TextAlign.Center,
            modifier = Modifier.padding(top = 8.dp).widthIn(max = 480.dp),
        )
        // The line and the button share one place: a download in progress has no button to press.
        Box(modifier = Modifier.padding(top = 24.dp), contentAlignment = Alignment.Center) {
            val action = words.action
            if (words.button != null && action != null) {
                if (tv) TvButton(words.button, onClick = { onAction(action) }, modifier = Modifier.focusRequester(main).testTag("compat-main"))
                else PlayButton(words.button, icon = if (action == CompatAction.Install) Icons.Outlined.SystemUpdate else Icons.Filled.Refresh, pill = true, modifier = Modifier.testTag("compat-main"), onClick = { onAction(action) })
            } else if (words.progress != null) {
                Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.testTag("compat-progress")) {
                    LoadingCircle(size = 18.dp)
                    Text(words.progress, color = RoxyColors.onBackground, fontSize = if (tv) 16.sp else 14.sp, modifier = Modifier.padding(start = 10.dp))
                }
            }
        }
        if (tv) TvButton("Sign out", onClick = onSignOut, modifier = Modifier.padding(top = 14.dp).focusRequester(out).testTag("compat-signout"))
        else Text(
            "Sign out", color = RoxyColors.muted, fontSize = 14.sp,
            modifier = Modifier.padding(top = 6.dp).clickable(onClick = onSignOut).padding(horizontal = 14.dp, vertical = 12.dp).testTag("compat-signout"),
        )
    }
    // The remote starts on the way out; with none to press (a download running), on Sign out,
    // so that a press of OK is never lost on nothing. It moves back when the button returns.
    if (tv) LaunchedEffect(words.button != null) { runCatching { (if (words.button != null) main else out).requestFocus() } }
}
