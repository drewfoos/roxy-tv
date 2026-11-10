package tv.roxy.tv.data

import android.content.Context
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.MutablePreferences
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.booleanPreferencesKey
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.intPreferencesKey
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.core.stringSetPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withContext

/** Where the app is logged in. Persisted; `token == null` means the login screen. */
data class Session(
    val serverUrl: String = "",
    val token: String? = null,
    val userName: String = "",
    /** Server admin (from login / auth/me): unlocks the Server section under Account. */
    val isAdmin: Boolean = false,
    /** The chosen profile (null = show the picker). Sent as X-Roxy-Profile on every request. */
    val profileId: String? = null,
    val profileName: String = "",
    val profileHasAvatar: Boolean = false,
    /** Bumped when the avatar changes so cached images are refetched. */
    val profileAvatarVersion: Int = 0,
    /** The server's avatarKey for the active profile (changes with the picture). */
    val profileAvatarKey: String = "",
    /** Terms version this login accepted, as last confirmed with the server ("" = unknown / not yet). */
    val acceptedTerms: String = "",
    /** The profile used last, kept across Switch profile so the picker can mark it and offer a way back. */
    val lastProfileId: String? = null,
    /** Signed in by the first-time setup, which isn't finished yet: the wizard stays on screen. */
    val setupInProgress: Boolean = false,
) {
    val isLoggedIn: Boolean get() = token != null && serverUrl.isNotEmpty()
    val hasProfile: Boolean get() = profileId != null
}

/**
 * A server this app has signed in to: its id (from /v1/system/info) for the address and
 * name it had then. A later answer with the same address or name but another id is not
 * the same server, and the login screen says so.
 */
data class ServerPin(val id: String, val name: String, val url: String)

/** Player preferences (Settings screen). Persisted. */
data class PlayerSettings(
    /** "auto" (zero-copy MediaCodec, copy mode as fallback) | "mediacodec" | "mediacodec-copy" | "no" */
    val hwdec: String = "auto",
    /**
     * Always "gpu" now: gpu-next with the bundled libplacebo 7.360.1 leaks GL sync objects
     * (purple or frozen picture after 15-20 min) and flips AV1 film grain on GLES. Kept as a
     * field for the adb bench extra; a stored "gpu-next" reads back as "gpu".
     */
    val vo: String = "gpu",
    /** Frame interpolation: smooth 24 fps on a 60 Hz screen (judder-free pans) at some GPU cost. */
    val smoothMotion: Boolean = false,
    /** HDR files go to the screen as HDR (no styled subtitles) instead of tone-mapped SDR. */
    val hdrNative: Boolean = false,
    /** Preferred audio languages in order, ISO 639-2 codes; the file's default track when none match. */
    val audioLangs: List<String> = listOf("eng"),
    /** Preferred subtitle language, ISO 639-2. */
    val subLang: String = "eng",
    /**
     * "signs" = dialogue subs on foreign audio, signs & songs over an anime dub;
     * "foreign" = dialogue subs on foreign audio only; "full" = the dialogue track
     * always; "none" = off. "smart" is an older stored spelling of "signs".
     */
    val subMode: String = "full",
    val showStats: Boolean = false,
    /** Play the next episode when one ends. Always on; kept as a field for the queue logic. */
    val autoplay: Boolean = true,
    /** Jump over intro/outro segments automatically instead of showing a Skip button. */
    val autoSkip: Boolean = false,
    /** How long the Skip intro/outro button stays up once a segment starts, in seconds. 0 = the whole segment. */
    val skipWindowSec: Int = 20,
    /**
     * Netflix's next-episode countdown card over the last seconds. Off by default: anime
     * often puts a scene or the preview there, and Next sits in the player's top bar.
     */
    val nextCard: Boolean = false,
    /**
     * Soundbar / receiver passthrough: "auto" sends Dolby Digital, DTS and the like
     * untouched to an HDMI audio output that takes them (see AudioPassthrough); "off"
     * always decodes on the device.
     */
    val passthrough: String = "auto",
    /**
     * Quality a playback starts at, by network: "original" (the file itself) or a
     * transcode preset key ("1080", "720", "480"). A preset is skipped for a file that is
     * already smaller. The player's Quality menu changes it for one playback.
     */
    val qualityWifi: String = "original",
    val qualityMobile: String = "original",
) {
    /** mpv `alang` list: every spelling of each preferred language, since tags vary. */
    val mpvAudioLangs: List<String>
        get() = audioLangs.flatMap { Languages.mpvCodes(it) }.distinct()

    /** mpv `slang` list for the subtitle language. */
    val mpvSubLangs: List<String>
        get() = Languages.mpvCodes(subLang)

    companion object {
        val hwdecChoices = listOf("auto", "mediacodec", "mediacodec-copy", "no")
        fun hwdecLabel(v: String): String = when (v) {
            "auto" -> "Auto (recommended)"
            "mediacodec" -> "Hardware, zero-copy"
            "mediacodec-copy" -> "Hardware, copy"
            "no" -> "Software only"
            else -> v
        }
        /** One explicit behaviour per choice; "smart" is an older stored name for "signs". */
        val subModeChoices = listOf("signs", "foreign", "full", "none")
        /** Seconds the Skip button stays; 0 = for the whole intro/outro. */
        val skipWindowChoices = listOf(5, 10, 15, 20, 30, 0)
        fun skipWindowLabel(sec: Int) = if (sec == 0) "Whole intro/outro" else "$sec seconds"

        fun subModeLabel(mode: String): String = when (mode) {
            "signs", "smart" -> "Signs & songs with a dub"
            "foreign" -> "Foreign audio only"
            "full" -> "Always full dialogue"
            "none" -> "Off"
            else -> mode
        }

        fun subModeHint(mode: String): String = when (mode) {
            "signs", "smart" -> "Dialogue subs on foreign audio; an anime dub shows just signs & songs"
            "foreign" -> "Dialogue subs only when the audio isn't in your language"
            "full" -> "The dialogue track whatever the audio, even over a dub"
            "none" -> "No subtitles unless you turn them on"
            else -> ""
        }
    }
}

private val Context.roxyDataStore: DataStore<Preferences> by preferencesDataStore("roxy")

/**
 * The app's persisted state: session (server, token, user) and player settings.
 * Both are mirrored into StateFlows so RoxyApi and the player can read them synchronously.
 */
class Prefs(context: Context, private val scope: CoroutineScope) {

    private val ds = context.applicationContext.roxyDataStore

    private val _session: MutableStateFlow<Session>
    private val _settings: MutableStateFlow<PlayerSettings>
    private val _downloadsWifiOnly: MutableStateFlow<Boolean>
    private val _cachedProfiles: MutableStateFlow<String?>

    val session: StateFlow<Session> get() = _session
    val settings: StateFlow<PlayerSettings> get() = _settings

    /** Downloads only run on unmetered networks (default on, like Netflix). */
    val downloadsWifiOnly: StateFlow<Boolean> get() = _downloadsWifiOnly

    /** The last profiles list from the server as JSON, so "Who's watching?" works offline. */
    val cachedProfiles: StateFlow<String?> get() = _cachedProfiles

    /** versionCode of an app update the user dismissed (the bar stays hidden for that build). */
    val dismissedUpdate: StateFlow<Int> get() = _dismissedUpdate
    private val _dismissedUpdate: MutableStateFlow<Int>

    /** Servers signed in to before (see [ServerPin]); a handful at most. */
    val serverPins: StateFlow<List<ServerPin>> get() = _serverPins
    private val _serverPins: MutableStateFlow<List<ServerPin>>

    /** Remember [id] as the server at [url] called [name]; replaces an older pin for that address or id. */
    suspend fun pinServer(url: String, id: String, name: String) {
        if (id.isBlank()) return
        val u = url.trim().trimEnd('/').lowercase()
        val next = (_serverPins.value.filterNot { it.url == u || it.id == id } + ServerPin(id.replace(PIN_SEP, ""), name.trim().replace(PIN_SEP, " "), u.replace(PIN_SEP, ""))).takeLast(MAX_PINS)
        _serverPins.value = next
        write { it[KEY_SERVER_PINS] = next.map { p -> listOf(p.id, p.name, p.url).joinToString(PIN_SEP) }.toSet() }
    }

    /**
     * True when a server answering at [url] as [name] with [id] is not the one signed in to
     * before under that address or that name. An id seen before (any address) is fine, as is
     * an answer with no id (an older server) or nothing pinned yet.
     */
    fun pinConflict(url: String, id: String, name: String): Boolean {
        if (id.isBlank()) return false
        val pins = _serverPins.value
        if (pins.any { it.id == id }) return false
        val u = url.trim().trimEnd('/').lowercase()
        val n = name.trim()
        return pins.any { it.url == u || (n.isNotEmpty() && it.name.equals(n, ignoreCase = true)) }
    }

    suspend fun setDismissedUpdate(code: Int) {
        _dismissedUpdate.value = code
        write { it[KEY_DISMISSED_UPDATE] = code }
    }

    init {
        // The preferences file is tiny; reading it once at startup keeps every later read synchronous.
        val seed = runBlocking { ds.data.first() }
        _session = MutableStateFlow(seed.toSession())
        _settings = MutableStateFlow(seed.toSettings())
        _downloadsWifiOnly = MutableStateFlow(seed[KEY_DL_WIFI] ?: true)
        _cachedProfiles = MutableStateFlow(seed[KEY_PROFILES_CACHE])
        _dismissedUpdate = MutableStateFlow(seed[KEY_DISMISSED_UPDATE] ?: 0)
        _serverPins = MutableStateFlow(seed.toPins())
        scope.launch {
            ds.data.collect { p ->
                _session.value = p.toSession()
                _settings.value = p.toSettings()
                _downloadsWifiOnly.value = p[KEY_DL_WIFI] ?: true
                _cachedProfiles.value = p[KEY_PROFILES_CACHE]
                _dismissedUpdate.value = p[KEY_DISMISSED_UPDATE] ?: 0
                _serverPins.value = p.toPins()
            }
        }
    }

    /**
     * Writes to the preferences file, and is not given up when the caller is cancelled. The
     * writers change what is in memory first, and many callers are screens that the change
     * itself takes away (choosing a profile, switching, signing out), which cancels them in
     * the middle of the write. A write dropped that way left the file with the old state:
     * the next start came back signed in (or on the old profile), and before that the mirror
     * above put the old state back into memory at the next write of anything.
     */
    private suspend fun write(change: (MutablePreferences) -> Unit) {
        withContext(NonCancellable) { ds.edit { change(it) } }
    }

    suspend fun setSetupInProgress(on: Boolean) {
        val cur = _session.value
        if (cur.setupInProgress != on) saveSession(cur.copy(setupInProgress = on))
    }

    suspend fun setAcceptedTerms(version: String) {
        val cur = _session.value
        if (cur.acceptedTerms != version) saveSession(cur.copy(acceptedTerms = version))
    }

    /** Keep the admin flag in step with what the server says at start-up. */
    suspend fun setAdmin(admin: Boolean) {
        val cur = _session.value
        if (cur.isAdmin != admin) saveSession(cur.copy(isAdmin = admin))
    }

    suspend fun setDownloadsWifiOnly(on: Boolean) {
        _downloadsWifiOnly.value = on
        write { it[KEY_DL_WIFI] = on }
    }

    suspend fun cacheProfiles(json: String) {
        _cachedProfiles.value = json
        write { it[KEY_PROFILES_CACHE] = json }
    }

    suspend fun saveSession(s: Session) {
        _session.value = s
        write {
            it[KEY_SERVER] = s.serverUrl
            if (s.token == null) it.remove(KEY_TOKEN) else it[KEY_TOKEN] = s.token
            it[KEY_USER] = s.userName
            it[KEY_ADMIN] = s.isAdmin
            if (s.profileId == null) it.remove(KEY_PROFILE_ID) else it[KEY_PROFILE_ID] = s.profileId
            it[KEY_PROFILE_NAME] = s.profileName
            it[KEY_PROFILE_AVATAR] = s.profileHasAvatar
            it[KEY_PROFILE_AVATAR_V] = s.profileAvatarVersion
            it[KEY_PROFILE_AVATAR_KEY] = s.profileAvatarKey
            it[KEY_TERMS] = s.acceptedTerms
            it[KEY_SETUP] = s.setupInProgress
            if (s.lastProfileId == null) it.remove(KEY_LAST_PROFILE) else it[KEY_LAST_PROFILE] = s.lastProfileId
        }
    }

    /** Choose (or update) the active profile; a new avatar bumps the version so Coil refetches it. */
    suspend fun selectProfile(id: String, name: String, hasAvatar: Boolean, avatarKey: String = "") {
        val cur = _session.value
        val bump = if (cur.profileId == id) cur.profileAvatarVersion + 1 else 0
        saveSession(cur.copy(profileId = id, profileName = name, profileHasAvatar = hasAvatar, profileAvatarVersion = bump, profileAvatarKey = avatarKey, lastProfileId = id))
    }

    /** Back to the picker (Switch profile); remembers which profile we came from. */
    suspend fun clearProfile() {
        val cur = _session.value
        saveSession(cur.copy(profileId = null, profileName = "", profileHasAvatar = false, lastProfileId = cur.profileId ?: cur.lastProfileId))
    }

    /**
     * The server refused a request naming profile [id]: it was deleted on another device.
     * If it is still the active one, back to the picker, where it is no longer "the one
     * used last" either. A profile picked meanwhile is left alone. Any thread.
     */
    fun forgetProfile(id: String) {
        if (_session.value.profileId != id) return
        scope.launch {
            val cur = _session.value
            if (cur.profileId != id) return@launch
            val next = cur.copy(profileId = null, profileName = "", profileHasAvatar = false, lastProfileId = cur.lastProfileId?.takeIf { it != id })
            _session.value = next // before the file is written: the next request must not name it again
            onProfileForgotten?.invoke(id)
            saveSession(next)
        }
    }

    /** Told which profile [forgetProfile] dropped (set by RoxyApp: its queued reports go too). */
    @Volatile var onProfileForgotten: ((String) -> Unit)? = null

    suspend fun clearSession() {
        val keepUrl = _session.value.serverUrl
        _session.value = Session(serverUrl = keepUrl)
        _cachedProfiles.value = null
        // Everything of the session but the address, so the file says what memory says: keys
        // left behind (admin, the avatar's version and key, setup) came back into memory with
        // the mirror, on a session with no login.
        write {
            it.remove(KEY_TOKEN)
            it.remove(KEY_USER)
            it.remove(KEY_ADMIN)
            it.remove(KEY_PROFILE_ID)
            it.remove(KEY_PROFILE_NAME)
            it.remove(KEY_PROFILE_AVATAR)
            it.remove(KEY_PROFILE_AVATAR_V)
            it.remove(KEY_PROFILE_AVATAR_KEY)
            it.remove(KEY_LAST_PROFILE)
            it.remove(KEY_PROFILES_CACHE)
            it.remove(KEY_TERMS)
            it.remove(KEY_SETUP)
        }
    }

    suspend fun saveSettings(s: PlayerSettings) {
        _settings.value = s
        write {
            it[KEY_HWDEC] = s.hwdec
            it[KEY_VO] = s.vo
            it[KEY_SMOOTH] = s.smoothMotion
            it[KEY_HDR_NATIVE] = s.hdrNative
            it[KEY_ALANG] = s.audioLangs.joinToString(",")
            it[KEY_SUBMODE] = s.subMode
            it[KEY_SLANG] = s.subLang
            it[KEY_STATS] = s.showStats
            it[KEY_AUTOPLAY] = s.autoplay
            it[KEY_AUTOSKIP] = s.autoSkip
            it[KEY_SKIP_WINDOW_2] = s.skipWindowSec
            it[KEY_NEXT_CARD] = s.nextCard
            it[KEY_PASSTHROUGH] = s.passthrough
            it[KEY_QUALITY_WIFI] = s.qualityWifi
            it[KEY_QUALITY_MOBILE] = s.qualityMobile
        }
    }

    private fun Preferences.toSession() = Session(
        serverUrl = this[KEY_SERVER].orEmpty(),
        token = this[KEY_TOKEN]?.takeIf { it.isNotEmpty() },
        userName = this[KEY_USER].orEmpty(),
        isAdmin = this[KEY_ADMIN] ?: false,
        profileId = this[KEY_PROFILE_ID]?.takeIf { it.isNotEmpty() },
        profileName = this[KEY_PROFILE_NAME].orEmpty(),
        profileHasAvatar = this[KEY_PROFILE_AVATAR] ?: false,
        profileAvatarVersion = this[KEY_PROFILE_AVATAR_V] ?: 0,
        profileAvatarKey = this[KEY_PROFILE_AVATAR_KEY].orEmpty(),
        acceptedTerms = this[KEY_TERMS].orEmpty(),
        lastProfileId = this[KEY_LAST_PROFILE]?.takeIf { it.isNotEmpty() },
        setupInProgress = this[KEY_SETUP] ?: false,
    )

    private fun Preferences.toPins(): List<ServerPin> =
        this[KEY_SERVER_PINS].orEmpty().mapNotNull { e ->
            val parts = e.split(PIN_SEP)
            if (parts.size == 3 && parts[0].isNotBlank()) ServerPin(parts[0], parts[1], parts[2]) else null
        }

    private fun Preferences.toSettings(): PlayerSettings {
        val d = PlayerSettings()
        return PlayerSettings(
            hwdec = this[KEY_HWDEC] ?: d.hwdec,
            vo = (this[KEY_VO] ?: d.vo).let { if (it == "gpu-next") "gpu" else it },
            smoothMotion = this[KEY_SMOOTH] ?: d.smoothMotion,
            hdrNative = this[KEY_HDR_NATIVE] ?: d.hdrNative,
            audioLangs = this[KEY_ALANG]?.split(',')?.map { it.trim() }?.filter { it.isNotEmpty() }?.takeIf { it.isNotEmpty() } ?: d.audioLangs,
            subMode = this[KEY_SUBMODE] ?: d.subMode,
            subLang = this[KEY_SLANG] ?: d.subLang,
            showStats = this[KEY_STATS] ?: d.showStats,
            autoplay = this[KEY_AUTOPLAY] ?: d.autoplay,
            autoSkip = this[KEY_AUTOSKIP] ?: d.autoSkip,
            // The default went from 15 to 20 s (2026-11-08); every settings save stored the old
            // one, so a 15 saved before then counts as the default. A choice made since is
            // kept under its own key.
            skipWindowSec = this[KEY_SKIP_WINDOW_2] ?: this[KEY_SKIP_WINDOW]?.takeIf { it != 15 } ?: d.skipWindowSec,
            nextCard = this[KEY_NEXT_CARD] ?: d.nextCard,
            passthrough = this[KEY_PASSTHROUGH] ?: d.passthrough,
            qualityWifi = this[KEY_QUALITY_WIFI] ?: d.qualityWifi,
            qualityMobile = this[KEY_QUALITY_MOBILE] ?: d.qualityMobile,
        )
    }

    private companion object {
        val KEY_SERVER = stringPreferencesKey("server_url")
        val KEY_TOKEN = stringPreferencesKey("token")
        val KEY_USER = stringPreferencesKey("user_name")
        val KEY_ADMIN = booleanPreferencesKey("is_admin")
        val KEY_PROFILE_ID = stringPreferencesKey("profile_id")
        val KEY_PROFILE_NAME = stringPreferencesKey("profile_name")
        val KEY_PROFILE_AVATAR = booleanPreferencesKey("profile_has_avatar")
        val KEY_PROFILE_AVATAR_V = intPreferencesKey("profile_avatar_v")
        val KEY_PROFILE_AVATAR_KEY = stringPreferencesKey("profile_avatar_key")
        val KEY_TERMS = stringPreferencesKey("accepted_terms")
        val KEY_SETUP = booleanPreferencesKey("setup_in_progress")
        val KEY_LAST_PROFILE = stringPreferencesKey("last_profile_id")
        // "_v2": "auto" (zero-copy first) became the default on 2026-10-31; the old key held
        // the previous default, mediacodec-copy, for everyone who ever saved a setting.
        val KEY_HWDEC = stringPreferencesKey("hwdec_v2")
        val KEY_SMOOTH = booleanPreferencesKey("smooth_motion")
        val KEY_HDR_NATIVE = booleanPreferencesKey("hdr_native")
        val KEY_VO = stringPreferencesKey("vo")
        val KEY_ALANG = stringPreferencesKey("audio_langs")
        // "_v2": full subtitles became the default on 2026-10-28; the old key held the previous default.
        val KEY_SUBMODE = stringPreferencesKey("sub_mode_v2")
        val KEY_SLANG = stringPreferencesKey("sub_lang")
        val KEY_STATS = booleanPreferencesKey("show_stats")
        val KEY_AUTOPLAY = booleanPreferencesKey("autoplay")
        val KEY_AUTOSKIP = booleanPreferencesKey("auto_skip")
        val KEY_SKIP_WINDOW = intPreferencesKey("skip_window_sec")
        val KEY_SKIP_WINDOW_2 = intPreferencesKey("skip_window_sec_2")
        val KEY_NEXT_CARD = booleanPreferencesKey("next_card")
        val KEY_PASSTHROUGH = stringPreferencesKey("audio_passthrough")
        val KEY_QUALITY_WIFI = stringPreferencesKey("quality_wifi")
        val KEY_QUALITY_MOBILE = stringPreferencesKey("quality_mobile")
        val KEY_DL_WIFI = booleanPreferencesKey("downloads_wifi_only")
        val KEY_PROFILES_CACHE = stringPreferencesKey("profiles_cache")
        val KEY_DISMISSED_UPDATE = intPreferencesKey("dismissed_update")
        val KEY_SERVER_PINS = stringSetPreferencesKey("server_pins")
        const val PIN_SEP = "\u001f"
        const val MAX_PINS = 8
    }
}
