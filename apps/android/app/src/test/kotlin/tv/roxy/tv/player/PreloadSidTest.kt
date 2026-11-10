package tv.roxy.tv.player

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import tv.roxy.tv.api.SubtitleInfo

/**
 * The subtitle track a file is opened on. Found on the TV: switching to the saved track
 * ("Dialogue") once Alya S1E9 was open sent mpv back ~11 s for its subtitles, and the
 * resumed episode decoded and dropped that video first: 2.5 s more before the picture.
 */
class PreloadSidTest {
    private val alya = listOf(
        SubtitleInfo(mpvId = 1, index = 3, codec = "ass", language = "eng", detectedLanguage = null, title = "Signs & Songs", isExternal = false),
        SubtitleInfo(mpvId = 2, index = 4, codec = "ass", language = "eng", detectedLanguage = "eng", title = "Dialogue", isExternal = false),
        SubtitleInfo(mpvId = 3, index = 1000, codec = "subrip", language = "eng", detectedLanguage = "eng", title = "", isExternal = true),
    )

    @Test fun theSavedTrackIsOpenedOn() = assertEquals(2, preloadSid(SubtitleStart.Saved(4), alya))

    @Test fun savedOffOpensWithout() = assertEquals(0, preloadSid(SubtitleStart.Off, alya))

    // An added file's mpv number is only known once it is loaded.
    @Test fun anAddedFileIsLeftToAfterTheLoad() = assertNull(preloadSid(SubtitleStart.Saved(1000), alya))

    @Test fun aStreamTheFileNoLongerHasIsLeftToThePolicy() = assertNull(preloadSid(SubtitleStart.Saved(7), alya))

    @Test fun thePolicyAndThisSessionsPickDecideAfterTheLoad() {
        assertNull(preloadSid(SubtitleStart.Policy, alya))
        assertNull(preloadSid(SubtitleStart.CarryPick, alya))
    }
}
