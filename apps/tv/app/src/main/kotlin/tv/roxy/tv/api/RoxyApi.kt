package tv.roxy.tv.api

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import okhttp3.Dispatcher
import okhttp3.HttpUrl.Companion.toHttpUrl
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
        // Never hammer the server: at most a handful of calls in flight (images share this
        // client), the rest queue. The server also rate-limits the JSON API per client.
        .dispatcher(Dispatcher().apply { maxRequests = 12; maxRequestsPerHost = 6 })
        .addInterceptor(Interceptor { chain ->
            val s = prefs.session.value
            val req = chain.request()
            // Only Roxy Server gets the token and profile; images from TMDB (Discover) go bare.
            val serverHost = runCatching { s.serverUrl.toHttpUrl().host }.getOrNull()
            if (serverHost == null || req.url.host != serverHost) return@Interceptor chain.proceed(req)
            val b = req.newBuilder()
            if (s.token != null && req.header("Authorization") == null) b.header("Authorization", "Bearer ${s.token}")
            // Watch state, favourites and Home are per profile (docs/api/native.md "Profiles").
            s.profileId?.let { b.header("X-Roxy-Profile", it) }
            chain.proceed(b.build())
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
        favorites: Boolean = false,
    ): ItemsResponse {
        val q = buildList {
            add("type=$type")
            parent?.let { add("parent=$it") }
            if (recursive) add("recursive=1")
            sort?.let { add("sort=$it") }
            if (desc) add("desc=1")
            limit?.let { add("limit=$it") }
            if (includeSources) add("include=sources")
            if (favorites) add("favorites=1")
        }.joinToString("&")
        return get("/v1/items?$q")
    }

    suspend fun item(id: String): Item = get("/v1/items/$id")

    /** The caller's favourites (the Watchlist): series and movies, newest first. */
    suspend fun favorites(limit: Int = 200): ItemsResponse =
        items(type = "series,movie", recursive = true, sort = "date_created", desc = true, limit = limit, favorites = true)

    suspend fun setFavorite(itemId: String, on: Boolean) {
        if (on) postNoBody("/v1/me/favorite/$itemId", "") else delete("/v1/me/favorite/$itemId")
    }

    suspend fun setPlayed(itemId: String, on: Boolean) {
        if (on) postNoBody("/v1/me/played/$itemId", "") else delete("/v1/me/played/$itemId")
    }

    // ---- profiles ----

    suspend fun profiles(): ProfilesResponse = get("/v1/me/profiles")

    suspend fun createProfile(name: String, avatarUrl: String?, avatarName: String?): Profile =
        postJson("/v1/me/profiles", json.encodeToString(ProfileRequest.serializer(), ProfileRequest(name, avatarUrl, avatarName)))

    /** [avatarUrl] null keeps the picture, "-" clears it. */
    suspend fun updateProfile(id: String, name: String?, avatarUrl: String?, avatarName: String?): Profile =
        patchJson("/v1/me/profiles/$id", json.encodeToString(ProfileRequest.serializer(), ProfileRequest(name, avatarUrl, avatarName)))

    suspend fun deleteProfile(id: String) = delete("/v1/me/profiles/$id")

    suspend fun avatarSearch(q: String): List<Character> = get<CharactersResponse>("/v1/avatars/search?q=" + java.net.URLEncoder.encode(q, "UTF-8")).characters

    suspend fun avatarSuggested(): List<Character> = get<CharactersResponse>("/v1/avatars/suggested").characters

    /** The cached avatar; [version] busts Coil's cache after a change. */
    fun profileAvatarUrl(id: String, widthPx: Int, version: Int = 0): String = "$baseUrl/v1/me/profiles/$id/avatar?w=$widthPx&v=$version"

    /**
     * Is the server reachable right now? A fast probe (3 s) of the unauthenticated system
     * endpoint, for the "you're not on the home network" state. Never throws.
     */
    suspend fun reachable(): Boolean = withContext(Dispatchers.IO) {
        val url = baseUrl
        if (url.isEmpty()) return@withContext false
        runCatching {
            probe.newCall(Request.Builder().url("$url/v1/system/info").get().build()).execute().use { it.isSuccessful }
        }.getOrDefault(false)
    }

    private val probe: OkHttpClient by lazy {
        http.newBuilder().connectTimeout(3, TimeUnit.SECONDS).readTimeout(3, TimeUnit.SECONDS).build()
    }

    /**
     * Seek-preview thumbnails for a source: the manifest, or null while the server is still
     * generating them (202) or has none (404). Sheets come from [trickplaySheetUrl].
     */
    suspend fun trickplay(sourceId: String): TrickplayManifest? = withContext(Dispatchers.IO) {
        http.newCall(request("/v1/sources/$sourceId/trickplay").get().build()).execute().use { resp ->
            if (resp.code != 200) return@withContext null
            json.decodeFromString<TrickplayManifest>(resp.body.string())
        }
    }

    fun trickplaySheetUrl(sourceId: String, sheet: Int): String = "$baseUrl/v1/sources/$sourceId/trickplay/$sheet"

    /** Raw bytes of an authenticated URL (sheets, attachments). */
    suspend fun bytes(url: String): ByteArray = withContext(Dispatchers.IO) {
        http.newCall(okhttp3.Request.Builder().url(url).get().build()).execute().use { resp ->
            if (!resp.isSuccessful) throw IOException("HTTP ${resp.code} GET $url")
            resp.body.bytes()
        }
    }

    /** Franchise view: movies/OVAs/side stories linked to an item, and the series it is part of. */
    suspend fun related(id: String): RelatedResponse = get("/v1/items/$id/related")

    suspend fun source(id: String): MediaSource = get("/v1/sources/$id")

    // ---- discover (TMDB through the server; "our own request app") ----

    suspend fun discover(): DiscoverResponse = get("/v1/discover")

    suspend fun discoverSearch(q: String): List<DiscoverItem> = get<DiscoverItemsResponse>("/v1/discover/search?q=" + java.net.URLEncoder.encode(q, "UTF-8")).items

    suspend fun discoverDetail(type: String, tmdbId: Int): DiscoverDetail = get("/v1/discover/$type/$tmdbId")

    /** Request a title; [seasons] narrows a series to those seasons (empty = everything the library lacks). */
    suspend fun discoverRequest(type: String, tmdbId: Int, seasons: List<Int> = emptyList()): RequestRow =
        postJson("/v1/discover/request", json.encodeToString(DiscoverRequestBody.serializer(), DiscoverRequestBody(type, tmdbId, seasons)))

    suspend fun discoverRequests(): List<DiscoverItem> = get<DiscoverItemsResponse>("/v1/discover/requests").items

    suspend fun deleteRequest(id: String) = delete("/v1/discover/requests/$id")

    /** Filtered browsing over TMDB's discover endpoint (one page of 20). */
    suspend fun discoverBrowse(f: BrowseFilters, page: Int): BrowseResult {
        val q = buildList {
            add("type=${f.type}")
            add("sort=${f.sort}")
            if (f.anime) add("anime=1")
            if (f.genres.isNotEmpty()) add("genres=" + f.genres.joinToString(","))
            if (f.language.isNotEmpty()) add("lang=${f.language}")
            if (f.yearFrom > 0) add("yearFrom=${f.yearFrom}")
            if (f.yearTo > 0) add("yearTo=${f.yearTo}")
            if (f.minRating > 0) add("minRating=${f.minRating}")
            if (f.status.isNotEmpty()) add("status=${f.status}")
            add("page=$page")
        }.joinToString("&")
        return get("/v1/discover/browse?$q")
    }

    suspend fun discoverEpisodes(tmdbId: Int, season: Int): List<DiscoverEpisode> = get<DiscoverEpisodesResponse>("/v1/discover/tv/$tmdbId/season/$season").episodes

    suspend fun discoverGenres(type: String): List<Genre> = get<GenresResponse>("/v1/discover/genres?type=$type").genres

    // ---- arr settings (admin) ----

    suspend fun arrSettings(): ArrSettingsResponse = get("/v1/settings/arr")

    suspend fun saveArrSettings(s: ArrSettings): ArrSettingsResponse = putJson("/v1/settings/arr", json.encodeToString(ArrSettings.serializer(), s))

    suspend fun testArr(kind: String, url: String, apiKey: String): ArrProbe =
        postJson("/v1/settings/arr/test", json.encodeToString(ArrTestBody.serializer(), ArrTestBody(kind, url, apiKey)))

    suspend fun retryRequests(): List<DiscoverItem> = postJson<DiscoverItemsResponse>("/v1/discover/requests/retry", "{}").items

    // ---- webhooks + API keys (admin) ----

    suspend fun apiKeys(): List<ApiKeyRow> = get<ApiKeysResponse>("/v1/auth/keys").keys

    /** Creates a key; the token comes back once. */
    suspend fun createApiKey(name: String): ApiKeyCreated = postJson("/v1/auth/keys", json.encodeToString(NameBody.serializer(), NameBody(name)))

    suspend fun deleteApiKey(id: String) = delete("/v1/auth/keys/$id")

    suspend fun webhookEvents(limit: Int = 30): List<WebhookEvent> = get<WebhookEventsResponse>("/v1/webhooks/events?limit=$limit").events

    // ---- app updates ----

    /** The newest build of this app the server hosts (404 when none is published). */
    suspend fun latestAppBuild(applicationId: String): tv.roxy.tv.data.AppUpdate = get("/v1/apps/tv/latest?package=$applicationId")

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
    suspend fun subtitleStreams(sourceId: String): List<SubtitleInfo> = subtitleInfos(source(sourceId).streams)

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

    private suspend inline fun <reified T> postJson(path: String, body: String): T = withContext(Dispatchers.IO) {
        execute<T>(request(path).post(body.toRequestBody(JSON_TYPE)).build())
    }

    private suspend inline fun <reified T> patchJson(path: String, body: String): T = withContext(Dispatchers.IO) {
        execute<T>(request(path).patch(body.toRequestBody(JSON_TYPE)).build())
    }

    private suspend inline fun <reified T> putJson(path: String, body: String): T = withContext(Dispatchers.IO) {
        execute<T>(request(path).put(body.toRequestBody(JSON_TYPE)).build())
    }

    private suspend fun delete(path: String) = withContext(Dispatchers.IO) {
        val req = request(path).delete().build()
        http.newCall(req).execute().use { resp ->
            if (!resp.isSuccessful) throw ApiException(resp.code, "HTTP ${resp.code} DELETE ${req.url.encodedPath}: ${resp.body.string().take(200)}")
        }
    }

    private fun request(path: String): Request.Builder = Request.Builder().url(baseUrl + path)

    private inline fun <reified T> execute(req: Request): T {
        http.newCall(req).execute().use { resp ->
            val text = resp.body.string()
            if (!resp.isSuccessful) {
                Timber.w("HTTP %d %s %s", resp.code, req.method, req.url.encodedPath)
                throw ApiException(resp.code, serverMessage(text) ?: "HTTP ${resp.code} ${req.method} ${req.url.encodedPath}: ${text.take(200)}")
            }
            return json.decodeFromString(text)
        }
    }

    /** The server's `{"error": "..."}` text when the body is one, for messages shown to people. */
    private fun serverMessage(body: String): String? = runCatching {
        kotlinx.serialization.json.Json.parseToJsonElement(body).let { (it as? kotlinx.serialization.json.JsonObject)?.get("error") as? kotlinx.serialization.json.JsonPrimitive }?.content
    }.getOrNull()?.takeIf { it.isNotBlank() }

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
data class MeResponse(val id: String? = null, val name: String? = null, val isAdmin: Boolean = false, val apiKey: Boolean = false, val profileId: String? = null)

@Serializable
data class Profile(
    val id: String,
    val name: String,
    val isDefault: Boolean = false,
    val avatarName: String = "",
    val hasAvatar: Boolean = false,
)

@Serializable
data class ProfilesResponse(val profiles: List<Profile> = emptyList(), val current: String? = null)

@Serializable
data class ProfileRequest(val name: String? = null, val avatarUrl: String? = null, val avatarName: String? = null)

/** An AniList character offered as an avatar. */
@Serializable
data class Character(val id: Int, val name: String, val image: String, val series: String = "")

@Serializable
data class CharactersResponse(val characters: List<Character> = emptyList())

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
data class TrickplayManifest(
    val intervalMs: Int = 10_000,
    val width: Int = 320,
    val height: Int = 180,
    val columns: Int = 10,
    val rows: Int = 10,
    val count: Int = 0,
    val sheets: Int = 0,
) {
    val perSheet: Int get() = columns * rows
    fun tileIndex(positionSec: Double): Int = ((positionSec * 1000) / intervalMs).toInt().coerceIn(0, (count - 1).coerceAtLeast(0))
    fun sheetOf(tile: Int): Int = tile / perSheet
    fun columnOf(tile: Int): Int = (tile % perSheet) % columns
    fun rowOf(tile: Int): Int = (tile % perSheet) / columns
}

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
    /** tmdb | tvdb | imdb | anilist | mal -> id (series and movies). */
    val providerIds: Map<String, String> = emptyMap(),
    /** Distinct audio languages across the title's media (series and movies), for "Dub | Sub". */
    val audioLanguages: List<String> = emptyList(),
    /** Matched on AniList (series, movies, and episodes via their series): use Dub / Sub wording. */
    val isAnime: Boolean = false,
    val userData: UserData? = null,
    val mediaSources: List<Item.Source> = emptyList(),
) {
    @Serializable
    data class Source(
        val id: String,
        val container: String? = null,
        /** File size in bytes (what a download will take). */
        val size: Long = 0,
        val durationMs: Long? = null,
        val isPrimary: Boolean = false,
        val streams: List<MediaStream> = emptyList(),
        val attachments: List<Attachment> = emptyList(),
        val segments: List<MediaSegment> = emptyList(),
    )

    val isPlayable: Boolean get() = hasMedia && availability != "missing" && availability != "unaired"
    val isEpisode: Boolean get() = type == "episode"
    val isSeries: Boolean get() = type == "series"
    val isMovie: Boolean get() = type == "movie"
    val isSeason: Boolean get() = type == "season"

    val primarySource: Source? get() = mediaSources.firstOrNull { it.isPrimary } ?: mediaSources.firstOrNull()

    /** A public page for the title, for sharing: TMDB first, then AniList, then IMDb. */
    val shareUrl: String?
        get() {
            providerIds["tmdb"]?.let { return "https://www.themoviedb.org/${if (isMovie) "movie" else "tv"}/$it" }
            providerIds["anilist"]?.let { return "https://anilist.co/anime/$it" }
            providerIds["imdb"]?.let { return "https://www.imdb.com/title/$it/" }
            return null
        }

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

    /**
     * The red bar under a thumbnail (Netflix): 0..1 of the runtime when there is a
     * resumable position, a full bar once the episode counts as watched, null otherwise.
     */
    val progressFraction: Float?
        get() {
            val ud = userData ?: return null
            val len = runtimeMs ?: primarySource?.durationMs
            if (ud.positionMs > 0 && len != null && len > 0) return (ud.positionMs.toFloat() / len.toFloat()).coerceIn(0f, 1f)
            return if (ud.played) 1f else null
        }
}

@Serializable
data class MediaSource(
    val id: String,
    val container: String? = null,
    val durationMs: Long? = null,
    val streams: List<MediaStream> = emptyList(),
    val attachments: List<Attachment> = emptyList(),
    val segments: List<MediaSegment> = emptyList(),
)

/** A skippable range: intro | outro | recap | preview. */
@Serializable
data class MediaSegment(val type: String, val startMs: Long, val endMs: Long, val origin: String? = null) {
    val startSec: Double get() = startMs / 1000.0
    val endSec: Double get() = endMs / 1000.0
    val label: String get() = when (type) { "intro" -> "Skip intro"; "outro" -> "Skip outro"; "recap" -> "Skip recap"; "preview" -> "Skip preview"; else -> "Skip" }
}

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

/** A TMDB title on Discover, with what the library and the request list know about it. */
@Serializable
data class DiscoverItem(
    val tmdbId: Int,
    val type: String, // tv | movie
    val title: String = "",
    val year: Int? = null,
    val overview: String? = null,
    val poster: String? = null,
    val backdrop: String? = null,
    val rating: Double? = null,
    val genres: List<String> = emptyList(),
    val isAnime: Boolean = false,
    val libraryItemId: String? = null,
    /** pending | sent | failed when someone asked for it. */
    val requestStatus: String? = null,
) {
    val isMovie: Boolean get() = type == "movie"
    val inLibrary: Boolean get() = !libraryItemId.isNullOrEmpty()
}

@Serializable
data class DiscoverEpisode(val number: Int, val title: String = "", val overview: String? = null, val airDate: String? = null, val still: String? = null, val have: Boolean = false)

@Serializable
data class DiscoverEpisodesResponse(val episodes: List<DiscoverEpisode> = emptyList())

@Serializable
data class DiscoverRow(val key: String, val title: String = "", val items: List<DiscoverItem> = emptyList())

@Serializable
data class DiscoverResponse(val rows: List<DiscoverRow> = emptyList(), val requestsEnabled: Map<String, Boolean> = emptyMap())

@Serializable
data class DiscoverItemsResponse(val items: List<DiscoverItem> = emptyList())

@Serializable
data class RequestRow(
    val id: String,
    val type: String = "",
    val tmdbId: Int = 0,
    val title: String = "",
    val year: Int? = null,
    val userName: String = "",
    val status: String = "pending",
    val error: String? = null,
    val createdAt: String = "",
    val seasons: List<Int> = emptyList(),
)

/** The Discover title page: a [DiscoverItem]'s fields plus the longer facts. */
@Serializable
data class DiscoverDetail(
    val tmdbId: Int,
    val type: String,
    val title: String = "",
    val year: Int? = null,
    val overview: String? = null,
    val poster: String? = null,
    val backdrop: String? = null,
    val rating: Double? = null,
    val genres: List<String> = emptyList(),
    val isAnime: Boolean = false,
    val libraryItemId: String? = null,
    val requestStatus: String? = null,
    val tagline: String? = null,
    val status: String? = null,
    val seasonCount: Int = 0,
    val episodes: Int = 0,
    val runtimeMinutes: Int = 0,
    val networks: List<String> = emptyList(),
    val tvdbId: Int = 0,
    val imdbId: String? = null,
    val request: RequestRow? = null,
    val requestsEnabled: Boolean = false,
    val seasons: List<DiscoverSeason> = emptyList(),
) {
    val isMovie: Boolean get() = type == "movie"
    val inLibrary: Boolean get() = !libraryItemId.isNullOrEmpty()
    /** Seasons with something still to fetch (not complete in the library, not requested). */
    val missingSeasons: List<DiscoverSeason> get() = seasons.filter { !it.complete && !it.requested }
}

@Serializable
private data class DiscoverRequestBody(val type: String, val tmdbId: Int, val seasons: List<Int> = emptyList())

/** One season on a Discover title page, with what the library holds and whether it is requested. */
@Serializable
data class DiscoverSeason(
    val number: Int,
    val name: String = "",
    val episodes: Int = 0,
    val airDate: String? = null,
    val poster: String? = null,
    val have: Int = 0,
    val known: Int = 0,
    val requested: Boolean = false,
) {
    val complete: Boolean get() = episodes > 0 && have >= episodes
    val partial: Boolean get() = have > 0 && !complete
}

/** The Discover filter bar. Defaults mean "no filter": the curated rows show instead of a grid. */
@Serializable
data class BrowseFilters(
    val type: String = "tv",        // tv | movie
    val sort: String = "popular",   // popular | top | newest | oldest | votes
    val anime: Boolean = false,
    val genres: List<Int> = emptyList(),
    val language: String = "",      // ISO 639-1 original language
    val yearFrom: Int = 0,
    val yearTo: Int = 0,
    val minRating: Double = 0.0,
    val status: String = "",        // tv: airing | ended
    /** The user touched the bar or applied the sheet: show the grid even with default values. */
    val browsing: Boolean = false,
) {
    /** Show the filtered grid instead of the curated rows? */
    val isActive: Boolean get() = browsing || sort != "popular" || anime || genres.isNotEmpty() || language.isNotEmpty() || yearFrom > 0 || yearTo > 0 || minRating > 0 || status.isNotEmpty()
    val count: Int get() = listOf(anime, genres.isNotEmpty(), language.isNotEmpty(), yearFrom > 0 || yearTo > 0, minRating > 0, status.isNotEmpty()).count { it }
}

@Serializable
data class BrowseResult(val items: List<DiscoverItem> = emptyList(), val page: Int = 1, val totalPages: Int = 1)

@Serializable
data class Genre(val id: Int, val name: String)

@Serializable
data class GenresResponse(val genres: List<Genre> = emptyList())

/** One arr server's connection and defaults (request app's Services page). */
@Serializable
data class ArrConfig(
    val url: String = "",
    val apiKey: String = "",
    val rootFolder: String = "",
    val qualityProfileId: Int = 0,
    val animeRootFolder: String = "",
    val animeQualityProfileId: Int = 0,
    val tags: List<Int> = emptyList(),
    val animeTags: List<Int> = emptyList(),
    val seasonFolders: Boolean? = null,
    val minimumAvailability: String = "",
    val searchOnAdd: Boolean? = null,
    val externalUrl: String = "",
)

@Serializable
data class ArrSettings(val sonarr: ArrConfig = ArrConfig(), val radarr: ArrConfig = ArrConfig())

@Serializable
data class ArrSettingsResponse(val sonarr: ArrConfig = ArrConfig(), val radarr: ArrConfig = ArrConfig(), val requestsEnabled: Map<String, Boolean> = emptyMap())

@Serializable
data class ArrNamed(val id: Int = 0, val name: String = "", val label: String = "", val path: String = "", val freeSpace: Long = 0)

@Serializable
data class ArrProbe(val version: String = "", val appName: String = "", val rootFolders: List<ArrNamed> = emptyList(), val qualityProfiles: List<ArrNamed> = emptyList(), val tags: List<ArrNamed> = emptyList())

@Serializable
private data class ArrTestBody(val kind: String, val url: String, val apiKey: String)

@Serializable
data class ApiKeyRow(val id: String, val name: String = "", val createdAt: String = "")

@Serializable
data class ApiKeysResponse(val keys: List<ApiKeyRow> = emptyList())

@Serializable
data class ApiKeyCreated(val id: String, val name: String = "", val token: String = "")

@Serializable
private data class NameBody(val name: String)

/** One Sonarr / Radarr notification the server received and what it did. */
@Serializable
data class WebhookEvent(val id: Long = 0, val at: String = "", val source: String = "", val eventType: String = "", val title: String = "", val path: String = "", val result: String = "")

@Serializable
data class WebhookEventsResponse(val events: List<WebhookEvent> = emptyList())

@Serializable
private data class ProgressRequest(val itemId: String, val positionMs: Long, val audioIndex: Int? = null, val subtitleIndex: Int? = null)

/**
 * Subtitle tracks as mpv will number them (1..N in container order; embedded first,
 * sidecars after, the order `--sub-auto` uses), paired with the server's sniffed language.
 * Works on a live source or on a downloaded item's snapshot.
 */
fun subtitleInfos(streams: List<MediaStream>): List<SubtitleInfo> {
    var n = 0
    return streams.filter { it.type == "subtitle" }.map { s ->
        n += 1
        SubtitleInfo(mpvId = n, index = s.index, codec = s.codec, language = s.language, detectedLanguage = s.detectedLanguage, title = s.title.orEmpty(), isExternal = s.isExternal)
    }
}

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
