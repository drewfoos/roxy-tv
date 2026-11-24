package tv.roxy.tv.ui.setup

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

    /** Both wizards show the field when the server asks, and what is typed goes out with the account. */
    @Test fun bothWizardsAskAndSend() {
        val dir = generateSequence(File("").absoluteFile) { it.parentFile }.map { File(it, "src/main/kotlin/tv/roxy/tv") }.first { it.isDirectory }
        for (name in listOf("ui/setup/SetupWizard.kt", "ui/setup/TvSetupWizard.kt")) {
            val src = File(dir, name).readText()
            assertTrue("$name asks the server", "setupNeedsCode(serverUrl)" in src)
            assertTrue("$name shows the field", "if (needsCode) {" in src && "testTag(\"setup-code\")" in src && "SetupCodeWhere" in src)
            assertTrue("$name sends it", "BuildConfig.VERSION_NAME, code.trim())" in src)
            assertTrue("$name holds the button back", "accountReady(name, pw, pw2, agreed, busy, needsCode, code)" in src)
        }
        val api = File(dir, "api/RoxyApi.kt").readText()
        assertTrue("the code is in what is posted", "SetupAdmin(name, password, device, deviceId, version, code)" in api)
        assertEquals(1, Regex("val code: String = \"\"\\)").findAll(api).count())
    }
}
