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

    // A viewer's pick earlier in this session, matched on the server's list as it is after
    // the load: opened on it, the next episode needs no switch (and no refresh seek).
    private val dialoguePick = TrackPick(off = false, lang = "eng", kind = "full", title = "Dialogue")

    @Test fun thisSessionsPickIsOpenedOnWhenTheListHasIt() = assertEquals(2, preloadSid(SubtitleStart.CarryPick, alya, dialoguePick))

    @Test fun thisSessionsOffIsOpenedWithout() =
        assertEquals(0, preloadSid(SubtitleStart.CarryPick, alya, TrackPick(off = true, lang = "", kind = "", title = "")))

    @Test fun aPickOfAKindTheFileLacksIsLeftToAfterTheLoad() =
        assertNull(preloadSid(SubtitleStart.CarryPick, alya, TrackPick(off = false, lang = "jpn", kind = "", title = "")))

    @Test fun aForcedFlagMakesASignsTrack() {
        val forced = listOf(SubtitleInfo(mpvId = 1, index = 2, codec = "ass", language = "eng", detectedLanguage = null, title = "English", isExternal = false, isForced = true),
            SubtitleInfo(mpvId = 2, index = 3, codec = "ass", language = "eng", detectedLanguage = null, title = "English", isExternal = false))
        assertEquals(1, preloadSid(SubtitleStart.CarryPick, forced, TrackPick(off = false, lang = "eng", kind = "signs", title = "English")))
    }
}
