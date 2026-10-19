package tv.roxy.tv.data

import android.content.Context
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.booleanPreferencesKey
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.intPreferencesKey
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking

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
    /** The profile used last, kept across Switch profile so the picker can mark it and offer a way back. */
    val lastProfileId: String? = null,
) {
    val isLoggedIn: Boolean get() = token != null && serverUrl.isNotEmpty()
    val hasProfile: Boolean get() = profileId != null
}

/** Player preferences (Settings screen). Persisted. */
data class PlayerSettings(
    /** "mediacodec" | "mediacodec-copy" | "no" */
    val hwdec: String = "mediacodec-copy",
    /** "gpu" | "gpu-next" */
    val vo: String = "gpu",
    /** Audio language order, ISO 639-2 codes. Owner default: English dub first, Japanese otherwise. */
    val audioLangs: List<String> = listOf("eng", "jpn"),
    /** "full" | "signs" | "none" */
    val subMode: String = "full",
    val showStats: Boolean = false,
    /** Play the next episode when one ends. Always on; kept as a field for the queue logic. */
    val autoplay: Boolean = true,
    /** Jump over intro/outro segments automatically instead of showing a Skip button. */
    val autoSkip: Boolean = false,
) {
    /** mpv `alang` list: each 3-letter code followed by its 2-letter twin, since tags vary. */
    val mpvAudioLangs: List<String>
        get() = audioLangs.flatMap { listOf(it, twoLetter[it] ?: it) }.distinct()

    companion object {
        val hwdecChoices = listOf("mediacodec", "mediacodec-copy", "no")
        val voChoices = listOf("gpu", "gpu-next")
        val audioChoices = listOf(listOf("eng", "jpn"), listOf("jpn", "eng"))
        val subModeChoices = listOf("full", "signs", "none")
        private val twoLetter = mapOf("eng" to "en", "jpn" to "ja")
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

    suspend fun setDismissedUpdate(code: Int) {
        _dismissedUpdate.value = code
        ds.edit { it[KEY_DISMISSED_UPDATE] = code }
    }

    init {
        // The preferences file is tiny; reading it once at startup keeps every later read synchronous.
        val seed = runBlocking { ds.data.first() }
        _session = MutableStateFlow(seed.toSession())
        _settings = MutableStateFlow(seed.toSettings())
        _downloadsWifiOnly = MutableStateFlow(seed[KEY_DL_WIFI] ?: true)
        _cachedProfiles = MutableStateFlow(seed[KEY_PROFILES_CACHE])
        _dismissedUpdate = MutableStateFlow(seed[KEY_DISMISSED_UPDATE] ?: 0)
        scope.launch {
            ds.data.collect { p ->
                _session.value = p.toSession()
                _settings.value = p.toSettings()
                _downloadsWifiOnly.value = p[KEY_DL_WIFI] ?: true
                _cachedProfiles.value = p[KEY_PROFILES_CACHE]
                _dismissedUpdate.value = p[KEY_DISMISSED_UPDATE] ?: 0
            }
        }
    }

    /** Keep the admin flag in step with what the server says at start-up. */
    suspend fun setAdmin(admin: Boolean) {
        val cur = _session.value
        if (cur.isAdmin != admin) saveSession(cur.copy(isAdmin = admin))
    }

    suspend fun setDownloadsWifiOnly(on: Boolean) {
        _downloadsWifiOnly.value = on
        ds.edit { it[KEY_DL_WIFI] = on }
    }

    suspend fun cacheProfiles(json: String) {
        _cachedProfiles.value = json
        ds.edit { it[KEY_PROFILES_CACHE] = json }
    }

    suspend fun saveSession(s: Session) {
        _session.value = s
        ds.edit {
            it[KEY_SERVER] = s.serverUrl
            if (s.token == null) it.remove(KEY_TOKEN) else it[KEY_TOKEN] = s.token
            it[KEY_USER] = s.userName
            it[KEY_ADMIN] = s.isAdmin
            if (s.profileId == null) it.remove(KEY_PROFILE_ID) else it[KEY_PROFILE_ID] = s.profileId
            it[KEY_PROFILE_NAME] = s.profileName
            it[KEY_PROFILE_AVATAR] = s.profileHasAvatar
            it[KEY_PROFILE_AVATAR_V] = s.profileAvatarVersion
            if (s.lastProfileId == null) it.remove(KEY_LAST_PROFILE) else it[KEY_LAST_PROFILE] = s.lastProfileId
        }
    }

    /** Choose (or update) the active profile; a new avatar bumps the version so Coil refetches it. */
    suspend fun selectProfile(id: String, name: String, hasAvatar: Boolean) {
        val cur = _session.value
        val bump = if (cur.profileId == id) cur.profileAvatarVersion + 1 else 0
        saveSession(cur.copy(profileId = id, profileName = name, profileHasAvatar = hasAvatar, profileAvatarVersion = bump, lastProfileId = id))
    }

    /** Back to the picker (Switch profile); remembers which profile we came from. */
    suspend fun clearProfile() {
        val cur = _session.value
        saveSession(cur.copy(profileId = null, profileName = "", profileHasAvatar = false, lastProfileId = cur.profileId ?: cur.lastProfileId))
    }

    suspend fun clearSession() {
        val keepUrl = _session.value.serverUrl
        _session.value = Session(serverUrl = keepUrl)
        ds.edit {
            it.remove(KEY_TOKEN)
            it.remove(KEY_USER)
            it.remove(KEY_PROFILE_ID)
            it.remove(KEY_PROFILE_NAME)
            it.remove(KEY_PROFILE_AVATAR)
            it.remove(KEY_LAST_PROFILE)
            it.remove(KEY_PROFILES_CACHE)
        }
    }

    suspend fun saveSettings(s: PlayerSettings) {
        _settings.value = s
        ds.edit {
            it[KEY_HWDEC] = s.hwdec
            it[KEY_VO] = s.vo
            it[KEY_ALANG] = s.audioLangs.joinToString(",")
            it[KEY_SUBMODE] = s.subMode
            it[KEY_STATS] = s.showStats
            it[KEY_AUTOPLAY] = s.autoplay
            it[KEY_AUTOSKIP] = s.autoSkip
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
        lastProfileId = this[KEY_LAST_PROFILE]?.takeIf { it.isNotEmpty() },
    )

    private fun Preferences.toSettings(): PlayerSettings {
        val d = PlayerSettings()
        return PlayerSettings(
            hwdec = this[KEY_HWDEC] ?: d.hwdec,
            vo = this[KEY_VO] ?: d.vo,
            audioLangs = this[KEY_ALANG]?.split(',')?.map { it.trim() }?.filter { it.isNotEmpty() }?.takeIf { it.isNotEmpty() } ?: d.audioLangs,
            subMode = this[KEY_SUBMODE] ?: d.subMode,
            showStats = this[KEY_STATS] ?: d.showStats,
            autoplay = this[KEY_AUTOPLAY] ?: d.autoplay,
            autoSkip = this[KEY_AUTOSKIP] ?: d.autoSkip,
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
        val KEY_LAST_PROFILE = stringPreferencesKey("last_profile_id")
        val KEY_HWDEC = stringPreferencesKey("hwdec")
        val KEY_VO = stringPreferencesKey("vo")
        val KEY_ALANG = stringPreferencesKey("audio_langs")
        val KEY_SUBMODE = stringPreferencesKey("sub_mode")
        val KEY_STATS = booleanPreferencesKey("show_stats")
        val KEY_AUTOPLAY = booleanPreferencesKey("autoplay")
        val KEY_AUTOSKIP = booleanPreferencesKey("auto_skip")
        val KEY_DL_WIFI = booleanPreferencesKey("downloads_wifi_only")
        val KEY_PROFILES_CACHE = stringPreferencesKey("profiles_cache")
        val KEY_DISMISSED_UPDATE = intPreferencesKey("dismissed_update")
    }
}
