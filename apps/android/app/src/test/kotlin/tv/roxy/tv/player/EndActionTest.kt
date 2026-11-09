package tv.roxy.tv.player

import org.junit.Assert.assertEquals
import org.junit.Test
import tv.roxy.tv.player.PlayerSession.EndAction

class EndActionTest {
    private fun end(autoplay: Boolean, known: Boolean, next: Boolean) = PlayerSession.endAction(autoplay, known, next)

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
}
