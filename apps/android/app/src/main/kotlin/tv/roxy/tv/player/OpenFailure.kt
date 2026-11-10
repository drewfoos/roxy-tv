package tv.roxy.tv.player

private val HTTP_STATUS = Regex("""(?i)\b(?:HTTP error|Server returned) ([45])(\d\d|XX)\b""")

/**
 * The HTTP status in one of mpv's log lines about a refused open ("http: HTTP error 404 Not
 * Found", "Server returned 5XX Server Error reply"), or null when the line has none. ffmpeg
 * names some answers only by their class: those come back as 400 and 500.
 */
internal fun httpErrorStatus(line: String): Int? {
    val m = HTTP_STATUS.find(line) ?: return null
    return m.groupValues[1].toInt() * 100 + (m.groupValues[2].toIntOrNull() ?: 0)
}

/** What follows a used-up round of reconnects (see [afterAttemptsUsedUp]). */
internal enum class AfterAttempts { Wait, GiveUp }

/**
 * All the quick reconnects failed. The wait that follows asks an endpoint that answers
 * whenever the server is up, and then starts a fresh round, which is right for a server
 * that is away and wrong for one that is there and will not serve this stream (the file is
 * gone, the source was removed, the transcode cannot start): that was reopened for ever.
 *
 * So with the server answering ([serverUp]): a 4xx for the stream ([httpStatus], from the
 * last failed open) is its answer and asking again changes nothing; anything else (a 5xx, a
 * stream that opens and dies) gets one wait and one more round, and stops when that fails
 * too ([failedRounds] counts rounds used up with the server answering and no real playback
 * in between, this one included). A server that does not answer is waited for as before.
 */
internal fun afterAttemptsUsedUp(serverUp: Boolean, httpStatus: Int?, failedRounds: Int): AfterAttempts = when {
    !serverUp -> AfterAttempts.Wait
    httpStatus != null && httpStatus in 400..499 -> AfterAttempts.GiveUp
    failedRounds >= 2 -> AfterAttempts.GiveUp
    else -> AfterAttempts.Wait
}

/** What the player says when it stops trying ([httpStatus]: the server's answer, when it gave one). */
internal fun cannotOpenMessage(httpStatus: Int?): String =
    if (httpStatus != null) "Roxy could not open this video (error $httpStatus). Press play to try again."
    else "Roxy is there, but this video keeps failing. Press play to try again."
