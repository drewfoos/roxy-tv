package tv.roxy.tv.player

/**
 * Where the player is with the file it plays: one phase, instead of the flags PlayerSession
 * used to keep in step by hand (switching, changingItem, reconnecting, waitingAt,
 * endReported, endPending, nextKnown, nextFailed, newFileRequested). Several of the bugs
 * found in November 2026 were combinations of those flags nobody meant (Next during a wait
 * for the server reopened the new episode at the old one's position; an old file reloading
 * mid-switch was taken for the new one). Here every question the player asks is answered
 * from the phase, and every change is a named transition, tested in PlaybackLifecycleTest.
 *
 * Plain Kotlin, no Android: the session changes it on the main thread.
 */
class PlaybackLifecycle {

    /** Why a file is being opened. */
    enum class Why {
        /** The player's first file (there is no older one to ignore). */
        First,
        /** The next episode, on the same player. */
        Next,
        /** A seek outside a transcode's buffer, or a change of quality: the same item, a new stream. */
        Reload,
        /** A lost stream reopened where it stopped. */
        Reconnect,
    }

    sealed interface Phase {
        /**
         * A file is being opened. [fileRequested]: it has been handed to mpv (until then mpv
         * still has the old one). [resumeWait]: a reload started while waiting for the server
         * goes back to that wait if it never starts.
         */
        data class Opening(val why: Why, val fileRequested: Boolean, val resumeWait: Double? = null) : Phase
        data object Playing : Phase
        /** The stream was lost at [at]; it is reopened there after a short wait. */
        data class Reconnecting(val at: Double) : Phase
        /** Reconnects used up: paused on [at] until the server answers. */
        data class WaitingForServer(val at: Double) : Phase
        /** This file played to its end ([waitingForNext]: waiting for the next-episode lookup). */
        data class Ended(val waitingForNext: Boolean) : Phase
        /** A downloaded file would not open: nothing plays, nothing to reconnect to. */
        data object Failed : Phase
    }

    /** What is known of the episode after this one. */
    enum class NextLookup { Pending, Answered, Failed }

    // Read from other threads too (the remote's position, mpv callbacks), as the flags were.
    @Volatile var phase: Phase = Phase.Opening(Why.First, fileRequested = true)
        private set

    /**
     * The current item's own file has opened at least once. False from Next until then: mpv
     * still plays the previous episode (a stream waits for the server's subtitle languages
     * and a token first), so its position belongs to that one, and the item, never opened,
     * is not "finished" by moving on.
     */
    @Volatile var itemOpened: Boolean = true
        private set

    @Volatile var nextLookup: NextLookup = NextLookup.Pending
        private set

    /**
     * The current item's own stream has been asked of mpv (by the start, a reconnect or a
     * reload). Until then a load is the old file reopening, whatever else happens meanwhile:
     * a reconnect begun before the next episode's file was asked for used to drop this.
     */
    @Volatile private var itemFileRequested: Boolean = true

    // ---- questions ----

    /** A new stream is on its way: the old stream's end events are not the end of this item. */
    val switching: Boolean
        get() = when (val p = phase) {
            is Phase.Opening -> p.why != Why.First // nothing older to ignore; Next must stay possible if it never opens
            is Phase.Reconnecting -> true
            else -> false
        }

    /** Positions read from mpv are not this item's (see [itemOpened]). */
    val changingItem: Boolean get() = !itemOpened

    /**
     * Moving on from this item finishes it (reported at its end, marked watched): only an
     * item that really opened. One that never did (a failed switch, a download that would
     * not open) is left as it was.
     */
    val finishedByMovingOn: Boolean get() = itemLoaded

    /**
     * The current item's file really loaded (not just "its positions are its own", which the
     * first item is from the start). Set only by [fileLoaded].
     */
    @Volatile private var itemLoaded: Boolean = false

    val reconnecting: Boolean
        get() = phase is Phase.Reconnecting || (phase as? Phase.Opening)?.why == Why.Reconnect

    /** Where the player waits for the server, or null. */
    val waitingAt: Double? get() = (phase as? Phase.WaitingForServer)?.at

    val ended: Boolean get() = phase is Phase.Ended

    val waitingForNext: Boolean get() = (phase as? Phase.Ended)?.waitingForNext == true

    val nextKnown: Boolean get() = nextLookup == NextLookup.Answered

    // ---- transitions ----

    /** The next episode takes over the player. Its file is asked of mpv later ([fileRequested]). */
    fun startNext() {
        phase = Phase.Opening(Why.Next, fileRequested = false)
        itemOpened = false
        itemLoaded = false
        itemFileRequested = false
        nextLookup = NextLookup.Pending
    }

    /** The current item's file has been handed to mpv. */
    fun fileRequested() {
        itemFileRequested = true
        (phase as? Phase.Opening)?.let { phase = it.copy(fileRequested = true) }
    }

    /**
     * mpv loaded a file. False when it is not this item's: mid-switch, before the next
     * episode's file was asked for, a load is the old file reopening (the passthrough
     * watchdog, a token retry). Otherwise the item plays.
     */
    fun fileLoaded(): Boolean {
        if (!itemOpened && !itemFileRequested) return false
        phase = Phase.Playing
        itemOpened = true
        itemLoaded = true
        return true
    }

    /**
     * A download would not open: there is nothing to reconnect to. The player stays, idle,
     * and the item is NOT opened (it used to count as opened, so Next marked a downloaded
     * episode that never played as watched).
     */
    fun localFileFailed() {
        phase = Phase.Failed
        itemFileRequested = true
    }

    /** A new stream of the same item (a seek past a transcode's buffer, a quality change). */
    fun reloadStarted() {
        // The item's own stream (its source and URL already name it), even mid-switch.
        itemFileRequested = true
        // Inside a reconnect (a transcode reopened) it is still the reconnect.
        phase = Phase.Opening(if (reconnecting) Why.Reconnect else Why.Reload, fileRequested = true, resumeWait = waitingAt)
    }

    /** A reload that never started (the server could not open that stream): back to playing. False if something else happened since. */
    fun reloadTimedOut(): Boolean {
        val p = phase
        if (p !is Phase.Opening || p.why != Why.Reload) return false
        // Started while waiting for the server: back to waiting (it resumes when the server answers).
        phase = p.resumeWait?.let { Phase.WaitingForServer(it) } ?: Phase.Playing
        return true
    }

    /** The stream was lost at [at]: reopened there after a short wait. */
    fun reconnectScheduled(at: Double) {
        phase = Phase.Reconnecting(at)
    }

    /** The wait is over: the stream is asked for again. */
    fun reopening() {
        itemFileRequested = true // the reconnect plays the item's own stream
        if (phase is Phase.Reconnecting) phase = Phase.Opening(Why.Reconnect, fileRequested = true)
    }

    /** Reconnects used up: paused until the server answers. */
    fun waitForServer(at: Double) {
        phase = Phase.WaitingForServer(at)
    }

    /** The server answered (or Play was pressed) while waiting: where to reopen, or null when not waiting. */
    fun resumeFromWait(): Double? = waitingAt

    /** The file ended. True the first time (report the end); false when its end is already known. */
    fun fileEnded(): Boolean {
        if (phase is Phase.Ended) return false
        phase = Phase.Ended(waitingForNext = false)
        return true
    }

    /** Start waiting for the next-episode lookup. False when already waiting (eof-reached after an end-file). */
    fun startWaitingForNext(): Boolean {
        val p = phase as? Phase.Ended ?: return false
        if (p.waitingForNext) return false
        phase = Phase.Ended(waitingForNext = true)
        return true
    }

    /** The wait for the next episode is over (answered, or timed out). False when nothing was waiting. */
    fun stopWaitingForNext(): Boolean {
        if (!waitingForNext) return false
        phase = Phase.Ended(waitingForNext = false)
        return true
    }

    fun lookupStarted() { nextLookup = NextLookup.Pending }
    fun lookupAnswered() { nextLookup = NextLookup.Answered }
    fun lookupFailed() { nextLookup = NextLookup.Failed }
    /** Nothing to look up (no item): the answer is "none". */
    fun noLookup() { nextLookup = NextLookup.Answered }
}
