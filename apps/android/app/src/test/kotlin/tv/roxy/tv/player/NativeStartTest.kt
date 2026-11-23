package tv.roxy.tv.player

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import tv.roxy.tv.api.MediaStream
import java.io.File

/**
 * A file the server already describes as HDR starts on the native output. Switching to it
 * only once the file was open rebuilt the decoder: 0.5 to 0.6 s of the app's main thread on
 * a TV at every start of an HDR film, and a later first picture.
 */
class NativeStartTest {
    private fun video(range: String?, w: Int = 3840, h: Int = 1604, transfer: String? = null, dv: Int = 0) =
        MediaStream(index = 0, type = "video", codec = "hevc", width = w, height = h, rangeType = range, colorTransfer = transfer, dvProfile = dv)
    private val audio = MediaStream(index = 1, type = "audio", codec = "opus")

    @Test fun theServersWordForHdrBecomesMpvs() {
        assertEquals(KnownPicture("pq", 3840, 1604), knownPicture(listOf(audio, video("HDR10"))))
        assertEquals(KnownPicture("pq", 3840, 1604), knownPicture(listOf(video("HDR10+"), audio)))
        assertEquals(KnownPicture("hlg", 1920, 1080), knownPicture(listOf(video("HLG", 1920, 1080))))
        assertEquals(KnownPicture("pq", 3840, 1604), knownPicture(listOf(video("HDR10", transfer = "smpte2084"))))
        assertEquals(KnownPicture("hlg", 3840, 1604), knownPicture(listOf(video("HLG", transfer = "arib-std-b67"))))
    }

    // The library's 4K film: the server calls it "DV" (profile 8), and it plays as the HDR10
    // picture it carries. Left to mpv, it started on the normal output like any other file.
    @Test fun dolbyVisionWithAnHdrPictureUnderneathIsThatPicture() {
        assertEquals(KnownPicture("pq", 3840, 1604), knownPicture(listOf(video("DV", transfer = "smpte2084", dv = 8))))
        assertEquals(KnownPicture("hlg", 3840, 1604), knownPicture(listOf(video("DV", transfer = "arib-std-b67", dv = 8))))
        assertEquals(KnownPicture("pq", 3840, 1604), knownPicture(listOf(video("DV", transfer = "smpte2084", dv = 7))))
    }

    @Test fun whatIsNotKnownToBeHdrIsLeftToMpv() {
        assertNull("ordinary video", knownPicture(listOf(video("SDR"))))
        assertNull("not said", knownPicture(listOf(video(null))))
        // Dolby Vision alone (profile 5), whatever it says of its light: mpv sees what it is.
        assertNull("Dolby Vision profile 5", knownPicture(listOf(video("DV", transfer = "smpte2084", dv = 5))))
        assertNull("Dolby Vision, nothing said of the picture underneath", knownPicture(listOf(video("DV", dv = 8))))
        assertNull("ordinary video by its light", knownPicture(listOf(video("SDR", transfer = "bt709"))))
        assertNull("no size", knownPicture(listOf(video("HDR10", 0, 0))))
        assertNull("no video", knownPicture(listOf(audio)))
        assertNull(knownPicture(null))
    }

    @Test fun nativeOutputIsForHdrWhenTheSettingAndTheDisplayAllowIt() {
        assertTrue(wantsNativeHdr("pq", bench = null, hdrNative = true, hdrDisplay = true))
        assertTrue(wantsNativeHdr("hlg", null, true, true))
        assertFalse("ordinary video", wantsNativeHdr("bt.1886", null, true, true))
        assertFalse("the setting is Tone-map", wantsNativeHdr("pq", null, false, true))
        assertFalse("the display is not an HDR one", wantsNativeHdr("pq", null, true, false))
        assertFalse(wantsNativeHdr("pq", "", false, false))
    }

    @Test fun theBenchSwitchStandsInForTheSettingOrForEverything() {
        assertTrue(wantsNativeHdr("pq", "native", false, false))
        assertFalse(wantsNativeHdr("bt.1886", "native", false, false))
        assertTrue(wantsNativeHdr("bt.1886", "native-all", false, false))
    }

    // In the source: the player itself needs a real libmpv to run.
    @Test fun aLoadBegunNativeIsNotPutBackOnTheNormalOutput() {
        val mpv = File("src/main/kotlin/tv/roxy/tv/player/MpvPlayer.kt").readText()
        val load = mpv.substring(mpv.indexOf("    private fun loadPending() {")).substringBefore("\n    }")
        val decided = load.indexOf("nativeFromStart = startNative(pendingPicture)")
        assertTrue("decided before the file is asked for", decided in 0 until load.indexOf("""mpv.command(arrayOf("loadfile""""))
        assertTrue("the new file's start leaves the native output only when the load did not begin on it",
            mpv.contains("post { frameRateGate.fileStarted(); if (!nativeFromStart) leaveNativeHdr() }"))
        val start = mpv.substring(mpv.indexOf("    private fun startNative(picture: KnownPicture?): Boolean {")).substringBefore("\n    }")
        assertTrue("only with nothing open", start.contains("""if (bool("idle-active") != true) return false"""))
        assertTrue("a transcode is ordinary video", mpv.contains("pendingPicture = if (transcoded) null else picture"))
    }
}
