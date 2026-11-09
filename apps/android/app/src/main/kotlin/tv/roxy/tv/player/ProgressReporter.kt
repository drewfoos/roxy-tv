package tv.roxy.tv.player

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.launch
import timber.log.Timber
import java.util.concurrent.atomic.AtomicBoolean

/**
 * Watch progress, from the player to the server: what a report says (position, how long
 * ago it was reached, tracks, the version and its length) and how it goes.
 *
 * Every report and watched mark is written to the durable queue first, at once, in the
 * order the methods are called ([Sink.persist]); a sender then drains that queue to the
 * server ([Sink.drain]). So:
 * - a report made just before Android kills the app is sent on the next start (it used to
 *   live in memory until its turn came);
 * - a slow or stalled request holds up nothing that piles up: the queue keeps one entry
 *   per item (the newest), a watched mark ahead of what came after it, and at most one
 *   drain runs with one more waiting;
 * - the order is the queue's order, fixed by the caller, whatever thread or dispatcher.
 * A minute's rest follows a failure to reach the server (not one it answered).
 */
class ProgressReporter(
    private val sink: Sink,
    private val queue: SendQueue,
    /** Monotonic milliseconds (SystemClock.elapsedRealtime): ages. */
    private val monoNow: () -> Long,
    /** Wall-clock milliseconds: the minute's rest while the server is away. */
    private val wallNow: () -> Long = System::currentTimeMillis,
) {
    /** The durable queue and the server, as the reporter uses them (a fake in tests). */
    interface Sink {
        /** Queue a report (durably, now). */
        fun persist(r: Report)
        /** Queue a watched mark (durably, now). */
        fun persistPlayed(itemId: String)
        /** Send the queue, oldest first; null when it was all sent, else the failure that stopped it. */
        suspend fun drain(): Throwable?
        /** Lists and menus on screen reload (the watched state may have changed). */
        fun userDataChanged()
        /** The server answered (with an error): it is not away. */
        fun answered(t: Throwable): Boolean
    }

    data class Report(
        val reason: String, val itemId: String, val positionMs: Long,
        val audioIndex: Int?, val subtitleIndex: Int?, val ageMs: Long,
        val sourceId: String?, val durationMs: Long?,
    )

    constructor(sink: Sink, scope: CoroutineScope, monoNow: () -> Long, wallNow: () -> Long = System::currentTimeMillis) :
        this(sink, SendQueue(scope), monoNow, wallNow)

    /**
     * Runs the drains, one at a time, in order. A drain asked for while one is already
     * waiting is the same drain (it sends whatever is queued when it runs), so a stall
     * never stacks them up. One for the process (the phone page replaces one player with
     * the next): [app].
     */
    class SendQueue(scope: CoroutineScope) {
        private val ops = Channel<suspend () -> Unit>(Channel.UNLIMITED)
        private val waiting = AtomicBoolean(false)
        /**
         * An end, a stop or a watched mark has been queued: lists reload once a drain that
         * started after it has sent everything. Shared (one drain sends every player's
         * entries) and taken when a drain starts: taken when one finished, a drain already
         * under way used it up before the mark it was for had gone.
         */
        internal val refresh = AtomicBoolean(false)

        init {
            scope.launch {
                for (op in ops) {
                    try { op() } catch (e: Exception) {
                        if (e is kotlinx.coroutines.CancellationException) throw e
                        Timber.w(e, "progress: sending failed")
                    }
                }
            }
        }

        /** Run [drain] soon, unless a drain is already waiting to run. */
        fun drainSoon(drain: suspend () -> Unit) {
            if (!waiting.compareAndSet(false, true)) return
            ops.trySend { waiting.set(false); drain() }
        }

        companion object {
            @Volatile private var shared: SendQueue? = null
            /** The process's queue, on the app's scope (it lives as long as the process). */
            fun app(scope: CoroutineScope): SendQueue = shared ?: synchronized(this) { shared ?: SendQueue(scope).also { shared = it } }
        }
    }

    companion object {
        const val SERVER_AWAY_REST_MS = 60_000L
    }

    // Per item: the last position seen and when it moved, the tracks, the last tick sent.
    private var lastPosSec: Double? = null
    private var lastPosAt = monoNow()
    private var lastAudio: Int? = null
    private var lastSub: Int? = null
    private var lastReportedMs = -1L
    @Volatile private var serverDownUntil = 0L

    /** Resting after the server was found away (tests). */
    internal fun resting(): Boolean = wallNow() < serverDownUntil

    /** A new item: nothing carried over from the last. */
    fun newItem() {
        lastPosSec = null; lastPosAt = monoNow(); lastAudio = null; lastSub = null
        lastReportedMs = -1
    }

    /** The player's position now (every UI tick): a report's age counts from its last move. */
    fun notePosition(pos: Double) {
        if (PlayerSession.positionMoved(lastPosSec, pos)) { lastPosSec = pos; lastPosAt = monoNow() }
    }

    /**
     * Report [posSec] for [itemId]: queued now, sent soon. Returns the report, or null when
     * nothing is reported (position 0, a tick with no change).
     */
    fun report(reason: String, itemId: String, posSec: Double, audioIndex: Int?, subtitleIndex: Int?, sourceId: String?, durationMs: Long?): Report? {
        val positionMs = (posSec * 1000).toLong()
        notePosition(posSec)
        if (positionMs <= 0) return null
        if (reason == "tick" && positionMs == lastReportedMs) return null
        lastReportedMs = positionMs
        if (audioIndex != lastAudio || subtitleIndex != lastSub) { lastAudio = audioIndex; lastSub = subtitleIndex; lastPosAt = monoNow() }
        // How long ago this position was reached (or the tracks picked): a player left paused
        // that reports on closing is older than an episode finished on another device since.
        val r = Report(reason, itemId, positionMs, audioIndex, subtitleIndex, monoNow() - lastPosAt, sourceId, durationMs)
        sink.persist(r)
        if (reason == "stop" || reason == "end") queue.refresh.set(true)
        queue.drainSoon(::drain)
        return r
    }

    /** [itemId] is finished (moving on to the next episode): queued after any report made before it. */
    fun markWatched(itemId: String) {
        sink.persistPlayed(itemId)
        queue.refresh.set(true)
        queue.drainSoon(::drain)
    }

    private suspend fun drain() {
        // While the server is unreachable (playing a download away from home) leave the
        // network alone for a minute at a time instead of timing out on every tick; what is
        // queued goes with the next report after it (or when the server is back).
        if (resting()) return
        // Taken before the queue is read: what set it is in what this drain sends.
        val refresh = queue.refresh.getAndSet(false)
        val failure = try { sink.drain() } catch (e: kotlinx.coroutines.CancellationException) { if (refresh) queue.refresh.set(true); throw e }
        when {
            failure == null -> if (refresh) sink.userDataChanged()
            else -> {
                if (refresh) queue.refresh.set(true) // not all sent: the next drain refreshes
                if (!sink.answered(failure)) {
                    Timber.w("progress: server away (%s); resting a minute", failure.message)
                    serverDownUntil = wallNow() + SERVER_AWAY_REST_MS
                }
            }
        }
    }
}
