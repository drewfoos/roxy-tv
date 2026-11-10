package tv.roxy.tv.ui

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/**
 * "Play on" to a phone whose episode page already plays that item: the page's player has
 * to take the request itself. Pushing a second page made a new player while the handover
 * waited for "a player showing the item", which the old one (about to be destroyed)
 * answered at once, so no acknowledgement was ever sent and the sender timed out.
 */
class SamePlayerTest {
    @Test fun anotherItemOrNoPlayerOpensANewOne() {
        assertNull(samePlayer(null, "e1", fromStart = false, startMs = 5_000))
        assertNull(samePlayer("e2", "e1", fromStart = false, startMs = 5_000))
    }

    @Test fun theSameItemSeeksToTheHandedOverPosition() {
        assertEquals(SamePlayer(seekToSec = 754), samePlayer("e1", "e1", fromStart = false, startMs = 754_300))
        assertEquals(SamePlayer(seekToSec = 0), samePlayer("e1", "e1", fromStart = true, startMs = null))
    }

    @Test fun aPlainRepeatCarriesOnWhereItIs() {
        assertEquals(SamePlayer(seekToSec = null), samePlayer("e1", "e1", fromStart = false, startMs = null))
    }
}
