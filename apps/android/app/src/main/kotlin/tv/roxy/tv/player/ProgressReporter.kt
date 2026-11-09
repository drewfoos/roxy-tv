package tv.roxy.tv.player

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.launch
import timber.log.Timber

/**
 * Watch progress, from the player to the server (or the offline queue): what a report says
 * (position, how long ago it was reached, tracks, the version and its length) and how it
 * goes (one at a time in call order, a minute's rest while the server is away, the offline
 * queue first, queued when it fails). It lived inside PlayerSession; here it is one tested
 * path (ProgressReporterTest), with the server and queue behind [Sink].
 *
 * The sending runs on [queue] (on the app's scope, so a final report outlives the player),
 * in the order the methods were called, whatever thread or dispatcher.
 */
class ProgressReporter(
    private val sink: Sink,
    private val queue: SendQueue,
    /** Monotonic milliseconds (SystemClock.elapsedRealtime): ages. */
    private val monoNow: () -> Long,
    /** Wall-clock milliseconds: the minute's rest while the server is away. */
    private val wallNow: () -> Long = System::currentTimeMillis,
) {
    /** The server and the offline queue, as the reporter uses them (a fake in tests). */
    interface Sink {
        /** POST /v1/me/progress. Throws on failure; [ServerAnswered] when the server answered with an error. */
        suspend fun progress(r: Report)
        suspend fun markPlayed(itemId: String)
        fun isDownload(itemId: String): Boolean
        fun hasPending(itemId: String): Boolean
        suspend fun syncPending()
        fun queue(r: Report, sent: Boolean)
        fun queuePlayed(itemId: String, sent: Boolean)
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
     * Watch-state calls go to the server one at a time, in the order they were made. Moving
     * on to the next episode sends "E1 is at 6:18" and "E1 is watched" together; if the
     * watched mark landed first, the late position turned E1 into a watched episode that is
     * also part-way through. Each call puts its work on this queue at once (the order is
     * fixed there, by the caller); one consumer sends it. A lock alone kept them one at a
     * time but left their order to the dispatcher. One queue for every player (the phone
     * page replaces one player with the next): [app].
     */
    class SendQueue(scope: CoroutineScope) {
        private val ops = Channel<suspend () -> Unit>(Channel.UNLIMITED)

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

        fun enqueue(op: suspend () -> Unit) { ops.trySend(op) }

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
     * Report [posSec] for [itemId]. Returns what is sent, or null when nothing is (no
     * position, position 0, a tick with no change). The sending happens on the scope.
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
        queue.enqueue { send(r) }
        return r
    }

    private suspend fun send(r: Report) {
        // While the server is unreachable (playing a download away from home) skip the
        // network for a minute at a time instead of timing out on every tick.
        val skip = wallNow() < serverDownUntil
        // A "watched" mark from offline goes before this report, or this newer report would
        // make the server drop it as older.
        if (!skip && sink.hasPending(r.itemId)) sink.syncPending()
        val ok = !skip && runCatching { sink.progress(r) }
            // Cancelled (the sending stopped mid-request) is not the server failing: no
            // queueing, no minute's rest. runCatching catches it like any other exception.
            .onFailure { if (it is kotlinx.coroutines.CancellationException) throw it }
            .onSuccess { Timber.i("progress %s item=%s pos=%dms audio=%s sub=%s", r.reason, r.itemId, r.positionMs, r.audioIndex, r.subtitleIndex) }
            .onFailure {
                Timber.w("progress %s item=%s pos=%dms failed: %s", r.reason, r.itemId, r.positionMs, it.message)
                if (!sink.answered(it)) serverDownUntil = wallNow() + SERVER_AWAY_REST_MS
            }
            .isSuccess
        // Downloaded items keep their position locally too (Downloads page offline); a failed
        // report is queued and replayed when the server is back. A success clears this item's
        // queued one, so an older position never replays over it.
        if (sink.isDownload(r.itemId) || !ok || sink.hasPending(r.itemId)) sink.queue(r, sent = ok)
        // The server answers again: send what was saved while it was away.
        if (ok) sink.syncPending()
        // Watched state may have changed (finished, or a rewatch made it "in progress"):
        // every list and menu on screen reloads, like a menu action.
        if (ok && (r.reason == "stop" || r.reason == "end")) sink.userDataChanged()
    }

    /** [itemId] is finished (moving on to the next episode): after any report made before it. */
    fun markWatched(itemId: String) {
        queue.enqueue {
            val ok = wallNow() >= serverDownUntil && runCatching { sink.markPlayed(itemId) }
                .onFailure { if (it is kotlinx.coroutines.CancellationException) throw it }.isSuccess
            if (ok) sink.userDataChanged()
            if (sink.isDownload(itemId) || !ok || sink.hasPending(itemId)) sink.queuePlayed(itemId, sent = ok)
        }
    }
}
