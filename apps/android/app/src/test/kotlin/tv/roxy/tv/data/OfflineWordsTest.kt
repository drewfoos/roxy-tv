package tv.roxy.tv.data

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/** The phone's offline notices say which thing is missing, and what still works. */
class OfflineWordsTest {
    @Test fun noNetworkIsOffline() {
        val w = offlineWords(hasNetwork = false, downloads = 0)
        assertEquals("No internet connection", w.strip)
        assertEquals("You're offline", w.title)
    }

    @Test fun aNetworkWithoutTheServerSaysTheServer() {
        val w = offlineWords(hasNetwork = true, downloads = 0)
        assertEquals("Can't reach your Roxy server", w.strip)
        assertTrue(w.body, "Tailscale" in w.body)
    }

    @Test fun downloadsOnThePhoneAreMentioned() {
        assertTrue(offlineWords(false, 3).body.endsWith("Your downloads still play."))
        assertTrue(offlineWords(true, 3).body.endsWith("Your downloads still play."))
        assertTrue("with none it does not promise any", "Your downloads" !in offlineWords(false, 0).body)
    }
}
