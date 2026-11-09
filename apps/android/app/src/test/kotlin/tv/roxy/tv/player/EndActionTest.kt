package tv.roxy.tv.player

import org.junit.Assert.assertEquals
import org.junit.Test
import tv.roxy.tv.player.PlayerSession.EndAction

class EndActionTest {
    private fun end(autoplay: Boolean, known: Boolean, next: Boolean, waitedOut: Boolean = false) = PlayerSession.endAction(autoplay, known, next, waitedOut)

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
}
