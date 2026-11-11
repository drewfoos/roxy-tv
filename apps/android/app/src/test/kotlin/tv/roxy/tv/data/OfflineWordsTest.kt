package tv.roxy.tv.data

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/** The phone's offline notices say which thing is missing, and what still works. */
class OfflineWordsTest {
    @Test fun noNetworkIsOffline() {
        val w = offlineWords(hasNetwork = false, downloads = 0)
        assertEquals("No network connection", w.strip)
        assertEquals("You're offline", w.title)
    }

    @Test fun aNetworkWithoutTheServerSaysTheServer() {
        val w = offlineWords(hasNetwork = true, downloads = 0)
        assertEquals("Can't reach your Roxy server", w.strip)
        assertTrue(w.body, "Tailscale" in w.body)
    }

    // The app knows the device has a network, and that the server did not answer. It does
    // not know whether the internet works, and Roxy at home does not need it.
    @Test fun noNoticeClaimsToKnowAboutTheInternet() {
        val texts = listOf(true, false).flatMap { net ->
            val p = offlineWords(net, 0); val t = tvOfflineWords(net)
            listOf(p.strip, p.title, p.body, p.short, t.pill, t.title, t.body)
        }
        for (text in texts) assertTrue(text, "internet" !in text.lowercase() && "is online" !in text)
    }

    @Test fun downloadsOnThePhoneAreMentioned() {
        assertTrue(offlineWords(false, 3).body.endsWith("Your downloads still play."))
        assertTrue(offlineWords(true, 3).body.endsWith("Your downloads still play."))
        assertTrue("with none it does not promise any", "Your downloads" !in offlineWords(false, 0).body)
    }

    @Test fun theTvNeverSpeaksOfDownloads() {
        for (net in listOf(true, false)) {
            val w = tvOfflineWords(net)
            for (text in listOf(w.pill, w.title, w.body)) assertTrue(text, "ownload" !in text && "phone" !in text)
        }
        assertTrue("no network is the TV's own problem", "offline" in tvOfflineWords(false).title)
        assertTrue("a network with no answer is the server's", "server" in tvOfflineWords(true).title)
    }
}
