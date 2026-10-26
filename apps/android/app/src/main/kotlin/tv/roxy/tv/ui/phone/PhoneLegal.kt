package tv.roxy.tv.ui.phone

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import timber.log.Timber
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.LegalDoc
import tv.roxy.tv.ui.theme.RoxyColors

/**
 * Terms of Use / Privacy, as the server serves them (same text on every device). The
 * body is plain paragraphs with **bold** lead-ins. Agreement happens by checkbox on the
 * sign-in screen; this page is for reading.
 */
@Composable
fun PhoneLegal(doc: String, onBack: (() -> Unit)? = null) {
    val app = RoxyApp.get(LocalContext.current)
    var text by remember(doc) { mutableStateOf<LegalDoc?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(doc) {
        runCatching { app.api.legal(doc) }.onSuccess { text = it }.onFailure { t -> Timber.w(t, "legal"); error = t.message }
    }
    val d = text
    Box(modifier = Modifier.fillMaxSize()) {
        Column(modifier = Modifier.fillMaxSize().statusBarsPadding().verticalScroll(rememberScrollState()).padding(horizontal = PhonePad).padding(bottom = 32.dp)) {
            Row(modifier = Modifier.fillMaxWidth().padding(vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
                if (onBack != null) { BackCircle(onBack); Spacer(Modifier.padding(6.dp)) }
                Text(d?.title ?: if (doc == "privacy") "Privacy" else "Terms of Use", fontSize = 22.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground)
            }
            when {
                d == null && error != null -> Text(error ?: "", color = RoxyColors.error)
                d == null -> repeat(6) { BoneLine(width = 320.dp, height = 12.dp, modifier = Modifier.padding(vertical = 7.dp)) }
                else -> {
                    Text("Version ${d.version}", fontSize = 12.sp, color = RoxyColors.muted, modifier = Modifier.padding(bottom = 12.dp))
                    d.body.split("\n\n").forEach { para -> Paragraph(para.trim()) }
                }
            }
        }
    }
}

/** One paragraph; a leading **bold** run becomes the lead-in. */
@Composable
private fun Paragraph(text: String) {
    val bold = Regex("""^\*\*(.+?)\*\*\s*""").find(text)
    Text(
        buildAnnotatedString {
            if (bold != null) {
                withStyle(SpanStyle(fontWeight = FontWeight.Bold, color = RoxyColors.onBackground)) { append(bold.groupValues[1] + " ") }
                append(text.removePrefix(bold.value))
            } else append(text)
        },
        fontSize = 14.sp, lineHeight = 21.sp, color = RoxyColors.onBackground.copy(alpha = 0.88f), modifier = Modifier.padding(bottom = 14.dp),
    )
}

