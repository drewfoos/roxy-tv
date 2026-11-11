package tv.roxy.tv.shots

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.padding
import androidx.compose.ui.Modifier
import androidx.compose.runtime.setValue
import androidx.compose.runtime.getValue
import androidx.compose.ui.layout.boundsInWindow
import androidx.compose.ui.layout.onGloballyPositioned
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.unit.dp
import androidx.test.core.app.ApplicationProvider
import org.junit.Assume.assumeTrue
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.Item
import tv.roxy.tv.api.UserData
import tv.roxy.tv.player.PlayerActions
import tv.roxy.tv.player.PlayerControls
import tv.roxy.tv.player.PlayerUiState
import tv.roxy.tv.player.Sheet
import tv.roxy.tv.ui.phone.ContinueRow
import tv.roxy.tv.ui.tv.ContinueCard
import tv.roxy.tv.ui.tv.TvDims
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.remember
import java.io.File

/**
 * Screens drawn without a device, to look at a layout change: ROXY_SHOTS=1 ./gradlew
 * testDebugUnitTest --tests '*Shots*' writes PNGs to app/build/shots. Skipped otherwise.
 * Images do not load here (no server): cards show their placeholders.
 */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35])
class Shots {
    @get:Rule val compose = createComposeRule()

    @Before fun only() = assumeTrue("set ROXY_SHOTS=1 to draw screens", System.getenv("ROXY_SHOTS") == "1")

    private fun shot(name: String) {
        val dir = File(System.getProperty("user.dir"), "build/shots").apply { mkdirs() }
        val bmp = compose.onRoot().captureToImage().asAndroidBitmap()
        File(dir, "$name.png").outputStream().use { bmp.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }

    private val app get() = ApplicationProvider.getApplicationContext<RoxyApp>()

    @Test @Config(qualifiers = "w412dp-h915dp-xxhdpi")
    fun phoneContinueWatching() {
        val items = listOf(
            Item(id = "e2", type = "episode", title = "An Explosion for This Chunchunmaru!", seriesTitle = "KonoSuba – God's Blessing on This Wonderful World!", indexAired = 2, seasonAired = 1, runtimeMs = 1_440_000, userData = UserData(positionMs = 60_000)),
            Item(id = "m1", type = "movie", title = "Rascal Does Not Dream of a Dreaming Girl", year = 2019, runtimeMs = 5_400_000, userData = UserData(positionMs = 2_000_000)),
        )
        compose.setContent {
            Box(Modifier.fillMaxSize().background(Color.Black).padding(top = 40.dp)) {
                ContinueRow("Continue Watching", items, app.api, onClick = {})
            }
        }
        shot("phone-continue-watching")
    }

    private val continueItems = listOf(
        Item(id = "e2", type = "episode", title = "An Explosion for This Chunchunmaru!", seriesTitle = "KonoSuba – God's Blessing on This Wonderful World!", indexAired = 2, seasonAired = 1, runtimeMs = 1_440_000, userData = UserData(positionMs = 60_000)),
        Item(id = "e7", type = "episode", title = "Dress", seriesTitle = "My Dress-Up Darling", indexAired = 7, seasonAired = 2, runtimeMs = 1_440_000, userData = UserData(positionMs = 0)),
        Item(id = "m1", type = "movie", title = "Rascal Does Not Dream of a Dreaming Girl", year = 2019, runtimeMs = 5_400_000, userData = UserData(positionMs = 2_000_000)),
    )

    @Test @Config(qualifiers = "w960dp-h540dp-land-television-xhdpi")
    fun tvContinueWatching() {
        compose.setContent {
            val first = remember { FocusRequester() }
            Box(Modifier.fillMaxSize().background(Color(0xFF101014)).padding(start = 40.dp, top = 60.dp)) {
                Row(horizontalArrangement = Arrangement.spacedBy(0.dp)) {
                    continueItems.forEachIndexed { i, it ->
                        ContinueCard(it, app.api, width = TvDims.ContinueCardWidth, onClick = {}, modifier = if (i == 0) Modifier.focusRequester(first) else Modifier)
                    }
                }
            }
            LaunchedEffect(Unit) { first.requestFocus() }
        }
        compose.waitForIdle()
        shot("tv-continue-watching")
    }

    private val noActions = object : PlayerActions {
        override fun togglePause() {}
        override fun seekTo(seconds: Double) {}
        override fun seekBy(seconds: Int) {}
        override fun selectSubtitle(id: Int) {}
        override fun selectAudio(id: Int) {}
        override fun setSpeed(speed: Double) {}
        override fun toggleMute() {}
        override fun toggleCaptions() {}
        override fun toggleAutoSkip() {}
        override fun toggleFill() {}
        override fun enterPip() {}
        override fun toggleFullscreen() {}
        override fun next() {}
        override fun back() {}
    }

    private fun tvPlayer(name: String, sheet: Sheet) {
        val state = PlayerUiState(title = "The End of the World", subtitle = "Sword Art Online  S01E14", positionSec = 1160.0, durationSec = 1421.0, bufferedSec = 1250.0, paused = true, hasNext = true, fullscreen = null)
        compose.setContent {
            Box(Modifier.fillMaxSize().background(Color(0xFF6A5A4A))) {
                PlayerControls(state = state, visible = true, sheet = sheet, onSheet = {}, onInteraction = {}, actions = noActions)
            }
        }
        compose.waitForIdle()
        shot(name)
    }

    // The Skip button with the controls showing (above the bar) and hidden (low in the corner).
    @Test @Config(qualifiers = "w960dp-h540dp-land-television-xhdpi")
    fun tvSkipShown() = tvSkip("tv-skip-controls", true)
    @Test @Config(qualifiers = "w960dp-h540dp-land-television-xhdpi")
    fun tvSkipHidden() = tvSkip("tv-skip-hidden", false)
    private fun tvSkip(name: String, visible: Boolean) {
        val state = PlayerUiState(title = "The End of the World", subtitle = "Sword Art Online  S01E14", positionSec = 95.0, durationSec = 1421.0, paused = false, hasNext = true, fullscreen = null,
            segments = listOf(tv.roxy.tv.api.MediaSegment("intro", 90_000, 180_000)))
        compose.setContent {
            Box(Modifier.fillMaxSize().background(Color(0xFF6A5A4A))) {
                PlayerControls(state = state, visible = visible, sheet = Sheet.None, onSheet = {}, onInteraction = {}, actions = noActions)
            }
        }
        compose.waitForIdle()
        shot(name)
    }

    @Test @Config(qualifiers = "w412dp-h915dp-port-xxhdpi")
    fun phoneUprightSettings() {
        val state = PlayerUiState(title = "An Explosion for This Chunchunmaru!", subtitle = "KonoSuba  S01E02", positionSec = 300.0, durationSec = 1440.0, paused = true, hasNext = true, fullscreen = false)
        compose.setContent {
            // The episode page in miniature: the video, then the series and episode title.
            androidx.compose.foundation.layout.Column(Modifier.fillMaxSize().background(Color(0xFF101014))) {
                Box(Modifier.fillMaxWidth().aspectRatio(16f / 9f).background(Color(0xFF6A5A4A))) {
                    PlayerControls(state = state, visible = true, sheet = Sheet.Settings, onSheet = {}, onInteraction = {}, actions = noActions)
                }
                androidx.compose.material3.Text("KonoSuba", color = Color(0xFF4FA8FF), modifier = Modifier.padding(start = 16.dp, top = 14.dp))
                androidx.compose.material3.Text("E2 - An Explosion for This Chunchunmaru!", color = Color.White,
                    modifier = Modifier.padding(start = 16.dp, top = 4.dp))
            }
        }
        compose.waitForIdle()
        val dir = File("build/shots").apply { mkdirs() }
        // Two windows: the episode page, and the settings page over it (a dialog).
        val act = compose.onAllNodes(androidx.compose.ui.test.isRoot())[0].captureToImage().asAndroidBitmap().copy(Bitmap.Config.ARGB_8888, true)
        val v = org.robolectric.shadows.ShadowDialog.getLatestDialog().window!!.decorView
        v.draw(android.graphics.Canvas(act))
        act.compress(Bitmap.CompressFormat.PNG, 100, File(dir, "phone-upright-settings.png").outputStream())
    }

    @Test @Config(qualifiers = "w915dp-h412dp-land-xxhdpi")
    fun phonePlayerSettings() {
        val state = PlayerUiState(title = "An Explosion for This Chunchunmaru!", subtitle = "KonoSuba  S01E02", positionSec = 300.0, durationSec = 1440.0, paused = true, hasNext = true, fullscreen = true, autoplay = false)
        compose.setContent {
            Box(Modifier.fillMaxSize().background(Color(0xFF6A5A4A))) {
                PlayerControls(state = state, visible = true, sheet = Sheet.Settings, onSheet = {}, onInteraction = {}, actions = noActions)
            }
        }
        compose.waitForIdle()
        shot("phone-player-settings")
    }

    @Test @Config(qualifiers = "w960dp-h540dp-land-television-xhdpi")
    fun tvPlayerControls() = tvPlayer("tv-player", Sheet.None)

    @Test @Config(qualifiers = "w960dp-h540dp-land-television-xhdpi")
    fun tvPlayerSettings() = tvPlayer("tv-player-settings", Sheet.Settings)

    // ---- Offline: the server away, with and without a network or downloads. ----

    private fun <T> setFlow(owner: Any, field: String, value: T) {
        val f = owner.javaClass.getDeclaredField(field).apply { isAccessible = true }
        @Suppress("UNCHECKED_CAST") (f.get(owner) as kotlinx.coroutines.flow.MutableStateFlow<T>).value = value
    }
    private fun away(hasNetwork: Boolean) { setFlow(app.server, "_online", false); setFlow(app.server, "_hasNetwork", hasNetwork) }

    private fun dl(show: String, showTitle: String, n: Int, title: String, pos: Long = 0, played: Boolean = false, at: String? = null) = tv.roxy.tv.data.downloads.DownloadEntry(
        itemId = "$show-e$n", sourceId = "src", titleId = show, seasonNumber = 1, state = tv.roxy.tv.data.downloads.DownloadState.Done, bytesTotal = 310_000_000,
        item = Item(id = "$show-e$n", type = "episode", title = title, seriesId = show, seriesTitle = showTitle, seasonAired = 1, indexAired = n, runtimeMs = 1_440_000,
            userData = UserData(positionMs = pos, played = played, lastPlayedAt = at)),
    )
    private val offlineTitles: List<tv.roxy.tv.data.downloads.DownloadedTitle> get() {
        val kono = listOf(dl("kono", "KonoSuba", 1, "This Self-Proclaimed Goddess and Reincarnation in Another World!", played = true, at = "2026-11-09T20:00:00Z"),
            dl("kono", "KonoSuba", 2, "An Explosion for This Chunchunmaru!", pos = 600_000, at = "2026-11-10T20:00:00Z"), dl("kono", "KonoSuba", 3, "A Panty Treasure in This Right Hand!"))
        val dress = listOf(dl("dress", "My Dress-Up Darling", 1, "Someone Who Lives in the Exact Opposite World as Me", pos = 200_000, at = "2026-11-08T20:00:00Z"), dl("dress", "My Dress-Up Darling", 2, "Wanna Hurry Up, and Do It?"))
        val film = tv.roxy.tv.data.downloads.DownloadEntry(itemId = "girl", sourceId = "src", titleId = "girl", state = tv.roxy.tv.data.downloads.DownloadState.Done, bytesTotal = 1_900_000_000,
            item = Item(id = "girl", type = "movie", title = "Rascal Does Not Dream of a Dreaming Girl", year = 2019, runtimeMs = 5_400_000))
        return listOf(
            tv.roxy.tv.data.downloads.DownloadedTitle("kono", Item(id = "kono", type = "series", title = "KonoSuba"), kono),
            tv.roxy.tv.data.downloads.DownloadedTitle("dress", Item(id = "dress", type = "series", title = "My Dress-Up Darling"), dress),
            tv.roxy.tv.data.downloads.DownloadedTitle("girl", film.item, listOf(film)),
        )
    }
    private fun seedDownloads() {
        val t = offlineTitles
        setFlow(app.downloads, "_index", tv.roxy.tv.data.downloads.DownloadIndex(entries = t.flatMap { it.entries }.associateBy { it.itemId }, titles = t.associate { it.id to tv.roxy.tv.data.downloads.TitleSnapshot(it.title) }))
    }
    private val offlineNav = tv.roxy.tv.ui.phone.OfflineNav(downloads = {}, title = {})

    private fun phoneOffline(name: String, content: @androidx.compose.runtime.Composable () -> Unit) {
        compose.setContent {
            tv.roxy.tv.ui.theme.RoxyTheme {
                androidx.compose.runtime.CompositionLocalProvider(tv.roxy.tv.ui.phone.LocalOfflineNav provides offlineNav) {
                    androidx.compose.foundation.layout.Column(Modifier.fillMaxSize().background(tv.roxy.tv.ui.theme.RoxyColors.background)) {
                        Box(Modifier.fillMaxWidth().weight(1f)) { content() }
                        tv.roxy.tv.ui.phone.OfflineStrip()
                        // Where the bottom bar sits.
                        Box(Modifier.fillMaxWidth().padding(0.dp).background(Color(0xFF121214)).padding(vertical = 28.dp))
                    }
                }
            }
        }
        compose.waitForIdle()
        shot(name)
    }

    @Test @Config(qualifiers = "w412dp-h915dp-port-xxhdpi")
    fun phoneOfflinePage() { away(hasNetwork = false); seedDownloads(); phoneOffline("phone-offline-page") { tv.roxy.tv.ui.phone.OfflinePage(onRetry = {}) } }

    @Test @Config(qualifiers = "w412dp-h915dp-port-xxhdpi")
    fun phoneServerAwayPage() { away(hasNetwork = true); phoneOffline("phone-server-away-page") { tv.roxy.tv.ui.phone.OfflinePage(onRetry = {}) } }

    @Test @Config(qualifiers = "w412dp-h915dp-port-xxhdpi")
    fun phoneOfflineHome() {
        away(hasNetwork = false); seedDownloads()
        phoneOffline("phone-offline-home") { tv.roxy.tv.ui.phone.OfflineState(serverUrl = "http://192.168.1.50:8097", titles = offlineTitles, bottomInset = 0.dp, onRetry = {}, onDownloads = {}, onOpenTitle = {}) }
    }

    @Test @Config(qualifiers = "w412dp-h915dp-port-xxhdpi")
    fun phoneOfflineHomeEmpty() {
        away(hasNetwork = true)
        phoneOffline("phone-offline-home-empty") { tv.roxy.tv.ui.phone.OfflineState(serverUrl = "http://192.168.1.50:8097", titles = emptyList(), bottomInset = 0.dp, onRetry = {}, onDownloads = {}, onOpenTitle = {}) }
    }

    @Test @Config(qualifiers = "w412dp-h915dp-port-xxhdpi")
    fun phoneOfflineTitle() {
        away(hasNetwork = false); seedDownloads()
        phoneOffline("phone-offline-title") { tv.roxy.tv.ui.phone.PhoneDownloadedTitle(titleId = "kono", onBack = {}, offlineNote = true) }
    }

    private fun tvOffline(name: String, hasNetwork: Boolean) {
        away(hasNetwork)
        // Signed in, so the pill at the top shows.
        setFlow(app.prefs, "_session", app.prefs.session.value.copy(token = "t", serverUrl = "http://192.168.1.50:8097"))
        compose.setContent {
            tv.roxy.tv.ui.theme.RoxyTheme {
                Box(Modifier.fillMaxSize().background(tv.roxy.tv.ui.theme.RoxyColors.background)) {
                    tv.roxy.tv.ui.components.TvOfflinePage(onRetry = {})
                    tv.roxy.tv.ui.components.ServerBanner(tv = true)
                }
            }
        }
        compose.waitForIdle()
        shot(name)
    }

    @Test @Config(qualifiers = "w960dp-h540dp-land-television-xhdpi")
    fun tvOfflinePage() = tvOffline("tv-offline-page", hasNetwork = false)

    @Test @Config(qualifiers = "w960dp-h540dp-land-television-xhdpi")
    fun tvServerAwayPage() = tvOffline("tv-server-away-page", hasNetwork = true)
}
