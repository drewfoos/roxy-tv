package tv.roxy.tv.player

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import tv.roxy.tv.api.MediaStream

/**
 * The audio track a file is opened on. Set after the load, the saved audio re-ran the
 * subtitle policy (the subtitle moved off the track the file was opened on, then back:
 * two of mpv's refresh seeks), and mpv kept the number as an option for the next file.
 */
class PreloadAidTest {
    // A dual-audio release: Japanese first, the English dub second (server stream numbers).
    private val streams = listOf(
        MediaStream(index = 0, type = "video"),
        MediaStream(index = 1, type = "audio", language = "jpn", title = "Japanese"),
        MediaStream(index = 2, type = "audio", language = "eng", title = "English 5.1"),
        MediaStream(index = 3, type = "subtitle", language = "eng", title = "Signs & Songs"),
        MediaStream(index = 4, type = "audio", language = "eng", title = "Commentary"),
    )

    @Test fun theSavedStreamIsOpenedOnByItsPlaceAmongTheAudio() {
        assertEquals(1, preloadAid(1, null, streams))
        assertEquals(2, preloadAid(2, null, streams))
        assertEquals("after a subtitle stream: still the third audio", 3, preloadAid(4, null, streams))
    }

    @Test fun nothingSavedIsMpvsOwnChoice() = assertNull(preloadAid(null, null, streams))

    @Test fun aStreamTheFileNoLongerHasIsMpvsOwnChoice() = assertNull(preloadAid(9, null, streams))

    @Test fun thisSessionsPickBeatsTheSavedStream() {
        val pick = TrackPick(off = false, lang = "eng", kind = "", title = "Commentary")
        assertEquals(3, preloadAid(1, pick, streams))
        val untitled = TrackPick(off = false, lang = "eng", kind = "", title = "English")
        assertEquals("same language, first of them", 2, preloadAid(1, untitled, streams))
    }

    @Test fun aPickInALanguageTheFileLacksIsMpvsOwnChoice() =
        assertNull(preloadAid(1, TrackPick(off = false, lang = "ger", kind = "", title = ""), streams))
}
