package tv.roxy.tv.data

import java.util.Locale

/** One language as the settings list and the track policy see it. */
data class Language(
    /** ISO 639-2/T code ("jpn", "deu"); what the app stores. */
    val iso3: String,
    /** ISO 639-1 code ("ja", "de"), or "" when the language has none. */
    val iso2: String,
    val name: String,
) {
    /** Every spelling a container might carry: 639-2/T, 639-2/B where it differs, and 639-1. */
    val codes: List<String> get() = listOfNotNull(iso3, bibliographic[iso3], iso2.takeIf { it.isNotEmpty() }).distinct()
}

/**
 * Every language the platform knows, with the ones a media library meets most often
 * first. Codes are matched loosely: MKV tags use 639-2/B ("ger", "fre"), Java reports
 * 639-2/T ("deu", "fra"), mpv accepts both, so [canonical] folds them all together.
 */
object Languages {
    private val common = listOf("eng", "jpn", "kor", "zho", "spa", "fra", "deu", "por", "ita", "rus", "hin", "ara", "tha", "vie", "ind", "tur", "pol", "nld", "swe", "nor", "dan", "fin")

    val all: List<Language> by lazy {
        val byIso3 = LinkedHashMap<String, Language>()
        for (two in Locale.getISOLanguages()) {
            val loc = Locale(two)
            val three = runCatching { loc.isO3Language }.getOrNull()?.lowercase().orEmpty()
            if (three.isEmpty()) continue
            val name = loc.getDisplayLanguage(Locale.ENGLISH)
            if (name.isEmpty() || name == two) continue
            byIso3[three] = Language(three, two, name)
        }
        val head = common.mapNotNull { byIso3[it] }
        val rest = byIso3.values.filter { it.iso3 !in common }.sortedBy { it.name }
        head + rest
    }

    /** 639-2/T -> 639-2/B for the languages where they differ (MKV files usually carry B). */
    val bibliographic: Map<String, String> = mapOf(
        "deu" to "ger", "fra" to "fre", "zho" to "chi", "nld" to "dut", "ell" to "gre", "ces" to "cze", "fas" to "per", "ron" to "rum",
        "slk" to "slo", "msa" to "may", "isl" to "ice", "mkd" to "mac", "hye" to "arm", "kat" to "geo", "mya" to "bur", "sqi" to "alb",
        "eus" to "baq", "cym" to "wel", "bod" to "tib", "mri" to "mao", "mlg" to "mlg",
    )
    private val fromBibliographic: Map<String, String> = bibliographic.entries.associate { (t, b) -> b to t }
    private val fromIso2: Map<String, String> by lazy { all.filter { it.iso2.isNotEmpty() }.associate { it.iso2 to it.iso3 } }

    /** Any spelling of a language code -> its 639-2/T code; unknown codes come back lowercased. */
    fun canonical(code: String): String {
        val c = code.trim().lowercase().substringBefore('-')
        if (c.isEmpty()) return ""
        fromBibliographic[c]?.let { return it }
        fromIso2[c]?.let { return it }
        return c
    }

    fun byCode(code: String): Language? = all.firstOrNull { it.iso3 == canonical(code) }

    /** "English", "Japanese"; unknown codes shown upper-cased. */
    fun name(code: String): String = byCode(code)?.name ?: code.uppercase()

    /** mpv's language list for one preference: every spelling, so the tag style of the file does not matter. */
    fun mpvCodes(code: String): List<String> = byCode(code)?.codes ?: listOf(code)
}

private val bibliographic get() = Languages.bibliographic
