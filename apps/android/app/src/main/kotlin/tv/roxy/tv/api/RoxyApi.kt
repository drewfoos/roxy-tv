package tv.roxy.tv.api

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.SerialName
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
            // A request that names its profile keeps it: a queued report belongs to the profile
            // that made it, whichever is active by the time it is sent.
            if (req.header(PROFILE_HEADER) == null) s.profileId?.let { b.header(PROFILE_HEADER, it) }
            // Any answer means the server is there; a failed connection (not one we cancelled)
            // tells ServerStatus to check, which drives the "Can't reach Roxy" banner.
            val resp = try { chain.proceed(b.build()) } catch (e: IOException) {
                if (!chain.call().isCanceled()) onReachability?.invoke(false)
                throw e
            }
            onReachability?.invoke(true)
            resp
        })
        .build()

    /** Told whether each call to the server got an answer (set by ServerStatus; any thread). */
    @Volatile var onReachability: ((Boolean) -> Unit)? = null

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

    // ---- first-time setup (before anyone is signed in) ----

    /** The public identity of the server at [serverUrl] (its id, name; product "roxy"). For pinning a server once signed in. */
    suspend fun serverInfoAt(serverUrl: String): ServerIdentity = withContext(Dispatchers.IO) {
        execute<ServerIdentity>(Request.Builder().url(serverUrl.trimEnd('/') + "/v1/system/info").get().build())
    }

    /** Whether the server at [serverUrl] still needs setting up, and whether this device may. */
    suspend fun setupStatus(serverUrl: String): SetupStatus = withContext(Dispatchers.IO) {
        execute<SetupStatus>(Request.Builder().url(serverUrl.trimEnd('/') + "/v1/setup").get().build())
    }

    /** Creates the server's admin and signs in as them. */
    suspend fun setupAdmin(serverUrl: String, name: String, password: String, device: String, deviceId: String, version: String): LoginResponse =
        withContext(Dispatchers.IO) {
            val body = SetupAdmin(name, password, device, deviceId, version)
            execute<LoginResponse>(
                Request.Builder().url(serverUrl.trimEnd('/') + "/v1/setup/admin")
                    .post(json.encodeToString(SetupAdmin.serializer(), body).toRequestBody(JSON_TYPE)).build(),
            )
        }

    suspend fun tmdbSettings(): TmdbSettings = get("/v1/settings/tmdb")
    suspend fun saveTmdbKey(key: String): TmdbSettings = putJson("/v1/settings/tmdb", json.encodeToString(TmdbKey.serializer(), TmdbKey(key)))

    suspend fun logout() { postNoBody("/v1/auth/logout", "") }

    // ---- users (admin) ----
    suspend fun users(): List<ServerUser> = get<UsersResponse>("/v1/users").users
    suspend fun createUser(name: String, password: String, role: String): ServerUser =
        postJson("/v1/users", json.encodeToString(UserEdit.serializer(), UserEdit(name = name, password = password, role = role)))
    suspend fun updateUser(id: String, role: String? = null, password: String? = null, disabled: Boolean? = null): ServerUser =
        patchJson("/v1/users/$id", json.encodeToString(UserEdit.serializer(), UserEdit(role = role, password = password, disabled = disabled)))
    suspend fun deleteUser(id: String) = delete("/v1/users/$id")
    suspend fun adminRequests(): List<AdminRequest> = get<AdminRequestsResponse>("/v1/requests").requests
    suspend fun waitingRequests(): Int = get<AdminRequestsResponse>("/v1/requests").waiting
    suspend fun approveRequest(id: String) = postNoBody("/v1/discover/requests/$id/approve", "")
    suspend fun declineRequest(id: String, reason: String = "") =
        postNoBody("/v1/discover/requests/$id/decline", json.encodeToString(DeclineBody.serializer(), DeclineBody(reason)))

    /** Change this account's password; other devices are signed out. 403 = wrong current password. */
    suspend fun changePassword(current: String, new: String) =
        postNoBody("/v1/me/password", json.encodeToString(PasswordChange.serializer(), PasswordChange(current, new)))

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
        offset: Int? = null,
    ): ItemsResponse {
        val q = buildList {
            add("type=$type")
            parent?.let { add("parent=$it") }
            if (recursive) add("recursive=1")
            sort?.let { add("sort=$it") }
            if (desc) add("desc=1")
            limit?.let { add("limit=$it") }
            offset?.takeIf { it > 0 }?.let { add("offset=$it") }
            if (includeSources) add("include=sources")
            if (favorites) add("favorites=1")
        }.joinToString("&")
        return get("/v1/items?$q")
    }

    /**
     * Every item a query matches, page after page: /v1/items sends 100 by default and at
     * most 2000 a request, so a long show's episodes came back cut off (and the next
     * episode was picked from the first page only). For whole lists: a show's or a
     * season's episodes.
     */
    suspend fun allItems(type: String, parent: String? = null, recursive: Boolean = false, sort: String? = null, includeSources: Boolean = false): ItemsResponse {
        // One request up to the server's cap: one consistent query; pages only past it.
        return collectPages(ITEMS_PAGE) { offset -> items(type = type, parent = parent, recursive = recursive, sort = sort, limit = ITEMS_PAGE, includeSources = includeSources, offset = offset) }
    }

    suspend fun item(id: String): Item = get("/v1/items/$id")

    /** The caller's favourites (the Watchlist): series and movies, newest first. */
    suspend fun favorites(limit: Int = 200): ItemsResponse =
        items(type = "series,movie", recursive = true, sort = "date_created", desc = true, limit = limit, favorites = true)

    suspend fun setFavorite(itemId: String, on: Boolean) {
        if (on) postNoBody("/v1/me/favorite/$itemId", "") else delete("/v1/me/favorite/$itemId")
    }

    /**
     * [at]: when an offline watched mark was made (Unix ms), so the server can drop it if the item was played since.
     * [profileId]: the profile the mark is for, when not the active one's request (a queued mark).
     */
    suspend fun setPlayed(itemId: String, on: Boolean, at: Long? = null, ageMs: Long? = null, profileId: String? = null) {
        val q = listOfNotNull(at?.let { "at=$it" }, ageMs?.let { "age=$it" }).joinToString("&")
        if (on) postNoBody("/v1/me/played/$itemId" + (if (q.isEmpty()) "" else "?$q"), "", profileId) else delete("/v1/me/played/$itemId")
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

    /** The cached avatar; [key] (the server's avatarKey) busts Coil's cache after a change. */
    fun profileAvatarUrl(id: String, widthPx: Int, key: String = ""): String = "$baseUrl/v1/me/profiles/$id/avatar?w=$widthPx&v=$key"

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

    /**
     * A series' season list in release order: its seasons, the specials that came out
     * between them ("OVAs"), and the franchise's films the library holds.
     */
    suspend fun sections(id: String): List<Section> = get<SectionsResponse>("/v1/items/$id/sections").sections

    /** The episode a show's Play means for this profile (a film: itself), worked out by the server. */
    suspend fun next(id: String): Item? = get<NextResponse>("/v1/items/$id/next").item

    /** A film's or show's trailers, featurettes and other extras (each plays like any item). */
    suspend fun extras(id: String): List<Item> = get<ItemsResponse>("/v1/items/$id/extras").items

    suspend fun source(id: String): MediaSource = get("/v1/sources/$id")

    /** The "More details" page: item, library facts, TMDB extras, provider links. */
    suspend fun itemDetails(id: String): ItemDetails = get("/v1/items/$id/details")

    /** "More details" for a Discover title (it may not be in the library). */
    suspend fun discoverMoreDetails(type: String, tmdbId: Int): ItemDetails = get("/v1/discover/$type/$tmdbId/details")

    // ---- discover (TMDB through the server; requests) ----

    suspend fun discover(): DiscoverResponse = get("/v1/discover")

    suspend fun discoverSearch(q: String): List<DiscoverItem> = get<DiscoverItemsResponse>("/v1/discover/search?q=" + java.net.URLEncoder.encode(q, "UTF-8")).items

    suspend fun discoverDetail(type: String, tmdbId: Int): DiscoverDetail = get("/v1/discover/$type/$tmdbId")

    suspend fun person(tmdbId: Int): Person = get("/v1/people/$tmdbId")

    /** "More like this" for a library title (TMDB recommendations, library titles marked). */
    suspend fun similar(itemId: String): List<DiscoverItem> = get<DiscoverItemsResponse>("/v1/items/$itemId/similar").items

    /** Request a title; [seasons] narrows a series to those seasons (empty = everything the library lacks). */
    suspend fun discoverRequest(type: String, tmdbId: Int, seasons: List<Int> = emptyList(), episodes: List<String> = emptyList()): RequestRow =
        postJson("/v1/discover/request", json.encodeToString(DiscoverRequestBody.serializer(), DiscoverRequestBody(type, tmdbId, seasons, episodes)))

    suspend fun discoverRequests(): List<DiscoverItem> = get<DiscoverItemsResponse>("/v1/discover/requests").items

    /**
     * Ask Sonarr / Radarr to look for what the request still lacks (all, or these "S1E3"
     * episodes / these seasons). Throws [ApiException] 409 with the server's words when
     * there is nothing to search.
     */
    suspend fun searchAgain(requestId: String, episodes: List<String> = emptyList(), seasons: List<Int> = emptyList()) {
        postNoBody("/v1/discover/requests/$requestId/search", json.encodeToString(SearchBody.serializer(), SearchBody(episodes, seasons)))
    }

    /** Admin: drop the failed / stalled download, blocklist it, and let the arr app pick another (all, or these "S1E3" episodes). */
    suspend fun tryAnotherRelease(requestId: String, episodes: List<String> = emptyList()) {
        postNoBody("/v1/discover/requests/$requestId/replace", json.encodeToString(EpisodeCodes.serializer(), EpisodeCodes(episodes)))
    }

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

    // ---- artwork (admin) ----

    suspend fun imageCandidates(itemId: String, type: String): ArtCandidates = get("/v1/items/$itemId/images/$type/candidates")

    /** Store [url] as the item's image of [type] and lock it against metadata refreshes. */
    suspend fun setImage(itemId: String, type: String, url: String) = withContext(Dispatchers.IO) {
        val req = request("/v1/items/$itemId/images/$type").put(json.encodeToString(ImageUrlBody.serializer(), ImageUrlBody(url)).toRequestBody(JSON_TYPE)).build()
        http.newCall(req).execute().use { resp ->
            if (!resp.isSuccessful) throw ApiException(resp.code, "HTTP ${resp.code} PUT ${req.url.encodedPath}: ${resp.body.string().take(200)}")
        }
    }

    suspend fun fanartSettings(): FanartSettings = get("/v1/settings/fanart")
    suspend fun saveFanartSettings(apiKey: String): FanartSettings = putJson("/v1/settings/fanart", json.encodeToString(FanartSettings.serializer(), FanartSettings(apiKey)))

    /** Which encoder the server transcodes with; the first call can take a few seconds (the server test-encodes). */
    suspend fun transcodingSettings(): TranscodingSettings = get("/v1/settings/transcoding")

    // ---- library management (admin) ----

    suspend fun libraryManage(): LibraryManage = get("/v1/library/manage")

    // ---- media folders (admin) ----
    suspend fun mediaFolders(): List<MediaFolder> = get<MediaFoldersResponse>("/v1/library/folders").libraries
    suspend fun browseFolders(path: String): FolderListing = get("/v1/library/browse?path=" + path.urlEncoded())
    suspend fun addMediaFolder(path: String, name: String, kind: String, anime: Boolean): MediaFolder =
        postJson("/v1/library/folders", json.encodeToString(FolderEdit.serializer(), FolderEdit(path, name, kind, anime)))
    suspend fun updateMediaFolder(id: Long, name: String, kind: String, anime: Boolean): MediaFolder =
        patchJson("/v1/library/folders/$id", json.encodeToString(FolderEdit.serializer(), FolderEdit("", name, kind, anime)))
    suspend fun removeMediaFolder(id: Long) = delete("/v1/library/folders/$id")
    /** Absolute URL for a relative image path from the server, sized for [widthPx]. */
    fun absoluteImage(rel: String?, widthPx: Int): String? = rel?.let { (if (it.startsWith("http")) it else baseUrl + it) + (if ('?' in it) "&w=" else "?w=") + widthPx }
    /** mode: files (new / removed files) | missing (titles without a match) | replace (look everything up again). */
    suspend fun libraryScan(mode: String) = postNoBody("/v1/library/scan", json.encodeToString(ScanMode.serializer(), ScanMode(mode)))
    suspend fun identifySearch(itemId: String, provider: String, query: String): IdentifyResults =
        get("/v1/items/$itemId/identify?provider=${provider.urlEncoded()}&q=${query.urlEncoded()}")
    suspend fun identify(itemId: String, provider: String, providerId: String) =
        postNoBody("/v1/items/$itemId/identify", json.encodeToString(IdentifyChoice.serializer(), IdentifyChoice(provider, providerId)))
    suspend fun editMetadata(itemId: String, edit: MetadataEdit): Item = putJson("/v1/items/$itemId/metadata", json.encodeToString(MetadataEdit.serializer(), edit))
    suspend fun refreshItem(itemId: String) = postNoBody("/v1/items/$itemId/refresh", "")
    /** Redo any mix of parts for some titles, or all of them (Library > Refresh). */
    suspend fun libraryRefresh(ids: List<String>, all: Boolean, parts: RefreshParts) = postNoBody(
        "/v1/library/refresh",
        json.encodeToString(RefreshRequest.serializer(), RefreshRequest(ids, all, parts.metadata, parts.images, parts.trickplay, parts.segments)),
    )
    suspend fun saveTranscodingSettings(hwAccel: String): TranscodingSettings = putJson("/v1/settings/transcoding", json.encodeToString(TranscodingChoice.serializer(), TranscodingChoice(hwAccel)))

    suspend fun arrSettings(): ArrSettingsResponse = get("/v1/settings/arr")

    // Every field is sent, defaults included: the server keeps any field left out, so an
    // emptied URL or "Same as above" (empty / 0) would otherwise never reach it.
    private val jsonAllFields = Json(json) { encodeDefaults = true }
    suspend fun saveArrSettings(s: ArrSettings): ArrSettingsResponse = putJson("/v1/settings/arr", jsonAllFields.encodeToString(ArrSettings.serializer(), s))

    suspend fun testArr(kind: String, url: String, apiKey: String): ArrProbe =
        postJson("/v1/settings/arr/test", json.encodeToString(ArrTestBody.serializer(), ArrTestBody(kind, url, apiKey)))

    suspend fun retryRequests(): List<DiscoverItem> = postJson<DiscoverItemsResponse>("/v1/discover/requests/retry", "{}").items

    // ---- webhooks + API keys (admin) ----

    suspend fun apiKeys(): List<ApiKeyRow> = get<ApiKeysResponse>("/v1/auth/keys").keys

    /** Creates a key; the token comes back once. */
    suspend fun createApiKey(name: String): ApiKeyCreated = postJson("/v1/auth/keys", json.encodeToString(NameBody.serializer(), NameBody(name)))

    suspend fun deleteApiKey(id: String) = delete("/v1/auth/keys/$id")

    suspend fun webhookEvents(limit: Int = 30): List<WebhookEvent> = get<WebhookEventsResponse>("/v1/webhooks/events?limit=$limit").events

    // ---- legal ----

    suspend fun legal(doc: String): LegalDoc = get("/v1/legal/$doc")

    /** The same, from a server URL typed on the login screen (no session yet). */
    // ---- signing a TV in from a phone (server link.go) ----

    /** Asks [serverUrl] for a link code; the TV shows it as a QR and as text. */
    suspend fun linkStart(serverUrl: String, device: String, deviceId: String, version: String): LinkStart = withContext(Dispatchers.IO) {
        val body = LinkStartRequest(client = "Roxy TV", device = device, deviceId = deviceId, version = version)
        execute<LinkStart>(Request.Builder().url(serverUrl.trimEnd('/') + "/v1/link/start").post(json.encodeToString(LinkStartRequest.serializer(), body).toRequestBody(JSON_TYPE)).build())
    }

    /** A crash saved by an earlier run (diagnostics/CrashReports); the server logs it. */
    suspend fun reportCrash(text: String) = postNoBody("/v1/diagnostics/crash", json.encodeToString(kotlinx.serialization.json.JsonObject.serializer(), kotlinx.serialization.json.JsonObject(mapOf("report" to kotlinx.serialization.json.JsonPrimitive(text)))))

    /** Phone side: which device a code belongs to (needs a signed-in user). */
    suspend fun linkInfo(code: String): LinkInfo = get("/v1/link/$code")

    /** Phone side: sign that device in as this user. */
    suspend fun linkApprove(code: String) = postNoBody("/v1/link/$code/approve", "")

    /** Where the QR picture for a code is. */
    fun linkQrUrl(serverUrl: String, link: LinkStart): String = serverUrl.trimEnd('/') + "/v1/link/${link.code}/qr.png?secret=${link.secret}"

    /** Waits (up to the server's 25 s) for the phone's approval; `status` is "pending" or "done". */
    suspend fun linkWait(serverUrl: String, link: LinkStart): LinkResult = withContext(Dispatchers.IO) {
        execute<LinkResult>(Request.Builder().url(serverUrl.trimEnd('/') + "/v1/link/${link.code}/wait?secret=${link.secret}").get().build())
    }

    /** How many posters the sign-in wall of [serverUrl] has (public; 0 on an older server). */
    suspend fun showcaseCountAt(serverUrl: String): Int = withContext(Dispatchers.IO) {
        val base = serverUrl.trim().trimEnd('/').let { if (it.startsWith("http")) it else "http://$it" }
        execute<ShowcaseCount>(Request.Builder().url("$base/v1/showcase").get().build()).count
    }

    suspend fun legalAt(serverUrl: String, doc: String): LegalDoc = withContext(Dispatchers.IO) {
        execute<LegalDoc>(Request.Builder().url(serverUrl.trimEnd('/') + "/v1/legal/$doc").get().build())
    }

    // ---- "Play on" (docs/api/native.md "Sessions") ----

    /** Long-poll for the next command aimed at this device; null after the server's wait with nothing to do. */
    suspend fun sessionCommands(name: String, client: String, waitSec: Int = 25): RemoteCommand? = withContext(Dispatchers.IO) {
        // caps=handoff: this app echoes a handed-over "play"'s id back in its state (RemotePlay.handOver).
        val req = request("/v1/sessions/commands?name=${name.urlEncoded()}&client=${client.urlEncoded()}&wait=$waitSec&caps=handoff").get().build()
        http.newCall(req).execute().use { resp ->
            if (resp.code == 204) return@use null
            val text = resp.body.string()
            if (!resp.isSuccessful) throw ApiException(resp.code, serverMessage(text) ?: "HTTP ${resp.code} sessions/commands")
            json.decodeFromString(RemoteCommandBody.serializer(), text).command
        }
    }

    /** This player's state for "Play on" ([RemoteNow.seq] orders the reports; see RemotePlay.reportState). */
    suspend fun sessionState(state: RemoteNow) =
        postNoBody("/v1/sessions/state", json.encodeToString(RemoteNow.serializer(), state))

    suspend fun sessionPlayers(): List<RemotePlayer> = get<RemotePlayers>("/v1/sessions/players").players

    suspend fun sendSessionCommand(deviceId: String, cmd: RemoteCommand) =
        postNoBody("/v1/sessions/players/$deviceId/command", json.encodeToString(RemoteCommand.serializer(), cmd))

    private fun String.urlEncoded() = java.net.URLEncoder.encode(this, "UTF-8")

    suspend fun legalStatus(): LegalStatus = get("/v1/legal/status")

    suspend fun acceptLegal(version: String) = postNoBody("/v1/legal/accept", json.encodeToString(VersionBody.serializer(), VersionBody(version)))

    // ---- app updates ----

    /** The newest build of this app the server hosts (404 when none is published). */
    suspend fun latestAppBuild(applicationId: String): tv.roxy.tv.data.AppUpdate = get("/v1/apps/tv/latest?package=$applicationId")

    // ---- watch state ----

    suspend fun resume(limit: Int = 20): ItemsResponse = get("/v1/me/resume?limit=$limit")

    /** What this profile played, newest first (My Lists > History). */
    suspend fun history(limit: Int = 100): ItemsResponse = get("/v1/me/history?limit=$limit")

    /** [at]: when an offline report was made (Unix ms), so the server can drop it if it is out of date. */
    /**
     * [ageMs]: how long ago, by this device's clock, the position was reached (a player left
     * paused, a report sent from the offline queue). The server orders reports by it, so a
     * stale one cannot undo what another device did since; [at] is for older servers.
     * [profileId]: the profile that watched, sent with this request whichever profile is
     * active now (a queued report); null = the active one, as every other call.
     */
    suspend fun progress(itemId: String, positionMs: Long, audioIndex: Int? = null, subtitleIndex: Int? = null, at: Long? = null, ageMs: Long? = null, sourceId: String? = null, durationMs: Long? = null, profileId: String? = null) {
        val body = ProgressRequest(itemId, positionMs, audioIndex, subtitleIndex, at, ageMs, sourceId, durationMs)
        postNoBody("/v1/me/progress", json.encodeToString(ProgressRequest.serializer(), body), profileId)
    }

    // ---- urls the player and image loader fetch on their own ----

    // libmpv (and an external player) cannot send headers, so the credential has to ride
    // in the URL. It is a media token: one source, one day, nothing else; the account
    // token never goes into a URL.
    //
    // A media token belongs to the sign-in that asked for it (the server ties it to that
    // sign-in's device, and signing out ends it), so the cache does too: after a sign-out
    // and a new sign-in in the same run of the app, a token of the old sign-in is never
    // handed out. (It used to be: the cache was keyed by source alone and kept for the
    // life of the process, so replaying something within its day sent a dead token.)
    private class MediaTokens(val account: String?) {
        val bySource = java.util.concurrent.ConcurrentHashMap<String, Pair<String, Long>>()
    }

    @Volatile private var mediaTokens = MediaTokens(null)

    /** The cache of the sign-in [account]; the cache of another sign-in is dropped. */
    private fun mediaTokensOf(account: String?): MediaTokens {
        mediaTokens.let { if (it.account == account) return it }
        return synchronized(this) {
            mediaTokens.takeIf { it.account == account } ?: MediaTokens(account).also { mediaTokens = it }
        }
    }

    /** A media token for [sourceId], reused while it has more than an hour left. */
    suspend fun mediaToken(sourceId: String): String {
        val now = System.currentTimeMillis()
        // Kept in the cache of the sign-in it was asked under: when the sign-in changes
        // while the request is out, the answer lands in a cache nobody reads any more.
        val cache = mediaTokensOf(token)
        cache.bySource[sourceId]?.let { (tok, exp) -> if (exp - now > 3_600_000L) return tok }
        val r: MediaTokenResponse = get("/v1/sources/$sourceId/token")
        val exp = runCatching { java.time.Instant.parse(r.expiresAt).toEpochMilli() }.getOrDefault(now + 6 * 3_600_000L)
        cache.bySource[sourceId] = r.token to exp
        return r.token
    }

    /** The cached media token, for places that cannot wait (fetched when playback started). */
    fun cachedMediaToken(sourceId: String): String? =
        mediaTokensOf(token).bySource[sourceId]?.takeIf { (_, exp) -> exp - System.currentTimeMillis() > 60_000L }?.first

    /**
     * Forgets the cached media token of [sourceId], so the next [mediaToken] asks the server
     * for a new one: for a caller the server has just refused (401 / 403) a stream, a
     * subtitle or a transcode with it.
     */
    fun forgetMediaToken(sourceId: String) {
        mediaTokensOf(token).bySource.remove(sourceId)
    }

    fun streamUrl(sourceId: String, mediaToken: String): String = "$baseUrl/v1/stream/$sourceId?mt=$mediaToken"

    /** The stream with no credential in it, for requests made with [http] (it sends the token as a header). */
    fun streamUrlForHttp(sourceId: String): String = "$baseUrl/v1/stream/$sourceId"

    /**
     * The source re-encoded by the server (H.264 at [height]p, [kbps] video, AAC audio;
     * subtitles, fonts and chapters copied) as a Matroska stream starting at [startSec].
     * Timestamps keep the file's clock, so the stream's first frame is at [startSec].
     */
    fun transcodeUrl(sourceId: String, height: Int, kbps: Int, startSec: Double, mediaToken: String): String =
        "$baseUrl/v1/sources/$sourceId/transcode?height=$height&bitrate=$kbps&start=${"%.3f".format(java.util.Locale.ROOT, startSec)}&mt=$mediaToken"

    /** Absolute image URL for an item's image, or null when the server has none (no 404 spam). */
    fun imageUrl(item: Item, type: String, widthPx: Int? = null): String? {
        val rel = item.images[type] ?: return null
        val abs = if (rel.startsWith("http")) rel else baseUrl + rel
        if (widthPx == null || widthPx <= 0) return abs
        return abs + (if ('?' in abs) "&w=" else "?w=") + widthPx
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
            streamUrl = source?.let { s -> cachedMediaToken(s.id)?.let { streamUrl(s.id, it) } } ?: "",
            hasAss = subs.any { it.codec.equals("ass", true) || it.codec.equals("ssa", true) },
            isHi10p = video?.codec.equals("h264", true) && video?.bitDepth == 10,
        )
    }

    // ---- transport ----

    private suspend inline fun <reified T> get(path: String): T = withContext(Dispatchers.IO) {
        execute<T>(request(path).get().build())
    }

    private suspend fun postNoBody(path: String, body: String, profileId: String? = null) = withContext(Dispatchers.IO) {
        val req = request(path).post(body.toRequestBody(JSON_TYPE)).apply { if (profileId != null) header(PROFILE_HEADER, profileId) }.build()
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

    private suspend inline fun <reified T> execute(req: Request): T {
        // Cancelled with its caller (see fetch).
        http.fetch(req).let { resp ->
            val text = resp.text
            if (!resp.ok) {
                Timber.w("HTTP %d %s %s", resp.code, req.method, req.url.encodedPath)
                throw ApiException(resp.code, serverMessage(text) ?: "HTTP ${resp.code} ${req.method} ${req.url.encodedPath}: ${text.take(200)}")
            }
            // A 200 that is not our JSON (a captive portal, a half-started server) is a
            // network-style failure, not a bug: an IOException like the rest.
            return try { parseUnlessCancelled(text) { json.decodeFromString<T>(it) } } catch (e: IllegalArgumentException) { // SerializationException is one
                throw IOException("Unexpected answer from the server at ${req.url.encodedPath}", e)
            }
        }
    }

    /** The server's `{"error": "..."}` text when the body is one, for messages shown to people. */
    private fun serverMessage(body: String): String? = runCatching {
        kotlinx.serialization.json.Json.parseToJsonElement(body).let { (it as? kotlinx.serialization.json.JsonObject)?.get("error") as? kotlinx.serialization.json.JsonPrimitive }?.content
    }.getOrNull()?.takeIf { it.isNotBlank() }

    private companion object {
        val JSON_TYPE = "application/json; charset=utf-8".toMediaType()
        const val PROFILE_HEADER = "X-Roxy-Profile"
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
private data class LinkStartRequest(val client: String, val device: String, val deviceId: String, val version: String)

@Serializable
data class ShowcaseCount(val count: Int = 0)

/** A TV sign-in code: what the TV shows, the secret it polls with, the link the QR carries. */
@Serializable
data class LinkStart(val code: String, val secret: String, val url: String, val expiresIn: Int = 600)

@Serializable
data class LinkInfo(
    val code: String, val client: String = "", val device: String = "", val expiresIn: Int = 0, val approved: Boolean = false,
    /** Where the asking device connected from: "home" | "tailscale" | "internet" (newer servers; "" otherwise). */
    val network: String = "",
)

/** GET /v1/system/info: public, no sign-in. */
@Serializable
data class ServerIdentity(val id: String = "", val name: String = "", val product: String = "", val version: String = "")

@Serializable
data class LinkResult(val status: String, val token: String? = null, val user: User? = null, val deviceId: String? = null)

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
    /** Changes with the picture; goes on the avatar URL so a new picture is never served from cache. */
    val avatarKey: String = "",
)

@Serializable
data class MediaFacts(
    val files: Int = 0,
    val bytes: Long = 0,
    val video: List<String> = emptyList(),
    val audio: List<String> = emptyList(),
    val subtitles: List<String> = emptyList(),
    val subFormats: List<String> = emptyList(),
)

@Serializable
data class CastMember(val name: String, val character: String = "", val photo: String? = null, val tmdbId: Int = 0)

@Serializable
data class Person(
    val tmdbId: Int, val name: String = "", val biography: String? = null, val birthday: String? = null, val deathday: String? = null,
    val placeOfBirth: String? = null, val knownFor: String? = null, val photo: String? = null, val homepage: String? = null, val imdbId: String? = null,
    val credits: List<DiscoverItem> = emptyList(),
)

@Serializable
data class TmdbExtras(
    val originalTitle: String = "",
    val firstAired: String = "",
    val lastAired: String = "",
    val status: String = "",
    val episodes: Int = 0,
    val seasons: Int = 0,
    val runtimeMinutes: Int = 0,
    val networks: List<String> = emptyList(),
    val companies: List<String> = emptyList(),
    val countries: List<String> = emptyList(),
    val languages: List<String> = emptyList(),
    val keywords: List<String> = emptyList(),
    val cast: List<CastMember> = emptyList(),
    val homepage: String = "",
    val tagline: String = "",
)

@Serializable
data class ItemDetails(val item: Item, val facts: MediaFacts? = null, val tmdb: TmdbExtras? = null, val links: Map<String, String> = emptyMap(), val backdrop: String? = null)

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
data class ItemsResponse(
    val items: List<Item> = emptyList(),
    val total: Int = 0,
    /**
     * The server's items change counter when the page was read (-1: an older server). It
     * moves when items are added, removed or changed (membership and order), not when their
     * files, user data or availability do: no snapshot of everything a page carries.
     */
    val version: Long = -1,
    /**
     * From collectPages: false when the list could not be read whole (an empty page short
     * of the server's total, or it changed through every try); [total] is then the server's.
     */
    val complete: Boolean = true,
)

/** What a whole-list read asks for: the most /v1/items sends in one request (5000 with sources), one consistent query. */
const val ITEMS_PAGE = 20000

/**
 * Pages of [page] from [fetch] (given the offset) until a short page or [ItemsResponse.total].
 * Offsets move when the list changes underneath (a scan adds or removes an episode between
 * two pages): one shifts across the boundary and comes twice, or is skipped. A change shows
 * as a different version (the server's items change counter): the read starts over (three
 * tries), and an item seen twice is kept once.
 */
suspend fun collectPages(page: Int, fetch: suspend (offset: Int) -> ItemsResponse): ItemsResponse {
    var out = mutableListOf<Item>()
    var total = 0
    var complete = false
    for (attempt in 0 until 3) {
        out = mutableListOf()
        var first = Long.MIN_VALUE
        var moved = false
        var stalled = false
        var offset = 0
        while (true) {
            val r = fetch(offset)
            // The version moves on any change, a removal plus an addition too (the total
            // alone stays the same then); an older server sends none: its total.
            val mark = if (r.version >= 0) r.version else r.total.toLong()
            if (offset == 0) { first = mark; total = r.total } else if (mark != first) { moved = true; break }
            out += r.items
            // Advance by what came, not by what was asked: the server may send fewer (its own
            // cap, lower with sources), and a short page is not the end then; the total is.
            offset += r.items.size
            if (out.size >= r.total) break
            if (r.items.isEmpty()) { stalled = true; break } // short of the total, nothing more: no progress
        }
        complete = !moved && !stalled
        if (complete || stalled) break // a stall would stall again
    }
    val unique = out.distinctBy { it.id }
    if (!complete) timber.log.Timber.w("items: read %d of %d; the list is incomplete", unique.size, total)
    return ItemsResponse(unique, if (complete) unique.size else maxOf(total, unique.size), complete = complete)
}

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
    /** The subtitle stream last chosen for this item; -1 = subtitles off, null = none saved. */
    val subtitleIndex: Int? = null,
    /** The audio stream last chosen for this item (the file's stream number), null = none saved. */
    val audioIndex: Int? = null,
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
data class SectionsResponse(val sections: List<Section> = emptyList())

@Serializable
data class NextResponse(val item: Item? = null)

/** One entry of a series' season list (see [RoxyApi.sections]). */
@Serializable
data class Section(
    /** season | specials | movie */
    val kind: String,
    val title: String = "",
    /** The season whose episodes it shows (for specials, the Specials season, narrowed to [episodeIds]). */
    val seasonId: String? = null,
    val episodeIds: List<String> = emptyList(),
    /** A film: the entry's one row. */
    val item: Item? = null,
    val episodes: Int = 0,
    val year: Int? = null,
)

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
    /** The provider's poster, for titles the library does not hold. */
    val posterUrl: String? = null,
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
    /** Content descriptors ("Sexual Content", "Violence"); on series and movies only. */
    val advisories: List<String> = emptyList(),
    val communityRating: Double? = null,
    val officialRating: String? = null,
    val premiered: String? = null,
    val airDate: String? = null,
    val status: String? = null,
    val studio: String? = null,
    /** Series: seasons the provider knows (specials excluded); the library may hold fewer. */
    val seasonCount: Int? = null,
    /** Series: linked movies / OVAs / side stories, so the title page knows to show the tab at once. */
    val relatedCount: Int = 0,
    val indexAired: Int? = null,
    val seasonAired: Int? = null,
    /** An extra (type "extra"): trailer, featurette, behindthescenes, deleted, interview, scene, short, clip, theme or extra. */
    val extraType: String? = null,
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

    /**
     * Watched with nothing to resume: the dimmed still, the replay badge, "Watched". A
     * watched episode that has a position again is a rewatch under way (the server keeps
     * it in Continue Watching), so it shows as in progress: the bar and "Nm left".
     */
    val isWatched: Boolean
        get() = userData?.played == true && (userData?.positionMs ?: 0L) <= 0L
}

@Serializable
data class MediaSource(
    val id: String,
    val container: String? = null,
    /** File size in bytes. */
    val size: Long = 0,
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
    /** The role, on a person's credits. */
    val character: String = "",
    val libraryItemId: String? = null,
    /** pending | sent | failed when someone asked for it. */
    val requestStatus: String? = null,
    /** What Sonarr / Radarr are doing with the request (null before it reached them). */
    val download: DownloadStatus? = null,
) {
    val isMovie: Boolean get() = type == "movie"
    val inLibrary: Boolean get() = !libraryItemId.isNullOrEmpty()
}

/**
 * A request's download status from the server's tracker. [state]: searching | notFound |
 * waiting | queued | downloading | stalled | importing | attention | failed | complete.
 */
@Serializable
data class DownloadStatus(
    /** The server's state as sent; read [state], which corrects a premature "complete". */
    @SerialName("state") val rawState: String = "",
    val progress: Double = 0.0,
    val timeLeftSec: Int = 0,
    /** Sonarr's / Radarr's own words when something is wrong. */
    val message: String? = null,
    val have: Int = 0,
    val wanted: Int = 0,
    val upcoming: Int = 0,
    val nextAir: String? = null,
    val lastSearch: String? = null,
    val groups: List<DownloadGroup> = emptyList(),
    val canSearch: Boolean = false,
    val canRetry: Boolean = false,
    /** When the server last looked; open episode lists reload when it changes. */
    val updatedAt: String? = null,
    /** Every requested episode still missing, each with its own state. */
    val episodes: List<EpisodeDownload> = emptyList(),
    /** waiting: "airing", "release" (not out at all) or "digital" (in cinemas, waiting for the digital release). */
    val waitingFor: String? = null,
    /** The server's own "an admin should look" flag (older servers leave it out). */
    @SerialName("isProblem") val serverProblem: Boolean = false,
) {
    /**
     * The state to show. "complete" with nothing had, wanted or upcoming means Sonarr has not
     * listed the episodes yet (older servers said "complete" then): treat it as searching.
     */
    val state: String get() = if (rawState == "complete" && wanted == 0 && have == 0 && upcoming == 0) "searching" else rawState
    /** Same as [state]; named for call sites that want to be explicit. */
    val effectiveState: String get() = state
    /** Episodes in the download client right now (downloading, stalled, importing, queued). */
    val activeEpisodes: List<EpisodeDownload> get() = episodes.filter { it.state in setOf("downloading", "stalled", "importing", "queued") }
    /** Something is moving in the download client (a ring on the poster). */
    val isMoving: Boolean get() = state in setOf("downloading", "stalled", "importing")
    /** Worth refreshing every few seconds. */
    val isActive: Boolean get() = state in setOf("downloading", "importing", "queued", "searching", "stalled")
    /** Something an admin can fix by trying another release. */
    val isProblem: Boolean get() = serverProblem || state in setOf("failed", "attention", "stalled")
}

@Serializable
data class DownloadGroup(val state: String, val label: String = "", val count: Int = 0, val progress: Double = 0.0, val timeLeftSec: Int = 0, val message: String? = null)

@Serializable
data class DiscoverEpisode(val number: Int, val title: String = "", val overview: String? = null, val airDate: String? = null, val still: String? = null, val have: Boolean = false, val requested: Boolean = false, val download: EpisodeDownload? = null)

/** One requested episode's own download state (see [DownloadStatus.state] for the values). */
@Serializable
data class EpisodeDownload(
    val season: Int = 0, val episode: Int = 0, val state: String = "", val progress: Double = 0.0, val timeLeftSec: Int = 0,
    val message: String? = null, val airDate: String? = null,
    /** The server's rules: not downloading, not being searched, not searched a moment ago. */
    val canSearch: Boolean = false,
    /** Failed, unimportable, or stalled long enough to be worth replacing. */
    val canRetry: Boolean = false,
)

@Serializable
private data class EpisodeCodes(val episodes: List<String> = emptyList())

@Serializable
private data class SearchBody(val episodes: List<String> = emptyList(), val seasons: List<Int> = emptyList())

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
    val download: DownloadStatus? = null,
    /** TMDB logo art (English or language-less), drawn over the banner. */
    val logo: String? = null,
    /** US age rating (TV-14, PG-13). */
    val officialRating: String? = null,
) {
    val isMovie: Boolean get() = type == "movie"
    val inLibrary: Boolean get() = !libraryItemId.isNullOrEmpty()
    /** Seasons with something still to fetch (not complete in the library, not requested). */
    val missingSeasons: List<DiscoverSeason> get() = seasons.filter { !it.special && !it.complete && !it.requested }
}

@Serializable
private data class DiscoverRequestBody(val type: String, val tmdbId: Int, val seasons: List<Int> = emptyList(), val episodes: List<String> = emptyList())

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
    /** TMDB season 0: requested one episode at a time. */
    val special: Boolean = false,
) {
    val complete: Boolean get() = episodes > 0 && have >= episodes
    /** Not out yet: no air date (announced) or one still to come ("2027" counts as to come). */
    val notOut: Boolean get() = airDate.isNullOrBlank() || java.time.LocalDate.now().toString().let { today -> airDate >= today || today.startsWith(airDate) }
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

/** One arr server's connection and defaults. */
@Serializable
data class ArrConfig(
    val url: String = "",
    val apiKey: String = "",
    val rootFolder: String = "",
    val qualityProfileId: Int = 0,
    val animeRootFolder: String = "",
    val animeQualityProfileId: Int = 0,
    /** Sonarr: older anime (first aired before 2010, ended); 0 = the anime profile. */
    val oldAnimeQualityProfileId: Int = 0,
    val animeSeriesType: String = "",
    val tags: List<Int> = emptyList(),
    val animeTags: List<Int> = emptyList(),
    val seasonFolders: Boolean? = null,
    val minimumAvailability: String = "",
    val searchOnAdd: Boolean? = null,
    val externalUrl: String = "",
)

@Serializable
data class RemoteCommand(val type: String, val itemId: String = "", val positionMs: Long = 0, val seconds: Int = 0, val from: String = "", val fromStart: Boolean = false, val handoff: String = "")

@Serializable
private data class RemoteCommandBody(val command: RemoteCommand)

@Serializable
data class RemoteNow(val itemId: String = "", val title: String = "", val positionMs: Long = 0, val paused: Boolean = false, val seq: Long = 0, val handoff: String = "", val handoffError: String = "")

@Serializable
data class RemotePlayer(val deviceId: String, val name: String = "", val client: String = "", val nowPlaying: RemoteNow? = null, val caps: List<String> = emptyList())

@Serializable
private data class RemotePlayers(val players: List<RemotePlayer> = emptyList())

@Serializable
data class ArtCandidate(val source: String = "", val url: String = "", val preview: String = "", val lang: String = "", val width: Int = 0, val height: Int = 0, val score: Double = 0.0, val selected: Boolean = false)

@Serializable
data class ArtCandidates(val candidates: List<ArtCandidate> = emptyList())

@Serializable
private data class ImageUrlBody(val url: String)

@Serializable
data class FanartSettings(val apiKey: String = "", val enabled: Boolean = false)

/** The server's transcoding encoder: [hwAccel] is the saved choice, [encoder] what runs now. */
@Serializable
/** [lastScanError]: one line per library the last full scan failed on (`library "Name": reason`), until a clean scan. */
data class LibraryManage(val scanning: Boolean = false, val lastScanAt: String? = null, val lastScanError: String? = null, val queue: Int = 0, val titles: List<LibraryTitle> = emptyList())

@Serializable
data class LibraryTitle(
    val id: String,
    val type: String = "series",
    val title: String = "",
    val year: Int? = null,
    val matched: Boolean = false,
    val locked: Boolean = false,
    val providerIds: Map<String, String> = emptyMap(),
    /** Relative poster URL (/v1/items/{id}/images/poster?v=...), when the title has one. */
    val poster: String? = null,
)

@Serializable
data class MatchCandidate(val provider: String = "tmdb", val id: String = "", val title: String = "", val year: Int? = null, val overview: String? = null, val poster: String? = null)

@Serializable
data class IdentifyResults(val candidates: List<MatchCandidate>? = null, val query: String = "")

@Serializable
private data class ScanMode(val mode: String)

@Serializable
data class SetupStatus(val required: Boolean = false, val allowed: Boolean = false, val tmdb: Boolean = false, val serverName: String = "")

@Serializable
private data class SetupAdmin(val name: String, val password: String, val device: String, val deviceId: String, val version: String)

@Serializable
data class TmdbSettings(val set: Boolean = false, val source: String = "")

@Serializable
private data class TmdbKey(val key: String)

@Serializable
data class ServerUser(val id: String, val name: String = "", val role: String = "requester", val isDisabled: Boolean = false, val isYou: Boolean = false, val createdAt: String = "")

@Serializable
private data class UsersResponse(val users: List<ServerUser> = emptyList())

/** Only the non-null fields are sent (explicitNulls = false). */
@Serializable
private data class UserEdit(val name: String? = null, val password: String? = null, val role: String? = null, val disabled: Boolean? = null)

@Serializable
private data class DeclineBody(val reason: String)

@Serializable
data class AdminRequest(
    val id: String,
    val type: String = "",
    val tmdbId: Int = 0,
    val title: String = "",
    val year: Int? = null,
    val userName: String = "",
    val status: String = "pending",
    val seasons: List<Int> = emptyList(),
    val episodes: List<String> = emptyList(),
    val error: String? = null,
    val createdAt: String = "",
    val poster: String? = null,
    val download: DownloadStatus? = null,
)

@Serializable
private data class AdminRequestsResponse(val requests: List<AdminRequest> = emptyList(), val waiting: Int = 0)

@Serializable
data class MediaFolder(
    val id: Long = 0,
    val name: String = "",
    val kind: String = "series",
    val root: String = "",
    val animeMode: Boolean = false,
    val series: Int = 0,
    val movies: Int = 0,
    val files: Int = 0,
    val reachable: Boolean = true,
)

@Serializable
private data class MediaFoldersResponse(val libraries: List<MediaFolder> = emptyList())

@Serializable
data class FolderEntry(val name: String = "", val path: String = "", val videos: Int = 0, val skip: String = "")

@Serializable
data class FolderListing(val path: String = "", val parent: String? = null, val dirs: List<FolderEntry> = emptyList(), val videos: Int = 0)

@Serializable
private data class FolderEdit(val path: String, val name: String, val kind: String, val anime: Boolean)

@Serializable
private data class PasswordChange(val current: String, val new: String)

@Serializable
data class MediaTokenResponse(val token: String = "", val expiresAt: String = "")

/** What Library > Refresh redoes. */
data class RefreshParts(val metadata: Boolean = true, val images: Boolean = false, val trickplay: Boolean = false, val segments: Boolean = false) {
    val any: Boolean get() = metadata || images || trickplay || segments
}

@Serializable
private data class RefreshRequest(val ids: List<String>, val all: Boolean, val metadata: Boolean, val images: Boolean, val trickplay: Boolean, val segments: Boolean)

@Serializable
private data class IdentifyChoice(val provider: String, val id: String)

/** Hand-entered details (no defaults, so every field is sent). */
@Serializable
data class MetadataEdit(val title: String, val year: Int?, val overview: String, val genres: List<String>, val studio: String, val premiered: String, val locked: Boolean)

@Serializable
data class TranscodingSettings(val hwAccel: String = "auto", val encoder: String = "", val options: List<EncoderOption> = emptyList())

@Serializable
data class EncoderOption(val id: String, val label: String, val available: Boolean = false, val suggested: String? = null)

@Serializable
data class TranscodingChoice(val hwAccel: String)

@Serializable
data class ArrSettings(val sonarr: ArrConfig = ArrConfig(), val radarr: ArrConfig = ArrConfig())

@Serializable
data class ArrSettingsResponse(val sonarr: ArrConfig = ArrConfig(), val radarr: ArrConfig = ArrConfig(), val requestsEnabled: Map<String, Boolean> = emptyMap())

@Serializable
data class ArrNamed(
    val id: Int = 0, val name: String = "", val label: String = "", val path: String = "", val freeSpace: Long = 0,
    /** Root folders: the Roxy library that reads it; roxyMatch "yes" | "maybe" | "" (Roxy doesn't read it). */
    val roxy: String = "", val roxyMatch: String = "",
)

@Serializable
data class ArrProbe(val version: String = "", val appName: String = "", val supported: Boolean = true, val note: String? = null, val rootFolders: List<ArrNamed> = emptyList(), val qualityProfiles: List<ArrNamed> = emptyList(), val tags: List<ArrNamed> = emptyList())

@Serializable
private data class ArrTestBody(val kind: String, val url: String, val apiKey: String)

@Serializable
data class LegalDoc(val doc: String = "", val title: String = "", val version: String = "", val body: String = "")

@Serializable
data class LegalStatus(val version: String = "", val accepted: Boolean = false, val acceptedVersion: String = "")

@Serializable
private data class VersionBody(val version: String)

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
private data class ProgressRequest(val itemId: String, val positionMs: Long, val audioIndex: Int? = null, val subtitleIndex: Int? = null, val at: Long? = null, val ageMs: Long? = null, val sourceId: String? = null, val durationMs: Long? = null)

/**
 * Subtitle tracks as mpv will number them (1..N in container order; embedded first,
 * sidecars after, the order `--sub-auto` uses), paired with the server's sniffed language.
 * Works on a live source or on a downloaded item's snapshot.
 */
fun subtitleInfos(streams: List<MediaStream>): List<SubtitleInfo> {
    var n = 0
    return streams.filter { it.type == "subtitle" }.map { s ->
        n += 1
        SubtitleInfo(mpvId = n, index = s.index, codec = s.codec, language = s.language, detectedLanguage = s.detectedLanguage, title = s.title.orEmpty(), isExternal = s.isExternal, isForced = s.isForced)
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
    /** Flagged forced in the container (a signs & songs track, to the player's track kinds). */
    val isForced: Boolean = false,
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
