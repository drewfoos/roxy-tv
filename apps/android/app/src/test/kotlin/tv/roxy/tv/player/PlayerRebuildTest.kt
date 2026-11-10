package tv.roxy.tv.player

import android.content.Intent
import android.os.Bundle
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import tv.roxy.tv.api.Item
import tv.roxy.tv.data.PlayerSettings
import tv.roxy.tv.ui.PlayRequest
import tv.roxy.tv.player.PlayerActivity.Companion.DetailsTarget
import tv.roxy.tv.player.PlayerActivity.Companion.Restart

/**
 * The TV's player rebuilt (process death on a TV short of memory, a recreation): it used to
 * open its first Intent again, which after autoplay still named episode 1, at its old start
 * (or 0 after Play from start), and its reports then overwrote episode 1's saved place.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class PlayerRebuildTest {
    private val context get() = ApplicationProvider.getApplicationContext<android.content.Context>()
    private val e1 = Item(id = "e1", type = "episode", title = "One", seriesId = "show", seriesTitle = "Show")
    private val e2 = Item(id = "e2", type = "episode", title = "Two", seriesId = "show", seriesTitle = "Show")
    private fun req(item: Item, start: Int, explicit: Boolean) =
        PlayRequest(item = item, url = "http://server/v1/stream/${item.id}", sourceId = "s-${item.id}", startSeconds = start, explicitStart = explicit, local = false)

    /** "Play from start" on E1: the Intent the player is started with. */
    private fun firstIntent() = PlayerActivity.intent(context, req(e1, 0, explicit = true), PlayerSettings())

    @Test fun autoplayRewritesTheIntentToTheItemOnScreen() {
        // No URL: the activity closes at once, but its host is what a session calls back.
        val activity = Robolectric.buildActivity(PlayerActivity::class.java, Intent(context, PlayerActivity::class.java)).create().get()
        activity.host.nowPlaying(req(e2, 0, explicit = true))
        assertEquals("e2", activity.intent.getStringExtra(PlayerActivity.EXTRA_ITEM_ID))
        assertFalse("only the item: a rebuild must not jump to a chosen start", activity.intent.getBooleanExtra(PlayerActivity.EXTRA_START_EXPLICIT, true))
    }

    @Test fun firstStartPlaysTheIntent() {
        assertNull(PlayerActivity.restartPlan(firstIntent(), null))
    }

    @Test fun rebuildResolvesTheItemThatWasOnScreen() {
        val saved = Bundle().also { PlayerActivity.saveState(it, "e2", 300) }
        // The system hands back the ORIGINAL Intent (E1, from the start).
        assertEquals(Restart("e2", 300), PlayerActivity.restartPlan(firstIntent(), saved))
    }

    @Test fun rebuildWithoutSavedItemStillResolvesInsteadOfReplayingTheStart() {
        assertEquals(Restart("e1", 0), PlayerActivity.restartPlan(firstIntent(), Bundle()))
    }

    // "Go to Details": the show comes from the Intent, without a lookup that took up to 3 s
    // while the video played on.
    @Test fun detailsComeFromTheIntentWithoutALookup() = runBlocking {
        var looked = false
        val t = PlayerActivity.resolveDetails(firstIntent(), "e1") { looked = true; null }
        assertEquals(DetailsTarget("show", movie = false, title = "Show"), t)
        assertFalse(looked)
    }

    @Test fun detailsAfterAutoplayFollowTheNewItem() = runBlocking {
        val i = PlayerActivity.nowPlayingIntent(context, req(e2.copy(seriesId = "show2", seriesTitle = "Other"), 0, explicit = false), PlayerSettings())
        assertEquals("show2", PlayerActivity.resolveDetails(i, "e2") { null }?.id)
    }

    @Test fun detailsLookupFailingKeepsThePlayer() = runBlocking {
        val bare = Intent(context, PlayerActivity::class.java).putExtra(PlayerActivity.EXTRA_URL, "http://x/f.mkv")
        assertNull("nothing known: no page to open (the player stays)", PlayerActivity.resolveDetails(bare, "e9") { null })
        assertEquals(DetailsTarget("m1", movie = true, title = "Film"),
            PlayerActivity.resolveDetails(bare, "m1") { Item(id = "m1", type = "movie", title = "Film") })
    }
}
