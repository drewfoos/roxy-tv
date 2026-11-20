package tv.roxy.tv.ui

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.test.ExperimentalTestApi
import androidx.compose.ui.test.assertIsFocused
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performKeyInput
import androidx.compose.ui.test.pressKey
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import tv.roxy.tv.data.AppUpdate
import tv.roxy.tv.data.Compat.Verdict
import tv.roxy.tv.data.UpdateState
import tv.roxy.tv.data.Updates.Checked

/**
 * The page that stands in for the app when it and the server cannot work together: it
 * names the side to update and, for the app, walks the server's own update from "looking
 * for it" to the installer. Before, a too-old app only showed pages that failed to load.
 */
@OptIn(ExperimentalTestApi::class)
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], qualifiers = "w960dp-h540dp-xhdpi")
class CompatPageTest {
    @get:Rule val compose = createComposeRule()

    private val build = AppUpdate(versionCode = 200, versionName = "0.7.0")
    private fun words(v: Verdict, u: UpdateState = UpdateState.None, c: Checked? = null) = compatWords(v, u, c, "0.6.47")

    @Test fun nothingToSayWhenTheyMatch() {
        assertNull(words(Verdict.Ok))
        assertNull(compatSignInWarning(Verdict.Ok))
    }

    @Test fun anOldAppIsWalkedThroughItsUpdate() {
        val looking = words(Verdict.AppTooOld)!!
        assertEquals("Update needed", looking.title)
        assertTrue(looking.body.contains("0.6.47"))
        assertNull("nothing to press while it looks", looking.button)
        assertEquals("Looking for the update…", looking.progress)
        assertEquals(looking, words(Verdict.AppTooOld, c = Checked.Checking))

        val found = words(Verdict.AppTooOld, UpdateState.Available(build), Checked.Found)!!
        assertEquals("Update", found.button)
        assertEquals(CompatAction.Install, found.action)
        assertTrue(found.body.contains("0.7.0"))

        val coming = words(Verdict.AppTooOld, UpdateState.Downloading(build, 0.42f))!!
        assertNull(coming.button)
        assertEquals("Downloading Roxy 0.7.0  ·  42%", coming.progress)

        val file = java.io.File("x.apk")
        val ready = words(Verdict.AppTooOld, UpdateState.Ready(build, file))!!
        assertEquals("Install", ready.button)
        assertEquals(CompatAction.Install, ready.action)
        val installing = words(Verdict.AppTooOld, UpdateState.Ready(build, file, installing = true))!!
        assertNull("a second press would start a second install", installing.button)
        assertEquals("Installing Roxy 0.7.0", installing.progress)

        val failed = words(Verdict.AppTooOld, UpdateState.Failed(build, "The download stopped."))!!
        assertEquals("Try again", failed.button)
        assertEquals(CompatAction.Install, failed.action)
        assertTrue(failed.body, failed.body.endsWith("The update didn't download: The download stopped."))
    }

    // The server left the app behind and offers nothing newer: say whose move it is.
    @Test fun anOldAppWithNoUpdateOnTheServerSaysSo() {
        val none = words(Verdict.AppTooOld, c = Checked.Current)!!
        assertTrue(none.body, none.body.contains("no newer version"))
        assertEquals("Look again", none.button)
        assertEquals(CompatAction.LookForUpdate, none.action)
        val unreachable = words(Verdict.AppTooOld, c = Checked.Unreachable)!!
        assertTrue(unreachable.body, unreachable.body.contains("did not answer"))
        assertEquals(CompatAction.LookForUpdate, unreachable.action)
    }

    @Test fun anOldServerIsNamedAndTheUpdateIsNotOffered() {
        val w = words(Verdict.ServerTooOld, UpdateState.Available(build), Checked.Found)!!
        assertEquals("The server needs updating", w.title)
        assertEquals("Try again", w.button)
        assertEquals(CompatAction.TryAgain, w.action)
    }

    @Test fun theSignInPageSaysItUnderTheAddress() {
        assertNotNull(compatSignInWarning(Verdict.AppTooOld))
        assertTrue(compatSignInWarning(Verdict.ServerTooOld)!!.contains("Update the server"))
    }

    @Test @Config(qualifiers = "w400dp-h800dp-xhdpi")
    fun onThePhoneTheButtonDoesWhatItSays() {
        val did = mutableListOf<String>()
        compose.setContent { CompatPage(tv = false, words = words(Verdict.AppTooOld, UpdateState.Available(build))!!, onAction = { did += it.name }, onSignOut = { did += "out" }) }
        compose.onNodeWithText("Update needed").assertExists()
        compose.onNodeWithTag("compat-main").performClick()
        compose.onNodeWithTag("compat-signout").performClick()
        assertEquals(listOf("Install", "out"), did)
    }

    @Test @Config(qualifiers = "w400dp-h800dp-xhdpi")
    fun aDownloadShowsItsProgressAndNoButton() {
        compose.setContent { CompatPage(tv = false, words = words(Verdict.AppTooOld, UpdateState.Downloading(build, 0.5f))!!, onAction = {}, onSignOut = {}) }
        compose.onNodeWithTag("compat-main").assertDoesNotExist()
        compose.onNodeWithTag("compat-progress", useUnmergedTree = true).assertExists()
        compose.onNodeWithText("Downloading Roxy 0.7.0  ·  50%", useUnmergedTree = true).assertExists()
    }

    private fun press(keys: List<Key>) { for (k in keys) { compose.onRoot().performKeyInput { pressKey(k) }; compose.waitForIdle() } }

    @Test fun onTheTvTheRemoteStartsOnTheWayOut() {
        val did = mutableListOf<String>()
        compose.setContent { CompatPage(tv = true, words = words(Verdict.ServerTooOld)!!, onAction = { did += it.name }, onSignOut = { did += "out" }) }
        compose.waitForIdle()
        compose.onNodeWithTag("compat-main").assertIsFocused()
        press(listOf(Key.DirectionCenter))
        assertEquals(listOf("TryAgain"), did)
        press(listOf(Key.DirectionDown))
        compose.onNodeWithTag("compat-signout").assertIsFocused()
        press(listOf(Key.DirectionCenter, Key.DirectionUp))
        assertEquals(listOf("TryAgain", "out"), did)
        compose.onNodeWithTag("compat-main").assertIsFocused()
    }

    // The main button goes while the update downloads and comes back as Install: the
    // remote is never left on nothing, and Install is one press of OK away.
    @Test fun onTheTvTheFocusFollowsTheButtonThroughADownload() {
        var update by mutableStateOf<UpdateState>(UpdateState.Available(build))
        val did = mutableListOf<String>()
        compose.setContent { CompatPage(tv = true, words = words(Verdict.AppTooOld, update)!!, onAction = { did += it.name }, onSignOut = { did += "out" }) }
        compose.waitForIdle()
        compose.onNodeWithTag("compat-main").assertIsFocused()
        update = UpdateState.Downloading(build, 0.3f)
        compose.waitForIdle()
        compose.onNodeWithTag("compat-signout").assertIsFocused()
        update = UpdateState.Ready(build, java.io.File("x.apk"))
        compose.waitForIdle()
        compose.onNodeWithTag("compat-main").assertIsFocused()
        press(listOf(Key.DirectionCenter))
        assertEquals(listOf("Install"), did)
    }

    // The page is only a page until the app puts it in front of everything else.
    @Test fun theAppIsBehindTheGate() {
        val main = java.io.File("src/main/kotlin/tv/roxy/tv/MainActivity.kt").readText()
        assertTrue(main.contains("CompatGate(tv) { AppRoot(tv) }"))
    }
}
