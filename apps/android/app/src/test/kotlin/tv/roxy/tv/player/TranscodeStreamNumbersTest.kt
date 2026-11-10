package tv.roxy.tv.player

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/**
 * Saved track numbers are the file's ffmpeg stream numbers. A server transcode maps the
 * video, then the audio, subtitles and attachments (v:0, a, s, t), so its own numbers
 * match the file's only for a file already in that order: a file with its subtitles before
 * its audio (index 1 = subtitle, 2 = audio) comes out as 1 = audio, 2 = subtitle, and a
 * saved "stream 2" would name the other track. On a transcode the numbers are neither
 * applied nor reported.
 */
class TranscodeStreamNumbersTest {
    @Test fun aSavedSubtitleNumberIsNotUsedOnATranscode() {
        assertEquals(SubtitleStart.Policy, subtitleStart(sessionPick = false, savedOff = false, savedIndex = 4, transcoding = true))
        assertEquals(SubtitleStart.Saved(4), subtitleStart(sessionPick = false, savedOff = false, savedIndex = 4, transcoding = false))
    }

    @Test fun aSavedOffStillAppliesOnATranscode() =
        assertEquals(SubtitleStart.Off, subtitleStart(sessionPick = false, savedOff = true, savedIndex = null, transcoding = true))

    @Test fun aSavedAudioNumberIsNotUsedOnATranscode() {
        assertNull(savedAudioStart(sessionPick = false, savedIndex = 2, transcoding = true))
        assertEquals(2, savedAudioStart(sessionPick = false, savedIndex = 2, transcoding = false))
        assertNull("a pick in this session wins", savedAudioStart(sessionPick = true, savedIndex = 2, transcoding = false))
    }

    @Test fun aTranscodesNumbersAreNotReported() {
        assertEquals(null to null, reportedStreams(transcoding = true, audioStream = 1, subOn = true, subStream = 2, subOffChosen = false))
        assertEquals("subtitles turned off are still saved as off", null to -1,
            reportedStreams(transcoding = true, audioStream = 1, subOn = false, subStream = null, subOffChosen = true))
    }

    @Test fun theFilesOwnNumbersAreReported() {
        assertEquals(1 to 2, reportedStreams(transcoding = false, audioStream = 1, subOn = true, subStream = 2, subOffChosen = false))
        assertEquals(1 to -1, reportedStreams(transcoding = false, audioStream = 1, subOn = false, subStream = null, subOffChosen = true))
        assertEquals("the policy chose none: the saved one is left alone", 1 to null,
            reportedStreams(transcoding = false, audioStream = 1, subOn = false, subStream = null, subOffChosen = false))
    }
}
