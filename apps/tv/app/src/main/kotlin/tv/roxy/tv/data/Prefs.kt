package tv.roxy.tv.data

import android.content.Context
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.booleanPreferencesKey
import androidx.datastore.preferences.core.edit
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
) {
    val isLoggedIn: Boolean get() = token != null && serverUrl.isNotEmpty()
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
    /** Play the next episode when one ends (the player's Autoplay switch). */
    val autoplay: Boolean = true,
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

    val session: StateFlow<Session> get() = _session
    val settings: StateFlow<PlayerSettings> get() = _settings

    init {
        // The preferences file is tiny; reading it once at startup keeps every later read synchronous.
        val seed = runBlocking { ds.data.first() }
        _session = MutableStateFlow(seed.toSession())
        _settings = MutableStateFlow(seed.toSettings())
        scope.launch {
            ds.data.collect { p ->
                _session.value = p.toSession()
                _settings.value = p.toSettings()
            }
        }
    }

    suspend fun saveSession(s: Session) {
        _session.value = s
        ds.edit {
            it[KEY_SERVER] = s.serverUrl
            if (s.token == null) it.remove(KEY_TOKEN) else it[KEY_TOKEN] = s.token
            it[KEY_USER] = s.userName
        }
    }

    suspend fun clearSession() {
        val keepUrl = _session.value.serverUrl
        _session.value = Session(serverUrl = keepUrl)
        ds.edit {
            it.remove(KEY_TOKEN)
            it.remove(KEY_USER)
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
        }
    }

    private fun Preferences.toSession() = Session(
        serverUrl = this[KEY_SERVER].orEmpty(),
        token = this[KEY_TOKEN]?.takeIf { it.isNotEmpty() },
        userName = this[KEY_USER].orEmpty(),
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
        )
    }

    private companion object {
        val KEY_SERVER = stringPreferencesKey("server_url")
        val KEY_TOKEN = stringPreferencesKey("token")
        val KEY_USER = stringPreferencesKey("user_name")
        val KEY_HWDEC = stringPreferencesKey("hwdec")
        val KEY_VO = stringPreferencesKey("vo")
        val KEY_ALANG = stringPreferencesKey("audio_langs")
        val KEY_SUBMODE = stringPreferencesKey("sub_mode")
        val KEY_STATS = booleanPreferencesKey("show_stats")
        val KEY_AUTOPLAY = booleanPreferencesKey("autoplay")
    }
}
