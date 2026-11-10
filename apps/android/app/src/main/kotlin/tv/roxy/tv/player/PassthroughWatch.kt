package tv.roxy.tv.player

/**
 * One file's passthrough watchdog (see MpvPlayer.watchPassthrough): polled every
 * [POLL_MS] while the file plays; [Verdict.Failed] when the position has not moved after
 * [GIVE_UP_MS] of the player really trying to play (time held by a pause, buffering or a
 * seek does not count). It belongs to the file it was armed for: [url] is that file, and a
 * new file cancels it. Without that it went on polling into the next file and reopened the
 * NEW file at the OLD one's position. Main thread only.
 */
internal class PassthroughWatch(val url: String, val transcoded: Boolean, private val startPos: Double) {
    sealed interface Verdict {
        /** Poll again in [POLL_MS]. */
        data object Wait : Verdict
        /** Nothing more to watch: the audio clock runs, the watch was cancelled, or it gave up polling. */
        data object Done : Verdict
        /** Passthrough never started: decode the codec here and reopen [url] at [at]. */
        data class Failed(val at: Double) : Verdict
    }

    private var tryingMs = 0L
    private var polls = 0
    var cancelled = false
        private set

    /** Another file is loading: this one's watch is over. */
    fun cancel() { cancelled = true }

    /** [pos]: mpv's time-pos now; [held]: paused, buffering or seeking. */
    fun poll(pos: Double?, held: Boolean): Verdict {
        if (cancelled) return Verdict.Done
        polls++
        if (pos != null && pos > startPos + 0.5) return Verdict.Done // the audio clock runs
        if (!held) tryingMs += POLL_MS
        if (tryingMs < GIVE_UP_MS) return if (polls < MAX_POLLS) Verdict.Wait else Verdict.Done
        cancelled = true
        return Verdict.Failed(pos ?: startPos)
    }

    companion object {
        const val POLL_MS = 1500L
        const val GIVE_UP_MS = 6000L
        private const val MAX_POLLS = 400
    }
}
