package tv.roxy.tv.player

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * The player's side of the subtitle files beside a video needs a real libmpv to run, so
 * it is checked in the source (as MpvWiringTest does); what it decides is in SidecarsTest.
 */
class SidecarWiringTest {
    private val mpv = File("src/main/kotlin/tv/roxy/tv/player/MpvPlayer.kt").readText()
    private val session = File("src/main/kotlin/tv/roxy/tv/player/PlayerSession.kt").readText()

    /** The body that follows [name] in [src], by brace counting. */
    private fun body(src: String, name: String): String {
        val at = src.indexOf(name)
        assertTrue("$name found", at >= 0)
        val open = src.indexOf('{', at)
        var depth = 0
        for (i in open until src.length) {
            when (src[i]) { '{' -> depth++; '}' -> if (--depth == 0) return src.substring(open, i + 1) }
        }
        error("unbalanced $name")
    }

    // mpv drops added tracks with the file: a reconnect, another quality, the next episode
    // all come through here.
    @Test fun everyLoadedFileIsGivenItsSubtitleFiles() =
        assertTrue("the session's onFileLoaded adds them", "addSidecars()" in body(session, "override fun onFileLoaded()"))

    @Test fun theyAreAddedWithoutBeingShownAndNeverOnTheMainThread() {
        val adder = body(mpv, "fun sidecarAdder(")
        assertTrue("sub-add with the flags that do not select", """arrayOf("sub-add", sidecarSource(f.source, streamUrl, inHeader), sidecarFlags(f.forced), f.title, f.lang)""" in adder)
        assertTrue("a file is not added twice to one load", "mine.containsValue(f.streamIndex)" in adder)
        assertTrue("nor to a file loaded since", "addedSubs !== mine" in adder)
        val add = body(session, "private fun addSidecars()")
        assertTrue("sub-add waits for the file: off the main thread", "withContext(Dispatchers.IO) { add() }" in add)
    }

    @Test fun aNewLoadForgetsTheLastFilesAddedTracks() =
        assertTrue("addedSubs = java.util.concurrent.ConcurrentHashMap()" in body(mpv, "private fun loadPending()"))

    // The media token must not reach the log: the URL given to mpv is never printed.
    @Test fun theSubtitleUrlIsNotLogged() {
        val adder = body(mpv, "fun sidecarAdder(")
        val logged = adder.lines().filter { "Timber." in it }
        assertTrue("it logs", logged.isNotEmpty())
        assertFalse("no source in a log line: $logged", logged.any { "source" in it || "streamUrl" in it })
    }

    // destroy() runs on another thread: mpv must not go away under a sub-add.
    @Test fun mpvIsNotDestroyedUnderAnAdd() {
        val destroy = body(mpv, "fun destroy()")
        assertTrue("synchronized(subAddLock) { runCatching { m.destroy() }" in destroy)
        assertTrue("synchronized(subAddLock)" in body(mpv, "fun sidecarAdder("))
    }

    // A switch of the audio re-runs the subtitle policy: with the same rule for added files.
    @Test fun theAudioSwitchUsesTheSameRuleForAddedFiles() {
        assertTrue("policySubtitle(all, lang)" in body(mpv, "fun setAudioTrack("))
        assertTrue("policyTakesSidecar(" in body(mpv, "private fun policySubtitle("))
    }
}
