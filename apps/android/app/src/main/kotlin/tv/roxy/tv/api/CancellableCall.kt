package tv.roxy.tv.api

import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.awaitCancellation
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import okhttp3.OkHttpClient
import okhttp3.Request
import java.util.concurrent.atomic.AtomicBoolean

/** A response read whole: its status and body text. */
class Fetched(val code: Int, val ok: Boolean, val text: String)

/**
 * Runs [req] and reads its body, and stops the HTTP call when the coroutine is cancelled.
 * A blocking execute() went on to the end after its caller had given up (a season switched
 * away from, a page read replaced by a newer one): a 20000-episode list was still downloaded
 * and parsed for nothing. A watcher beside the call cancels it, which makes execute() and
 * the body read throw at once. Still execute(), not OkHttp's queue: the queue's per-host
 * limit is shared with images and would hold JSON calls behind them.
 */
suspend fun OkHttpClient.fetch(req: Request): Fetched = withContext(Dispatchers.IO) {
    val call = newCall(req)
    val done = AtomicBoolean(false)
    val watcher = launch(start = CoroutineStart.UNDISPATCHED) {
        try { awaitCancellation() } finally { if (!done.get()) call.cancel() }
    }
    try {
        call.execute().use { Fetched(it.code, it.isSuccessful, it.body.string()) }
    } catch (e: java.io.IOException) {
        // Stopped because the caller gave up: that is a cancellation, not a network error
        // (as an error it would be shown, "Couldn't load", for a season already left).
        ensureActive()
        throw e
    } finally {
        done.set(true)
        watcher.cancel()
    }
}
