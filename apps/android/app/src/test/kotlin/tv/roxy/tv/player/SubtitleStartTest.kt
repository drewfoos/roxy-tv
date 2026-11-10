package tv.roxy.tv.player

import org.junit.Assert.assertEquals
import org.junit.Test

/** Which subtitle track a file starts on. */
class SubtitleStartTest {
    // Found on the TV: Alya S1E9 resumed on "Signs & Songs" (the policy's pick for a dub)
    // with "Dialogue" (stream 4) saved for the episode; only a saved "off" was honoured.
    @Test fun aSavedTrackIsStartedOn() =
        assertEquals(SubtitleStart.Saved(4), subtitleStart(sessionPick = false, savedOff = false, savedIndex = 4))

    @Test fun aSavedOffStartsWithout() =
        assertEquals(SubtitleStart.Off, subtitleStart(sessionPick = false, savedOff = true, savedIndex = null))

    // A pick made earlier in this session (carried from episode to episode) wins over what
    // was saved for the next episode on another evening.
    @Test fun thisSessionsPickWins() {
        assertEquals(SubtitleStart.CarryPick, subtitleStart(sessionPick = true, savedOff = false, savedIndex = 3))
        assertEquals(SubtitleStart.CarryPick, subtitleStart(sessionPick = true, savedOff = true, savedIndex = null))
    }

    @Test fun nothingSavedLeavesItToThePolicy() {
        assertEquals(SubtitleStart.Policy, subtitleStart(sessionPick = false, savedOff = false, savedIndex = null))
        assertEquals(SubtitleStart.Policy, subtitleStart(sessionPick = false, savedOff = false, savedIndex = 0))
    }
}
