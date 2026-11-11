package tv.roxy.tv.data

import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.api.ApiException
import tv.roxy.tv.api.RoxyApi
import java.io.IOException

/**
 * Is Roxy Server answering? Every call to it reports in (see [RoxyApi.onReachability]);
 * a failed connection starts a probe, and while the server is away the probe repeats
 * (3 s, growing to 30 s, only while the app is on screen) until it answers again.
 *
 * [online] drives the "Can't reach Roxy" banner; [backOnline] counts the returns, so a
 * screen showing an error can reload by itself (see OnServerBack in the UI).
 */
class ServerStatus(private val context: Context, private val api: RoxyApi, private val prefs: Prefs, private val scope: CoroutineScope) {

    private val _online = MutableStateFlow(true)
    val online: StateFlow<Boolean> get() = _online

    private val _backOnline = MutableStateFlow(0)
    val backOnline: StateFlow<Int> get() = _backOnline

    /**
     * Whether the device has a network at all. With one and the server silent, the phone is
     * away from home (or the server is down), not offline: the notices say which.
     */
    private val _hasNetwork = MutableStateFlow(true)
    val hasNetwork: StateFlow<Boolean> get() = _hasNetwork

    /** True while a player page is up on the phone: it shows its own "reconnecting" message. */
    val playerActive = MutableStateFlow(false)

    /** Runs each time the server answers again after being away (sync offline progress, resume downloads). */
    @Volatile var onBack: (() -> Unit)? = null

    @Volatile private var foreground = 0

    /** Fails calls at once while the server is away, on a look no older than [AwayGate.FRESH_MS]. */
    private val gate = AwayGate(now = { android.os.SystemClock.elapsedRealtime() }, probe = { api.reachableNow().also { if (it) scope.launch { markUp() } } })
    private var probeJob: Job? = null

    init {
        // Called on OkHttp's threads: state changes happen on the scope's (main) thread.
        api.onReachability = { ok -> if (!ok) checkSoon() else if (!_online.value) scope.launch { markUp() } }
        api.awayGate = { prefs.session.value.isLoggedIn && gate.failNow(away = !_online.value) }
        watchNetwork()
    }

    /** Activities call this from onStart/onStop; the probe only repeats while something is on screen. */
    fun setForeground(on: Boolean) {
        foreground = (foreground + if (on) 1 else -1).coerceAtLeast(0)
        if (foreground > 0 && !_online.value) checkSoon()
    }

    /** Check now (a Retry button, a network change). */
    fun checkSoon() {
        scope.launch {
            // A probe between two looks (up to 30 s apart) starts over: Retry, a new network
            // or a failed call means "look now", not "at the next turn".
            if (probeJob?.isActive == true && !resting) return@launch
            probeJob?.cancel()
            probeJob = scope.launch { probe() }
        }
    }

    /** The probe is waiting for its next look (main thread only). */
    private var resting = false

    private fun markUp() {
        if (_online.value) return
        _online.value = true
        _backOnline.value++
        Timber.i("server reachable again")
        runCatching { onBack?.invoke() }.onFailure { Timber.w(it, "server back handler") }
    }

    private suspend fun probe() {
        var wait = 3_000L
        while (scope.isActive) {
            if (!prefs.session.value.isLoggedIn) return
            if (api.reachable()) { gate.looked(true); markUp(); return }
            gate.looked(false)
            if (_online.value) { _online.value = false; Timber.w("server unreachable") }
            // Off screen: stop; the next onStart (or a network change) checks again.
            if (foreground == 0) return
            resting = true
            try { delay(wait) } finally { resting = false }
            wait = (wait * 2).coerceAtMost(30_000L)
        }
    }

    /** A new network (Wi-Fi back, a switch from cellular) is the moment to look again. */
    private fun watchNetwork() {
        val cm = context.getSystemService(Context.CONNECTIVITY_SERVICE) as? ConnectivityManager ?: return
        val req = NetworkRequest.Builder().addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET).build()
        runCatching {
            // The networks that are up now: none left means offline (asking the system for
            // "the active one" inside onLost can still name the one just lost).
            val up = java.util.Collections.synchronizedSet(HashSet<Network>())
            _hasNetwork.value = cm.activeNetwork != null
            cm.registerNetworkCallback(req, object : ConnectivityManager.NetworkCallback() {
                override fun onAvailable(network: Network) { up += network; _hasNetwork.value = true; if (!_online.value) checkSoon() }
                override fun onLost(network: Network) {
                    up -= network
                    _hasNetwork.value = up.isNotEmpty()
                    // Wi-Fi gone with mobile data still up (or nothing): find out which at once.
                    checkSoon()
                }
            })
        }.onFailure { Timber.w(it, "server status network callback") }
    }
}

/**
 * Whether a call to the server should fail without being sent. While the server is away a
 * call is refused at once if the last look ([probe], 3 s at most) was under [FRESH_MS] ago;
 * otherwise it waits for one new look, shared by every call that arrives meanwhile. So a
 * page opened away from home says "Can't reach Roxy" at once (3 s at worst) instead of
 * after the connect timeout, and a server that came back is noticed by the next call.
 */
class AwayGate(private val now: () -> Long, private val probe: () -> Boolean) {
    private var lookedAt = Long.MIN_VALUE
    private var answered = true

    /** The background probe's result, so a call right after it does not look again. */
    @Synchronized fun looked(ok: Boolean) { lookedAt = now(); answered = ok }

    @Synchronized fun failNow(away: Boolean): Boolean {
        if (!away) return false
        if (lookedAt == Long.MIN_VALUE || now() - lookedAt >= FRESH_MS) looked(probe())
        return !answered
    }

    companion object { const val FRESH_MS = 10_000L }
}

/** The words of the phone's offline notices: the strip over the bottom bar, and a page that cannot load. */
data class OfflineWords(val strip: String, val title: String, val body: String, /** One line for the notice over a Home made of downloads. */ val short: String)

/**
 * No network at all reads "You're offline"; a network with the server silent says so
 * instead (away from home, Tailscale off, the server down), since the fix differs. Either
 * way the last sentence says what still works: the [downloads] finished on this phone.
 */
fun offlineWords(hasNetwork: Boolean, downloads: Int): OfflineWords {
    val still = if (downloads > 0) "Your downloads still play." else "Episodes and movies you download play without it."
    return if (hasNetwork) OfflineWords(
        strip = "Can't reach your Roxy server",
        title = "Can't reach your server",
        body = "This phone is online, but Roxy isn't answering. You may be away from your home network or Tailscale. $still",
        short = "Showing what's downloaded on this phone.",
    ) else OfflineWords(
        strip = "No internet connection",
        title = "You're offline",
        body = "Connect to Wi-Fi or mobile data to browse your library. $still",
        short = "Showing what's downloaded on this phone.",
    )
}

/** The words of the TV's notices while the server is away: the pill at the top, and a page that cannot load. */
data class TvOfflineWords(val pill: String, val title: String, val body: String)

/**
 * The TV has no downloads, so its notices only say what is wrong and what to check: the
 * TV's own connection when it has no network, the server when it has one.
 */
fun tvOfflineWords(hasNetwork: Boolean): TvOfflineWords = if (hasNetwork) TvOfflineWords(
    pill = "Can't reach your Roxy server  ·  Reconnecting…",
    title = "Can't reach your Roxy server",
    body = "This TV is online, but Roxy isn't answering. Check that the server is on and on the same network. This page loads by itself when it's back.",
) else TvOfflineWords(
    pill = "No internet connection",
    title = "This TV is offline",
    body = "Check the TV's Wi-Fi or network cable. Roxy comes back by itself once it's connected.",
)

/**
 * Words for an error shown to people: a connection failure reads "Can't reach Roxy"
 * instead of "failed to connect to /192.168.1.50 (port 8096)…"; the server's own
 * messages (ApiException) pass through.
 */
fun friendlyError(t: Throwable, what: String? = null): String = when {
    t is ApiException && t.isUnauthorized -> "Your session has expired. Sign in again."
    t is ApiException && t.code >= 500 -> "Roxy had a problem${what?.let { " loading $it" } ?: ""}. Try again in a moment."
    t is ApiException -> t.message ?: "Something went wrong"
    t is IOException -> "Can't reach Roxy right now."
    else -> t.message ?: "Something went wrong"
}
