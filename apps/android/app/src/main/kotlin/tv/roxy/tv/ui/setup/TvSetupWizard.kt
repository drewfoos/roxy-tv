package tv.roxy.tv.ui.setup

import android.os.Build
import android.provider.Settings
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.focusable
import androidx.compose.foundation.gestures.animateScrollBy
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.collectIsFocusedAsState
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.ArrowForward
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.ChevronRight
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.RadioButtonChecked
import androidx.compose.material.icons.filled.RadioButtonUnchecked
import androidx.compose.material.icons.outlined.CheckBox
import androidx.compose.material.icons.outlined.CheckBoxOutlineBlank
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material.icons.outlined.Folder
import androidx.compose.material.icons.outlined.FolderOff
import androidx.compose.material.icons.outlined.Key
import androidx.compose.material.icons.outlined.Lightbulb
import androidx.compose.material.icons.outlined.Lock
import androidx.compose.material.icons.outlined.Person
import androidx.compose.material.icons.outlined.Visibility
import androidx.compose.material.icons.outlined.VisibilityOff
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.onPreviewKeyEvent
import androidx.compose.ui.input.key.type
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.launch
import tv.roxy.tv.BuildConfig
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.FolderEntry
import tv.roxy.tv.api.MediaFolder
import tv.roxy.tv.data.Session
import tv.roxy.tv.ui.login.CardButton
import tv.roxy.tv.ui.phone.OnBrand
import tv.roxy.tv.ui.phone.WitchHat
import tv.roxy.tv.ui.theme.RoxyColors
import tv.roxy.tv.ui.tv.TvFocusable
import tv.roxy.tv.ui.tv.TvTextField

/*
 * The setup wizard on a TV: the phone's steps (Welcome, Account, Details, Media, Done) laid
 * out for 960x540 dp and a D-pad. Each step opens with focus on its main control, Back is
 * the previous step (and out of the wizard from Welcome), text fields open the keyboard on
 * OK and its Next key moves on to the next field. The two-column steps keep the reading
 * at the left and every control in one column at the right, so Up / Down always means
 * "previous / next control". The folder browser and the Terms are pages of their own.
 */

/** The TV's overscan margin: 5% of the screen on each side, as Google's TV guidelines ask. */
private val SafeH = 48.dp
private val SafeV = 27.dp
/** The widest the content gets: the two columns, or the browser's list. */
private val ContentWidth = 860.dp

/** Focus [req] once it is attached (a step's controls appear a frame or two after it does). */
@Composable
private fun FocusOnStart(req: FocusRequester, vararg keys: Any?) {
    LaunchedEffect(*keys) { repeat(8) { withFrameNanos { }; if (runCatching { req.requestFocus() }.getOrDefault(false)) return@LaunchedEffect } }
}

private suspend fun focusSoon(req: FocusRequester) {
    repeat(8) { withFrameNanos { }; if (runCatching { req.requestFocus() }.getOrDefault(false)) return }
}

@Composable
internal fun TvSetupWizard(serverUrl: String, serverName: String, onCancel: (() -> Unit)?) {
    val app = RoxyApp.get(LocalContext.current)
    val session = app.prefs.session.value
    val signedIn = session.isLoggedIn && session.setupInProgress
    var step by rememberSaveable { mutableIntStateOf(if (signedIn) STEP_DETAILS else STEP_WELCOME) }
    var browsing by remember { mutableStateOf(false) }
    var readingTerms by remember { mutableStateOf(false) }
    var editingFolder by remember { mutableStateOf<MediaFolder?>(null) }
    // The account form lives here, not in its step: opening the Terms takes the step off
    // screen, and the name and passwords must still be there on the way back.
    var name by rememberSaveable { mutableStateOf("") }
    var pw by rememberSaveable { mutableStateOf("") }
    var pw2 by rememberSaveable { mutableStateOf("") }
    var agreed by rememberSaveable { mutableStateOf(false) }
    var backFromTerms by remember { mutableStateOf(false) }
    // Bumped when a folder is added, changed or removed: the Media step reloads and focuses Continue.
    var foldersChanged by remember { mutableIntStateOf(0) }
    val back: (() -> Unit)? = when (step) {
        STEP_WELCOME -> onCancel
        STEP_ACCOUNT -> ({ step = STEP_WELCOME })
        STEP_DETAILS -> ({ step = STEP_ACCOUNT })
        STEP_MEDIA -> ({ step = STEP_DETAILS })
        else -> ({ step = STEP_MEDIA })
    }
    // Back walks the steps, as on the phone (it never leaves the app mid-setup).
    BackHandler(enabled = !browsing && !readingTerms && editingFolder == null) { back?.invoke() }

    Box(modifier = Modifier.fillMaxSize().background(RoxyColors.background)) {
        // The phone's blue glow, held still: an endless animation is a frame drain on a TV.
        Box(
            modifier = Modifier.fillMaxWidth().height(420.dp).align(Alignment.TopCenter)
                .background(Brush.radialGradient(listOf(RoxyColors.accent.copy(alpha = 0.18f), Color.Transparent), radius = 1400f)),
        )
        val folder = editingFolder
        when {
            readingTerms -> TvTermsPage(
                serverUrl,
                onBack = { readingTerms = false; backFromTerms = true },
                onAgree = { agreed = true; readingTerms = false; backFromTerms = true },
            )
            browsing -> TvFolderBrowser(onBack = { browsing = false }, onAdded = { browsing = false; foldersChanged++ })
            folder != null -> TvFolderEdit(folder, onClose = { changed -> editingFolder = null; if (changed) foldersChanged++ })
            else -> Column(modifier = Modifier.fillMaxSize().padding(horizontal = SafeH, vertical = SafeV), horizontalAlignment = Alignment.CenterHorizontally) {
                if (step != STEP_WELCOME) TvStepHeader(step)
                Box(modifier = Modifier.fillMaxWidth().weight(1f), contentAlignment = Alignment.Center) {
                    // No slide between steps on a TV: one clean swap, and focus lands at once.
                    key(step) {
                        when (step) {
                            STEP_WELCOME -> TvWelcome(serverName, onStart = { step = STEP_ACCOUNT }, onCancel = onCancel)
                            STEP_ACCOUNT -> TvAccountStep(
                                serverUrl,
                                name, { name = it }, pw, { pw = it }, pw2, { pw2 = it }, agreed, { agreed = it },
                                backFromTerms = backFromTerms,
                                onDone = { backFromTerms = false; step = STEP_DETAILS },
                                onReadTerms = { readingTerms = true },
                            )
                            STEP_DETAILS -> TvDetailsStep(onNext = { step = STEP_MEDIA })
                            STEP_MEDIA -> TvMediaStep(
                                changed = foldersChanged,
                                onAdd = { browsing = true },
                                onEdit = { editingFolder = it },
                                onNext = { step = STEP_DONE },
                            )
                            else -> TvDoneStep(onFinish = { app.scope.launch { app.prefs.setSetupInProgress(false) } })
                        }
                    }
                }
            }
        }
    }
}

/** The hat and "Set up Roxy TV", then four bars for Account, Details, Media, Done. */
@Composable
private fun TvStepHeader(step: Int) {
    Row(modifier = Modifier.widthIn(max = ContentWidth).fillMaxWidth().padding(bottom = 14.dp), verticalAlignment = Alignment.CenterVertically) {
        WitchHat(size = 30.dp)
        Text("Set up Roxy TV", fontSize = 18.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(start = 10.dp))
        Spacer(Modifier.weight(1f))
        Row(modifier = Modifier.width(240.dp), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            for (i in STEP_ACCOUNT..STEP_DONE) {
                Box(
                    modifier = Modifier.weight(1f).height(5.dp).clip(RoundedCornerShape(3.dp))
                        .background(if (i <= step) RoxyColors.accent else Color.White.copy(alpha = 0.15f)),
                )
            }
        }
        Text("Step $step of 4", fontSize = 14.sp, color = RoxyColors.muted, modifier = Modifier.padding(start = 14.dp))
    }
}

/**
 * Reading at the left, controls at the right. The right column scrolls by itself as focus
 * moves down it; the left one is only text.
 */
@Composable
private fun TwoColumns(title: String, body: String, left: @Composable ColumnScope.() -> Unit = {}, right: @Composable ColumnScope.() -> Unit) {
    Row(modifier = Modifier.widthIn(max = ContentWidth).fillMaxWidth().fillMaxHeight(), horizontalArrangement = Arrangement.spacedBy(48.dp)) {
        Column(modifier = Modifier.weight(1f).fillMaxHeight().verticalScroll(rememberScrollState())) {
            Text(title, fontSize = 30.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(top = 10.dp))
            Text(body, fontSize = 16.sp, lineHeight = 23.sp, color = RoxyColors.muted, modifier = Modifier.padding(top = 10.dp, bottom = 18.dp))
            left()
        }
        Column(
            modifier = Modifier.weight(1.1f).fillMaxHeight().verticalScroll(rememberScrollState()).padding(top = 10.dp, bottom = 10.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) { right() }
    }
}

@Composable
private fun TvWelcome(serverName: String, onStart: () -> Unit, onCancel: (() -> Unit)?) {
    val first = remember { FocusRequester() }
    FocusOnStart(first, Unit)
    Column(modifier = Modifier.width(620.dp).verticalScroll(rememberScrollState()), horizontalAlignment = Alignment.CenterHorizontally) {
        WitchHat(size = 96.dp)
        Text(
            "Roxy TV", fontSize = 44.sp, fontWeight = FontWeight.ExtraBold, letterSpacing = 2.sp, modifier = Modifier.padding(top = 14.dp),
            style = TextStyle(brush = Brush.linearGradient(listOf(Color.White, RoxyColors.accentLight))),
        )
        Text("Your shows and movies, beautifully.", fontSize = 17.sp, color = RoxyColors.muted, modifier = Modifier.padding(top = 4.dp))
        Row(
            modifier = Modifier.padding(top = 20.dp).clip(RoundedCornerShape(20.dp)).background(Color.White.copy(alpha = 0.06f))
                .border(1.dp, Color.White.copy(alpha = 0.08f), RoundedCornerShape(20.dp)).padding(horizontal = 16.dp, vertical = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Box(Modifier.size(9.dp).clip(CircleShape).background(Color(0xFF4ADE80)))
            Text("New server found: $serverName", fontSize = 15.sp, color = Color.White.copy(alpha = 0.85f), modifier = Modifier.padding(start = 8.dp))
        }
        Text(
            "Let's get it ready. It takes a few minutes: an account for you, where posters come from, and where your media lives.",
            fontSize = 16.sp, lineHeight = 23.sp, color = RoxyColors.onBackground.copy(alpha = 0.85f), textAlign = TextAlign.Center,
            modifier = Modifier.padding(top = 18.dp, bottom = 24.dp),
        )
        CardButton("Get started", primary = true, icon = Icons.AutoMirrored.Filled.ArrowForward, onClick = onStart, modifier = Modifier.width(380.dp).focusRequester(first))
        if (onCancel != null) {
            Spacer(Modifier.height(12.dp))
            CardButton("Not your server? Go back", primary = false, icon = Icons.AutoMirrored.Filled.ArrowBack, onClick = onCancel, modifier = Modifier.width(380.dp))
        }
    }
}

@Composable
private fun TvAccountStep(
    serverUrl: String,
    name: String, onName: (String) -> Unit,
    pw: String, onPw: (String) -> Unit,
    pw2: String, onPw2: (String) -> Unit,
    agreed: Boolean, onAgreed: (Boolean) -> Unit,
    backFromTerms: Boolean,
    onDone: () -> Unit, onReadTerms: () -> Unit,
) {
    val app = RoxyApp.get(LocalContext.current)
    val session by app.prefs.session.collectAsStateWithLifecycle()
    val first = remember { FocusRequester() }
    // Back here after creating it: the server's setup door is shut, so show what was made.
    if (session.isLoggedIn && session.setupInProgress) {
        FocusOnStart(first, Unit)
        TwoColumns("Your account", "Created and signed in. You can change the password later in Settings.") {
            Card {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Icon(Icons.Filled.CheckCircle, null, tint = Color(0xFF4ADE80), modifier = Modifier.size(24.dp))
                    Column(modifier = Modifier.padding(start = 12.dp)) {
                        Text(session.userName, fontSize = 18.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground)
                        Text("Admin", fontSize = 14.sp, color = RoxyColors.accent)
                    }
                }
            }
            CardButton("Continue", primary = true, icon = Icons.AutoMirrored.Filled.ArrowForward, onClick = onDone, modifier = Modifier.focusRequester(first))
        }
        return
    }
    val scope = rememberCoroutineScope()
    var showPw by remember { mutableStateOf(false) }
    var busy by remember { mutableStateOf(false) }
    var err by remember { mutableStateOf<String?>(null) }
    var pwEdit by remember { mutableIntStateOf(0) }
    var pw2Edit by remember { mutableIntStateOf(0) }
    val agreeReq = remember { FocusRequester() }
    val createReq = remember { FocusRequester() }
    // Back from the Terms: focus returns to the box beside them, not the top of the form.
    FocusOnStart(if (backFromTerms) agreeReq else first, Unit)
    val ready = name.isNotBlank() && pw.length >= 8 && pw == pw2 && agreed && !busy
    val create: () -> Unit = {
        if (ready) {
            busy = true
            scope.launch {
                val deviceId = Settings.Secure.getString(app.contentResolver, Settings.Secure.ANDROID_ID) ?: "unknown"
                runCatching { app.api.setupAdmin(serverUrl, name.trim(), pw, "${Build.MANUFACTURER} ${Build.MODEL}", deviceId, BuildConfig.VERSION_NAME) }
                    .onSuccess { resp ->
                        val terms = runCatching { app.api.legalAt(serverUrl, "terms").version }.getOrDefault("")
                        tv.roxy.tv.ui.login.rememberServerIdentity(app, serverUrl.trimEnd('/'))
                        app.prefs.saveSession(Session(serverUrl = serverUrl.trimEnd('/'), token = resp.token, userName = resp.user.name, isAdmin = true, acceptedTerms = terms, setupInProgress = true))
                        onDone()
                    }
                    .onFailure { err = apiError(it) }
                busy = false
            }
        }
    }
    TwoColumns(
        "Create your account",
        "You're the admin: you'll manage the server and can add your family later, each with their own account and profiles.",
        left = {
            Card {
                Text("Typing with the remote", fontSize = 15.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground)
                Bullet("Press OK on a field to type in it")
                Bullet("The keyboard's Next key moves to the next field")
                Bullet("Back closes the keyboard; press it again for the previous step")
            }
        },
    ) {
        TvTextField(
            name, { onName(it); err = null }, placeholder = "Your name (what you'll sign in with)", icon = Icons.Outlined.Person,
            modifier = Modifier.focusRequester(first), onNext = { pwEdit++ },
        )
        Row(verticalAlignment = Alignment.CenterVertically) {
            TvTextField(
                pw, { onPw(it); err = null }, placeholder = "Password", icon = Icons.Outlined.Lock, keyboardType = KeyboardType.Password,
                password = !showPw, editRequest = pwEdit, onNext = { pw2Edit++ }, modifier = Modifier.weight(1f),
            )
            Spacer(Modifier.width(10.dp))
            // Shows or hides both password fields.
            TvFocusable(onClick = { showPw = !showPw }, shape = RoundedCornerShape(12.dp), background = RoxyColors.surfaceHigh, focusedBackground = RoxyColors.accent) { focused ->
                Box(modifier = Modifier.size(50.dp), contentAlignment = Alignment.Center) {
                    Icon(
                        if (showPw) Icons.Outlined.VisibilityOff else Icons.Outlined.Visibility, if (showPw) "Hide password" else "Show password",
                        tint = if (focused) OnBrand else RoxyColors.onBackground, modifier = Modifier.size(24.dp),
                    )
                }
            }
        }
        TvTextField(
            pw2, { onPw2(it); err = null }, placeholder = "Password again", icon = Icons.Outlined.Lock, keyboardType = KeyboardType.Password,
            imeAction = ImeAction.Done, password = !showPw, editRequest = pw2Edit,
            onDone = {
                // Done on the last field: create if everything is in; else on to the box still to tick.
                if (ready) create() else scope.launch { focusSoon(if (!agreed) agreeReq else createReq) }
            },
        )
        PasswordHint(pw, pw2)
        // "I agree to the Terms of Use": OK on the box ticks it; Right goes to the blue
        // "Terms of Use", which opens them (as on the TV's sign-in card).
        Row(verticalAlignment = Alignment.CenterVertically) {
            TvFocusable(onClick = { onAgreed(!agreed) }, shape = RoundedCornerShape(8.dp), focusedBackground = RoxyColors.surfaceHigh, modifier = Modifier.focusRequester(agreeReq)) { focused ->
                Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.padding(horizontal = 8.dp, vertical = 8.dp)) {
                    Box(
                        modifier = Modifier.size(24.dp).background(if (agreed) RoxyColors.accent else Color.Transparent, RoundedCornerShape(5.dp))
                            .border(2.dp, if (focused || agreed) RoxyColors.accent else RoxyColors.muted, RoundedCornerShape(5.dp)),
                        contentAlignment = Alignment.Center,
                    ) { if (agreed) Icon(Icons.Filled.Check, contentDescription = null, tint = OnBrand, modifier = Modifier.size(18.dp)) }
                    Text("I agree to the", color = RoxyColors.onBackground, fontSize = 15.sp, modifier = Modifier.padding(start = 12.dp))
                }
            }
            TvFocusable(onClick = onReadTerms, shape = RoundedCornerShape(6.dp), focusedBackground = RoxyColors.accent) { focused ->
                Text("Terms of Use", fontSize = 15.sp, fontWeight = FontWeight.SemiBold, color = if (focused) OnBrand else RoxyColors.accent, modifier = Modifier.padding(horizontal = 6.dp, vertical = 4.dp))
            }
        }
        err?.let { Text(it, fontSize = 14.sp, color = RoxyColors.error) }
        CardButton(
            if (busy) "Creating…" else "Create account", primary = true, enabled = ready, icon = Icons.Filled.Check,
            onClick = create, modifier = Modifier.focusRequester(createReq),
        )
        if (!ready && !busy && name.isNotBlank() && pw.length >= 8 && pw == pw2 && !agreed) {
            Text("Tick the box to agree to the Terms of Use first.", fontSize = 13.sp, color = RoxyColors.muted)
        }
    }
}

@Composable
private fun TvDetailsStep(onNext: () -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    val scope = rememberCoroutineScope()
    var has by remember { mutableStateOf<Boolean?>(null) }
    var key by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var err by remember { mutableStateOf<String?>(null) }
    val field = remember { FocusRequester() }
    val next = remember { FocusRequester() }
    LaunchedEffect(Unit) { has = runCatching { app.api.tmdbSettings().set }.getOrDefault(false) }
    // A key already there: Continue is the thing to press. Otherwise the field.
    LaunchedEffect(has) { if (has != null) focusSoon(if (has == true) next else field) }
    val submit: () -> Unit = {
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
    }
    TwoColumns(
        "Posters and details",
        "Roxy names your files and fetches posters, descriptions, cast and the Discover page from TMDB. It's free, but every server needs its own key.",
        left = {
            Card {
                Text("Get a free key", fontSize = 16.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground)
                Numbered(1, "On a phone or computer, make a free account at themoviedb.org")
                Numbered(2, "Open themoviedb.org/settings/api and create a key (choose \"Personal\")")
                Numbered(3, "Type the API Key (the short one) here")
            }
            Tip("Anime details come from AniList, which needs no key at all.")
        },
    ) {
        if (has == true) {
            Card {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Icon(Icons.Filled.CheckCircle, null, tint = Color(0xFF4ADE80), modifier = Modifier.size(24.dp))
                    Text("This server already has a TMDB key.", fontSize = 16.sp, color = RoxyColors.onBackground, modifier = Modifier.padding(start = 10.dp))
                }
                Text("You can type a different one below, or just continue.", fontSize = 14.sp, color = RoxyColors.muted, modifier = Modifier.padding(top = 6.dp))
            }
        }
        // Masked like a password (the TV may be streamed); the eye button shows it to check typing.
        var showKey by remember { mutableStateOf(false) }
        Row(verticalAlignment = Alignment.CenterVertically) {
            TvTextField(
                key, { key = it.trim(); err = null }, placeholder = "TMDB API key", icon = Icons.Outlined.Key, keyboardType = KeyboardType.Ascii,
                imeAction = ImeAction.Done, password = !showKey, modifier = Modifier.weight(1f).focusRequester(field),
                onDone = { if (key.isNotEmpty()) submit() else scope.launch { focusSoon(next) } },
            )
            Spacer(Modifier.width(10.dp))
            TvFocusable(onClick = { showKey = !showKey }, shape = RoundedCornerShape(12.dp), background = RoxyColors.surfaceHigh, focusedBackground = RoxyColors.accent) { focused ->
                Box(modifier = Modifier.size(50.dp), contentAlignment = Alignment.Center) {
                    Icon(
                        if (showKey) Icons.Outlined.VisibilityOff else Icons.Outlined.Visibility, if (showKey) "Hide key" else "Show key",
                        tint = if (focused) OnBrand else RoxyColors.onBackground, modifier = Modifier.size(24.dp),
                    )
                }
            }
        }
        err?.let { Text(it, fontSize = 14.sp, color = RoxyColors.error) }
        val can = key.isNotEmpty() || has == true
        CardButton(
            when { busy -> "Checking…"; key.isNotEmpty() -> "Check and continue"; has == true -> "Continue"; else -> "Type a key to continue" },
            primary = true, enabled = can, icon = Icons.AutoMirrored.Filled.ArrowForward, onClick = submit, modifier = Modifier.focusRequester(next),
        )
        // No skip: without a key the library has no posters, titles or Discover.
        if (has == false) Text(
            "A key is needed to continue. It takes a few minutes and never costs anything.",
            fontSize = 14.sp, lineHeight = 19.sp, color = RoxyColors.muted,
        )
    }
}

@Composable
private fun TvMediaStep(changed: Int, onAdd: () -> Unit, onEdit: (MediaFolder) -> Unit, onNext: () -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    var folders by remember { mutableStateOf<List<MediaFolder>?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    val add = remember { FocusRequester() }
    val next = remember { FocusRequester() }
    LaunchedEffect(changed) {
        runCatching { app.api.mediaFolders() }.onSuccess { folders = it; error = null }
            .onFailure { if (it is kotlinx.coroutines.CancellationException) throw it; error = it.message; folders = emptyList() }
    }
    // No folders yet: Add folder. Some (or one just added): Continue.
    LaunchedEffect(folders != null) { val f = folders ?: return@LaunchedEffect; focusSoon(if (f.isEmpty()) add else next) }
    TwoColumns(
        "Your media",
        "Point Roxy at the folders that hold your shows and movies. It only ever reads them; nothing is moved or deleted.",
        left = {
            Card {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Icon(Icons.Outlined.Lightbulb, null, tint = Color(0xFFF2C35B), modifier = Modifier.size(20.dp))
                    Text("Name things like this", fontSize = 16.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground, modifier = Modifier.padding(start = 8.dp))
                }
                Spacer(Modifier.height(10.dp))
                Tree(
                    listOf(
                        0 to "Shows/",
                        1 to "Frieren (2023)/",
                        2 to "Season 01/",
                        3 to "Frieren (2023) - S01E01.mkv",
                        0 to "Movies/",
                        1 to "Spirited Away (2001)/",
                        2 to "Spirited Away (2001).mkv",
                    ),
                )
                Bullet("One folder per show or movie, with the year in brackets")
                Bullet("Episodes as S01E01; anime numbered 1, 2, 3… works too")
                Bullet("Sonarr and Radarr already name everything this way")
            }
        },
    ) {
        Text("Media folders", fontSize = 16.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground)
        val list = folders
        when {
            list == null -> Text("Loading…", fontSize = 15.sp, color = RoxyColors.muted)
            list.isEmpty() -> Text("None yet. Add the folder that holds your shows, then the one with your movies.", fontSize = 15.sp, lineHeight = 21.sp, color = RoxyColors.muted)
            else -> list.forEach { f ->
                val counts = listOfNotNull(
                    f.series.takeIf { it > 0 }?.let { "$it ${if (it == 1) "show" else "shows"}" },
                    f.movies.takeIf { it > 0 }?.let { "$it ${if (it == 1) "movie" else "movies"}" },
                ).joinToString(", ").ifEmpty { "scanning…" }
                SetupRow(
                    title = f.name,
                    sub = if (f.reachable) "${f.root}  ·  ${kindName(f.kind, f.animeMode)}  ·  $counts" else "${f.root}  ·  the server can't see this folder",
                    icon = if (f.reachable) Icons.Outlined.Folder else Icons.Outlined.ErrorOutline,
                    iconTint = if (f.reachable) RoxyColors.onBackground else RoxyColors.error,
                    onClick = { onEdit(f) },
                )
            }
        }
        error?.let { Text(it, fontSize = 13.sp, color = RoxyColors.error) }
        CardButton("Add folder", primary = false, icon = Icons.Filled.Add, onClick = onAdd, modifier = Modifier.focusRequester(add))
        CardButton("Continue", primary = true, icon = Icons.AutoMirrored.Filled.ArrowForward, onClick = onNext, modifier = Modifier.focusRequester(next))
        Text("You can add or change folders any time in Settings.", fontSize = 13.sp, color = RoxyColors.muted)
    }
}

private fun kindName(kind: String, anime: Boolean) = (if (kind == "movies") "Movies only" else "Shows & movies") + if (anime) " · anime" else ""

@Composable
private fun TvDoneStep(onFinish: () -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    var tmdb by remember { mutableStateOf<Boolean?>(null) }
    var folders by remember { mutableStateOf<Int?>(null) }
    val first = remember { FocusRequester() }
    FocusOnStart(first, Unit)
    LaunchedEffect(Unit) {
        tmdb = runCatching { app.api.tmdbSettings().set }.getOrDefault(false)
        folders = runCatching { app.api.mediaFolders().size }.getOrDefault(0)
    }
    TwoColumns(
        "You're all set",
        "Roxy is scanning your folders now. Shows appear on Home as they're found.",
        left = {
            Card {
                Text("Next, when you like", fontSize = 15.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground)
                Bullet("Connect Sonarr and Radarr to request shows from Discover")
                Bullet("Add your family as users")
                Bullet("Sign in other TVs with the phone code")
            }
        },
    ) {
        Card {
            Check(true, "Your admin account")
            Check(tmdb == true, if (tmdb == true) "Posters and details from TMDB" else "TMDB key: add one in Settings for posters and Discover")
            Check((folders ?: 0) > 0, when (folders) { null -> "Media folders"; 0 -> "No media folders yet: add them in Settings"; 1 -> "1 media folder"; else -> "$folders media folders" })
        }
        CardButton("Start watching", primary = true, icon = Icons.Filled.PlayArrow, onClick = onFinish, modifier = Modifier.focusRequester(first))
    }
}

/** A focusable row in a list (a media folder, a folder on the server, an option): an accent ring when focused. */
@Composable
private fun SetupRow(
    title: String, sub: String?, icon: ImageVector, onClick: () -> Unit, modifier: Modifier = Modifier,
    iconTint: Color = RoxyColors.onBackground, dim: Boolean = false, tag: String? = null, trailing: ImageVector? = Icons.Filled.ChevronRight,
) {
    val shape = RoundedCornerShape(12.dp)
    TvFocusable(onClick = onClick, shape = shape, background = RoxyColors.surfaceHigh, focusedBackground = RoxyColors.surfaceHigh, modifier = modifier.fillMaxWidth()) { focused ->
        Row(
            modifier = Modifier.fillMaxWidth().height(if (sub.isNullOrEmpty()) 52.dp else 60.dp).border(3.dp, if (focused) RoxyColors.accent else Color.Transparent, shape).padding(horizontal = 16.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Row(modifier = Modifier.weight(1f).alpha(if (dim && !focused) 0.55f else 1f), verticalAlignment = Alignment.CenterVertically) {
                Icon(icon, contentDescription = null, tint = if (focused) RoxyColors.accent else iconTint, modifier = Modifier.size(24.dp))
                Column(modifier = Modifier.weight(1f).padding(start = 14.dp)) {
                    Text(title, fontSize = 17.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    if (!sub.isNullOrEmpty()) Text(sub, fontSize = 13.sp, color = RoxyColors.muted, maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
            }
            if (tag != null) Text(
                tag, fontSize = 12.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.muted,
                modifier = Modifier.padding(start = 8.dp).border(1.dp, RoxyColors.muted.copy(alpha = 0.6f), RoundedCornerShape(50)).padding(horizontal = 8.dp, vertical = 3.dp),
            )
            if (trailing != null) Icon(trailing, null, tint = if (focused) RoxyColors.accent else RoxyColors.muted, modifier = Modifier.padding(start = 8.dp))
        }
    }
}

/**
 * The server's Terms of Use. The text box takes focus and Up / Down scroll it; at the
 * bottom, Down moves on to I agree / Back. Back returns to the account form.
 */
@Composable
private fun TvTermsPage(serverUrl: String, onBack: () -> Unit, onAgree: () -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    var body by remember { mutableStateOf<String?>(null) }
    var err by remember { mutableStateOf<String?>(null) }
    BackHandler(onBack = onBack)
    LaunchedEffect(serverUrl) {
        runCatching { app.api.legalAt(serverUrl, "terms") }.onSuccess { body = it.body }.onFailure { err = "Couldn't load the terms from the server: ${it.message}" }
    }
    val scroll = rememberScrollState()
    val scope = rememberCoroutineScope()
    val textReq = remember { FocusRequester() }
    val interaction = remember { MutableInteractionSource() }
    val focused by interaction.collectIsFocusedAsState()
    val step = with(LocalDensity.current) { 140.dp.toPx() }
    FocusOnStart(textReq, Unit)
    Column(
        modifier = Modifier.fillMaxSize().padding(horizontal = SafeH, vertical = SafeV),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Column(modifier = Modifier.widthIn(max = ContentWidth).fillMaxWidth().fillMaxHeight()) {
            Text("Terms of Use", fontSize = 28.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground)
            Text("Up and Down scroll the text. Back returns to your account.", fontSize = 14.sp, color = RoxyColors.muted, modifier = Modifier.padding(top = 4.dp, bottom = 12.dp))
            Box(
                modifier = Modifier.fillMaxWidth().weight(1f)
                    .clip(RoundedCornerShape(14.dp)).background(RoxyColors.surface)
                    .border(if (focused) 3.dp else 1.dp, if (focused) RoxyColors.accent else Color.White.copy(alpha = 0.08f), RoundedCornerShape(14.dp))
                    .onPreviewKeyEvent { ev ->
                        if (ev.type != KeyEventType.KeyDown) return@onPreviewKeyEvent false
                        when {
                            ev.key == Key.DirectionDown && scroll.canScrollForward -> { scope.launch { scroll.animateScrollBy(step) }; true }
                            ev.key == Key.DirectionUp && scroll.canScrollBackward -> { scope.launch { scroll.animateScrollBy(-step) }; true }
                            else -> false
                        }
                    }
                    .focusRequester(textReq)
                    .focusable(interactionSource = interaction)
                    .verticalScroll(scroll)
                    .padding(horizontal = 26.dp, vertical = 20.dp),
            ) {
                val b = body
                when {
                    b != null -> Text(b.replace("**", ""), fontSize = 16.sp, lineHeight = 24.sp, color = RoxyColors.onBackground.copy(alpha = 0.9f))
                    err != null -> Text(err ?: "", fontSize = 16.sp, color = RoxyColors.error)
                    else -> Text("Loading…", fontSize = 16.sp, color = RoxyColors.muted)
                }
            }
            Row(modifier = Modifier.padding(top = 14.dp), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                CardButton("I agree", primary = true, enabled = body != null, icon = Icons.Filled.Check, onClick = { if (body != null) onAgree() }, modifier = Modifier.width(240.dp))
                CardButton("Back", primary = false, icon = Icons.AutoMirrored.Filled.ArrowBack, onClick = onBack, modifier = Modifier.width(200.dp))
            }
        }
    }
}

/**
 * The server's folders for the remote: OK opens a folder, "Use this folder" at the top
 * takes the one being shown, Back goes up a level (and out from the starting places).
 * In Docker the server sees only folders mounted into its container.
 */
@Composable
private fun TvFolderBrowser(onBack: () -> Unit, onAdded: () -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    val scope = rememberCoroutineScope()
    var path by remember { mutableStateOf("") }
    var parent by remember { mutableStateOf<String?>(null) }
    var dirs by remember { mutableStateOf<List<FolderEntry>?>(null) }
    var videosHere by remember { mutableIntStateOf(0) }
    var error by remember { mutableStateOf<String?>(null) }
    // The folder just left by going up: focus lands back on it.
    var cameFrom by remember { mutableStateOf<String?>(null) }
    var choosing by remember { mutableStateOf(false) }
    val listState = rememberLazyListState()
    val target = remember { FocusRequester() }
    val useReq = remember { FocusRequester() }
    val upReq = remember { FocusRequester() }

    fun open(p: String, from: String? = null) {
        dirs = null; error = null; cameFrom = from
        scope.launch {
            runCatching { app.api.browseFolders(p) }
                .onSuccess { r -> path = r.path; parent = r.parent?.takeIf { it.isNotEmpty() }; dirs = r.dirs; videosHere = r.videos }
                .onFailure { t -> if (t is kotlinx.coroutines.CancellationException) throw t; error = t.message; dirs = emptyList() }
        }
    }
    val goUp = { if (path.isNotEmpty()) open(parent ?: "", from = path) else onBack() }
    LaunchedEffect(Unit) { open("") }
    BackHandler(enabled = !choosing) { goUp() }

    if (choosing) {
        val guess = path.substringAfterLast('/').ifEmpty { path }
        TvFolderForm(
            title = "New library",
            path = path,
            warning = if (videosHere == 0 && dirs.isNullOrEmpty()) "This folder looks empty." else null,
            initialName = guess,
            initialKind = if (guess.contains("movie", true) || guess.contains("film", true)) "movies" else "series",
            initialAnime = guess.contains("anime", true),
            confirm = "Add and scan", busyLabel = "Adding…",
            onConfirm = { name, kind, anime -> app.api.addMediaFolder(path, name, kind, anime); app.items.userDataChanged(); onAdded() },
            onCancel = { choosing = false },
        )
        return
    }

    val list = dirs
    val focusPath = list?.firstOrNull { it.path == cameFrom }?.path ?: list?.firstOrNull()?.path
    // A listing arrived: focus the folder we came up from (or the first), else the top buttons.
    LaunchedEffect(list) {
        if (list == null) return@LaunchedEffect
        val idx = list.indexOfFirst { it.path == focusPath }
        if (idx >= 0) { listState.scrollToItem(idx); focusSoon(target) } else focusSoon(if (path.isNotEmpty()) useReq else upReq)
    }
    Column(modifier = Modifier.fillMaxSize().padding(horizontal = SafeH, vertical = SafeV), horizontalAlignment = Alignment.CenterHorizontally) {
        Column(modifier = Modifier.widthIn(max = ContentWidth).fillMaxWidth().fillMaxHeight()) {
            Text("Add a media folder", fontSize = 28.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground)
            Text(
                path.ifEmpty { "Starting places on the server" }, fontSize = 16.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.accent,
                maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 4.dp),
            )
            Text(
                "OK opens a folder, Back goes up. Open the folder that holds your shows or movies, then choose Use this folder. Running in Docker, the server only sees folders mounted into its container.",
                fontSize = 14.sp, lineHeight = 20.sp, color = RoxyColors.muted, modifier = Modifier.padding(top = 6.dp, bottom = 12.dp),
            )
            Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                if (path.isNotEmpty()) CardButton(
                    "Use this folder", primary = true, icon = Icons.Filled.Check, modifier = Modifier.width(280.dp).focusRequester(useReq),
                    onClick = { choosing = true },
                )
                CardButton(
                    if (path.isNotEmpty()) "Up one level" else "Cancel", primary = false, icon = Icons.AutoMirrored.Filled.ArrowBack,
                    modifier = Modifier.width(240.dp).focusRequester(upReq), onClick = goUp,
                )
            }
            error?.let { Text(it, fontSize = 14.sp, color = RoxyColors.error, modifier = Modifier.padding(top = 8.dp)) }
            LazyColumn(
                state = listState, modifier = Modifier.fillMaxWidth().weight(1f).padding(top = 12.dp),
                contentPadding = PaddingValues(vertical = 4.dp), verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                when {
                    list == null -> item { Text("Loading…", fontSize = 15.sp, color = RoxyColors.muted) }
                    list.isEmpty() && error == null -> item { Text("No folders in here.", fontSize = 15.sp, color = RoxyColors.muted) }
                    else -> items(list, key = { it.path }) { d ->
                        val mod = if (d.path == focusPath) Modifier.focusRequester(target) else Modifier
                        if (d.skip.isNotEmpty()) {
                            SetupRow(d.name, d.skip, Icons.Outlined.FolderOff, onClick = { open(d.path) }, modifier = mod, dim = true, tag = "Skipped")
                        } else {
                            val count = when (d.videos) { 0 -> null; 1 -> "1 video"; in 2..999 -> "${d.videos} videos"; else -> "1000+ videos" }
                            SetupRow(d.name, listOfNotNull(count, d.path).joinToString("  ·  "), Icons.Outlined.Folder, onClick = { open(d.path) }, modifier = mod)
                        }
                    }
                }
            }
        }
    }
}

/** A media folder from the Media step: rename it, change its type, or remove it (the files stay). */
@Composable
private fun TvFolderEdit(folder: MediaFolder, onClose: (changed: Boolean) -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    TvFolderForm(
        title = folder.name, path = folder.root, warning = null,
        initialName = folder.name, initialKind = folder.kind, initialAnime = folder.animeMode,
        confirm = "Save", busyLabel = "Saving…",
        onConfirm = { name, kind, anime -> app.api.updateMediaFolder(folder.id, name, kind, anime); onClose(true) },
        onCancel = { onClose(false) },
        onRemove = { app.api.removeMediaFolder(folder.id); app.items.userDataChanged(); onClose(true) },
    )
}

/**
 * Name, type and anime for a folder, one control per row: the name field, the two types
 * (OK picks), the anime switch, then the buttons. Back cancels.
 */
@Composable
private fun TvFolderForm(
    title: String, path: String, warning: String?,
    initialName: String, initialKind: String, initialAnime: Boolean,
    confirm: String, busyLabel: String,
    onConfirm: suspend (name: String, kind: String, anime: Boolean) -> Unit,
    onCancel: () -> Unit,
    onRemove: (suspend () -> Unit)? = null,
) {
    val scope = rememberCoroutineScope()
    var name by remember { mutableStateOf(initialName) }
    var kind by remember { mutableStateOf(initialKind) }
    var anime by remember { mutableStateOf(initialAnime) }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var confirmRemove by remember { mutableStateOf(false) }
    val first = remember { FocusRequester() }
    BackHandler(onBack = onCancel)
    FocusOnStart(first, Unit)
    fun act(action: suspend () -> Unit) {
        if (busy) return
        busy = true
        scope.launch {
            runCatching { action() }.onFailure { t -> if (t is kotlinx.coroutines.CancellationException) throw t; error = apiError(t) }
            busy = false
        }
    }
    Column(modifier = Modifier.fillMaxSize().padding(horizontal = SafeH, vertical = SafeV), horizontalAlignment = Alignment.CenterHorizontally) {
        Column(modifier = Modifier.width(640.dp).fillMaxHeight().verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Text(title, fontSize = 28.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(path, fontSize = 14.sp, color = RoxyColors.muted, maxLines = 1, overflow = TextOverflow.Ellipsis)
            warning?.let { Text(it, fontSize = 14.sp, color = RoxyColors.error) }
            Text("Name", fontSize = 14.sp, color = RoxyColors.muted, modifier = Modifier.padding(top = 6.dp))
            TvTextField(name, { name = it; error = null }, placeholder = "Library name", icon = Icons.Outlined.Folder, imeAction = ImeAction.Done, modifier = Modifier.focusRequester(first))
            Text("What's in it", fontSize = 14.sp, color = RoxyColors.muted, modifier = Modifier.padding(top = 6.dp))
            SetupRow(
                "Shows & movies", "Finds shows and movies in the folder by itself",
                if (kind == "series") Icons.Filled.RadioButtonChecked else Icons.Filled.RadioButtonUnchecked,
                iconTint = if (kind == "series") RoxyColors.accent else RoxyColors.muted, trailing = null, onClick = { kind = "series" },
            )
            SetupRow(
                "Movies only", "Every video is a movie",
                if (kind == "movies") Icons.Filled.RadioButtonChecked else Icons.Filled.RadioButtonUnchecked,
                iconTint = if (kind == "movies") RoxyColors.accent else RoxyColors.muted, trailing = null, onClick = { kind = "movies" },
            )
            SetupRow(
                "Anime", "Look titles up on AniList first (Dub | Sub, anime episode numbering)",
                if (anime) Icons.Outlined.CheckBox else Icons.Outlined.CheckBoxOutlineBlank,
                iconTint = if (anime) RoxyColors.accent else RoxyColors.muted, trailing = null, tag = if (anime) "On" else "Off", onClick = { anime = !anime },
            )
            if (onRemove != null && (kind != initialKind || anime != initialAnime)) {
                Text("Changing the type or anime setting looks every title up again.", fontSize = 13.sp, color = RoxyColors.muted)
            }
            error?.let { Text(it, fontSize = 14.sp, color = RoxyColors.error) }
            Row(modifier = Modifier.padding(top = 6.dp), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                CardButton(
                    if (busy) busyLabel else confirm, primary = true, enabled = name.isNotBlank() && !busy, icon = Icons.Filled.Check, modifier = Modifier.weight(1f),
                    onClick = { if (name.isNotBlank()) act { onConfirm(name.trim(), kind, anime) } },
                )
                CardButton("Cancel", primary = false, onClick = onCancel, modifier = Modifier.weight(1f))
            }
            if (onRemove != null) {
                // Two presses: the first asks, the second removes.
                CardButton(
                    if (confirmRemove) "Press OK again to remove (the files stay)" else "Remove folder", primary = false, icon = Icons.Outlined.Delete,
                    onClick = { if (confirmRemove) act { onRemove() } else confirmRemove = true },
                )
            }
        }
    }
}
