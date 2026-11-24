package tv.roxy.tv.app

import android.content.pm.PackageManager
import android.content.res.Configuration
import android.graphics.Rect
import android.os.Looper
import android.os.SystemClock
import android.view.MotionEvent
import androidx.compose.runtime.getValue
import androidx.compose.runtime.setValue
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf
import org.robolectric.android.controller.ActivityController
import org.robolectric.annotation.Config
import tv.roxy.tv.MainActivity
import java.io.File
import java.util.concurrent.TimeUnit

/**
 * Going into the PiP window, the system shrinks the app's whole window and crops it to the
 * picture only gradually: the rest of the page rode along most of the way, first with its
 * text (the series' name and the episode's along the bottom of the small window), then as a
 * tall dark card under the video. While the app is being left with a video playing
 * ([MainActivity.pipLeaving]) the page is see-through, so the video alone flies to its
 * corner. The Home gesture says nothing to the app but "your window lost the focus".
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class PipLeavingTest {
    private lateinit var controller: ActivityController<MainActivity>
    private lateinit var activity: MainActivity
    private val page = Any()
    private val box = Rect(0, 128, 1440, 938)
    private var controlsHidden = 0

    @Before fun setUp() {
        shadowOf(androidx.test.core.app.ApplicationProvider.getApplicationContext<android.content.Context>().packageManager)
            .setSystemFeature(PackageManager.FEATURE_PICTURE_IN_PICTURE, true)
        controller = Robolectric.buildActivity(MainActivity::class.java).setup()
        activity = controller.get()
        activity.claimPip(page) { controlsHidden++ }
    }

    private fun playing(on: Boolean) = activity.updatePip(page, on, box)
    private fun pass(ms: Long) = shadowOf(Looper.getMainLooper()).idleFor(ms, TimeUnit.MILLISECONDS)

    /** The launcher is already shrinking the app when this arrives: no waiting when nothing of the app's own can have the focus. */
    @Test fun theWindowLosingTheFocusWithAVideoPlayingIsLeaving() {
        playing(true)
        controller.windowFocusChanged(false)
        assertTrue("the controls go at once", controlsHidden == 1)
        assertTrue("and so does the page", activity.pipLeaving)
        pass(500)
        assertTrue(activity.pipLeaving)
        // Not left after all (the gesture was called off, the notification shade closed).
        controller.windowFocusChanged(true)
        assertFalse(activity.pipLeaving)
        pass(500)
        assertFalse("no late look undoes that", activity.pipLeaving)
    }

    @Test fun pausedThereIsNoPipWindowToLeaveFor() {
        playing(false)
        controller.windowFocusChanged(false)
        pass(500)
        assertFalse(activity.pipLeaving)
        assertTrue("nor do the controls go", controlsHidden == 0)
        controller.pause()
        assertFalse(activity.pipLeaving)
    }

    /** A menu, a sheet or the settings page of the app's own is a window too: the page is still in use, and must not blink. */
    @Test fun aWindowOfTheAppsOwnTakingTheFocusIsNotLeaving() {
        playing(true)
        val dialog = android.app.Dialog(activity)
        dialog.setContentView(android.view.View(activity))
        dialog.show()
        shadowOf(Looper.getMainLooper()).idle()
        controller.windowFocusChanged(false)
        assertFalse("not even for a moment", activity.pipLeaving)
        // The system hands the focus to the dialog's window (Robolectric does not by itself).
        val root = org.robolectric.util.ReflectionHelpers.callInstanceMethod<Any>(dialog.window!!.decorView, "getViewRootImpl")
        org.robolectric.util.ReflectionHelpers.callInstanceMethod<Any?>(root, "windowFocusChanged", org.robolectric.util.ReflectionHelpers.ClassParameter.from(Boolean::class.javaPrimitiveType, true))
        shadowOf(Looper.getMainLooper()).idle()
        assertTrue("the test's dialog has the focus", dialog.window!!.decorView.hasWindowFocus())
        pass(500)
        assertFalse(activity.pipLeaving)
        dialog.dismiss()
    }

    /**
     * With a window of the app's own up beside the page, one that takes no focus (a
     * tooltip), the focus going to something else is seen at the look a moment later: it
     * is not a window on its way to having the focus, and is not waited for.
     */
    @Test fun withAWindowOfTheAppsOwnUpLeavingIsSeenAMomentLater() {
        playing(true)
        val dialog = android.app.Dialog(activity)
        dialog.window!!.addFlags(android.view.WindowManager.LayoutParams.FLAG_NOT_FOCUSABLE)
        dialog.setContentView(android.view.View(activity))
        dialog.show()
        shadowOf(Looper.getMainLooper()).idle()
        controller.windowFocusChanged(false) // the launcher has it
        assertFalse(activity.pipLeaving)
        pass(60)
        assertTrue(activity.pipLeaving)
        dialog.dismiss()
    }

    /** Gives a window the focus, or takes it away, as the system does (Robolectric does not by itself). */
    private fun focus(dialog: android.app.Dialog, has: Boolean) {
        val root = org.robolectric.util.ReflectionHelpers.callInstanceMethod<Any>(dialog.window!!.decorView, "getViewRootImpl")
        org.robolectric.util.ReflectionHelpers.callInstanceMethod<Any?>(root, "windowFocusChanged", org.robolectric.util.ReflectionHelpers.ClassParameter.from(Boolean::class.javaPrimitiveType, has))
        shadowOf(Looper.getMainLooper()).idle()
    }

    private fun shown(): android.app.Dialog = android.app.Dialog(activity).also {
        it.setContentView(android.view.View(activity))
        it.show()
        shadowOf(Looper.getMainLooper()).idle()
    }

    /**
     * File info open over a playing video, then the Home gesture: the focus goes from the
     * DIALOG to the launcher, and the page's own window, which lost it to the dialog long
     * before, hears nothing. The app went into the PiP window with the dialog over the
     * picture. It keeps looking while a window of its own holds the focus.
     */
    @Test fun leftWithAWindowOfTheAppsOwnOpenIsSeen() {
        playing(true)
        val info = shown()
        controller.windowFocusChanged(false) // the page's window, to the dialog
        focus(info, true)
        pass(2000) // read for a while
        assertFalse(activity.pipLeaving)
        focus(info, false) // the Home gesture
        pass(40)
        assertFalse("not at the first sight of it: see the next test", activity.pipLeaving)
        pass(260)
        assertTrue("the app is being left, and knows", activity.pipLeaving)
        info.dismiss()
    }

    /**
     * The menu to the dialog it opens ("File info"): the system takes the focus from the
     * menu when the dialog is asked for and gives it to the dialog once that is drawn,
     * frames later. Nobody of the app's has it in between, and that is not being left:
     * the page must not blink, and the dialog must not be closed as it opens.
     */
    @Test fun aWindowThatIsStillOpeningIsWaitedFor() {
        playing(true)
        val menu = shown()
        controller.windowFocusChanged(false)
        focus(menu, true)
        pass(300)
        val info = shown() // made, not drawn yet
        focus(menu, false)
        repeat(18) { pass(10); assertFalse("no look in between takes it for leaving", activity.pipLeaving) } // a slow one: 180 ms
        focus(info, true)
        menu.dismiss()
        pass(1000)
        assertFalse(activity.pipLeaving)
        // And from there the app can still be left.
        focus(info, false)
        pass(300)
        assertTrue(activity.pipLeaving)
        info.dismiss()
    }

    /** The page to a menu that is slow to come up: the same wait, where the page used to go blank until the menu had the focus. */
    @Test fun aMenuSlowToOpenDoesNotBlankThePage() {
        playing(true)
        val menu = shown()
        controller.windowFocusChanged(false)
        repeat(20) { pass(10); assertFalse(activity.pipLeaving) }
        focus(menu, true)
        pass(500)
        assertFalse(activity.pipLeaving)
        menu.dismiss()
    }

    /** A window that never gets the focus it should is not waited for for ever: the app is left with it open, and knows. */
    @Test fun aWindowThatNeverGetsTheFocusDoesNotHoldItUp() {
        playing(true)
        val info = shown()
        controller.windowFocusChanged(false)
        focus(info, true)
        pass(300)
        val late = shown() // asked for at the very moment of the Home gesture: the launcher has the focus, it never will
        focus(info, false)
        pass(MainActivity.OPENING_MS - 100)
        assertFalse("waited for", activity.pipLeaving)
        pass(400)
        assertTrue("but not for ever", activity.pipLeaving)
        late.dismiss(); info.dismiss()
    }

    /** A window of the app's closes: for an instant nobody has the focus, before the page's own window is told it has it back. */
    @Test fun aWindowClosingIsNotLeaving() {
        playing(true)
        val info = shown()
        controller.windowFocusChanged(false)
        focus(info, true)
        pass(340) // the next look is 10 ms off
        info.dismiss()
        pass(20) // it falls here
        assertFalse(activity.pipLeaving)
        controller.windowFocusChanged(true)
        pass(500)
        assertFalse(activity.pipLeaving)
    }

    /** Away to the recent apps and back with File info still open: the dialog has the focus again, the page's window is told nothing. */
    @Test fun backWithAWindowStillOpenTheLookingGoesOn() {
        playing(true)
        val info = shown()
        controller.windowFocusChanged(false)
        focus(info, true)
        pass(300)
        focus(info, false)
        controller.pause()
        pass(1000)
        assertFalse("paused outside PiP: not on the way into it", activity.pipLeaving)
        controller.resume()
        focus(info, true)
        pass(1000)
        assertFalse(activity.pipLeaving)
        focus(info, false) // now the Home gesture
        pass(300)
        assertTrue(activity.pipLeaving)
        info.dismiss()
    }

    /** Nothing is looked for under an open window while the video is paused: there is no PiP window to leave for. */
    @Test fun pausedUnderAWindowOfTheAppsOwnNothingIsLookedFor() {
        playing(false)
        val info = shown()
        controller.windowFocusChanged(false)
        focus(info, true)
        pass(300)
        focus(info, false)
        pass(1000)
        assertFalse(activity.pipLeaving)
        // It starts to play under the open window (the next episode): now it is.
        focus(info, true)
        playing(true)
        pass(300)
        focus(info, false)
        pass(300)
        assertTrue(activity.pipLeaving)
        info.dismiss()
    }

    /** What a page has open over itself closes when the app is left for the PiP window, and the window it leaves is the picture's alone. */
    @Test fun aWindowThePageOpenedIsClosedForThePipWindow() {
        playing(true)
        var open by androidx.compose.runtime.mutableStateOf(true)
        var closed = 0
        val info = androidx.activity.ComponentDialog(activity)
        info.setContentView(androidx.compose.ui.platform.ComposeView(activity).apply {
            setContent { if (open) tv.roxy.tv.ui.phone.ClosedForPip { closed++; open = false } }
        })
        info.show()
        shadowOf(Looper.getMainLooper()).idle()
        controller.windowFocusChanged(false)
        focus(info, true)
        pass(500)
        assertTrue("open while the page is in use", open && closed == 0)
        focus(info, false)
        pass(400)
        assertTrue(activity.pipLeaving)
        assertFalse("File info went with the page", open)
        assertTrue("and was closed once", closed == 1)
        info.dismiss()
    }

    /**
     * On every way into PiP the app is in PiP by the time it is paused. Paused outside it,
     * the app was left some other way (the recent-apps overview, a dialog of another app, the
     * screen off): the page is itself again. The overview's card for the app showed a video
     * over nothing.
     */
    @Test fun pausedOutsidePipThePageIsItselfAgain() {
        playing(true)
        controller.windowFocusChanged(false)
        assertTrue(activity.pipLeaving)
        controller.pause()
        assertFalse(activity.pipLeaving)
        pass(500)
        assertFalse("no late look sets it again", activity.pipLeaving)
        controller.resume()
        assertFalse(activity.pipLeaving)
    }

    /** Another app or a dialog coming over this one pauses it first; the focus goes after. That is not the Home gesture. */
    @Test fun aFocusLostAfterThePauseIsNotLeaving() {
        playing(true)
        controller.pause()
        controller.windowFocusChanged(false)
        pass(500)
        assertFalse(activity.pipLeaving)
        assertTrue("the controls still go: the page is not in use", controlsHidden == 1)
    }

    @Test fun aTouchOnThePageMeansItIsInUse() {
        playing(true)
        controller.windowFocusChanged(false)
        assertTrue(activity.pipLeaving)
        val t = SystemClock.uptimeMillis()
        val down = MotionEvent.obtain(t, t, MotionEvent.ACTION_DOWN, 100f, 100f, 0)
        activity.dispatchTouchEvent(down)
        down.recycle()
        assertFalse(activity.pipLeaving)
    }

    /** Out of the PiP window the page is all there again from the first frame: the system grows the window back with the page in it. */
    @Test fun leavingEndsWithThePipWindow() {
        playing(true)
        controller.windowFocusChanged(false)
        assertTrue(activity.pipLeaving)
        activity.onPictureInPictureModeChanged(true, Configuration(activity.resources.configuration))
        assertFalse("in the PiP window the page is the picture alone anyway", activity.pipLeaving)
        activity.onPictureInPictureModeChanged(false, Configuration(activity.resources.configuration))
        assertFalse(activity.pipLeaving)
    }

    @Test fun aPageThatHasGoneLeavesNothingSet() {
        playing(true)
        controller.windowFocusChanged(false)
        assertTrue(activity.pipLeaving)
        activity.clearPip(page)
        assertFalse(activity.pipLeaving)
    }

    // ---- the page (it needs the app's server and player to run: its source is read) ----

    private val pageSource = File("src/main/kotlin/tv/roxy/tv/ui/phone/PhoneEpisode.kt").readText()

    private val activitySource = File("src/main/kotlin/tv/roxy/tv/MainActivity.kt").readText()

    /** The whole window's content goes, at the root, with nothing between it and the window (see seeThrough). */
    @Test fun thePageIsSeeThroughWhileLeaving() {
        assertTrue("goingToPip is the activity's word", "val goingToPip = main?.pipLeaving == true" in pageSource)
        assertTrue("the player's own layers go at once", "override val inPip: Boolean get() = mainActivity?.inPip == true || mainActivity?.pipLeaving == true" in pageSource)
        assertTrue("the root follows pipLeaving", "val leaving = pipLeaving" in activitySource && "targetValue = if (leaving) 1f else 0f" in activitySource)
        assertTrue("and is see-through by it", "androidx.compose.foundation.layout.Box(Modifier.fillMaxSize().seeThrough { gone.value }) {" in activitySource)
        assertTrue("fading out, but back at once", "animationSpec = if (leaving) androidx.compose.animation.core.tween(LEAVING_FADE_MS, easing = androidx.compose.animation.core.LinearEasing) else androidx.compose.animation.core.snap()," in activitySource)
        // Nothing of the window's own lies under the page.
        assertTrue("window.setBackgroundDrawable(null)" in activitySource)
    }

    /** The layout is not what changes: the system crops the shrinking window to where the picture is in it. */
    @Test fun thePageKeepsItsLayoutWhileLeaving() {
        assertTrue("val full = landscape || inPip" in pageSource)
        assertTrue("val inPip = main?.inPip == true\n" in pageSource)
    }
}
