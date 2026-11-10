package tv.roxy.tv.api

import com.sun.net.httpserver.HttpServer
import java.net.InetSocketAddress
import java.util.concurrent.CopyOnWriteArrayList

/**
 * A stand-in for Roxy Server on this machine, for tests of what the app really sends: it
 * keeps every request (method, path, query, headers, body) and answers with what [answer]
 * says (status and body; 200 and `{}` unless told otherwise).
 */
class RecordingServer(
    /** Headers to answer with, beside Content-Type. */
    private val answerHeaders: (Seen) -> Map<String, String> = { emptyMap() },
    private val answer: (Seen) -> Pair<Int, String> = { 200 to "{}" },
) : AutoCloseable {
    data class Seen(val method: String, val path: String, val query: String, val headers: Map<String, String>, val body: String) {
        fun header(name: String): String? = headers[name.lowercase()]
    }

    val seen = CopyOnWriteArrayList<Seen>()
    private val server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0).apply {
        createContext("/") { ex ->
            val s = Seen(
                ex.requestMethod, ex.requestURI.path, ex.requestURI.query.orEmpty(),
                ex.requestHeaders.entries.associate { (k, v) -> k.lowercase() to v.joinToString(",") },
                ex.requestBody.readBytes().decodeToString(),
            )
            seen += s
            val (code, body) = answer(s)
            val bytes = body.toByteArray()
            ex.responseHeaders.add("Content-Type", "application/json")
            answerHeaders(s).forEach { (k, v) -> ex.responseHeaders.add(k, v) }
            ex.sendResponseHeaders(code, bytes.size.toLong().takeIf { it > 0 } ?: -1)
            if (bytes.isNotEmpty()) ex.responseBody.use { it.write(bytes) }
            ex.close()
        }
        start()
    }

    val url: String get() = "http://127.0.0.1:${server.address.port}"

    override fun close() = server.stop(0)
}
