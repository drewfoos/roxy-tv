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

    /**
     * Rounds of reconnects in a row that were used up while the server answered, with no
     * real playback in between: the stream itself is what fails (see afterAttemptsUsedUp).
     */
    var failedRounds = 0
        private set

    /** A reconnect at [at] seconds. */
    fun reconnected(at: Double) {
        used++
        from = at
    }

    /** Real playback reached [pos] seconds: far enough past the reconnect, it recovered. */
    fun progress(pos: Double) {
        if ((used > 0 || failedRounds > 0) && pos >= from + recoveredAfterSec) {
            used = 0
            failedRounds = 0
        }
    }

    /**
     * Every attempt of this round failed and the wait begins: the next round starts at the
     * first attempt. Unlike [reset] it is not a fresh start: [roundFailed] keeps count.
     */
    fun attemptsUsedUp() {
        used = 0
    }

    /**
     * The round that was just used up, once the server has been asked: with it answering
     * ([serverUp]) the round counts against the stream; with it away the failures were the
     * connection's, and the count starts again.
     */
    fun roundFailed(serverUp: Boolean) {
        failedRounds = if (serverUp) failedRounds + 1 else 0
    }

    /** A new item: a fresh start. */
    fun reset() {
        used = 0
        from = 0.0
        failedRounds = 0
    }

    companion object {
        const val RECOVERED_AFTER_SEC = 30.0
    }
}
