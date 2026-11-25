package tv.roxy.tv.ui.phone

import androidx.compose.runtime.getValue
import androidx.compose.runtime.setValue
import androidx.compose.ui.test.junit4.createComposeRule
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.util.ReflectionHelpers
import java.io.File

/**
 * With a menu or File info open over a playing video, the page's own window is told
 * nothing when the app is left: the focus is the dialog's to lose. The teller, inside
 * each such window, passes that on (once: when the window loses the focus it had).
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class TellsWhenUnfocusedTest {
    @get:Rule val compose = createComposeRule()

    private fun windowFocus(view: android.view.View, has: Boolean) {
        compose.runOnUiThread {
            val root = ReflectionHelpers.callInstanceMethod<Any>(view.rootView, "getViewRootImpl")
            ReflectionHelpers.callInstanceMethod<Any?>(root, "windowFocusChanged", ReflectionHelpers.ClassParameter.from(Boolean::class.javaPrimitiveType, has))
        }
        compose.waitForIdle()
    }

    @Test fun tellsOnceWhenTheWindowLosesTheFocusItHad() {
        val told = mutableListOf<android.view.View>()
        lateinit var view: android.view.View
        compose.setContent {
            view = androidx.compose.ui.platform.LocalView.current
            TellsWhenUnfocused { told += it }
        }
        compose.waitForIdle()
        // The test's window has the focus as it opens. Having it is nothing to tell.
        assertEquals(0, told.size)
        windowFocus(view, true)
        assertEquals(0, told.size)
        windowFocus(view, false)
        assertEquals(1, told.size)
        assertTrue("with the window's own root, which is how the activity knows it", told[0] === view.rootView)
        // Not again for a window that is still without it.
        windowFocus(view, false)
        assertEquals(1, told.size)
        // Again each time it loses the focus it had.
        windowFocus(view, true)
        assertEquals(1, told.size)
        windowFocus(view, false)
        assertEquals(2, told.size)
    }

    /** A window that has not had the focus yet (it is still opening) has none to lose: nothing is told. */
    @Test fun aWindowThatNeverHadTheFocusTellsNothing() {
        val told = mutableListOf<android.view.View>()
        var inIt by androidx.compose.runtime.mutableStateOf(false)
        lateinit var view: android.view.View
        compose.setContent {
            view = androidx.compose.ui.platform.LocalView.current
            if (inIt) TellsWhenUnfocused { told += it }
        }
        compose.waitForIdle()
        windowFocus(view, false) // the window is without the focus when the teller comes into it
        inIt = true
        compose.waitForIdle()
        assertEquals(0, told.size)
        windowFocus(view, true)
        windowFocus(view, false)
        assertEquals("told once it had it, and lost it", 1, told.size)
    }

    /** By default it is the activity that is told, and the windows a page opens over itself all carry the teller. */
    @Test fun theAppsOwnWindowsCarryIt() {
        val dir = generateSequence(File("").absoluteFile) { it.parentFile }.map { File(it, "src/main/kotlin/tv/roxy/tv/ui/phone") }.first { it.isDirectory }
        val ui = File(dir, "PhoneUi.kt").readText()
        assertTrue("the activity is told", "(tell ?: { root -> main?.ownWindowLostFocus(root) })(view.rootView)" in ui)
        assertTrue("every menu", "DropdownMenu(expanded = expanded, onDismissRequest = onDismissRequest, containerColor = RoxyColors.surfaceHigh) {\n        TellsWhenUnfocused()" in ui)
        assertTrue("File info", "text = {\n            TellsWhenUnfocused()" in File(dir, "PhoneSeries.kt").readText())
        val downloads = File(dir, "PhoneDownloads.kt").readText()
        assertEquals("the two dialogs of downloads", 2, Regex("text = \\{ TellsWhenUnfocused\\(\\); Text\\(").findAll(downloads).count())
        assertTrue("the play-on sheet", "TellsWhenUnfocused()\n        Column(modifier = Modifier.fillMaxWidth().padding(bottom = 24.dp).navigationBarsPadding())" in File(dir, "PhoneCast.kt").readText())
        // No menu of a page is drawn without the wrapper that carries it.
        for (name in listOf("PhoneSeries.kt", "PhoneDetails.kt", "PhoneDownloads.kt")) {
            assertTrue("$name draws its menus through PageMenu", "DropdownMenu(expanded" !in File(dir, name).readText())
        }
    }
}
