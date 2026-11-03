package tv.roxy.tv.ui.setup

import android.content.Intent
import android.net.Uri
import android.os.Build
import android.provider.Settings
import androidx.activity.compose.BackHandler
import androidx.compose.animation.AnimatedContent
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.slideInHorizontally
import androidx.compose.animation.slideOutHorizontally
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.ArrowForward
import androidx.compose.material.icons.automirrored.outlined.OpenInNew
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.outlined.Circle
import androidx.compose.material.icons.outlined.Lightbulb
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CheckboxDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.launch
import tv.roxy.tv.BuildConfig
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.ApiException
import tv.roxy.tv.data.Session
import tv.roxy.tv.isTelevision
import tv.roxy.tv.ui.components.RoxyTextField
import tv.roxy.tv.ui.phone.GreyButton
import tv.roxy.tv.ui.phone.MediaFoldersCard
import tv.roxy.tv.ui.phone.OnBrand
import tv.roxy.tv.ui.phone.PhoneAddFolder
import tv.roxy.tv.ui.phone.PlayButton
import tv.roxy.tv.ui.phone.WitchHat
import tv.roxy.tv.ui.theme.RoxyColors

internal const val STEP_WELCOME = 0
internal const val STEP_ACCOUNT = 1
internal const val STEP_DETAILS = 2
internal const val STEP_MEDIA = 3
internal const val STEP_DONE = 4

internal fun apiError(t: Throwable): String {
    val m = t.message ?: return "Something went wrong."
    return Regex(""""error":"([^"]+)"""").find(m)?.groupValues?.get(1)?.replaceFirstChar { it.uppercase() }
        ?: if (t is ApiException) m else "Can't reach the server: $m"
}

/**
 * First-time setup: someone's first look at Roxy TV. Welcome (the hat and the name),
 * the admin account, TMDB for posters and Discover, the media folders with naming tips,
 * then "You're all set". Before the account exists it runs from the sign-in screen
 * ([serverUrl]); after, the app is signed in with [Session.setupInProgress] set and comes
 * back here (even after a restart) until Finish.
 */
@Composable
fun SetupWizard(serverUrl: String, serverName: String, onCancel: (() -> Unit)? = null) {
    val context = LocalContext.current
    // A TV gets its own layout for the remote (TvSetupWizard.kt): same steps, same calls.
    if (remember { isTelevision(context) }) {
        TvSetupWizard(serverUrl, serverName, onCancel)
        return
    }
    val app = RoxyApp.get(context)
    val session = app.prefs.session.value
    val signedIn = session.isLoggedIn && session.setupInProgress
    var step by rememberSaveable { mutableIntStateOf(if (signedIn) STEP_DETAILS else STEP_WELCOME) }
    var browsing by remember { mutableStateOf(false) }
    var readingTerms by remember { mutableStateOf(false) }
    val back: (() -> Unit)? = when (step) {
        STEP_WELCOME -> onCancel
        STEP_ACCOUNT -> ({ step = STEP_WELCOME })
        STEP_DETAILS -> ({ step = STEP_ACCOUNT })
        STEP_MEDIA -> ({ step = STEP_DETAILS })
        else -> ({ step = STEP_MEDIA })
    }
    // The phone's back gesture moves between steps (it never leaves the app mid-setup).
    BackHandler(enabled = !browsing && !readingTerms) { back?.invoke() }

    if (readingTerms) {
        TermsPage(serverUrl, onBack = { readingTerms = false })
        return
    }
    if (browsing) {
        PhoneAddFolder(onBack = { browsing = false })
        return
    }

    Box(modifier = Modifier.fillMaxSize().background(RoxyColors.background)) {
        Glow()
        Column(modifier = Modifier.fillMaxSize().statusBarsPadding().navigationBarsPadding().imePadding()) {
            if (step != STEP_WELCOME) StepHeader(step, onBack = back)
            AnimatedContent(
                targetState = step, label = "setup",
                transitionSpec = {
                    val dir = if (targetState > initialState) 1 else -1
                    (slideInHorizontally(tween(320)) { it * dir / 4 } + fadeIn(tween(320))) togetherWith
                        (slideOutHorizontally(tween(240)) { -it * dir / 4 } + fadeOut(tween(200)))
                },
                modifier = Modifier.fillMaxSize(),
            ) { s ->
                Column(
                    modifier = Modifier.fillMaxSize().verticalScroll(rememberScrollState()),
                    horizontalAlignment = Alignment.CenterHorizontally,
                ) {
                    Column(modifier = Modifier.widthIn(max = 480.dp).fillMaxWidth().padding(horizontal = 26.dp, vertical = 12.dp)) {
                        when (s) {
                            STEP_WELCOME -> Welcome(serverName, onStart = { step = STEP_ACCOUNT }, onCancel = onCancel)
                            STEP_ACCOUNT -> AccountStep(serverUrl, onDone = { step = STEP_DETAILS }, onReadTerms = { readingTerms = true })
                            STEP_DETAILS -> DetailsStep(onNext = { step = STEP_MEDIA })
                            STEP_MEDIA -> MediaStep(onAdd = { browsing = true }, onNext = { step = STEP_DONE })
                            else -> DoneStep(onFinish = { app.scope.launch { app.prefs.setSetupInProgress(false) } })
                        }
                    }
                }
            }
        }
    }
}

/** The soft blue light behind everything, slowly breathing. */
@Composable
private fun Glow() {
    val t = rememberInfiniteTransition(label = "glow")
    val a by t.animateFloat(0.16f, 0.26f, infiniteRepeatable(tween(3600, easing = FastOutSlowInEasing), RepeatMode.Reverse), label = "a")
    Box(
        modifier = Modifier.fillMaxWidth().height(520.dp)
            .background(Brush.radialGradient(listOf(RoxyColors.accent.copy(alpha = a), Color.Transparent), radius = 1000f)),
    )
}

/** Back arrow, then four short bars for Account, Details, Media, Done. */
@Composable
private fun StepHeader(step: Int, onBack: (() -> Unit)?) {
    Row(modifier = Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
        Box(modifier = Modifier.size(44.dp), contentAlignment = Alignment.Center) {
            if (onBack != null) Icon(Icons.AutoMirrored.Filled.ArrowBack, "Back", tint = Color.White, modifier = Modifier.size(26.dp).clip(CircleShape).clickable(onClick = onBack))
        }
        Row(modifier = Modifier.weight(1f).padding(horizontal = 12.dp), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            for (i in STEP_ACCOUNT..STEP_DONE) {
                Box(
                    modifier = Modifier.weight(1f).height(4.dp).clip(RoundedCornerShape(2.dp))
                        .background(if (i <= step) RoxyColors.accent else Color.White.copy(alpha = 0.15f)),
                )
            }
        }
        Text("${step} of 4", fontSize = 12.sp, color = RoxyColors.muted, modifier = Modifier.width(44.dp), textAlign = TextAlign.End)
    }
}

@Composable
private fun Welcome(serverName: String, onStart: () -> Unit, onCancel: (() -> Unit)?) {
    val t = rememberInfiniteTransition(label = "hat")
    val float by t.animateFloat(-6f, 6f, infiniteRepeatable(tween(2600, easing = FastOutSlowInEasing), RepeatMode.Reverse), label = "f")
    Column(modifier = Modifier.fillMaxWidth().padding(top = 56.dp), horizontalAlignment = Alignment.CenterHorizontally) {
        WitchHat(size = 132.dp, modifier = Modifier.offset(y = float.dp))
        Text(
            "Roxy TV", fontSize = 46.sp, fontWeight = FontWeight.ExtraBold, letterSpacing = 2.sp, modifier = Modifier.padding(top = 22.dp),
            style = TextStyle(brush = Brush.linearGradient(listOf(Color.White, RoxyColors.accentLight))),
        )
        Text("Your shows and movies, beautifully.", fontSize = 16.sp, color = RoxyColors.muted, modifier = Modifier.padding(top = 6.dp))
        Row(
            modifier = Modifier.padding(top = 30.dp).clip(RoundedCornerShape(20.dp)).background(Color.White.copy(alpha = 0.06f))
                .border(1.dp, Color.White.copy(alpha = 0.08f), RoundedCornerShape(20.dp)).padding(horizontal = 14.dp, vertical = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Box(Modifier.size(8.dp).clip(CircleShape).background(Color(0xFF4ADE80)))
            Text("New server found: $serverName", fontSize = 13.sp, color = Color.White.copy(alpha = 0.85f), modifier = Modifier.padding(start = 8.dp))
        }
        Text(
            "Let's get it ready. It takes about two minutes: an account for you, where posters come from, and where your media lives.",
            fontSize = 15.sp, lineHeight = 22.sp, color = RoxyColors.onBackground.copy(alpha = 0.85f), textAlign = TextAlign.Center,
            modifier = Modifier.padding(top = 26.dp, bottom = 34.dp),
        )
        PlayButton("Get started", icon = Icons.AutoMirrored.Filled.ArrowForward, height = 54.dp, pill = true, container = RoxyColors.accent, content = OnBrand, modifier = Modifier.fillMaxWidth(), onClick = onStart)
        if (onCancel != null) Text(
            "Not your server? Go back", fontSize = 13.sp, color = RoxyColors.muted,
            modifier = Modifier.padding(top = 18.dp).clickable(onClick = onCancel).padding(6.dp),
        )
    }
}

@Composable
private fun StepTitle(title: String, body: String) {
    Text(title, fontSize = 28.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(top = 18.dp))
    Text(body, fontSize = 15.sp, lineHeight = 22.sp, color = RoxyColors.muted, modifier = Modifier.padding(top = 8.dp, bottom = 24.dp))
}

@Composable
private fun AccountStep(serverUrl: String, onDone: () -> Unit, onReadTerms: () -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    val session by app.prefs.session.collectAsStateWithLifecycle()
    // Back here after creating it: the server's setup door is shut (it can't be made twice),
    // so show what was made instead of a form that would be refused.
    if (session.isLoggedIn && session.setupInProgress) {
        StepTitle("Your account", "Created and signed in. You can change the password later in Account › Change password.")
        Card {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Icon(Icons.Filled.CheckCircle, null, tint = Color(0xFF4ADE80), modifier = Modifier.size(22.dp))
                Column(modifier = Modifier.padding(start = 10.dp)) {
                    Text(session.userName, fontSize = 16.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground)
                    Text("Admin", fontSize = 13.sp, color = RoxyColors.accent)
                }
            }
        }
        PlayButton(
            "Continue", icon = Icons.AutoMirrored.Filled.ArrowForward, height = 52.dp, pill = true, container = RoxyColors.accent, content = OnBrand,
            modifier = Modifier.fillMaxWidth().padding(top = 22.dp), onClick = onDone,
        )
        return
    }
    val scope = rememberCoroutineScope()
    var name by rememberSaveable { mutableStateOf("") }
    var pw by rememberSaveable { mutableStateOf("") }
    var pw2 by rememberSaveable { mutableStateOf("") }
    var agreed by rememberSaveable { mutableStateOf(false) }
    var busy by remember { mutableStateOf(false) }
    var err by remember { mutableStateOf<String?>(null) }
    StepTitle("Create your account", "You're the admin: you'll manage the server and can add your family later, each with their own account and profiles.")
    Column(verticalArrangement = Arrangement.spacedBy(14.dp)) {
        RoxyTextField(name, { name = it; err = null }, label = "Your name (what you'll sign in with)")
        RoxyTextField(pw, { pw = it; err = null }, label = "Password", password = true)
        RoxyTextField(pw2, { pw2 = it; err = null }, label = "Password again", password = true, imeAction = ImeAction.Done)
        PasswordHint(pw, pw2)
        Row(verticalAlignment = Alignment.CenterVertically) {
            Checkbox(checked = agreed, onCheckedChange = { agreed = it }, colors = CheckboxDefaults.colors(checkedColor = RoxyColors.accent, uncheckedColor = RoxyColors.muted, checkmarkColor = OnBrand))
            Text("I agree to the ", fontSize = 14.sp, color = RoxyColors.onBackground, modifier = Modifier.clickable { agreed = !agreed })
            Text("Terms of Use", fontSize = 14.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.accent, modifier = Modifier.clickable(onClick = onReadTerms).padding(vertical = 6.dp))
        }
        err?.let { Text(it, fontSize = 13.sp, color = RoxyColors.error) }
        val ready = name.isNotBlank() && pw.length >= 8 && pw == pw2 && agreed && !busy
        PlayButton(
            if (busy) "Creating…" else "Create account", icon = Icons.Filled.Check, height = 52.dp, pill = true,
            container = if (ready) RoxyColors.accent else RoxyColors.accent.copy(alpha = 0.3f), content = if (ready) OnBrand else OnBrand.copy(alpha = 0.6f),
            modifier = Modifier.fillMaxWidth().padding(top = 6.dp),
            onClick = {
                if (!ready) return@PlayButton
                busy = true
                scope.launch {
                    val deviceId = Settings.Secure.getString(app.contentResolver, Settings.Secure.ANDROID_ID) ?: "unknown"
                    runCatching { app.api.setupAdmin(serverUrl, name.trim(), pw, "${Build.MANUFACTURER} ${Build.MODEL}", deviceId, BuildConfig.VERSION_NAME) }
                        .onSuccess { resp ->
                            val terms = runCatching { app.api.legalAt(serverUrl, "terms").version }.getOrDefault("")
                            app.prefs.saveSession(Session(serverUrl = serverUrl.trimEnd('/'), token = resp.token, userName = resp.user.name, isAdmin = true, acceptedTerms = terms, setupInProgress = true))
                            onDone()
                        }
                        .onFailure { err = apiError(it) }
                    busy = false
                }
            },
        )
    }
}

@Composable
internal fun PasswordHint(pw: String, pw2: String) {
    val (text, color) = when {
        pw.isEmpty() -> "At least 8 characters. A short sentence is easy to remember and hard to guess." to RoxyColors.muted
        pw.length < 8 -> "${8 - pw.length} more character${if (8 - pw.length == 1) "" else "s"}" to RoxyColors.muted
        pw2.isNotEmpty() && pw != pw2 -> "The two passwords don't match yet" to RoxyColors.error
        pw.length >= 14 -> "Strong password" to Color(0xFF4ADE80)
        else -> "Good. Longer is even better." to RoxyColors.accent
    }
    Text(text, fontSize = 12.sp, color = color)
}

@Composable
private fun DetailsStep(onNext: () -> Unit) {
    val context = LocalContext.current
    val app = RoxyApp.get(context)
    val scope = rememberCoroutineScope()
    var has by remember { mutableStateOf<Boolean?>(null) }
    var key by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var err by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(Unit) { has = runCatching { app.api.tmdbSettings().set }.getOrDefault(false) }
    StepTitle(
        "Posters and details",
        "Roxy names your files and fetches posters, descriptions, cast and the Discover page from TMDB. It's free, but every server needs its own key.",
    )
    Card {
        if (has == true) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Icon(Icons.Filled.CheckCircle, null, tint = Color(0xFF4ADE80), modifier = Modifier.size(22.dp))
                Text("This server already has a TMDB key.", fontSize = 15.sp, color = RoxyColors.onBackground, modifier = Modifier.padding(start = 10.dp))
            }
            Text("You can paste a different one below, or just continue.", fontSize = 13.sp, color = RoxyColors.muted, modifier = Modifier.padding(top = 6.dp))
        } else {
            Text("Get a free key", fontSize = 16.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground)
            Numbered(1, "Make a free account at themoviedb.org")
            Numbered(2, "Open Settings › API and create a key (choose \"Personal\")")
            Numbered(3, "Copy the API Key (or the longer Read Access Token) and paste it here")
            Row(
                modifier = Modifier.padding(top = 12.dp).clip(RoundedCornerShape(10.dp)).clickable {
                    runCatching { context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse("https://www.themoviedb.org/settings/api"))) }
                }.padding(vertical = 6.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text("Open TMDB", fontSize = 15.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.accent)
                Icon(Icons.AutoMirrored.Outlined.OpenInNew, null, tint = RoxyColors.accent, modifier = Modifier.padding(start = 6.dp).size(18.dp))
            }
        }
    }
    Spacer(Modifier.height(16.dp))
    RoxyTextField(key, { key = it.trim(); err = null }, label = "TMDB API key", imeAction = ImeAction.Done)
    err?.let { Text(it, fontSize = 13.sp, color = RoxyColors.error, modifier = Modifier.padding(top = 8.dp)) }
    Tip("Anime details come from AniList, which needs no key at all.")
    PlayButton(
        when { busy -> "Checking…"; key.isNotEmpty() -> "Check and continue"; has == true -> "Continue"; else -> "Paste a key to continue" },
        icon = Icons.AutoMirrored.Filled.ArrowForward, height = 52.dp, pill = true,
        container = if (key.isNotEmpty() || has == true) RoxyColors.accent else RoxyColors.accent.copy(alpha = 0.3f),
        content = if (key.isNotEmpty() || has == true) OnBrand else OnBrand.copy(alpha = 0.6f),
        modifier = Modifier.fillMaxWidth().padding(top = 20.dp),
        onClick = {
            when {
                busy -> {}
                key.isNotEmpty() -> {
                    busy = true
                    scope.launch {
                        runCatching { app.api.saveTmdbKey(key) }.onSuccess { onNext() }.onFailure { err = apiError(it) }
                        busy = false
                    }
                }
                has == true -> onNext()
            }
        },
    )
    // No skip: without a key the library has no posters, titles or Discover, which is no first impression.
    if (has != true) Text(
        "A key is needed to continue. It takes about two minutes and never costs anything.", fontSize = 13.sp, lineHeight = 18.sp,
        color = RoxyColors.muted, textAlign = TextAlign.Center, modifier = Modifier.fillMaxWidth().padding(top = 14.dp, start = 6.dp, end = 6.dp),
    )
}

@Composable
private fun MediaStep(onAdd: () -> Unit, onNext: () -> Unit) {
    var reload by remember { mutableIntStateOf(0) }
    LaunchedEffect(Unit) { reload++ } // back from the folder browser: show what was added
    StepTitle("Your media", "Point Roxy at the folders that hold your shows and movies. It only ever reads them; nothing is moved or deleted.")
    MediaFoldersCard(reloadKey = reload, onAdd = onAdd, onChanged = { reload++ })
    Spacer(Modifier.height(18.dp))
    Card {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Icon(Icons.Outlined.Lightbulb, null, tint = Color(0xFFF2C35B), modifier = Modifier.size(20.dp))
            Text("Name things like this", fontSize = 16.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground, modifier = Modifier.padding(start = 8.dp))
        }
        Text("Roxy finds the right show, season and episode from the names:", fontSize = 13.sp, color = RoxyColors.muted, modifier = Modifier.padding(top = 6.dp, bottom = 10.dp))
        Tree(
            listOf(
                0 to "Shows/",
                1 to "Frieren (2023)/",
                2 to "Season 01/",
                3 to "Frieren (2023) - S01E01.mkv",
                3 to "Frieren (2023) - S01E02.mkv",
                0 to "Movies/",
                1 to "Spirited Away (2001)/",
                2 to "Spirited Away (2001).mkv",
            ),
        )
        Bullet("One folder per show or movie, with the year in brackets")
        Bullet("Episodes as S01E01; anime numbered 1, 2, 3… works too")
        Bullet("Shows and movies can share a folder, or keep their own")
        Bullet("Sonarr and Radarr already name everything this way")
        Bullet("Custom shows (fan edits like One Pace) can bring a tvshow.nfo")
    }
    PlayButton(
        "Continue", icon = Icons.AutoMirrored.Filled.ArrowForward, height = 52.dp, pill = true, container = RoxyColors.accent, content = OnBrand,
        modifier = Modifier.fillMaxWidth().padding(top = 22.dp), onClick = onNext,
    )
    Text(
        "You can add or change folders any time in Account › Library.", fontSize = 12.sp, color = RoxyColors.muted, textAlign = TextAlign.Center,
        modifier = Modifier.fillMaxWidth().padding(top = 12.dp, bottom = 24.dp),
    )
}

@Composable
private fun DoneStep(onFinish: () -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    var tmdb by remember { mutableStateOf<Boolean?>(null) }
    var folders by remember { mutableStateOf<Int?>(null) }
    LaunchedEffect(Unit) {
        tmdb = runCatching { app.api.tmdbSettings().set }.getOrDefault(false)
        folders = runCatching { app.api.mediaFolders().size }.getOrDefault(0)
    }
    Column(modifier = Modifier.fillMaxWidth().padding(top = 30.dp), horizontalAlignment = Alignment.CenterHorizontally) {
        WitchHat(size = 96.dp)
        Text("You're all set", fontSize = 30.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(top = 18.dp))
        Text("Roxy is scanning your folders now. Shows appear on Home as they're found.", fontSize = 15.sp, lineHeight = 22.sp, color = RoxyColors.muted, textAlign = TextAlign.Center, modifier = Modifier.padding(top = 8.dp))
    }
    Card(modifier = Modifier.padding(top = 26.dp)) {
        Check(true, "Your admin account")
        Check(tmdb == true, if (tmdb == true) "Posters and details from TMDB" else "TMDB key: add one in Settings for posters and Discover")
        Check((folders ?: 0) > 0, when (folders) { null -> "Media folders"; 0 -> "No media folders yet: add them in Account › Library"; 1 -> "1 media folder"; else -> "$folders media folders" })
    }
    Card(modifier = Modifier.padding(top = 14.dp)) {
        Text("Next, when you like", fontSize = 15.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground)
        Bullet("Connect Sonarr and Radarr to request shows from Discover (Account › Sonarr & Radarr)")
        Bullet("Add your family in Account › Users")
    }
    PlayButton(
        "Start watching", icon = Icons.Filled.PlayArrow, height = 54.dp, pill = true, container = RoxyColors.accent, content = OnBrand,
        modifier = Modifier.fillMaxWidth().padding(top = 26.dp, bottom = 30.dp), onClick = onFinish,
    )
}

@Composable
internal fun Card(modifier: Modifier = Modifier, content: @Composable () -> Unit) {
    Column(
        modifier = modifier.fillMaxWidth().clip(RoundedCornerShape(18.dp)).background(RoxyColors.surface)
            .border(1.dp, Color.White.copy(alpha = 0.06f), RoundedCornerShape(18.dp)).padding(18.dp),
    ) { content() }
}

@Composable
internal fun Numbered(n: Int, text: String) {
    Row(modifier = Modifier.padding(top = 10.dp), verticalAlignment = Alignment.Top) {
        Box(modifier = Modifier.size(22.dp).clip(CircleShape).background(RoxyColors.accent.copy(alpha = 0.18f)), contentAlignment = Alignment.Center) {
            Text("$n", fontSize = 12.sp, fontWeight = FontWeight.Bold, color = RoxyColors.accent)
        }
        Text(text, fontSize = 14.sp, lineHeight = 20.sp, color = RoxyColors.onBackground.copy(alpha = 0.9f), modifier = Modifier.padding(start = 10.dp))
    }
}

@Composable
internal fun Bullet(text: String) {
    Row(modifier = Modifier.padding(top = 8.dp), verticalAlignment = Alignment.Top) {
        Text("•", fontSize = 14.sp, color = RoxyColors.accent, modifier = Modifier.padding(end = 8.dp))
        Text(text, fontSize = 13.sp, lineHeight = 19.sp, color = RoxyColors.onBackground.copy(alpha = 0.85f))
    }
}

@Composable
internal fun Tip(text: String) {
    Text(text, fontSize = 12.sp, lineHeight = 17.sp, color = RoxyColors.muted, modifier = Modifier.padding(top = 10.dp))
}

@Composable
internal fun Check(ok: Boolean, text: String) {
    Row(modifier = Modifier.padding(vertical = 6.dp), verticalAlignment = Alignment.CenterVertically) {
        Icon(if (ok) Icons.Filled.CheckCircle else Icons.Outlined.Circle, null, tint = if (ok) Color(0xFF4ADE80) else RoxyColors.muted, modifier = Modifier.size(20.dp))
        Text(text, fontSize = 14.sp, color = if (ok) RoxyColors.onBackground else RoxyColors.muted, modifier = Modifier.padding(start = 10.dp))
    }
}

/** A folder tree in a monospaced box; files in the brand colour. */
@Composable
internal fun Tree(lines: List<Pair<Int, String>>) {
    Column(
        modifier = Modifier.fillMaxWidth().clip(RoundedCornerShape(10.dp)).background(Color.Black.copy(alpha = 0.35f)).padding(12.dp),
    ) {
        lines.forEach { (depth, name) ->
            val file = !name.endsWith("/")
            Text(
                "  ".repeat(depth) + (if (depth > 0) "└ " else "") + name,
                fontFamily = FontFamily.Monospace, fontSize = 12.sp, lineHeight = 18.sp,
                color = if (file) RoxyColors.accentLight else Color.White.copy(alpha = 0.85f), maxLines = 1,
            )
        }
    }
}

/** The server's Terms of Use on a page of their own (read before agreeing). */
@Composable
private fun TermsPage(serverUrl: String, onBack: () -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    var body by remember { mutableStateOf<String?>(null) }
    var err by remember { mutableStateOf<String?>(null) }
    BackHandler(onBack = onBack)
    LaunchedEffect(serverUrl) {
        runCatching { app.api.legalAt(serverUrl, "terms") }.onSuccess { body = it.body }.onFailure { err = "Couldn't load the terms from the server: ${it.message}" }
    }
    Column(modifier = Modifier.fillMaxSize().background(RoxyColors.background).statusBarsPadding().navigationBarsPadding()) {
        Row(modifier = Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
            Icon(Icons.AutoMirrored.Filled.ArrowBack, "Back", tint = Color.White, modifier = Modifier.size(44.dp).clip(CircleShape).clickable(onClick = onBack).padding(9.dp))
            Text("Terms of Use", fontSize = 20.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(start = 8.dp))
        }
        Column(modifier = Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(horizontal = 24.dp, vertical = 8.dp)) {
            val b = body
            when {
                b != null -> Text(b.replace("**", ""), fontSize = 14.sp, lineHeight = 21.sp, color = RoxyColors.onBackground.copy(alpha = 0.9f))
                err != null -> Text(err ?: "", fontSize = 14.sp, color = RoxyColors.error)
                else -> Text("Loading…", fontSize = 14.sp, color = RoxyColors.muted)
            }
            Spacer(Modifier.height(32.dp))
        }
    }
}
