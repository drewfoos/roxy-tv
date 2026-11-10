package tv.roxy.tv.app

import android.content.Intent
import android.net.Uri
import android.os.Bundle
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import tv.roxy.tv.MainActivity

/**
 * A Watch Next card (roxytv://play/<id>) or "Go to Details" link launches the app once. A
 * rebuilt activity (process death behind the player, a configuration change) gets the same
 * launch Intent again with a saved state: reading the link then played the card again, so
 * Back from the player reopened it.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class LaunchLinkTest {
    private fun launch(uri: String) = Intent(Intent.ACTION_VIEW, Uri.parse(uri))

    @Test fun aFreshLaunchReadsTheLink() {
        val play = Robolectric.buildActivity(MainActivity::class.java, launch("roxytv://play/e1")).create().get()
        assertEquals("e1", play.pendingPlayId)
        val open = Robolectric.buildActivity(MainActivity::class.java, launch("roxytv://open/s1?movie=0&title=Show")).create().get()
        assertEquals("s1", open.pendingOpen?.id)
    }

    @Test fun aRebuildDoesNotReplayTheLink() {
        val play = Robolectric.buildActivity(MainActivity::class.java, launch("roxytv://play/e1")).create(Bundle()).get()
        assertNull("rebuilt: the card was already played", play.pendingPlayId)
        val open = Robolectric.buildActivity(MainActivity::class.java, launch("roxytv://open/s1?movie=0&title=Show")).create(Bundle()).get()
        assertNull(open.pendingOpen)
    }
}
