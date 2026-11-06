package tv.roxy.tv.data.downloads

/**
 * The rules for resuming a download, kept apart from the service so they can be tested on
 * the JVM.
 *
 * Where to resume: only a part whose validator (ETag or Last-Modified) was recorded can be
 * resumed, since If-Range then makes the server send the whole file if it changed. A part
 * without one cannot be checked against the server's file (a replacement would have its
 * bytes appended to the old ones), so it starts over from byte 0.
 */
internal fun resumeOffset(partLength: Long, validator: String?): Long =
    if (partLength > 0 && validator != null) partLength else 0

private val contentRange = Regex("^bytes (\\d+)-(\\d+)/(\\d+|\\*)$")

/**
 * Whether a 206 answer is exactly the rest of the file the part came from: a Content-Range
 * that starts at [offset] and, when both are known, has the total recorded for the download.
 * Anything else (no or malformed Content-Range, another start, another size) would splice
 * bytes from elsewhere into the part.
 */
internal fun partialFits(header: String?, offset: Long, expectedTotal: Long): Boolean {
    val m = contentRange.find(header?.trim().orEmpty()) ?: return false
    val from = m.groupValues[1].toLongOrNull() ?: return false
    val to = m.groupValues[2].toLongOrNull() ?: return false
    if (from != offset || to < from) return false
    val total = m.groupValues[3].toLongOrNull() ?: return true // "*": size unknown
    return to < total && (expectedTotal <= 0 || total == expectedTotal)
}
