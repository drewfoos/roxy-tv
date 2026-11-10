package tv.roxy.tv.player

/**
 * When to ask the display for a film's frame rate: only once the file's first frame is up.
 *
 * Found on the TV (November 2026): the request went out as the file loaded, and when the
 * display answered before the first frame, mpv's video output waited 2.6 to 3.4 s for it
 * (the display stayed at 60 Hz either way). Starts where the answer came after the first
 * frame took 1.3 s. Holding the request until the first frame makes that order certain.
 * Main thread only.
 */
internal class FrameRateGate {
    private var fps = 0f
    private var shown = false
    /** A file was handed to mpv and its START_FILE has not come yet: frames up now are the old file's. */
    private var awaitingStart = false

    /**
     * A file was handed to mpv (loadfile). Nothing is reset yet: mpv still plays the old
     * file, and its queued playback-restart must not count as the new file's first frame.
     * Only a real load calls this (a surface coming back with nothing to load does not).
     */
    fun loadRequested() { awaitingStart = true }

    /** mpv's START_FILE, in event order: the new file is opening; hold its rate until its first frame. */
    fun fileStarted() { awaitingStart = false; shown = false; fps = 0f }

    /** The file's rate is known: the rate to ask for now, or null while held. */
    fun hint(rate: Float): Float? { fps = rate; return if (shown && !awaitingStart) rate else null }

    /** A frame is up (mpv's playback-restart, also after seeks): the held rate, the first time only. */
    fun firstFrame(): Float? {
        if (shown || awaitingStart) return null
        shown = true
        return fps.takeIf { it != 0f }
    }
}
