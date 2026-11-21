package tv.roxy.tv.app

import android.content.pm.PackageManager
import android.content.res.Configuration
import android.graphics.Rect
import android.os.Looper
import android.os.SystemClock
import android.view.MotionEvent
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
 * Going into the PiP window, the system shrinks the app's window as it is and crops it to
 * the picture only gradually: the series' name and the episode's, under the picture on the
 * page, rode along inside the small window until they were cropped away. While the app is
 * being left with a video playing ([MainActivity.pipLeaving]) the page draws nothing but
 * the picture. The Home gesture says nothing to the app but "your window lost the focus".
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

    @Test fun theWindowLosingTheFocusWithAVideoPlayingIsLeaving() {
        playing(true)
        controller.windowFocusChanged(false)
        assertTrue("the controls go at once", controlsHidden == 1)
        assertFalse("the page waits to see who has the focus", activity.pipLeaving)
        pass(60)
        assertTrue(activity.pipLeaving)
        // Not left after all (the gesture was called off, the notification shade closed).
        controller.windowFocusChanged(true)
        assertFalse(activity.pipLeaving)
    }

    @Test fun aFocusThatComesBackAtOnceIsNotLeaving() {
        playing(true)
        controller.windowFocusChanged(false)
        controller.windowFocusChanged(true)
        pass(500)
        assertFalse(activity.pipLeaving)
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

    /** A menu, a sheet or the settings page of the app's own is a window too: the page is still in use. */
    @Test fun aWindowOfTheAppsOwnTakingTheFocusIsNotLeaving() {
        playing(true)
        val dialog = android.app.Dialog(activity)
        dialog.setContentView(android.view.View(activity))
        dialog.show()
        shadowOf(Looper.getMainLooper()).idle()
        controller.windowFocusChanged(false)
        // The system hands the focus to the dialog's window (Robolectric does not by itself).
        val root = org.robolectric.util.ReflectionHelpers.callInstanceMethod<Any>(dialog.window!!.decorView, "getViewRootImpl")
        org.robolectric.util.ReflectionHelpers.callInstanceMethod<Any?>(root, "windowFocusChanged", org.robolectric.util.ReflectionHelpers.ClassParameter.from(Boolean::class.javaPrimitiveType, true))
        shadowOf(Looper.getMainLooper()).idle()
        assertTrue("the test's dialog has the focus", dialog.window!!.decorView.hasWindowFocus())
        pass(500)
        assertFalse(activity.pipLeaving)
        dialog.dismiss()
    }

    /** Home by the button, another app on top: the activity pauses first. */
    @Test fun pausingWithAVideoPlayingIsLeavingAndResumingIsBeingBack() {
        playing(true)
        controller.pause()
        assertTrue(activity.pipLeaving)
        controller.resume()
        assertFalse(activity.pipLeaving)
    }

    @Test fun aTouchOnThePageMeansItIsInUse() {
        playing(true)
        controller.windowFocusChanged(false)
        pass(60)
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
        controller.pause()
        assertTrue(activity.pipLeaving)
        activity.onPictureInPictureModeChanged(true, Configuration(activity.resources.configuration))
        activity.onPictureInPictureModeChanged(false, Configuration(activity.resources.configuration))
        assertFalse(activity.pipLeaving)
    }

    @Test fun aPageThatHasGoneLeavesNothingSet() {
        playing(true)
        controller.pause()
        assertTrue(activity.pipLeaving)
        activity.clearPip(page)
        assertFalse(activity.pipLeaving)
    }

    // ---- the page (it needs the app's server and player to run: its source is read) ----

    private val pageSource = File("src/main/kotlin/tv/roxy/tv/ui/phone/PhoneEpisode.kt").readText()

    @Test fun thePageDrawsNothingButThePictureWhileLeaving() {
        assertTrue("goingToPip is the activity's word", "val goingToPip = main?.pipLeaving == true" in pageSource)
        assertTrue("the text under the picture is not drawn", "if (!full) Column(modifier = Modifier.fillMaxSize().alpha(if (goingToPip) 0f else 1f)" in pageSource)
        assertTrue("the player's own layers go too", "override val inPip: Boolean get() = mainActivity?.inPip == true || mainActivity?.pipLeaving == true" in pageSource)
    }

    /** The layout is not what changes: the system crops the shrinking window to where the picture is in it. */
    @Test fun thePageKeepsItsLayoutWhileLeaving() {
        assertTrue("val full = landscape || inPip" in pageSource)
        assertTrue("val inPip = main?.inPip == true\n" in pageSource)
    }
}
