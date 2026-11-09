package tv.roxy.tv.player

import org.junit.Assert.assertEquals
import org.junit.Test
import tv.roxy.tv.player.PlayerSession.EndAction

class EndActionTest {
    private fun end(autoplay: Boolean, known: Boolean, next: Boolean, waitedOut: Boolean = false, cardUp: Boolean = true) =
        PlayerSession.endAction(autoplay, known, next, waitedOut, cardUp)

    // A film or the last episode: nothing to play, so the player closes instead of
    // holding the last frame.
    @Test fun nothingFollowsCloses() {
        assertEquals(EndAction.Close, end(autoplay = true, known = true, next = false))
        assertEquals(EndAction.Close, end(autoplay = false, known = true, next = false))
    }

    @Test fun autoplayMovesOn() = assertEquals(EndAction.PlayNext, end(autoplay = true, known = true, next = true))

    // Autoplay off with an episode after: stay, the card offers it.
    @Test fun autoplayOffStays() = assertEquals(EndAction.Stay, end(autoplay = false, known = true, next = true))

    // The end came before the lookup answered: neither close nor play yet.
    @Test fun unknownWaits() {
        assertEquals(EndAction.WaitForNext, end(autoplay = true, known = false, next = false))
        assertEquals(EndAction.WaitForNext, end(autoplay = false, known = false, next = false))
    }

    // A lookup that hangs (network gone) must not hold the last frame for ever: once the
    // wait is over, it counts as nothing next and the player closes.
    @Test fun hungLookupClosesAfterTheWait() {
        assertEquals(EndAction.Close, end(autoplay = true, known = false, next = false, waitedOut = true))
        assertEquals(EndAction.Close, end(autoplay = false, known = false, next = false, waitedOut = true))
    }

    // An answer that did arrive is used as it is, whatever the wait.
    @Test fun knownAnswerWinsOverTheWait() =
        assertEquals(EndAction.PlayNext, end(autoplay = true, known = true, next = true, waitedOut = true))

    // Autoplay off with the card not up (it is opt-in, or was dismissed): nothing offers
    // the next episode, so staying was a frozen last frame. Close instead.
    @Test fun autoplayOffWithoutTheCardCloses() = assertEquals(EndAction.Close, end(autoplay = false, known = true, next = true, cardUp = false))

    // One margin for both: a stream running out more than 10 s early is a dropped
    // connection; within it, the end. A file on disk always ended.
    @Test fun droppedOrEnded() {
        assertEquals(true, PlayerSession.droppedStream(pos = 1000.0, dur = 1400.0, fromDisk = false))
        assertEquals(false, PlayerSession.droppedStream(pos = 1392.0, dur = 1400.0, fromDisk = false))
        assertEquals(false, PlayerSession.droppedStream(pos = 1000.0, dur = 1400.0, fromDisk = true))
        assertEquals(false, PlayerSession.droppedStream(pos = 1000.0, dur = null, fromDisk = false))
    }

    // An end taken 2-10 s early (a transcode a little short) is still "at the end" when the
    // lookup answers or the wait runs out: the old 2 s check left the player waiting for
    // ever. A seek back since is not.
    @Test fun stillAtEndUsesTheSameMargin() {
        assertEquals(true, PlayerSession.stillAtEnd(pos = 1393.0, dur = 1400.0))
        assertEquals(false, PlayerSession.stillAtEnd(pos = 600.0, dur = 1400.0))
        assertEquals(true, PlayerSession.stillAtEnd(pos = 600.0, dur = null))
    }
}
