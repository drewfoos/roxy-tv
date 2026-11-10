package tv.roxy.tv.player

import androidx.test.core.app.ApplicationProvider
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import tv.roxy.tv.api.Item
import tv.roxy.tv.api.UserData
import tv.roxy.tv.data.PlayerSettings
import tv.roxy.tv.ui.PlayRequest

/**
 * The TV's player gets its request through an Intent. Found on the TV: the item's saved
 * subtitle track (Alya: "Dialogue", stream 4) was not put in it, so a resumed episode
 * started on the language policy's pick.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class PlayerIntentTest {
    private fun request(ud: UserData?) = PlayRequest(
        item = Item(id = "e9", type = "episode", title = "Rom-commy", userData = ud),
        url = "http://server/v1/stream/s", sourceId = "s", startSeconds = 1209, explicitStart = false, local = false,
    )

    @Test fun savedTracksReachThePlayer() {
        val i = PlayerActivity.intent(ApplicationProvider.getApplicationContext(), request(UserData(positionMs = 1, subtitleIndex = 4, audioIndex = 2)), PlayerSettings())
        assertEquals(4 to 2, PlayerActivity.savedStreams(i))
    }

    @Test fun offAndNothingSavedCarryNoTrack() {
        val off = PlayerActivity.intent(ApplicationProvider.getApplicationContext(), request(UserData(subtitleIndex = -1)), PlayerSettings())
        assertEquals(null to null, PlayerActivity.savedStreams(off))
        val none = PlayerActivity.intent(ApplicationProvider.getApplicationContext(), request(null), PlayerSettings())
        assertEquals(null to null, PlayerActivity.savedStreams(none))
    }
}
