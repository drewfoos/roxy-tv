package tv.roxy.tv.data

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json

/**
 * Can this app and the server it is signed in to work together?
 *
 * The two are updated apart: a phone that was off for months meets a server that was
 * updated every week, and a new app can be pointed at a server nobody has updated. The
 * server states its side in `/v1/system/info` (`api.level`, `api.minClient`; see the
 * server's compat.go), the app holds its own two numbers here, and a mismatch gets a page
 * that says which side to update (ui/CompatPage.kt) instead of pages that fail to load.
 *
 * [CLIENT_LEVEL]: raise it together with the server's `MinClientLevel` when a server
 * change leaves older apps behind, in the first app that works with the changed server.
 *
 * [MIN_SERVER_LEVEL]: raise it to the server's `APILevel` of the day when the app comes
 * to need something older servers lack. A server from before the rule states nothing and
 * counts as level 0, so at 0 no server is refused.
 */
object Compat {
    const val CLIENT_LEVEL = 1
    const val MIN_SERVER_LEVEL = 0

    /** Sent with every call to the server, which refuses an app below its minimum with [REFUSED]. */
    const val HEADER = "X-Roxy-Api"
    const val REFUSED = 426

    enum class Verdict { Ok, AppTooOld, ServerTooOld }

    @Serializable
    data class Levels(val level: Int = 0, val minClient: Int = 0)

    @Serializable
    private data class Info(val product: String = "", val api: Levels? = null, val code: String = "")

    private val json = Json { ignoreUnknownKeys = true; isLenient = true; coerceInputValues = true }

    fun verdict(server: Levels?, clientLevel: Int = CLIENT_LEVEL, minServerLevel: Int = MIN_SERVER_LEVEL): Verdict {
        val s = server ?: Levels()
        return when {
            s.minClient > clientLevel -> Verdict.AppTooOld
            s.level < minServerLevel -> Verdict.ServerTooOld
            else -> Verdict.Ok
        }
    }

    /**
     * The verdict in a `/v1/system/info` answer, or null when [body] is not Roxy's (a
     * proxy's page, a router's login): only Roxy's own word may lock the app.
     */
    fun verdictOf(body: String, clientLevel: Int = CLIENT_LEVEL, minServerLevel: Int = MIN_SERVER_LEVEL): Verdict? {
        val info = runCatching { json.decodeFromString(Info.serializer(), body) }.getOrNull() ?: return null
        if (info.product != "roxy") return null
        return verdict(info.api, clientLevel, minServerLevel)
    }

    /** Is this answer the server's refusal of an app that is too old (and not some proxy's 426)? */
    fun refused(code: Int, body: String): Boolean =
        code == REFUSED && runCatching { json.decodeFromString(Info.serializer(), body) }.getOrNull()?.code == "client_too_old"
}
