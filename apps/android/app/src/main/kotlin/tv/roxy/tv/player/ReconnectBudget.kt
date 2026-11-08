package tv.roxy.tv.player

/**
 * Reconnects in a row after a dropped stream, kept apart from the session so it can be
 * tested on the JVM.
 *
 * "In a row" ends only when a reopened stream has really played on: [RECOVERED_AFTER_SEC]
 * past the point it was reopened at. The count used to be reset as soon as a reopened file
 * loaded, so a stream that died two seconds after every reopening started again at the
 * first, immediate attempt each time: a reconnect every couple of seconds, each a new
 * transcode on the server, for ever, and never the wait for the server that the used-up
 * attempts lead to.
 */
internal class ReconnectBudget(private val recoveredAfterSec: Double = RECOVERED_AFTER_SEC) {
    /** Reconnects in a row so far. */
    var used = 0
        private set
    private var from = 0.0

    /** A reconnect at [at] seconds. */
    fun reconnected(at: Double) {
        used++
        from = at
    }

    /** Real playback reached [pos] seconds: far enough past the reconnect, it recovered. */
    fun progress(pos: Double) {
        if (used > 0 && pos >= from + recoveredAfterSec) used = 0
    }

    /** A new item, or the wait for the server begins: a fresh start. */
    fun reset() {
        used = 0
        from = 0.0
    }

    companion object {
        const val RECOVERED_AFTER_SEC = 30.0
    }
}
