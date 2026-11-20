package tv.roxy.tv.player

import android.content.Context
import android.content.Intent
import android.content.res.Configuration
import android.os.Bundle
import androidx.compose.ui.test.junit4.StateRestorationTester
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleOwner
import androidx.lifecycle.LifecycleRegistry
import androidx.test.core.app.ApplicationProvider
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config

/**
 * Leaving the app with a player open. The TV's player closes when it is no longer on
 * screen; the phone's stays for a short time away and for the floating window, and closes
 * when the app comes back after half an hour (it used to be there, paused, a day later).
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class AwayTest {
    @get:Rule val compose = createComposeRule()
    private val min = 60_000L
    private val context get() = ApplicationProvider.getApplicationContext<Context>()

    // ---- the rule ----

    @Test fun theTvPlayerClosesWhenStopped() {
        assertTrue(Away.closesWhenStopped(tv = true, changingConfigurations = false, finishing = false))
        assertFalse("a rebuild is not leaving", Away.closesWhenStopped(tv = true, changingConfigurations = true, finishing = false))
        assertFalse("closing already", Away.closesWhenStopped(tv = true, changingConfigurations = false, finishing = true))
        assertFalse("the phone's is kept", Away.closesWhenStopped(tv = false, changingConfigurations = false, finishing = false))
    }

    @Test fun thePhonePlayerClosesOnlyAfterALongTimeAway() {
        val t = 1_000_000_000L
        assertFalse("never left", Away.closesOnReturn(0, t, inPip = false))
        assertFalse(Away.closesOnReturn(t, t + 29 * min, inPip = false))
        assertTrue(Away.closesOnReturn(t, t + 30 * min, inPip = false))
        assertTrue(Away.closesOnReturn(t, t + 24 * 60 * min, inPip = false))
        assertFalse("the floating window is never closed", Away.closesOnReturn(t, t + 24 * 60 * min, inPip = true))
        assertFalse("clock set back while away", Away.closesOnReturn(t, t - 60 * min, inPip = false))
    }

    // ---- the TV's player activity ----

    /** A player being rebuilt (nothing playing yet, not closing): stopped by the system. */
    private fun stoppedPlayer(tv: Boolean): PlayerActivity {
        shadowOf(context.getSystemService(Context.UI_MODE_SERVICE) as android.app.UiModeManager)
            .setCurrentModeType(if (tv) Configuration.UI_MODE_TYPE_TELEVISION else Configuration.UI_MODE_TYPE_NORMAL)
        val saved = Bundle().also { PlayerActivity.saveState(it, "e1", 300) }
        val c = Robolectric.buildActivity(PlayerActivity::class.java, Intent(context, PlayerActivity::class.java).putExtra(PlayerActivity.EXTRA_ITEM_ID, "e1"))
        val a = c.create(saved).start().resume().get()
        assertFalse("open before it is left", a.isFinishing)
        c.pause().stop()
        return a
    }

    @Test fun theTvPlayerActivityClosesWhenTheTvLeavesIt() {
        assertTrue(stoppedPlayer(tv = true).isFinishing)
    }

    @Test fun thePhonePlayerActivityStays() {
        assertFalse(stoppedPlayer(tv = false).isFinishing)
    }

    // ---- the phone's player page ----

    private class Owner : LifecycleOwner {
        val registry = LifecycleRegistry.createUnsafe(this)
        override val lifecycle: Lifecycle get() = registry
        fun start() = registry.handleLifecycleEvent(Lifecycle.Event.ON_START)
        fun stop() = registry.handleLifecycleEvent(Lifecycle.Event.ON_STOP)
    }

    private val owner = Owner()
    private var now = 1_000_000_000L
    private var inPip = false
    private var closed = 0

    private fun page() {
        owner.start()
        compose.setContent { CloseAfterLongAbsence(owner, inPip = { inPip }, now = { now }, onClose = { closed++ }) }
        compose.waitForIdle()
    }

    private fun away(ms: Long) {
        compose.runOnUiThread { owner.stop() }
        now += ms
        compose.runOnUiThread { owner.start() }
        compose.waitForIdle()
    }

    @Test fun openingThePageClosesNothing() {
        page()
        assertEquals(0, closed)
    }

    @Test fun aShortTimeAwayKeepsThePlayer() {
        page()
        away(29 * min)
        assertEquals(0, closed)
    }

    @Test fun backAfterALongTimeThePageCloses() {
        page()
        away(31 * min)
        assertEquals(1, closed)
    }

    @Test fun shortAbsencesDoNotAddUp() {
        page()
        away(20 * min); away(20 * min); away(20 * min)
        assertEquals(0, closed)
    }

    @Test fun theFloatingWindowIsNotClosed() {
        page()
        inPip = true
        away(24 * 60 * min)
        assertEquals(0, closed)
        // Its time away is spent: back to full size and away briefly, the player stays.
        inPip = false
        away(5 * min)
        assertEquals(0, closed)
    }

    // The system ended the app while it was away and rebuilds the page from its saved state.
    @Test fun aPageRebuiltADayLaterCloses() {
        val restore = StateRestorationTester(compose)
        owner.start()
        restore.setContent { CloseAfterLongAbsence(owner, inPip = { inPip }, now = { now }, onClose = { closed++ }) }
        compose.runOnUiThread { owner.stop() }
        now += 24 * 60 * min
        restore.emulateSavedInstanceStateRestore()
        compose.runOnUiThread { owner.start() }
        compose.waitForIdle()
        assertEquals(1, closed)
    }

    @Test fun aPageRebuiltSoonAfterStays() {
        val restore = StateRestorationTester(compose)
        owner.start()
        restore.setContent { CloseAfterLongAbsence(owner, inPip = { inPip }, now = { now }, onClose = { closed++ }) }
        compose.runOnUiThread { owner.stop() }
        now += 5 * min
        restore.emulateSavedInstanceStateRestore()
        compose.runOnUiThread { owner.start() }
        compose.waitForIdle()
        assertEquals(0, closed)
    }

    // The page itself uses it, with the activity's stops and the page's own Back.
    @Test fun theEpisodePageUsesIt() {
        val src = java.io.File("src/main/kotlin/tv/roxy/tv/ui/phone/PhoneEpisode.kt").readText()
        assertTrue(Regex("""CloseAfterLongAbsence\(owner = main \?: .*inPip = \{ main\?\.inPip == true \}, onClose = onBack\)""").containsMatchIn(src))
    }
}
