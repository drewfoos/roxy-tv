package tv.roxy.tv.api

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.async
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import okhttp3.OkHttpClient
import okhttp3.Request
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.net.ServerSocket
import java.util.concurrent.TimeUnit
import kotlin.concurrent.thread

class CancellableCallTest {
    private val client = OkHttpClient.Builder().readTimeout(30, TimeUnit.SECONDS).build()

    // A server that takes the request and never answers: cancelling the caller closes the
    // connection at once (the server sees the end of the stream), instead of the call
    // running on until the 30 s read timeout.
    @Test(timeout = 10_000) fun cancellingTheCallerStopsTheCall() = runBlocking {
        ServerSocket(0).use { server ->
            var closedAfterMs = -1L
            var cancelledAt = 0L
            val side = thread {
                server.accept().use { sock ->
                    val input = sock.getInputStream()
                    // Read the request, then wait for the client to hang up.
                    val buf = ByteArray(4096)
                    while (input.read(buf) >= 0) Unit
                    closedAfterMs = System.currentTimeMillis() - cancelledAt
                }
            }
            val job = async(Dispatchers.Default) { client.fetch(Request.Builder().url("http://127.0.0.1:${server.localPort}/v1/items").build()) }
            delay(300)
            cancelledAt = System.currentTimeMillis()
            job.cancel()
            withTimeout(5_000) { job.join() }
            side.join(5_000)
            assertTrue("the server should see the connection closed; took $closedAfterMs ms", closedAfterMs in 0..3_000)
        }
    }

    // An answered call reads the whole body, status and all.
    @Test(timeout = 10_000) fun answeredCallReadsTheBody() = runBlocking {
        ServerSocket(0).use { server ->
            thread {
                server.accept().use { sock ->
                    val input = sock.getInputStream().bufferedReader()
                    while (input.readLine().orEmpty().isNotEmpty()) Unit
                    sock.getOutputStream().write("HTTP/1.1 404 Not Found\r\nContent-Length: 15\r\nConnection: close\r\n\r\n{\"error\":\"no\"}\n".toByteArray())
                }
            }
            val r = client.fetch(Request.Builder().url("http://127.0.0.1:${server.localPort}/x").build())
            assertEquals(404, r.code)
            assertEquals(false, r.ok)
            assertEquals("{\"error\":\"no\"}\n", r.text)
        }
    }

    // Given up between the read and the parse: the parse does not run.
    @Test fun noParseAfterCancel() = runBlocking {
        var parsed = false
        val job = launch(Dispatchers.Default) {
            coroutineContext.cancel() // the cancel lands now, the body already read
            parseUnlessCancelled("[1,2,3]") { parsed = true; it.length }
        }
        job.join()
        assertTrue(job.isCancelled)
        assertEquals(false, parsed)
        // Not cancelled: parsed as usual.
        assertEquals(7, parseUnlessCancelled("[1,2,3]") { it.length })
    }
}
