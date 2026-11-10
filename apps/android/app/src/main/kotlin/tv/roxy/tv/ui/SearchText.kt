package tv.roxy.tv.ui

import java.text.Normalizer

private val marks = Regex("\\p{M}+")

/**
 * Text as the library search compares it: lower case with the accents off, so "pokemon"
 * finds "Pokémon" (a TV's on-screen keyboard has no é). Full-width letters fold to plain
 * ones too (NFKD).
 */
internal fun searchFold(s: String): String = marks.replace(Normalizer.normalize(s, Normalizer.Form.NFKD), "").lowercase()

/** True when a title or one of its genres contains the query, compared with [searchFold]. A blank query matches nothing here: the caller decides what an empty search shows. */
internal fun matchesSearch(query: String, title: String, genres: List<String> = emptyList()): Boolean {
    val q = searchFold(query.trim())
    if (q.isEmpty()) return false
    return searchFold(title).contains(q) || genres.any { searchFold(it).contains(q) }
}
