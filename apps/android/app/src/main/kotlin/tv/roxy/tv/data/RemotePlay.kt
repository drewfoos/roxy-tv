package tv.roxy.tv.data

import android.content.Context
import android.os.Build
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.RemoteCommand
import tv.roxy.tv.api.RemoteNow
import tv.roxy.tv.api.RemotePlayer
import tv.roxy.tv.api.RoxyApi
import tv.roxy.tv.isTelevision
import tv.roxy.tv.player.PlayerSession
import tv.roxy.tv.ui.playItem
import java.util.Locale

/** A failed "Play on" status report is sent again after this long, doubling up to the max. */
private const val STATE_RETRY_MIN_MS = 5_000L
private const val STATE_RETRY_MAX_MS = 60_000L

/**
 * "Play on": this device as a player other devices can drive, and as a remote for them.
 *
 * As a player, while the app is on screen and signed in, it long-polls the server for
 * commands ("play this", pause, seek, next) and reports what it is playing; the TV app
 * is the main case. As a remote, the phone picks a [target] from the login's connected
 * players; every play then goes there instead of starting locally, and a bar above the
 * navigation drives it. Both sides are the same class because both apps are one APK.
 */
class RemotePlay(private val app: RoxyApp, private val api: RoxyApi, private val prefs: Prefs, private val scope: CoroutineScope) {

    /** The player other devices see this one as. */
    val deviceName: String = run {
        val maker = Build.MANUFACTURER.orEmpty().replaceFirstChar { it.titlecase(Locale.ROOT) }
        if (isTelevision(app)) "$maker TV" else "$maker ${Build.MODEL}"
    }
    private val client = if (isTelevision(app)) "Roxy TV" else "Roxy phone"

    /** The device the phone plays on, or null for this one. */
    val target = MutableStateFlow<RemotePlayer?>(null)

    /** Connected players (refreshed by the picker, and every few seconds while a target is set). */
    val players = MutableStateFlow<List<RemotePlayer>>(emptyList())

    /** The local player session that is up right now, which incoming commands drive. */
    @Volatile var activePlayer: PlayerSession? = null

    private var foreground = 0
    private var pollJob: Job? = null
    private var watchJob: Job? = null

    // ---- as a player ----

    /** Activities call this from onStart/onStop; the poll runs while any is on screen. */
    fun setForeground(on: Boolean) {
        foreground = (foreground + if (on) 1 else -1).coerceAtLeast(0)
        if (foreground > 0 && pollJob?.isActive != true) pollJob = scope.launch { poll() }
        if (foreground == 0) { pollJob?.cancel(); pollJob = null }
    }

    private suspend fun poll() {
        while (scope.isActive) {
            if (!prefs.session.value.isLoggedIn) { delay(5_000); continue }
            val cmd = runCatching { api.sessionCommands(deviceName, client) }
                .onFailure { Timber.d("remote poll: %s", it.message); delay(5_000) }
                .getOrNull() ?: continue
            Timber.i("remote command %s from %s", cmd.type, cmd.from)
            handle(cmd)
        }
    }

    private fun handle(cmd: RemoteCommand) {
        val p = activePlayer
        when (cmd.type) {
            "play" -> scope.launch {
                runCatching {
                    val item = api.item(cmd.itemId)
                    // Handing over mid-episode: the sender's position becomes the saved one first.
                    if (cmd.positionMs > 0) runCatching { api.progress(item.id, cmd.positionMs) }
                    app.items.put(item)
                    // A position or "from the start" is explicit: a player already showing this
                    // item jumps there instead of carrying on where it was.
                    playItem(app, item, fromStart = cmd.fromStart, startMs = cmd.positionMs.takeIf { it > 0 }).getOrThrow()
                }.onFailure { Timber.w(it, "remote play") }
            }
            "pause" -> p?.remotePause(true)
            "resume" -> p?.remotePause(false)
            "toggle" -> p?.remoteToggle()
            "seek" -> p?.remoteSeekTo(cmd.seconds)
            "seekBy" -> p?.remoteSeekBy(cmd.seconds)
            "next" -> p?.remoteNext()
            "stop" -> p?.remoteStop()
        }
    }

    // Every report is numbered (starting from the clock, so a restarted app keeps counting
    // up) and sent by one coroutine, in order, one at a time; the queue keeps only the
    // newest unsent report. Each used to be its own coroutine, so a "playing" report could
    // reach the server after the "stopped" sent later and show the episode playing again.
    // The server also drops a number older than the last it took (Hub.SetNowPlaying).
    private val stateSeq = java.util.concurrent.atomic.AtomicLong(System.currentTimeMillis() * 1000)
    private val states = Channel<RemoteNow>(Channel.CONFLATED)

    // The newest state asked for. It is not "sent" until the request succeeds: a failed one
    // is sent again by itself (below) for as long as it is still the newest, so an identical
    // report meanwhile is left out (it is in flight or waiting for its retry), and a newer
    // one replaces it. Marking a state sent before the request (as this used to) left the
    // server showing the old state after a failed report, until the position or pause
    // changed; a paused or stopped player reports nothing more, so it could stay wrong.
    private var latest: RemoteNow? = null
    private var retryDelayMs = STATE_RETRY_MIN_MS

    init {
        scope.launch {
            for (s in states) {
                val ok = runCatching { api.sessionState(s) }.onFailure { Timber.d("remote state: %s", it.message) }.isSuccess
                sent(s, ok)
            }
        }
    }

    @Synchronized
    private fun sent(s: RemoteNow, ok: Boolean) {
        if (ok) { retryDelayMs = STATE_RETRY_MIN_MS; return }
        if (latest !== s) return // a newer state is already queued; it supersedes this one
        val wait = retryDelayMs
        retryDelayMs = (retryDelayMs * 2).coerceAtMost(STATE_RETRY_MAX_MS)
        scope.launch {
            delay(wait)
            // Same report, same number: if the failed request did reach the server after all,
            // the server ignores the repeat.
            synchronized(this@RemotePlay) { if (latest === s && prefs.session.value.isLoggedIn) states.trySend(s) }
        }
    }

    /** Called by the local player on every progress report and on stop (itemId null). */
    @Synchronized
    fun reportState(itemId: String?, title: String, positionMs: Long, paused: Boolean) {
        if (!prefs.session.value.isLoggedIn) { latest = null; return }
        val cur = latest
        if (cur != null && cur.itemId == itemId.orEmpty() && cur.positionMs == positionMs && cur.paused == paused) return
        val s = RemoteNow(itemId.orEmpty(), title, positionMs, paused, seq = stateSeq.incrementAndGet())
        latest = s
        states.trySend(s)
    }

    // ---- as a remote ----

    suspend fun refreshPlayers(): List<RemotePlayer> {
        val list = runCatching { api.sessionPlayers() }.getOrElse { Timber.w(it, "players"); emptyList() }
        players.value = list
        target.value?.let { t -> target.value = list.firstOrNull { it.deviceId == t.deviceId } ?: t }
        return list
    }

    /** Pick a device to play on (null = this one). A local playback in progress is handed over. */
    fun select(player: RemotePlayer?) {
        target.value = player
        watchJob?.cancel()
        if (player == null) return
        activePlayer?.let { local ->
            val id = local.itemId
            if (id != null) {
                val pos = local.positionMs()
                local.remotePause(true)
                scope.launch { send(RemoteCommand(type = "play", itemId = id, positionMs = pos)) }
            }
        }
        // Keep the bar's "now playing" fresh while a target is set.
        watchJob = scope.launch { while (isActive && target.value != null) { refreshPlayers(); delay(3_000) } }
    }

    suspend fun send(cmd: RemoteCommand): Boolean {
        val t = target.value ?: return false
        return runCatching { api.sendSessionCommand(t.deviceId, cmd.copy(from = deviceName)); true }
            .getOrElse { Timber.w(it, "remote send"); if (it is tv.roxy.tv.api.ApiException && it.code == 404) { target.value = null }; false }
    }

    companion object {
        fun get(context: Context): RemotePlay = RoxyApp.get(context).remote
    }
}
