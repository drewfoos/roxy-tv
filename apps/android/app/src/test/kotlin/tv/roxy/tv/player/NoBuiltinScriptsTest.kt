package tv.roxy.tv.player

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * mpv's built-in Lua scripts stay off, and nothing in the app needs one. Checked in the
 * source: the player itself needs a real libmpv to run.
 */
class NoBuiltinScriptsTest {
    private val main = File("src/main/kotlin/tv/roxy/tv")
    private val player = File(main, "player/MpvPlayer.kt").readText()

    @Test fun everyBuiltinScriptIsTurnedOffBeforeMpvStarts() {
        val off = player.indexOf("for (name in BUILTIN_SCRIPTS) opt(name, \"no\")")
        val folder = player.indexOf("opt(\"load-scripts\", \"no\")")
        val start = player.indexOf("mpv.init()")
        assertTrue("built-in scripts are turned off before mpv.init()", off in 0 until start)
        assertTrue("so are scripts from the config folder", folder in 0 until start)
        for (name in listOf("osc", "load-stats-overlay", "load-console", "load-auto-profiles", "load-select", "load-positioning", "load-commands", "load-context-menu"))
            assertTrue("$name is in the list", name in BUILTIN_SCRIPTS)
    }

    // What a script would be needed for: a command sent to it, or mpv's own on-screen text.
    @Test fun nothingInTheAppTalksToAScript() {
        val needs = Regex("\"(script-message(-to)?|script-binding|show-text|show-progress|keypress|keydown|load-script|osd-msg[0-9]?|osd-overlay)\"")
        val found = main.walkTopDown().filter { it.extension == "kt" }
            .flatMap { f -> needs.findAll(f.readText()).map { "${f.name}: ${it.value}" } }.toList()
        assertEquals(emptyList<String>(), found)
    }

    // Every command the app sends mpv, by name: all built into mpv itself.
    @Test fun theAppSendsOnlyMpvsOwnCommands() {
        val sent = Regex("(?:command\\(arrayOf|cmd)\\(\"([a-z-]+)\"").findAll(player).map { it.groupValues[1] }.toSet()
        assertTrue("found the commands", sent.isNotEmpty())
        assertEquals(emptySet<String>(), sent - setOf("loadfile", "seek", "sub-add", "stop", "sub-remove", "set"))
    }
}
