package tv.roxy.tv.ui

import tv.roxy.tv.api.CastGroup
import tv.roxy.tv.api.CastMember
import tv.roxy.tv.api.ItemDetails

/** What a cast row shows: the casts to switch between (none when there is one), the one showing and its people. */
data class ShownCast(val groups: List<CastGroup>, val language: String, val name: String, val members: List<CastMember>)

/**
 * The server says which voice casts a title has and which to show first (the dub's for
 * someone who listens to the dub, when the title has it); [picked] is the viewer's switch
 * on this page. Without casts from the server it is the TMDB cast, as before.
 */
fun shownCast(details: ItemDetails?, picked: String? = null): ShownCast {
    val groups = details?.casts.orEmpty().filter { it.members.isNotEmpty() }
    if (groups.size < 2) return ShownCast(emptyList(), "", "", details?.tmdb?.cast.orEmpty())
    val g = groups.firstOrNull { it.language == picked } ?: groups.firstOrNull { it.language == details?.castLanguage } ?: groups.first()
    return ShownCast(groups, g.language, g.name, g.members)
}

/** The heading over a cast: "Cast", or "English cast" when the title has more than one. */
fun castHeading(shown: ShownCast): String = if (shown.name.isBlank()) "Cast" else "${shown.name} cast"

/** The TV's one line of cast: its label and the first [max] names of the cast shown first; null without a cast. */
fun castLine(details: ItemDetails?, max: Int = 8): Pair<String, String>? {
    val shown = shownCast(details)
    if (shown.members.isEmpty()) return null
    return castHeading(shown) to shown.members.take(max).joinToString(", ") { it.name }
}

/** A language in the list of casts: "Japanese (original)", "English". */
fun castLanguageLabel(g: CastGroup): String = g.name.ifBlank { g.language } + if (g.original) " (original)" else ""
