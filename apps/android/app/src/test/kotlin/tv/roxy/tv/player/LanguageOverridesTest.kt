package tv.roxy.tv.player

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import tv.roxy.tv.api.MediaStream
import tv.roxy.tv.api.subtitleInfos

/**
 * The server's sniffed subtitle languages are keyed by mpv's track ids, and mpv numbers the
 * tracks of what it opened. A transcode can leave a track out, so the file's numbering put
 * the languages one track off there.
 */
class LanguageOverridesTest {
    private fun sub(index: Int, codec: String, detected: String? = null, lang: String? = null, external: Boolean = false) =
        MediaStream(index = index, type = "subtitle", codec = codec, language = lang, detectedLanguage = detected, isExternal = external)

    // A TS recording: teletext (no transcode can carry it), then two text tracks tagged wrong.
    private val ts = subtitleInfos(listOf(
        MediaStream(index = 0, type = "video", codec = "h264"),
        MediaStream(index = 1, type = "audio", codec = "ac3"),
        sub(2, "dvb_teletext"),
        sub(3, "subrip", detected = "eng", lang = "und"),
        sub(4, "subrip", detected = "fra", lang = "und"),
    ))

    @Test fun theFileItselfNumbersEveryTrack() {
        assertEquals(mapOf(2 to "eng", 3 to "fra"), languageOverrides(ts, transcoding = false, container = "mpegts"))
    }

    @Test fun aTranscodeNumbersOnlyTheTracksItKeeps() {
        // Teletext is left out: the two text tracks are the transcode's first and second.
        assertEquals(mapOf(1 to "eng", 2 to "fra"), languageOverrides(ts, transcoding = true, container = "mpegts"))
    }

    @Test fun aMatroskaSourceIsTranscodedWhole() {
        val mkv = subtitleInfos(listOf(sub(2, "dvb_teletext"), sub(3, "ass", detected = "eng"), sub(4, "hdmv_pgs_subtitle"), sub(5, "subrip", detected = "spa")))
        assertEquals(mapOf(2 to "eng", 4 to "spa"), languageOverrides(mkv, transcoding = true, container = "matroska,webm"))
        assertEquals(languageOverrides(mkv, transcoding = false, container = "matroska,webm"), languageOverrides(mkv, transcoding = true, container = "matroska,webm"))
    }

    @Test fun mp4TextBecomesSrtAndPicturesAreCarried() {
        val mp4 = subtitleInfos(listOf(sub(2, "dvd_subtitle"), sub(3, "mov_text", detected = "eng"), sub(4, "eia_608"), sub(5, "mov_text", detected = "deu")))
        assertEquals(mapOf(2 to "eng", 4 to "deu"), languageOverrides(mp4, transcoding = false, container = "mov,mp4,m4a,3gp,3g2,mj2"))
        assertEquals(mapOf(2 to "eng", 3 to "deu"), languageOverrides(mp4, transcoding = true, container = "mov,mp4,m4a,3gp,3g2,mj2"))
    }

    @Test fun filesBesideTheVideoAreNotInTheStream() {
        // A sidecar listed between two embedded tracks must not shift the second one.
        val subs = subtitleInfos(listOf(sub(2, "ass", detected = "eng"), sub(1000, "subrip", detected = "eng", external = true), sub(3, "ass", detected = "jpn")))
        assertEquals(mapOf(1 to "eng", 2 to "jpn"), languageOverrides(subs, transcoding = false, container = "matroska,webm"))
    }

    @Test fun tracksWithNothingSniffedAreLeftAlone() {
        val subs = subtitleInfos(listOf(sub(2, "ass", lang = "eng"), sub(3, "ass", detected = "eng", lang = "jpn")))
        assertEquals(mapOf(2 to "eng"), languageOverrides(subs, transcoding = false, container = "mkv"))
        assertEquals(emptyMap<Int, String>(), languageOverrides(emptyList(), transcoding = true, container = null))
    }

    @Test fun theKeepRuleIsTheServers() {
        assertTrue(transcodeKeepsSubtitle("dvb_teletext", "matroska,webm"))
        assertTrue(transcodeKeepsSubtitle("HDMV_PGS_SUBTITLE", "mpegts"))
        assertTrue(transcodeKeepsSubtitle("mov_text", "mov,mp4,m4a,3gp,3g2,mj2"))
        assertFalse(transcodeKeepsSubtitle("dvb_teletext", "mpegts"))
        assertFalse(transcodeKeepsSubtitle("eia_608", null))
    }
}
