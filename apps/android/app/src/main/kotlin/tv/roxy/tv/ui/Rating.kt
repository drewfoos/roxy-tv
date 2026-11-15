package tv.roxy.tv.ui

import java.util.Locale

/*
 * A title's community rating, said the same way everywhere: stars out of five (the
 * providers score out of ten), and how many ratings it comes from when the server knows.
 * "★ 3.8 (4K)".
 */

/** "3.8": the providers' 0-10 score as stars out of five; null when there is no score. */
fun starScore(outOfTen: Double?): String? = outOfTen?.takeIf { it > 0 }?.let { String.format(Locale.US, "%.1f", it / 2) }

/** "(87)", "(42.9K)", "(1.2M)": how many ratings a score comes from; null when unknown. */
fun ratingCountLabel(count: Int?): String? {
    val n = count?.takeIf { it > 0 } ?: return null
    fun short(tenths: Int) = if (tenths % 10 == 0) "${tenths / 10}" else "${tenths / 10}.${tenths % 10}"
    return when {
        n < 1_000 -> "($n)"
        n < 1_000_000 -> "(${short(n / 100)}K)"
        else -> "(${short(n / 100_000)}M)"
    }
}

/** "★ 3.8 (4K)", or "★ 3.8" without a count; null when there is no score. */
fun ratingLine(outOfTen: Double?, count: Int? = null): String? =
    starScore(outOfTen)?.let { listOfNotNull("★ $it", ratingCountLabel(count)).joinToString(" ") }
