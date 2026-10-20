package tv.roxy.tv.ui.phone

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import coil3.compose.AsyncImage
import timber.log.Timber
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.DiscoverItem
import tv.roxy.tv.api.Person
import tv.roxy.tv.ui.theme.RoxyColors
import java.time.LocalDate
import java.time.Period

/**
 * A cast member's page (TMDB's person): photo, name, "Acting · Born 9 July 1956 in
 * Concord, California (age 70)", the biography with Read more, then everything they have
 * been in as a poster grid, the library's titles ticked. Tapping a title opens its page
 * when the library has it, else its Discover page where it can be requested.
 */
@Composable
fun PhonePerson(tmdbId: Int, fallbackName: String, onBack: () -> Unit, onOpenLibrary: (DiscoverItem) -> Unit, onOpenDiscover: (DiscoverItem) -> Unit) {
    val app = RoxyApp.get(LocalContext.current)
    var person by remember { mutableStateOf<Person?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var expanded by rememberSaveable { mutableStateOf(false) }
    LaunchedEffect(tmdbId) {
        runCatching { app.api.person(tmdbId) }.onSuccess { person = it }.onFailure { t -> Timber.w(t, "person"); error = t.message }
    }
    val p = person
    Column(modifier = Modifier.fillMaxSize().statusBarsPadding()) {
        Row(modifier = Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
            BackCircle(onBack)
            Text(p?.name ?: fallbackName, fontSize = 20.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(start = 12.dp))
        }
        LazyColumn(modifier = Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 32.dp)) {
            item(key = "head") {
                Row(modifier = Modifier.fillMaxWidth().padding(horizontal = PhonePad, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                    Box(modifier = Modifier.size(112.dp).clip(CircleShape).background(RoxyColors.surfaceHigh), contentAlignment = Alignment.Center) {
                        when {
                            p?.photo != null -> AsyncImage(model = p.photo, contentDescription = p.name, contentScale = ContentScale.Crop, modifier = Modifier.fillMaxSize())
                            p != null -> Text(p.name.take(1), color = RoxyColors.muted, fontSize = 36.sp, fontWeight = FontWeight.Bold)
                        }
                    }
                    Column(modifier = Modifier.weight(1f).padding(start = 18.dp)) {
                        if (p == null) { BoneLine(width = 160.dp, height = 18.dp); Spacer(Modifier.height(8.dp)); BoneLine(width = 220.dp, height = 14.dp) } else {
                            Text(p.name, fontSize = 22.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground)
                            lifeLine(p)?.let { Text(it, fontSize = 13.sp, lineHeight = 18.sp, color = RoxyColors.muted, modifier = Modifier.padding(top = 4.dp)) }
                            p.placeOfBirth?.takeIf { it.isNotBlank() }?.let { Text(it, fontSize = 13.sp, lineHeight = 18.sp, color = RoxyColors.muted) }
                        }
                    }
                }
            }
            if (p?.biography?.isNotBlank() == true) {
                item(key = "bio") {
                    Column(modifier = Modifier.padding(horizontal = PhonePad)) {
                        ExpandableSynopsis(p.biography, expanded = expanded, onToggle = { expanded = !expanded })
                    }
                }
            }
            error?.let { item(key = "error") { Text("Could not load this person: $it", color = RoxyColors.error, fontSize = 13.sp, modifier = Modifier.padding(PhonePad)) } }
            if (p != null && p.credits.isNotEmpty()) {
                item(key = "credits-title") {
                    Text("Known for", fontSize = 18.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(start = PhonePad, top = 22.dp, bottom = 10.dp))
                }
                items(p.credits.chunked(3), key = { row -> row.joinToString("|") { "${it.type}:${it.tmdbId}" } }) { row ->
                    Row(modifier = Modifier.fillMaxWidth().padding(horizontal = PhonePad, vertical = 6.dp), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                        row.forEach { c -> CreditCard(c, modifier = Modifier.weight(1f)) { if (c.inLibrary) onOpenLibrary(c) else onOpenDiscover(c) } }
                        repeat(3 - row.size) { Spacer(Modifier.weight(1f)) }
                    }
                }
            } else if (p == null && error == null) {
                item(key = "credits-bones") {
                    Row(modifier = Modifier.fillMaxWidth().padding(horizontal = PhonePad, vertical = 6.dp), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                        repeat(3) { Bone(Modifier.weight(1f).aspectRatio(2f / 3f), shape = RoundedCornerShape(6.dp)) }
                    }
                }
            }
        }
    }
}

/** "Acting · Born 9 July 1956 (age 70)", with the death date when there is one. */
private fun lifeLine(p: Person): String? {
    val parts = mutableListOf<String>()
    p.knownFor?.takeIf { it.isNotBlank() }?.let { parts += it }
    val born = p.birthday?.let { runCatching { LocalDate.parse(it) }.getOrNull() }
    val died = p.deathday?.let { runCatching { LocalDate.parse(it) }.getOrNull() }
    if (born != null) {
        val age = Period.between(born, died ?: LocalDate.now()).years
        parts += if (died != null) "${born.year} – ${died.year} (aged $age)" else "Born ${born.dayOfMonth} ${born.month.name.lowercase().replaceFirstChar { it.titlecase() }} ${born.year} (age $age)"
    }
    return parts.takeIf { it.isNotEmpty() }?.joinToString("  ·  ")
}

@Composable
private fun CreditCard(item: DiscoverItem, modifier: Modifier = Modifier, onClick: () -> Unit) {
    Column(modifier = modifier) {
        Box(modifier = Modifier.fillMaxWidth().aspectRatio(2f / 3f).clip(RoundedCornerShape(6.dp)).background(RoxyColors.surfaceHigh).clickable(onClick = onClick)) {
            if (item.poster != null) AsyncImage(model = item.poster, contentDescription = item.title, contentScale = ContentScale.Crop, modifier = Modifier.fillMaxSize())
            else Text(item.title, color = RoxyColors.muted, fontSize = 11.sp, textAlign = TextAlign.Center, modifier = Modifier.align(Alignment.Center).padding(6.dp))
            StatusBadge(item.inLibrary, item.requestStatus, modifier = Modifier.align(Alignment.TopEnd).padding(5.dp))
        }
        Text(item.title, fontSize = 12.sp, lineHeight = 15.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground, maxLines = 2, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 6.dp))
        val sub = listOfNotNull(item.character.takeIf { it.isNotBlank() }?.let { "as $it" } ?: item.year?.toString()).joinToString()
        Text(sub, fontSize = 11.sp, color = RoxyColors.muted, maxLines = 2, overflow = TextOverflow.Ellipsis)
    }
}
