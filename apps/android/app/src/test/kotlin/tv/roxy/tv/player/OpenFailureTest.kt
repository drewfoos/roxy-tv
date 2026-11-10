package tv.roxy.tv.player

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * A stream the server refuses (the file is gone: 404; the transcode cannot start: 503) used
 * to be reopened for ever: six quick attempts, a wait that ended at once because the server
 * does answer, six more, and so on. The player now stops and says why.
 */
class OpenFailureTest {
    @Test fun readsTheStatusFromMpvsLogLines() {
        assertEquals(404, httpErrorStatus("http: HTTP error 404 Not Found"))
        assertEquals(503, httpErrorStatus("ffmpeg: http: HTTP error 503 Service Unavailable"))
        assertEquals(401, httpErrorStatus("Server returned 401 Unauthorized (authorization failed)"))
        assertEquals(429, httpErrorStatus("Server returned 429 Too Many Requests"))
        // ffmpeg names some answers only by their class.
        assertEquals(400, httpErrorStatus("Server returned 4XX Client Error, but not one of 40{0,1,3,4}"))
        assertEquals(500, httpErrorStatus("Server returned 5XX Server Error reply"))
    }

    @Test fun otherLinesHaveNoStatus() {
        assertNull(httpErrorStatus("Failed to open http://192.168.1.125:8097/v1/stream/abc."))
        assertNull(httpErrorStatus("Connection timed out"))
        assertNull(httpErrorStatus("Opening done: http://host/v1/stream/404"))
        assertNull(httpErrorStatus("HTTP error 200 OK"))
        assertNull(httpErrorStatus("video: 1404x800 at 503 kb/s"))
    }

    @Test fun aServerThatDoesNotAnswerIsWaitedFor() {
        assertEquals(AfterAttempts.Wait, afterAttemptsUsedUp(serverUp = false, httpStatus = null, failedRounds = 0))
        // Whatever the last open said, and however long it has been away.
        assertEquals(AfterAttempts.Wait, afterAttemptsUsedUp(serverUp = false, httpStatus = 404, failedRounds = 0))
        assertEquals(AfterAttempts.Wait, afterAttemptsUsedUp(serverUp = false, httpStatus = 503, failedRounds = 5))
    }

    @Test fun aRefusedStreamStopsAfterItsFirstRound() {
        for (status in listOf(400, 401, 403, 404, 410, 429)) {
            assertEquals("http $status", AfterAttempts.GiveUp, afterAttemptsUsedUp(serverUp = true, httpStatus = status, failedRounds = 1))
        }
    }

    @Test fun aServerErrorOrABrokenStreamGetsOneMoreRound() {
        assertEquals(AfterAttempts.Wait, afterAttemptsUsedUp(serverUp = true, httpStatus = 503, failedRounds = 1))
        assertEquals(AfterAttempts.Wait, afterAttemptsUsedUp(serverUp = true, httpStatus = null, failedRounds = 1))
        assertEquals(AfterAttempts.GiveUp, afterAttemptsUsedUp(serverUp = true, httpStatus = 503, failedRounds = 2))
        assertEquals(AfterAttempts.GiveUp, afterAttemptsUsedUp(serverUp = true, httpStatus = null, failedRounds = 2))
    }

    /** One round: every attempt fails, then the server is asked. What the session does, without mpv. */
    private fun round(b: ReconnectBudget, attempts: Int, at: Double, serverUp: Boolean, status: Int?): AfterAttempts {
        repeat(attempts) { b.reconnected(at) }
        b.attemptsUsedUp()
        b.roundFailed(serverUp)
        return afterAttemptsUsedUp(serverUp, status, b.failedRounds)
    }

    @Test fun aStreamAnswering503IsNotReopenedForEver() {
        val b = ReconnectBudget()
        assertEquals(AfterAttempts.Wait, round(b, 6, 300.0, serverUp = true, status = 503))
        assertEquals(0, b.used) // the round after the wait starts at the first attempt
        assertEquals(AfterAttempts.GiveUp, round(b, 6, 300.0, serverUp = true, status = 503))
    }

    @Test fun anOutageDoesNotCountAgainstTheStream() {
        val b = ReconnectBudget()
        // The server is away: round after round of waiting, never a stop.
        repeat(4) { assertEquals(AfterAttempts.Wait, round(b, 6, 300.0, serverUp = false, status = null)) }
        // Back, but still starting up: that round is the first against the stream.
        assertEquals(AfterAttempts.Wait, round(b, 6, 300.0, serverUp = true, status = 503))
        assertEquals(1, b.failedRounds)
    }

    @Test fun realPlaybackBetweenTwoFailedRoundsForgivesTheFirst() {
        val b = ReconnectBudget()
        assertEquals(AfterAttempts.Wait, round(b, 6, 300.0, serverUp = true, status = null))
        // Waiting, paused where it failed: no progress, nothing forgiven.
        b.progress(300.0)
        assertEquals(1, b.failedRounds)
        // The reopened stream plays on for a good while.
        b.reconnected(300.0)
        b.progress(300.0 + ReconnectBudget.RECOVERED_AFTER_SEC)
        assertEquals(0, b.failedRounds)
        assertEquals(AfterAttempts.Wait, round(b, 6, 900.0, serverUp = true, status = null))
    }

    @Test fun aNewItemStartsAfresh() {
        val b = ReconnectBudget()
        round(b, 6, 300.0, serverUp = true, status = 503)
        b.reset()
        assertEquals(0, b.failedRounds)
        assertEquals(0, b.used)
    }

    // The app keeps a media token per source for hours: one the server refuses has to be
    // dropped before the reconnect, or every attempt sends it again.
    @Test fun aRefusedTokenIsNotKeptForTheReconnect() {
        assertTrue(tokenRefused(401))
        assertTrue(tokenRefused(403))
        for (other in listOf(null, 400, 404, 416, 500, 503)) assertFalse("$other", tokenRefused(other))
        val session = java.io.File("src/main/kotlin/tv/roxy/tv/player/PlayerSession.kt").readText()
        val reconnect = session.substring(session.indexOf("private fun reconnect("), session.indexOf("private fun waitForServer("))
        val forget = reconnect.indexOf("if (tokenRefused(failedHttp)) sourceId?.let { app.api.forgetMediaToken(it) }")
        assertTrue("reconnect drops the refused token", forget >= 0)
        assertTrue("before the reopen asks for one", forget < reconnect.indexOf("app.api.mediaToken(sid)"))
        assertTrue("and before a transcode is restarted", forget < reconnect.indexOf("restartTranscode(at)"))
    }

    @Test fun theMessageNamesTheServersAnswer() {
        assertTrue(cannotOpenMessage(404).contains("404"))
        assertTrue(cannotOpenMessage(null).contains("Press play"))
    }
}
