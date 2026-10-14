package tv.roxy.tv.api

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import okhttp3.Interceptor
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import timber.log.Timber
import tv.roxy.tv.data.Prefs
import java.io.IOException
import java.util.concurrent.TimeUnit

/** An HTTP error from Roxy Server. `code == 404` on a contract endpoint means "server not updated yet". */
class ApiException(val code: Int, message: String) : IOException(message) {
    val isUnauthorized: Boolean get() = code == 401
    val isNotFound: Boolean get() = code == 404
}

/**
 * Client for Roxy Server's native API (docs/api/native.md). Suspend functions, lenient JSON:
 * the app reads only the fields it needs and ignores the rest. Server URL and token come
 * from [Prefs.session]; [http] adds `Authorization: Bearer` to every request (Coil shares it
 * for images).
 */
class RoxyApi(private val prefs: Prefs) {

    private val json = Json { ignoreUnknownKeys = true; isLenient = true; coerceInputValues = true; explicitNulls = false }

    val http: OkHttpClient = OkHttpClient.Builder()
        .connectTimeout(10, TimeUnit.SECONDS)
        .readTimeout(30, TimeUnit.SECONDS)
        .addInterceptor(Interceptor { chain ->
            val token = prefs.session.value.token
            val req = chain.request()
            if (token != null && req.header("Authorization") == null) {
                chain.proceed(req.newBuilder().header("Authorization", "Bearer $token").build())
            } else chain.proceed(req)
        })
        .build()

    val baseUrl: String get() = prefs.session.value.serverUrl.trimEnd('/')
    private val token: String? get() = prefs.session.value.token

    // ---- auth ----

    /** Logs in against [serverUrl] (not the stored one) so the login screen can try a new server. Does not persist anything. */
    suspend fun login(serverUrl: String, username: String, password: String, device: String, deviceId: String, version: String): LoginResponse =
        withContext(Dispatchers.IO) {
            val body = LoginRequest(username, password, client = "Roxy TV", device = device, deviceId = deviceId, version = version)
            val req = Request.Builder()
                .url(serverUrl.trimEnd('/') + "/v1/auth/login")
                .post(json.encodeToString(LoginRequest.serializer(), body).toRequestBody(JSON_TYPE))
                .build()
            execute<LoginResponse>(req)
        }

    suspend fun me(): MeResponse = get("/v1/auth/me")

    suspend fun logout() { postNoBody("/v1/auth/logout", "") }

    // ---- library ----

    suspend fun home(): HomeResponse = get("/v1/home")

    suspend fun items(
        type: String,
        parent: String? = null,
        recursive: Boolean = false,
        sort: String? = null,
        desc: Boolean = false,
        limit: Int? = null,
        includeSources: Boolean = false,
    ): ItemsResponse {
        val q = buildList {
            add("type=$type")
            parent?.let { add("parent=$it") }
            if (recursive) add("recursive=1")
            sort?.let { add("sort=$it") }
            if (desc) add("desc=1")
            limit?.let { add("limit=$it") }
            if (includeSources) add("include=sources")
        }.joinToString("&")
        return get("/v1/items?$q")
    }

    suspend fun item(id: String): Item = get("/v1/items/$id")

    /** Franchise view: movies/OVAs/side stories linked to an item, and the series it is part of. */
    suspend fun related(id: String): RelatedResponse = get("/v1/items/$id/related")

    suspend fun source(id: String): MediaSource = get("/v1/sources/$id")

    // ---- watch state ----

    suspend fun resume(limit: Int = 20): ItemsResponse = get("/v1/me/resume?limit=$limit")

    suspend fun progress(itemId: String, positionMs: Long, audioIndex: Int? = null, subtitleIndex: Int? = null) {
        val body = ProgressRequest(itemId, positionMs, audioIndex, subtitleIndex)
        postNoBody("/v1/me/progress", json.encodeToString(ProgressRequest.serializer(), body))
    }

    // ---- urls the player and image loader fetch on their own ----

    /** libmpv cannot send headers, so the token rides as `api_key`. */
    fun streamUrl(sourceId: String): String = "$baseUrl/v1/stream/$sourceId?api_key=$token"

    /** Absolute image URL for an item's image, or null when the server has none (no 404 spam). */
    fun imageUrl(item: Item, type: String, widthPx: Int? = null): String? {
        val rel = item.images[type] ?: return null
        val abs = if (rel.startsWith("http")) rel else baseUrl + rel
        return if (widthPx != null && widthPx > 0) "$abs?w=$widthPx" else abs
    }

    // ---- player helpers ----

    /** Subtitle streams of one source, with the server's sniffed language (tags lie). */
    suspend fun subtitleStreams(sourceId: String): List<SubtitleInfo> {
        val src = source(sourceId)
        var n = 0
        return src.streams.filter { it.type == "subtitle" }.map { s ->
            // mpv numbers subtitle tracks 1..N in container order; embedded tracks come
            // first in Roxy's stream list, sidecars after (same order mpv --sub-auto uses).
            n += 1
            SubtitleInfo(mpvId = n, index = s.index, codec = s.codec, language = s.language, detectedLanguage = s.detectedLanguage, title = s.title.orEmpty(), isExternal = s.isExternal)
        }
    }

    /** Label + stream summary + direct-play URL for an item that carries `mediaSources`. */
    fun Item.toEpisode(): Episode {
        val source = primarySource
        val streams = source?.streams.orEmpty()
        val video = streams.firstOrNull { it.type == "video" }
        val audios = streams.filter { it.type == "audio" }
        val subs = streams.filter { it.type == "subtitle" }

        val videoLine = buildString {
            append(video?.codec ?: "?")
            video?.profile?.let { append(' ').append(it) }
            append(' ').append(video?.bitDepth?.takeIf { it > 0 } ?: 8).append("-bit")
            append(' ').append(video?.width ?: 0).append('x').append(video?.height ?: 0)
            video?.rangeType?.let { append(' ').append(it) }
            append(" | ").append(source?.container ?: "?")
        }
        val tracksLine = buildString {
            append("audio: ").append(audios.joinToString(", ") { "${it.codec}${it.language?.let { l -> "/$l" } ?: ""}" }.ifEmpty { "none" })
            append(" | subs: ").append(subs.joinToString(", ") { "${it.codec}${it.language?.let { l -> "/$l" } ?: ""}${if (it.isExternal) "(ext)" else ""}" }.ifEmpty { "none" })
            append(" | fonts: ").append(source?.attachments?.size ?: 0)
        }
        return Episode(
            id = id,
            title = displayLabel,
            videoLine = videoLine,
            tracksLine = tracksLine,
            streamUrl = source?.let { streamUrl(it.id) } ?: "",
            hasAss = subs.any { it.codec.equals("ass", true) || it.codec.equals("ssa", true) },
            isHi10p = video?.codec.equals("h264", true) && video?.bitDepth == 10,
        )
    }

    // ---- transport ----

    private suspend inline fun <reified T> get(path: String): T = withContext(Dispatchers.IO) {
        execute<T>(request(path).get().build())
    }

    private suspend fun postNoBody(path: String, body: String) = withContext(Dispatchers.IO) {
        val req = request(path).post(body.toRequestBody(JSON_TYPE)).build()
        http.newCall(req).execute().use { resp ->
            if (!resp.isSuccessful) throw ApiException(resp.code, "HTTP ${resp.code} POST ${req.url.encodedPath}: ${resp.body.string().take(200)}")
        }
    }

    private fun request(path: String): Request.Builder = Request.Builder().url(baseUrl + path)

    private inline fun <reified T> execute(req: Request): T {
        http.newCall(req).execute().use { resp ->
            val text = resp.body.string()
            if (!resp.isSuccessful) {
                Timber.w("HTTP %d %s %s", resp.code, req.method, req.url.encodedPath)
                throw ApiException(resp.code, "HTTP ${resp.code} ${req.method} ${req.url.encodedPath}: ${text.take(200)}")
            }
            return json.decodeFromString(text)
        }
    }

    private companion object {
        val JSON_TYPE = "application/json; charset=utf-8".toMediaType()
    }
}

// ---- wire types (subset of the native API; unknown fields are ignored) ----

@Serializable
data class LoginRequest(
    val username: String,
    val password: String,
    val client: String,
    val device: String,
    val deviceId: String,
    val version: String,
)

@Serializable
data class LoginResponse(val token: String, val user: User)

@Serializable
data class User(val id: String, val name: String, val isAdmin: Boolean = false)

/** `/v1/auth/me`: a user, or `{apiKey: true}` for an integration key. */
@Serializable
data class MeResponse(val id: String? = null, val name: String? = null, val isAdmin: Boolean = false, val apiKey: Boolean = false)

@Serializable
data class ItemsResponse(val items: List<Item> = emptyList(), val total: Int = 0)

@Serializable
data class HomeResponse(val rows: List<HomeRow> = emptyList())

/** One horizontal row on Home. `key`: continue | recentEpisodes | recentMovies | series | movies. */
@Serializable
data class HomeRow(val key: String, val title: String = "", val items: List<Item> = emptyList())

@Serializable
data class UserData(
    val positionMs: Long = 0,
    val played: Boolean = false,
    val playCount: Int = 0,
    val isFavorite: Boolean = false,
    val lastPlayedAt: String? = null,
)

@Serializable
data class RelatedResponse(val related: List<RelatedEntry> = emptyList(), val partOf: List<Item> = emptyList())

@Serializable
data class RelatedEntry(
    /** movie | ova | special | side_story | spin_off | alternative | summary | sequel | prequel | parent | other */
    val relation: String = "other",
    val provider: String = "",
    val providerId: String = "",
    val title: String = "",
    val format: String? = null,
    val year: Int? = null,
    val episodes: Int? = null,
    val inLibrary: Boolean = false,
    val item: Item? = null,
) {
    val key: String get() = "$provider:$providerId"
    val label: String get() = when (relation) {
        "movie" -> "Movie"
        "ova" -> "OVA"
        "special" -> "Special"
        "side_story" -> "Side story"
        "spin_off" -> "Spin-off"
        "alternative" -> "Alternative"
        "summary" -> "Recap"
        "sequel" -> "Sequel"
        "prequel" -> "Prequel"
        else -> "Related"
    }
}

@Serializable
data class Item(
    val id: String,
    val type: String,
    val title: String = "",
    val sortTitle: String? = null,
    val parentId: String? = null,
    val seriesId: String? = null,
    val seriesTitle: String? = null,
    val year: Int? = null,
    val overview: String? = null,
    val tagline: String? = null,
    val genres: List<String> = emptyList(),
    val communityRating: Double? = null,
    val officialRating: String? = null,
    val premiered: String? = null,
    val airDate: String? = null,
    val status: String? = null,
    val studio: String? = null,
    val indexAired: Int? = null,
    val seasonAired: Int? = null,
    val indexAbsolute: Int? = null,
    val runtimeMs: Long? = null,
    val dateCreated: String? = null,
    /** Placeholder from provider metadata (no file). */
    val isVirtual: Boolean = false,
    val hasMedia: Boolean = true,
    /** available | missing | unaired (episodes and movies). */
    val availability: String? = null,
    /** poster | backdrop | logo | thumb -> relative URL. Absent key = no image. */
    val images: Map<String, String> = emptyMap(),
    val userData: UserData? = null,
    val mediaSources: List<Item.Source> = emptyList(),
) {
    @Serializable
    data class Source(
        val id: String,
        val container: String? = null,
        val durationMs: Long? = null,
        val isPrimary: Boolean = false,
        val streams: List<MediaStream> = emptyList(),
        val attachments: List<Attachment> = emptyList(),
    )

    val isPlayable: Boolean get() = hasMedia && availability != "missing" && availability != "unaired"
    val isEpisode: Boolean get() = type == "episode"
    val isSeries: Boolean get() = type == "series"
    val isMovie: Boolean get() = type == "movie"
    val isSeason: Boolean get() = type == "season"

    val primarySource: Source? get() = mediaSources.firstOrNull { it.isPrimary } ?: mediaSources.firstOrNull()

    /** "S01E03" for episodes ("S01" when the episode number is unknown), "" otherwise. */
    val episodeCode: String
        get() {
            if (!isEpisode) return ""
            val e = indexAired ?: indexAbsolute
            return if (e != null) "S%02dE%02d".format(seasonAired ?: 0, e) else "S%02d".format(seasonAired ?: 0)
        }

    /** Whole minutes, rounded; null when unknown or under half a minute. */
    val runtimeMinutes: Long?
        get() = runtimeMs?.takeIf { it > 0 }?.let { (it + 30_000) / 60_000 }?.takeIf { it > 0 }

    /** Episode: "Series - S01E03 - Title"; others: title. */
    val displayLabel: String
        get() = if (isEpisode) listOfNotNull(seriesTitle, episodeCode, title).filter { it.isNotEmpty() }.joinToString(" - ") else title

    /** 0..1 fraction watched when there is a resumable position, else null. */
    val progressFraction: Float?
        get() {
            val pos = userData?.positionMs ?: return null
            val len = runtimeMs ?: primarySource?.durationMs ?: return null
            if (pos <= 0 || len <= 0) return null
            return (pos.toFloat() / len.toFloat()).coerceIn(0f, 1f)
        }
}

@Serializable
data class MediaSource(
    val id: String,
    val container: String? = null,
    val durationMs: Long? = null,
    val streams: List<MediaStream> = emptyList(),
    val attachments: List<Attachment> = emptyList(),
)

@Serializable
data class MediaStream(
    val index: Int = 0,
    val type: String,
    val codec: String = "",
    val profile: String? = null,
    val bitDepth: Int = 0,
    val width: Int = 0,
    val height: Int = 0,
    val rangeType: String? = null,
    val language: String? = null,
    val detectedLanguage: String? = null,
    val title: String? = null,
    val isDefault: Boolean = false,
    val isForced: Boolean = false,
    val isExternal: Boolean = false,
)

@Serializable
data class Attachment(val index: Int = 0, val filename: String = "", val mime: String? = null)

@Serializable
private data class ProgressRequest(val itemId: String, val positionMs: Long, val audioIndex: Int? = null, val subtitleIndex: Int? = null)

/** A subtitle track as the player sees it, with the server's sniffed language. */
data class SubtitleInfo(
    val mpvId: Int,
    val index: Int,
    val codec: String,
    val language: String?,
    val detectedLanguage: String?,
    val title: String,
    val isExternal: Boolean,
) {
    /** The language to trust: sniffed text beats the container tag. */
    val effectiveLanguage: String get() = (detectedLanguage ?: language).orEmpty().lowercase()
}

/** Playable summary of an item: label, stream lines for the episode list, direct-play URL. */
data class Episode(
    val id: String,
    val title: String,
    val videoLine: String,
    val tracksLine: String,
    val streamUrl: String,
    val hasAss: Boolean,
    val isHi10p: Boolean,
)
