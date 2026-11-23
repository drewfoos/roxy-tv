package tv.roxy.tv.player

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import tv.roxy.tv.data.HDR_NATIVE
import tv.roxy.tv.data.HDR_TONEMAP
import tv.roxy.tv.data.hdrNativeFor
import java.nio.ByteBuffer
import java.nio.ByteOrder

/**
 * Native HDR shows the video on the device's own video hardware, where mpv draws nothing:
 * subtitles were simply off there. A layer over the picture draws them now, from the
 * server's copy of the shown track. These are the rules for what it asks for.
 */
class OverlayFilesTest {
    private val stream = "http://server.example:8097/v1/stream/src-1?mt=TOKEN"

    @Test fun aTrackInsideTheVideoIsAskedForByItsStreamNumber() {
        val f = overlayFiles(stream, tokenInHeader = true, external = false, externalFile = null, streamIndex = 4, codec = "subrip")!!
        assertEquals("http://server.example:8097/v1/sources/src-1/subtitles/4", f.subtitle)
        assertEquals("X-Roxy-Media-Token: TOKEN", f.header)
        assertEquals("", f.query)
        assertEquals("src-1", f.sourceId)
        assertNull("a plain track needs no fonts", f.fonts)
    }

    @Test fun aStyledTrackComesWithTheAddressOfTheFilesFonts() {
        val f = overlayFiles(stream, tokenInHeader = true, external = false, externalFile = null, streamIndex = 3, codec = "ass")!!
        assertEquals("http://server.example:8097/v1/sources/src-1/attachments/", f.fonts)
        assertEquals("http://server.example:8097/v1/sources/src-1/attachments/", overlayFiles(stream, true, false, null, 3, "SSA")!!.fonts)
    }

    @Test fun anOlderServersTokenGoesInTheAddresses() {
        val f = overlayFiles(stream, tokenInHeader = false, external = false, externalFile = null, streamIndex = 4, codec = "ass")!!
        assertEquals("http://server.example:8097/v1/sources/src-1/subtitles/4?mt=TOKEN", f.subtitle)
        assertEquals("?mt=TOKEN", f.query)
        assertNull(f.header)
    }

    @Test fun aServerUnderAPathKeepsIt() {
        val f = overlayFiles("https://home.example/roxy/v1/stream/abc?mt=T", true, false, null, 2, "ass")!!
        assertEquals("https://home.example/roxy/v1/sources/abc/subtitles/2", f.subtitle)
        assertEquals("https://home.example/roxy/v1/sources/abc/attachments/", f.fonts)
    }

    @Test fun anAddedSubtitleFileIsAskedForWhereItCameFrom() {
        val added = "http://server.example:8097/v1/sources/src-1/subtitles/1001"
        val f = overlayFiles(stream, tokenInHeader = true, external = true, externalFile = added, streamIndex = 0, codec = "subrip")!!
        assertEquals(added, f.subtitle)
        assertEquals("X-Roxy-Media-Token: TOKEN", f.header)
    }

    @Test fun pictureTracksCannotBeDrawn() {
        for (codec in listOf("hdmv_pgs_subtitle", "dvd_subtitle", "dvb_subtitle", "HDMV_PGS_SUBTITLE")) {
            assertNull(codec, overlayFiles(stream, true, false, null, 3, codec))
            assertTrue(codec, isPictureSubtitle(codec))
        }
        assertFalse(isPictureSubtitle("ass"))
        assertFalse(isPictureSubtitle("subrip"))
    }

    @Test fun nothingForAFileOnTheDeviceOrATrackThatCannotBeNamed() {
        assertNull(overlayFiles("/data/user/0/app/files/downloads/a.mkv", true, false, null, 3, "ass"))
        assertNull(overlayFiles(null, true, false, null, 3, "ass"))
        assertNull("no stream number", overlayFiles(stream, true, false, null, null, "ass"))
        assertNull("an added file with no address", overlayFiles(stream, true, true, null, 0, "ass"))
        assertNull("an added file on the device", overlayFiles(stream, true, true, "/sdcard/a.srt", 0, "subrip"))
        assertNull("not this server's stream", overlayFiles("http://elsewhere/video.mkv", true, false, null, 3, "ass"))
    }

    @Test fun onlyFontsAreFetchedFromTheAttachments() {
        assertTrue(isFontAttachment("Rubik-Bold.ttf", "application/x-truetype-font"))
        assertTrue(isFontAttachment("font", "font/otf"))
        assertTrue(isFontAttachment("NotoSans.OTF", null))
        assertTrue(isFontAttachment("a.ttc", "application/octet-stream"))
        assertTrue(isFontAttachment("x", "application/vnd.ms-opentype"))
        assertFalse(isFontAttachment("cover.jpg", "image/jpeg"))
        assertFalse(isFontAttachment("notes.txt", "text/plain"))
        assertFalse(isFontAttachment("cover", null))
    }

    // mpv has to hold something to draw subtitles at all: a second of silence, paused.
    @Test fun theSilenceIsAWavFileMpvCanOpen() {
        val wav = silenceWav()
        val b = ByteBuffer.wrap(wav).order(ByteOrder.LITTLE_ENDIAN)
        assertEquals("RIFF", String(wav, 0, 4)); assertEquals("WAVE", String(wav, 8, 4)); assertEquals("fmt ", String(wav, 12, 4)); assertEquals("data", String(wav, 36, 4))
        assertEquals(wav.size - 8, b.getInt(4))
        assertEquals(1, b.getShort(20).toInt())   // PCM
        assertEquals(1, b.getShort(22).toInt())   // mono
        assertEquals(8000, b.getInt(24))
        assertEquals(8, b.getShort(34).toInt())
        assertEquals(wav.size - 44, b.getInt(40))
        assertTrue("8-bit silence is 0x80", wav.drop(44).all { it == 0x80.toByte() })
    }

    // Native HDR used to mean no subtitles, so it was off unless turned on, and its "off" was
    // saved with every other setting. With subtitles it is the default on a TV (whose
    // graphics chip showed a 4K HDR film at about 15 frames a second), and that old "off" is
    // not a choice to keep.
    @Test fun nativeHdrIsTheDefaultOnATelevisionOnly() {
        assertTrue(hdrNativeFor(mode = null, oldSwitch = null, television = true))
        assertTrue("the old switch's off is not a choice", hdrNativeFor(null, false, true))
        assertFalse(hdrNativeFor(null, null, false))
        assertFalse(hdrNativeFor(null, false, false))
        assertTrue("turned on before on a phone: kept", hdrNativeFor(null, true, false))
    }

    @Test fun aChoiceMadeSinceIsKept() {
        assertFalse(hdrNativeFor(HDR_TONEMAP, null, true))
        assertFalse(hdrNativeFor(HDR_TONEMAP, true, true))
        assertTrue(hdrNativeFor(HDR_NATIVE, false, false))
    }
}
