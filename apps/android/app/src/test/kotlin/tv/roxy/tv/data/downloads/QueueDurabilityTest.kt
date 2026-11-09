package tv.roxy.tv.data.downloads

import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import tv.roxy.tv.data.Prefs

/**
 * Found in review: the progress queue was saved with the downloads index, 400 ms after the
 * last change, so a watched mark recorded just before Android killed the app could be lost.
 * Here the store that recorded it is dropped at once (its delayed save never runs) and a new
 * one, as the restarted app makes, reads the mark back.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = android.app.Application::class)
class QueueDurabilityTest {
    @Test fun aWatchedMarkSurvivesAnImmediateKill() {
        val context = ApplicationProvider.getApplicationContext<android.app.Application>()
        val first = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined)
        val prefs = Prefs(context, first)
        val before = Downloads(context, prefs, first)
        before.recordProgress("e1", 1_400_000, reportedToServer = false)
        before.recordProgress("e1", 0, reportedToServer = false, played = true)
        first.cancel() // killed: the debounced index save never happens

        val restarted = Downloads(context, prefs, CoroutineScope(SupervisorJob() + Dispatchers.Unconfined))
        assertEquals(listOf("e1:1400000:false", "e1:0:true"), restarted.index.value.pendingProgress.map { "${it.itemId}:${it.positionMs}:${it.played}" })
    }
}
