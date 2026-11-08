package tv.roxy.tv.player

import org.junit.Assert.assertEquals
import org.junit.Test

class ReconnectBudgetTest {
    // A review found the web player's version of this: the count was reset as soon as a
    // reopened stream started, so a stream dying 2 s after every reopening was reopened for
    // ever (here: always the first, immediate attempt). Brief recoveries no longer reset it.
    @Test fun briefRecoveriesKeepCounting() {
        val b = ReconnectBudget()
        var pos = 100.0
        repeat(6) {
            b.reconnected(pos)
            pos += 2.0 // loads, plays 2 s, dies again
            b.progress(pos)
        }
        assertEquals(6, b.used)
    }

    @Test fun realPlaybackRestoresTheAllowance() {
        val b = ReconnectBudget()
        b.reconnected(100.0)
        b.reconnected(101.0)
        b.progress(101.0 + ReconnectBudget.RECOVERED_AFTER_SEC - 1)
        assertEquals(2, b.used)
        b.progress(101.0 + ReconnectBudget.RECOVERED_AFTER_SEC)
        assertEquals(0, b.used)
    }

    @Test fun resetStartsAfresh() {
        val b = ReconnectBudget()
        repeat(3) { b.reconnected(10.0) }
        b.reset()
        assertEquals(0, b.used)
        b.progress(1000.0) // no reconnect pending: nothing to reset
        assertEquals(0, b.used)
    }
}
