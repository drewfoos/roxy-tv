package tv.roxy.tv.player

import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * mpv keeps `aid`/`sid` as options for later files, by number (checked on mpv 0.38: set
 * while file 1 played, file 2 opened on them; `sid=no` carried too). Every load sets both.
 */
class LoadTrackOptionsTest {
    @Test fun nothingChosenIsAutoForBoth() =
        assertEquals(listOf("aid" to "auto", "sid" to "auto"), MpvTracks.loadTrackOptions(sid = null, aid = null))

    @Test fun chosenTracksAreSet() =
        assertEquals(listOf("aid" to "2", "sid" to "3"), MpvTracks.loadTrackOptions(sid = 3, aid = 2))

    @Test fun subtitlesOffIsNo() =
        assertEquals(listOf("aid" to "auto", "sid" to "no"), MpvTracks.loadTrackOptions(sid = 0, aid = null))
}
