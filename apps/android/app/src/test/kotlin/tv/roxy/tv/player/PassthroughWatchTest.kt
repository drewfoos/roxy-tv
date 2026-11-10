package tv.roxy.tv.player

import org.junit.Assert.assertEquals
import org.junit.Test
import tv.roxy.tv.player.PassthroughWatch.Verdict

/** The passthrough watchdog's decisions (MpvPlayer only polls it and acts on the verdict). */
class PassthroughWatchTest {
    private fun stuck(w: PassthroughWatch, polls: Int): Verdict {
        var v: Verdict = Verdict.Wait
        repeat(polls) { v = w.poll(120.0, held = false) }
        return v
    }

    @Test fun aClockThatNeverMovesFailsAfterSixSecondsOfTrying() {
        val w = PassthroughWatch("http://roxy/ep1", transcoded = false, startPos = 120.0)
        assertEquals(Verdict.Wait, stuck(w, 3))
        assertEquals(Verdict.Failed(120.0), w.poll(120.0, held = false))
        assertEquals("one failure per watch", Verdict.Done, w.poll(120.0, held = false))
    }

    @Test fun pausedOrBufferingTimeDoesNotCount() {
        val w = PassthroughWatch("http://roxy/ep1", transcoded = false, startPos = 120.0)
        repeat(10) { assertEquals(Verdict.Wait, w.poll(120.0, held = true)) }
        assertEquals(Verdict.Wait, stuck(w, 3))
    }

    @Test fun aRunningClockEndsTheWatch() {
        val w = PassthroughWatch("http://roxy/ep1", transcoded = false, startPos = 120.0)
        assertEquals(Verdict.Done, w.poll(121.0, held = false))
    }

    // The audit: the watch went on into the next episode (the old clock's 120 s, the new file
    // opening at 0 looked "stuck") and reopened the NEW file at the OLD file's position.
    @Test fun aNewFileCancelsTheWatch() {
        val w = PassthroughWatch("http://roxy/ep1", transcoded = false, startPos = 120.0)
        assertEquals(Verdict.Wait, stuck(w, 3))
        w.cancel() // loadPending for episode 2
        assertEquals(Verdict.Done, w.poll(0.0, held = false))
        assertEquals(Verdict.Done, w.poll(0.0, held = false))
    }

    @Test fun theReloadNamesTheFileTheWatchWasArmedFor() {
        val w = PassthroughWatch("http://roxy/ep1", transcoded = true, startPos = 300.0)
        assertEquals("http://roxy/ep1", w.url)
        assertEquals(true, w.transcoded)
    }
}
