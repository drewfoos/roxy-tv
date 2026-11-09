package tv.roxy.tv.ui.detail

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.withContext
import tv.roxy.tv.api.Item
import tv.roxy.tv.api.ItemsResponse
import tv.roxy.tv.api.RelatedEntry
import tv.roxy.tv.api.Section
import java.io.IOException

/**
 * A server the test answers by hand: every call waits until the test responds to it, so the
 * order in which answers land is the test's choice. With [ignoresCancel] a call answers even
 * after its caller was cancelled, like an HTTP call already on the wire.
 */
class FakeSeriesServer(private val ignoresCancel: Boolean = false) : SeriesSource {
    val calls = mutableListOf<String>()
    private val waiting = mutableMapOf<String, ArrayDeque<CompletableDeferred<Result<Any?>>>>()

    @Suppress("UNCHECKED_CAST")
    private suspend fun <T> call(key: String): T {
        calls += key
        val d = CompletableDeferred<Result<Any?>>()
        waiting.getOrPut(key) { ArrayDeque() }.addLast(d)
        val r = if (ignoresCancel) withContext(NonCancellable) { d.await() } else d.await()
        return r.getOrThrow() as T
    }

    fun pending(key: String) = waiting[key]?.size ?: 0
    private fun take(key: String, newest: Boolean) = waiting.getValue(key).let { if (newest) it.removeLast() else it.removeFirst() }
    /** Answers the oldest waiting call of [key] (the newest with [newest]). */
    fun answer(key: String, value: Any?, newest: Boolean = false) = take(key, newest).complete(Result.success(value))
    fun fail(key: String, newest: Boolean = false) = take(key, newest).complete(Result.failure(IOException("network down")))

    override suspend fun item(id: String): Item = call("item")
    override suspend fun seasons(seriesId: String): List<Item> = call("seasons")
    override suspend fun sections(seriesId: String): List<Section> = call("sections")
    override suspend fun next(seriesId: String): Item? = call("next")
    override suspend fun related(seriesId: String): List<RelatedEntry> = call("related")
    override suspend fun extras(seriesId: String): List<Item> = call("extras")
    override suspend fun episodes(parentId: String, recursive: Boolean): ItemsResponse = call("episodes:$parentId")
}
