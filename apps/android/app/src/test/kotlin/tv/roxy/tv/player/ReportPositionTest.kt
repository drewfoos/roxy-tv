package tv.roxy.tv.player

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ReportPositionTest {
    // Back pressed while a seek (transcode restart) loads: the seek's target, not mpv's
    // pre-seek clock; while the file opens (no clock yet), the start position, not nothing.
    @Test fun reportsWhereTheViewerIs() {
        assertEquals(1500.0, PlayerSession.reportPosition(null, reloadTarget = 1500.0, pendingStart = null, timePos = 300.0))
        assertEquals(1200.0, PlayerSession.reportPosition(null, reloadTarget = null, pendingStart = 1200.0, timePos = null))
        assertEquals(300.0, PlayerSession.reportPosition(null, reloadTarget = null, pendingStart = null, timePos = 300.0))
        assertEquals(42.0, PlayerSession.reportPosition(42.0, reloadTarget = 1500.0, pendingStart = 1200.0, timePos = 300.0))
    }

    // A paused frame's clock jitters a little: that is not the position moving (which would
    // make a stale report look fresh).
    @Test fun movementIgnoresJitter() {
        assertTrue(PlayerSession.positionMoved(null, 10.0))
        assertFalse(PlayerSession.positionMoved(10.0, 10.2))
        assertTrue(PlayerSession.positionMoved(10.0, 11.0))
    }
}
