package tv.roxy.tv.ui.setup

import kotlinx.coroutines.launch
import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import tv.roxy.tv.api.SetupStatus
import java.io.File

/**
 * A new server asks for its setup code (it is in the server's log and its data folder):
 * being on the server's network does not show that somebody is its owner. Both wizards
 * ask for it when the server says so, and send it.
 */
class SetupCodeTest {
    private val json = Json { ignoreUnknownKeys = true }

    @Test fun theServerSaysWhetherItAsksForACode() {
        assertTrue(json.decodeFromString(SetupStatus.serializer(), """{"required":true,"allowed":true,"code":true}""").code)
        // A server from before the code: no such field, no code asked for.
        assertFalse(json.decodeFromString(SetupStatus.serializer(), """{"required":true,"allowed":true}""").code)
    }

    @Test fun theAccountIsNotSentWithoutTheCodeTheServerAsksFor() {
        fun ready(needsCode: Boolean, code: String) = accountReady("owner", "long enough 1", "long enough 1", agreed = true, busy = false, needsCode = needsCode, code = code)
        assertFalse(ready(needsCode = true, code = ""))
        assertFalse(ready(needsCode = true, code = "   "))
        assertTrue(ready(needsCode = true, code = "k7qm-3xpd"))
        assertTrue("a server that asks for none", ready(needsCode = false, code = ""))
        // The rest of the form still counts.
        assertFalse(accountReady("", "long enough 1", "long enough 1", true, false, true, "K7QM-3XPD"))
        assertFalse(accountReady("owner", "short", "short", true, false, true, "K7QM-3XPD"))
        assertFalse(accountReady("owner", "long enough 1", "long enough 2", true, false, true, "K7QM-3XPD"))
        assertFalse(accountReady("owner", "long enough 1", "long enough 1", agreed = false, busy = false, needsCode = true, code = "K7QM-3XPD"))
        assertFalse(accountReady("owner", "long enough 1", "long enough 1", agreed = true, busy = true, needsCode = true, code = "K7QM-3XPD"))
    }

    /**
     * One question that got no answer used to settle "no code" for good, and the form then
     * had no place for the code the server went on to ask for. The server is asked until
     * it answers.
     */
    @Test fun theServerIsAskedUntilItAnswers() = kotlinx.coroutines.test.runTest {
        val need = SetupCodeNeed()
        var asked = 0
        val job = launch {
            askUntilAnswered(need, firstWaitMs = 1_000) {
                asked++
                if (asked < 4) throw java.io.IOException("no answer") else true
            }
        }
        testScheduler.runCurrent()
        assertEquals(1, asked)
        assertFalse("not known yet: no field, and no settled \"no\"", need.asked)
        testScheduler.advanceTimeBy(1_001); testScheduler.runCurrent()
        assertEquals(2, asked)
        testScheduler.advanceTimeBy(2_001); testScheduler.runCurrent()
        assertEquals(3, asked)
        testScheduler.advanceTimeBy(4_001); testScheduler.runCurrent()
        assertEquals(4, asked)
        assertTrue(need.asked)
        assertTrue("and then no more", job.isCompleted)
        // A server that answers "no code" is not asked again either.
        val none = SetupCodeNeed()
        var times = 0
        askUntilAnswered(none) { times++; false }
        assertEquals(1, times)
        assertFalse(none.asked)
    }

    /** The waits grow, to a quarter of a minute at most: a server that is away is not hammered. */
    @Test fun theWaitsBetweenQuestionsGrowAndStop() = kotlinx.coroutines.test.runTest {
        val need = SetupCodeNeed()
        val at = mutableListOf<Long>()
        val job = launch { askUntilAnswered(need, firstWaitMs = 1_000) { at += testScheduler.currentTime; throw java.io.IOException("away") } }
        testScheduler.advanceTimeBy(60_000); testScheduler.runCurrent()
        job.cancel()
        assertEquals(listOf(0L, 1_000L, 3_000L, 7_000L, 15_000L, 30_000L, 45_000L, 60_000L), at)
    }

    /** Whatever the page learnt or did not, the server's refusal for the code shows the field. */
    @Test fun theServersRefusalShowsTheField() {
        assertTrue(setupCodeRefused(tv.roxy.tv.api.ApiException(403, "That is not this server's setup code.", "setup_code")))
        assertFalse("another refusal", setupCodeRefused(tv.roxy.tv.api.ApiException(403, "setup only works from the server's home network")))
        assertFalse(setupCodeRefused(tv.roxy.tv.api.ApiException(409, "already set up", "something_else")))
        assertFalse(setupCodeRefused(java.io.IOException("no network")))
        val need = SetupCodeNeed()
        need.answered(false)
        assertFalse(need.asked)
        need.refused()
        assertTrue(need.asked)
        need.answered(false) // a late "no" does not take the field away again
        assertTrue(need.asked)
    }

    /** Both wizards show the field when the server asks, and what is typed goes out with the account. */
    @Test fun bothWizardsAskAndSend() {
        val dir = generateSequence(File("").absoluteFile) { it.parentFile }.map { File(it, "src/main/kotlin/tv/roxy/tv") }.first { it.isDirectory }
        for (name in listOf("ui/setup/SetupWizard.kt", "ui/setup/TvSetupWizard.kt")) {
            val src = File(dir, name).readText()
            assertTrue("$name asks the server", "rememberSetupCodeNeed(serverUrl)" in src)
            assertTrue("$name shows the field when the server refuses for the code", "if (setupCodeRefused(it)) codeNeed.refused()" in src)
            // The passwords and the code are not put into the state the system saves for the app.
            for (secret in listOf("code", "pw", "pw2")) {
                assertTrue("$name keeps $secret out of saved state", "var $secret by remember { mutableStateOf(\"\") }" in src)
                assertFalse("var $secret by rememberSaveable" in src)
            }
            assertTrue("$name shows the field", "if (needsCode) {" in src && "testTag(\"setup-code\")" in src && "SetupCodeWhere" in src)
            assertTrue("$name sends it", "BuildConfig.VERSION_NAME, code.trim())" in src)
            assertTrue("$name holds the button back", "accountReady(name, pw, pw2, agreed, busy, needsCode, code)" in src)
        }
        val api = File(dir, "api/RoxyApi.kt").readText()
        assertTrue("the code is in what is posted", "SetupAdmin(name, password, device, deviceId, version, code)" in api)
        assertTrue("the server's name for a refusal is read", "\"HTTP \${resp.code} \${req.method} \${req.url.encodedPath}: \${text.take(200)}\", serverCode(text))" in api)
        assertEquals(1, Regex("val code: String = \"\"\\)").findAll(api).count())
    }
}
