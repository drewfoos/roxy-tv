package tv.roxy.tv.ui.phone

import android.content.Intent
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.OpenInNew
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
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.material.icons.filled.Close
import androidx.compose.foundation.layout.statusBars
import androidx.compose.foundation.layout.asPaddingValues
import androidx.compose.foundation.layout.WindowInsets
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
import tv.roxy.tv.api.CastMember
import tv.roxy.tv.api.ItemDetails
import tv.roxy.tv.data.downloads.formatBytes
import tv.roxy.tv.ui.components.RoxyImage
import tv.roxy.tv.ui.components.widthPx
import tv.roxy.tv.ui.theme.RoxyColors

/**
 * The "More details" page (Crunchyroll's): a short backdrop, then everything there is to
 * know about the title in labelled rows: the full synopsis, Details (original title,
 * status, aired, network, studio, country, episodes, runtime, ratings), Audio &
 * subtitles and Video from the files on the server, the cast with photos, keywords, and
 * links to the provider pages.
 */
@Composable
fun PhoneDetails(itemId: String, fallbackTitle: String, onBack: () -> Unit) {
    val context = LocalContext.current
    val app = RoxyApp.get(context)
    var details by remember { mutableStateOf<ItemDetails?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(itemId) {
        runCatching { app.api.itemDetails(itemId) }
            .onSuccess { details = it }
            .onFailure { t -> Timber.w(t, "details"); error = t.message }
    }
    val d = details
    if (d == null) {
        if (error != null) Column(modifier = Modifier.fillMaxSize().statusBarsPadding().padding(PhonePad)) { BackCircle(onBack); Text(error ?: "", color = RoxyColors.error, modifier = Modifier.padding(top = 16.dp)) }
        else DetailSkeleton(onBack)
        return
    }
    val item = d.item
    val x = d.tmdb
    val f = d.facts

    // Same strip as the title page (X at the left, the name beside it) over a plain list
    // of facts; the art stays on the title page.
    Box(modifier = Modifier.fillMaxSize()) {
        val top = WindowInsets.statusBars.asPaddingValues().calculateTopPadding() + HeaderRow
        Column(modifier = Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(top = top).navigationBarsPadding().padding(bottom = 24.dp)) {
            Box(modifier = Modifier.fillMaxWidth().height(220.dp)) {
                RoxyImage(app.api.imageUrl(item, "backdrop", widthPx(420.dp) * 2) ?: app.api.imageUrl(item, "poster", widthPx(420.dp) * 2), item.title, modifier = Modifier.fillMaxSize(), placeholderTextSize = 14)
                Box(modifier = Modifier.fillMaxSize().background(Brush.verticalGradient(0.45f to Color.Transparent, 1f to RoxyColors.background)))
            }
            Column(modifier = Modifier.padding(horizontal = PhonePad)) {
                x?.tagline?.takeIf { it.isNotBlank() }?.let { Text(it, fontSize = 14.sp, fontStyle = FontStyle.Italic, color = RoxyColors.muted, modifier = Modifier.padding(top = 8.dp)) }

                item.overview?.takeIf { it.isNotBlank() }?.let {
                    DetailSection("Synopsis")
                    Text(it, fontSize = 14.sp, lineHeight = 20.sp, color = RoxyColors.onBackground.copy(alpha = 0.9f))
                }

                if (!x?.cast.isNullOrEmpty()) {
                    DetailSection("Cast")
                }
            }
            if (!x?.cast.isNullOrEmpty()) CastRow(x!!.cast, modifier = Modifier.padding(top = 4.dp))
            Column(modifier = Modifier.padding(horizontal = PhonePad)) {
                DetailSection("Details")
                DetailRow("Original title", x?.originalTitle)
                DetailRow("Type", when { item.isMovie -> "Movie"; item.isAnime -> "Anime series"; else -> "Series" })
                DetailRow("Status", (x?.status ?: item.status)?.takeIf { it.isNotBlank() }?.let { statusLabel(it) })
                DetailRow(if (item.isMovie) "Released" else "First aired", x?.firstAired?.takeIf { it.isNotBlank() } ?: item.premiered)
                DetailRow("Last aired", x?.lastAired?.takeIf { it.isNotBlank() && !item.isMovie })
                DetailRow(if (x?.networks.orEmpty().size > 1) "Networks" else "Network", x?.networks?.joinToString(", "))
                // TMDB's first production company is the animation studio; the library's
                // "studio" field sometimes holds the network instead, so it is the fallback.
                val studio = x?.companies?.firstOrNull() ?: item.studio?.takeIf { it.isNotBlank() && it !in x?.networks.orEmpty() }
                DetailRow("Studio", studio)
                DetailRow("Production", x?.companies?.drop(1)?.take(4)?.joinToString(", "))
                DetailRow("Country", x?.countries?.joinToString(", "))
                DetailRow("Spoken languages", x?.languages?.joinToString(", "))
                DetailRow("Seasons", (item.seasonCount ?: x?.seasons)?.takeIf { it > 0 }?.toString())
                DetailRow("Episodes", x?.episodes?.takeIf { it > 0 }?.toString())
                DetailRow(if (item.isMovie) "Runtime" else "Episode length", x?.runtimeMinutes?.takeIf { it > 0 }?.let { if (item.isMovie) "${it / 60}h ${it % 60}m".removePrefix("0h ") else "$it min" } ?: item.runtimeMinutes?.let { "$it min" })
                DetailRow("Genres", item.genres.joinToString(", "))
                DetailRow("Rating", item.communityRating?.let { "★ %.1f on TMDB".format(it) })
                DetailRow("Content rating", item.officialRating)
                DetailRow("Added to library", item.dateCreated?.take(10))

                // Languages only: codecs and resolutions differ from episode to episode (each
                // row's three-dot menu has that file's facts).
                if (f != null && f.files > 0) {
                    DetailSection("Audio & subtitles")
                    DetailRow("Audio", f.audio.joinToString(", ") { languageName(it) }.ifEmpty { null })
                    DetailRow("Subtitles", f.subtitles.joinToString(", ") { languageName(it) }.ifEmpty { null })
                    DetailRow(if (item.isMovie) "File" else "Files", "${f.files}  ·  ${formatBytes(f.bytes)}")
                }

                if (!x?.keywords.isNullOrEmpty()) {
                    DetailSection("Keywords")
                    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        x!!.keywords.forEach { k ->
                            Text(k, fontSize = 12.sp, color = RoxyColors.onBackground, modifier = Modifier.clip(RoundedCornerShape(14.dp)).background(ChipGrey).padding(horizontal = 12.dp, vertical = 6.dp))
                        }
                    }
                }
                val links = listOf("tmdb" to "TMDB", "anilist" to "AniList", "mal" to "MyAnimeList", "imdb" to "IMDb", "tvdb" to "TheTVDB").mapNotNull { (k, label) -> d.links[k]?.let { label to it } } +
                    listOfNotNull(x?.homepage?.takeIf { it.isNotBlank() }?.let { "Official site" to it })
                if (links.isNotEmpty()) {
                    DetailSection("Links")
                    links.forEach { (label, url) ->
                        Row(
                            modifier = Modifier.fillMaxWidth().clickable { runCatching { context.startActivity(Intent(Intent.ACTION_VIEW, android.net.Uri.parse(url))) } }.padding(vertical = 10.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Text(label, fontSize = 14.sp, color = RoxyColors.accent, fontWeight = FontWeight.SemiBold, modifier = Modifier.weight(1f))
                            Icon(Icons.Filled.OpenInNew, contentDescription = null, tint = RoxyColors.muted, modifier = Modifier.size(18.dp))
                        }
                    }
                }
                Spacer(Modifier.height(16.dp))
            }
        }
        Row(
            modifier = Modifier.fillMaxWidth().background(RoxyColors.background).statusBarsPadding().padding(horizontal = 6.dp, vertical = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            HeaderIcon(Icons.Filled.Close, "Close", onClick = onBack)
            Text(item.title.ifEmpty { fallbackTitle }, fontSize = 17.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f).padding(horizontal = 6.dp))
        }
    }
}

/** The cast as round photos with name and role, scrolling sideways (More details, and a movie's player page). */
@Composable
internal fun CastRow(cast: List<CastMember>, modifier: Modifier = Modifier) {
    LazyRow(contentPadding = PaddingValues(horizontal = PhonePad), horizontalArrangement = Arrangement.spacedBy(14.dp), modifier = modifier) {
        items(cast, key = { it.name + it.character }) { c ->
            Column(modifier = Modifier.width(84.dp), horizontalAlignment = Alignment.CenterHorizontally) {
                Box(modifier = Modifier.size(72.dp).clip(CircleShape).background(RoxyColors.surfaceHigh), contentAlignment = Alignment.Center) {
                    if (c.photo != null) AsyncImage(model = c.photo, contentDescription = c.name, contentScale = ContentScale.Crop, modifier = Modifier.fillMaxSize())
                    else Text(c.name.take(1), color = RoxyColors.muted, fontSize = 22.sp, fontWeight = FontWeight.Bold)
                }
                Text(c.name, fontSize = 12.sp, lineHeight = 15.sp, fontWeight = FontWeight.SemiBold, color = RoxyColors.onBackground, textAlign = TextAlign.Center, maxLines = 2, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 6.dp))
                if (c.character.isNotBlank()) Text(c.character, fontSize = 11.sp, lineHeight = 14.sp, color = RoxyColors.muted, textAlign = TextAlign.Center, maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
        }
    }
}

@Composable
private fun DetailSection(title: String) {
    Text(title, fontSize = 16.sp, fontWeight = FontWeight.Bold, color = RoxyColors.onBackground, modifier = Modifier.padding(top = 22.dp, bottom = 8.dp))
}

/** Label on the left, value on the right; skipped when there is no value. */
@Composable
private fun DetailRow(label: String, value: String?) {
    if (value.isNullOrBlank()) return
    Row(modifier = Modifier.fillMaxWidth().padding(vertical = 5.dp), verticalAlignment = Alignment.Top) {
        Text(label, fontSize = 13.sp, color = RoxyColors.muted, modifier = Modifier.width(128.dp))
        Text(value, fontSize = 13.sp, lineHeight = 18.sp, color = RoxyColors.onBackground, modifier = Modifier.weight(1f))
    }
}

private fun statusLabel(status: String): String = when (status.trim().lowercase()) {
    "returning series", "continuing" -> "Airing"
    "ended" -> "Ended"
    "canceled", "cancelled" -> "Cancelled"
    "in production", "planned", "post production", "upcoming" -> "Upcoming"
    "released" -> "Released"
    else -> status
}
