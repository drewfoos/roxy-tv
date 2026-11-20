package tv.roxy.tv.data

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import tv.roxy.tv.data.Compat.Levels
import tv.roxy.tv.data.Compat.Verdict

/**
 * An app and a server that cannot work together say which of them to update, instead of
 * pages that fail to load: the rule that decides it, from what the server states in
 * `/v1/system/info` (`api.level`, `api.minClient`) and the app's own two numbers.
 */
class CompatTest {
    @Test fun aServerThatStillServesThisAppIsFine() {
        assertEquals(Verdict.Ok, Compat.verdict(Levels(level = 1, minClient = 1), clientLevel = 1, minServerLevel = 0))
        assertEquals(Verdict.Ok, Compat.verdict(Levels(level = 9, minClient = 3), clientLevel = 3, minServerLevel = 2))
        assertEquals("a newer app than the server asks for", Verdict.Ok, Compat.verdict(Levels(level = 4, minClient = 2), clientLevel = 7, minServerLevel = 4))
    }

    @Test fun anAppBelowTheServersMinimumNeedsUpdating() {
        assertEquals(Verdict.AppTooOld, Compat.verdict(Levels(level = 5, minClient = 2), clientLevel = 1, minServerLevel = 0))
    }

    @Test fun aServerBelowTheAppsMinimumNeedsUpdating() {
        assertEquals(Verdict.ServerTooOld, Compat.verdict(Levels(level = 2, minClient = 1), clientLevel = 3, minServerLevel = 3))
    }

    // Every server from before the rule: it states nothing. Today's app works with them.
    @Test fun aServerThatStatesNothingIsLevelZero() {
        assertEquals(Verdict.Ok, Compat.verdict(null, clientLevel = 1, minServerLevel = 0))
        assertEquals(Verdict.ServerTooOld, Compat.verdict(null, clientLevel = 2, minServerLevel = 1))
        assertEquals("with the numbers this build ships", Verdict.Ok, Compat.verdict(null))
    }

    @Test fun theVerdictIsReadFromTheServersOwnAnswer() {
        assertEquals(Verdict.Ok, Compat.verdictOf("""{"product":"roxy","id":"x","name":"Home","version":"1.2"}"""))
        assertEquals(Verdict.Ok, Compat.verdictOf("""{"product":"roxy","api":{"level":${Compat.CLIENT_LEVEL},"minClient":${Compat.CLIENT_LEVEL}}}"""))
        assertEquals(Verdict.AppTooOld, Compat.verdictOf("""{"product":"roxy","api":{"level":${Compat.CLIENT_LEVEL + 3},"minClient":${Compat.CLIENT_LEVEL + 1},"later":true}}"""))
        assertEquals(Verdict.ServerTooOld, Compat.verdictOf("""{"product":"roxy","api":{"level":1,"minClient":1}}""", clientLevel = 2, minServerLevel = 2))
    }

    // A wrong verdict locks the whole app: only Roxy's own answer may give one.
    @Test fun whatIsNotRoxysAnswerGivesNoVerdict() {
        assertNull(Compat.verdictOf("<html>Sign in to the Wi-Fi</html>"))
        assertNull(Compat.verdictOf(""))
        assertNull(Compat.verdictOf("""{"product":"other","api":{"level":9,"minClient":9}}"""))
        assertNull(Compat.verdictOf("""{"api":{"level":9,"minClient":9}}"""))
    }

    @Test fun onlyTheServersCodedRefusalCounts() {
        assertTrue(Compat.refused(426, """{"error":"This app is too old for this server. Update the app.","code":"client_too_old","api":{"level":2,"minClient":2}}"""))
        assertFalse("a proxy's 426", Compat.refused(426, "<html>Upgrade Required</html>"))
        assertFalse(Compat.refused(426, """{"error":"something"}"""))
        assertFalse(Compat.refused(409, """{"code":"client_too_old"}"""))
    }
}
