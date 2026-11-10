package tv.roxy.tv.ui.phone

import androidx.compose.ui.test.junit4.StateRestorationTester
import androidx.compose.ui.test.junit4.createComposeRule
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * The phone's episode page is dropped from the composition when another page is pushed
 * (a cast member, the Next Episode row) and rebuilt from its saved state on the way back.
 * It used to start again from its route: the route's episode (episode 1 after autoplay had
 * moved to episode 5) and its explicit start (Play from start, a handover's position),
 * restarting it and overwriting its saved place.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class EpisodeStartTest {
    @get:Rule val compose = createComposeRule()

    private fun page(fromStart: Boolean, startMs: Long?): Pair<StateRestorationTester, () -> EpisodeStart> {
        val tester = StateRestorationTester(compose)
        var holder: EpisodeStart? = null
        tester.setContent { holder = rememberEpisodeStart("e1", fromStart, startMs) }
        return tester to { holder!! }
    }

    @Test fun firstStartUsesTheRouteArguments() {
        val (_, start) = page(fromStart = true, startMs = null)
        assertEquals(EpisodeStart.Args("e1", fromStart = true, startMs = null), start().args())
    }

    @Test fun backOnThePageAfterAutoplayResumesTheEpisodeOnScreen() {
        val (tester, start) = page(fromStart = true, startMs = null)
        compose.runOnIdle { start().started(); start().moved("e5") }
        tester.emulateSavedInstanceStateRestore()
        compose.runOnIdle { assertEquals(EpisodeStart.Args("e5", fromStart = false, startMs = null), start().args()) }
    }

    @Test fun backOnThePageDoesNotRepeatAnExplicitStart() {
        val (tester, start) = page(fromStart = false, startMs = 600_000)
        compose.runOnIdle { start().started() }
        tester.emulateSavedInstanceStateRestore()
        compose.runOnIdle { assertEquals(EpisodeStart.Args("e1", fromStart = false, startMs = null), start().args()) }
    }
}
