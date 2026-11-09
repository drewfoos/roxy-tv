package tv.roxy.tv.player

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import tv.roxy.tv.player.PlaybackLifecycle.Phase
import tv.roxy.tv.player.PlaybackLifecycle.Why

/**
 * The player's lifecycle through the sequences that broke in November 2026 (each was a
 * combination of hand-kept flags), and the ordinary ones.
 */
class PlaybackLifecycleTest {
    private fun playing() = PlaybackLifecycle().apply { assertTrue(fileLoaded()) }

    @Test fun firstFileOpensAndPlays() {
        val l = PlaybackLifecycle()
        assertFalse("the first file has nothing older to ignore: Next stays possible if it never opens", l.switching)
        assertFalse(l.changingItem)
        assertTrue(l.fileLoaded())
        assertEquals(Phase.Playing, l.phase)
    }

    // Next: until the next episode's file is asked for, a load is the OLD file reopening
    // (passthrough watchdog, token retry) and must not count as the new one.
    @Test fun oldFileReloadedMidSwitchIsNotTheNewOne() {
        val l = playing()
        l.startNext()
        assertTrue(l.switching)
        assertTrue(l.changingItem)
        assertFalse(l.fileLoaded())
        assertTrue("positions still belong to the old episode", l.changingItem)
        l.fileRequested()
        assertTrue(l.fileLoaded())
        assertFalse(l.changingItem)
        assertFalse(l.switching)
    }

    // The next episode's stream fails and reconnects: until it really opens, it is still an
    // episode that never played (moving past it must not mark it watched).
    @Test fun nextEpisodeThatNeverOpenedStaysUnopenedThroughReconnects() {
        val l = playing()
        l.startNext(); l.fileRequested()
        l.reconnectScheduled(0.0)
        assertTrue(l.reconnecting)
        assertTrue(l.changingItem)
        l.reopening()
        l.waitForServer(0.0)
        assertTrue("never opened: Next must not mark it watched", l.changingItem)
        assertFalse("Next stays possible while waiting", l.switching)
    }

    // Next while waiting for the server: the wait belongs to the item left behind. Resumed
    // later it reopened the new episode at the old one's position.
    @Test fun nextEndsTheWaitForTheServer() {
        val l = playing()
        l.reconnectScheduled(1350.0); l.reopening(); l.waitForServer(1350.0)
        assertEquals(1350.0, l.resumeFromWait())
        assertFalse(l.switching)
        l.startNext()
        assertNull("the old item's wait is gone", l.resumeFromWait())
        assertNull(l.waitingAt)
    }

    // The end arrives twice (eof-reached and end-file): reported once.
    @Test fun endIsReportedOnce() {
        val l = playing()
        assertTrue(l.fileEnded())
        assertFalse(l.fileEnded())
        assertTrue(l.ended)
    }

    // A new file (a seek back after the end, the next episode) clears the end.
    @Test fun aNewFileClearsTheEnd() {
        val l = playing()
        l.fileEnded()
        l.reloadStarted(); assertTrue(l.fileLoaded())
        assertFalse(l.ended)
        assertTrue(l.fileEnded())
    }

    // The end before the next-episode lookup answered: one wait, ended once by the answer
    // or the time limit (a second eof does not start another).
    @Test fun oneWaitForTheNextEpisode() {
        val l = playing()
        l.fileEnded()
        assertTrue(l.startWaitingForNext())
        assertFalse(l.startWaitingForNext())
        assertTrue(l.waitingForNext)
        assertTrue(l.stopWaitingForNext())
        assertFalse(l.stopWaitingForNext())
        assertFalse(l.waitingForNext)
        assertTrue(l.ended)
    }

    @Test fun waitingForNextNeedsTheEnd() = assertFalse(playing().startWaitingForNext())

    // A reload (seek past a transcode's buffer, quality change): the old stream's end is not
    // the end; one that never starts gives the player back after its time limit.
    @Test fun reloadThatNeverStarts() {
        val l = playing()
        l.reloadStarted()
        assertTrue(l.switching)
        assertFalse(l.reconnecting)
        assertTrue(l.reloadTimedOut())
        assertEquals(Phase.Playing, l.phase)
        assertFalse("nothing to time out now", l.reloadTimedOut())
    }

    // A transcode reopened by a reconnect is still the reconnect (its time limit is not the
    // reload's, and the "lost connection" message stays until it opens).
    @Test fun aTranscodeReopenedByAReconnectIsStillTheReconnect() {
        val l = playing()
        l.reconnectScheduled(600.0)
        l.reloadStarted()
        assertEquals(Phase.Opening(Why.Reconnect, true), l.phase)
        assertTrue(l.reconnecting)
        assertFalse(l.reloadTimedOut())
        assertTrue(l.fileLoaded())
        assertFalse(l.reconnecting)
    }

    // Reconnects used up: waiting, neither reconnecting nor switching (seeks and Next work),
    // and the server's answer reopens where it stopped.
    @Test fun waitingForTheServer() {
        val l = playing()
        l.reconnectScheduled(42.0); l.reopening()
        assertTrue(l.reconnecting)
        l.waitForServer(42.0)
        assertFalse(l.reconnecting)
        assertFalse(l.switching)
        assertEquals(42.0, l.waitingAt)
        assertEquals(42.0, l.resumeFromWait())
        l.reconnectScheduled(42.0)
        assertNull(l.waitingAt)
    }

    // A download that will not open: the player stays, idle, its own item.
    @Test fun localFileFailing() {
        val l = playing()
        l.startNext(); l.fileRequested()
        l.localFileFailed()
        assertFalse(l.switching)
        assertFalse(l.changingItem)
    }

    @Test fun nextLookup() {
        val l = playing()
        assertFalse(l.nextKnown)
        l.lookupFailed(); assertFalse(l.nextKnown)
        l.lookupAnswered(); assertTrue(l.nextKnown)
        l.startNext(); assertFalse("a new item asks again", l.nextKnown)
        l.noLookup(); assertTrue(l.nextKnown)
    }

    // Found in review: Next, the old file's reopen fails before the new file is asked for,
    // a reconnect starts; during its delay the OLD file reloads (passthrough watchdog). That
    // load must still be ignored. The reconnect's own load (the new item's stream) is not.
    @Test fun oldFileReloadDuringAReconnectBeforeTheNewFileIsStillIgnored() {
        val l = playing()
        l.startNext()
        l.reconnectScheduled(120.0)
        assertFalse("the old file reloading", l.fileLoaded())
        assertTrue(l.changingItem)
        l.reopening()
        assertTrue("the reconnect's own load: the new item", l.fileLoaded())
        assertFalse(l.changingItem)
    }

    // Found in review: a reload (seek, quality) while waiting for the server that never
    // starts went back to playing, paused for good. It goes back to the wait.
    @Test fun aReloadThatNeverStartsGoesBackToWaiting() {
        val l = playing()
        l.reconnectScheduled(300.0); l.reopening(); l.waitForServer(300.0)
        l.reloadStarted()
        assertNull("the reload plays, not the wait", l.waitingAt)
        assertTrue(l.reloadTimedOut())
        assertEquals(300.0, l.waitingAt)
        // A reload that does start leaves the wait behind.
        l.reloadStarted()
        assertTrue(l.fileLoaded())
        assertNull(l.waitingAt)
    }
}

