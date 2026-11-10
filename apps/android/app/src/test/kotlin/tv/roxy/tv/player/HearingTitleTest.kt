package tv.roxy.tv.player

import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Test
import tv.roxy.tv.api.MediaStream
import tv.roxy.tv.api.subtitleInfos

/**
 * A subtitle file named `Show.en.hi.srt` (or `.sdh`, `.cc`) is for the deaf and hard of
 * hearing. The server says so (`isHearingImpaired`), the app did not read it: beside
 * `Show.en.srt` the menu listed two tracks that read the same, "English · External".
 */
class HearingTitleTest {
    @Test fun aTrackForTheHardOfHearingSaysSo() {
        assertEquals("SDH", MpvTracks.hearingTitle("", hearingImpaired = true))
        assertEquals("Full · SDH", MpvTracks.hearingTitle("Full", hearingImpaired = true))
    }

    @Test fun aTitleThatAlreadySaysSoIsLeftAlone() {
        for (t in listOf("SDH", "English [SDH]", "English (CC)", "Hearing Impaired", "hearing-impaired", "Closed Captions"))
            assertEquals(t, MpvTracks.hearingTitle(t, hearingImpaired = true))
    }

    @Test fun anyOtherTrackKeepsItsTitle() {
        assertEquals("", MpvTracks.hearingTitle("", hearingImpaired = false))
        assertEquals("Signs", MpvTracks.hearingTitle("Signs", hearingImpaired = false))
    }

    /** From the server's list of streams to what mpv is given as the added file's title. */
    @Test fun theFileBesideTheVideoIsListedAsSdh() {
        val json = Json { ignoreUnknownKeys = true }
        val streams = json.decodeFromString<List<MediaStream>>(
            """[
              {"index":1000,"type":"subtitle","codec":"subrip","language":"eng","isExternal":true},
              {"index":1001,"type":"subtitle","codec":"subrip","language":"eng","isExternal":true,"isHearingImpaired":true},
              {"index":1002,"type":"subtitle","codec":"subrip","language":"eng","title":"Group","isExternal":true,"isHearingImpaired":true}
            ]""",
        )
        val files = streamSidecars("http://box:8097", "src1", subtitleInfos(streams))
        assertEquals(listOf("External", "SDH", "Group · SDH"), files.map { it.title })
    }
}
