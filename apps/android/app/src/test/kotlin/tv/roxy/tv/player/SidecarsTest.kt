package tv.roxy.tv.player

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import tv.roxy.tv.api.SubtitleInfo

/**
 * Subtitle files beside the video (the server's external streams, numbered from 1000) are
 * not in the stream mpv opens. Nothing gave them to mpv: they were not in the menu, a
 * choice of one saved from the web was never shown, and the policy's track was then saved
 * over it. A download's copies (`video.<language>.<number>.<ext>`) have a name mpv does
 * not match to the video either. The player adds them after every load; these are the
 * decisions around that (mpv cannot run here, see MpvInitOrderTest).
 */
class SidecarsTest {
    private fun sub(mpvId: Int, index: Int, codec: String, lang: String?, title: String = "", external: Boolean = false, detected: String? = null, forced: Boolean = false) =
        SubtitleInfo(mpvId = mpvId, index = index, codec = codec, language = lang, detectedLanguage = detected, title = title, isExternal = external, isForced = forced)

    // Korra: picture subtitles in the file, the text ones downloaded beside it.
    private val korra = listOf(
        sub(1, 2, "hdmv_pgs_subtitle", "eng"),
        sub(2, 1000, "subrip", "eng", external = true),
        sub(3, 1001, "subrip", null, title = "SDH", external = true, detected = "ENG"),
        sub(4, 1002, "hdmv_pgs_subtitle", "eng", external = true),
    )
    private val korraFiles = streamSidecars("http://box:8097", "src1", korra)

    // ---- which files, and from where ----

    @Test fun theTextFilesBesideAStreamAreAddedFromTheServer() {
        assertEquals(
            listOf(
                Sidecar(1000, "http://box:8097/v1/sources/src1/subtitles/1000", "External", "eng", false),
                Sidecar(1001, "http://box:8097/v1/sources/src1/subtitles/1001", "SDH", "eng", false),
            ),
            korraFiles,
        )
    }

    @Test fun aFileWithNoSubtitleFilesAddsNone() =
        assertEquals(emptyList<Sidecar>(), streamSidecars("http://box:8097", "src1", korra.take(1)))

    @Test fun aDownloadsSubtitleFilesAreFoundByTheirNumber() {
        assertEquals(1000, localSidecarIndex("video.mkv", "video.eng.1000.srt"))
        assertEquals(4, localSidecarIndex("video.mkv", "video.und.4.ass"))
        assertEquals(1000, localSidecarIndex("my.film.mp4", "my.film.eng.1000.vtt"))
        assertNull("the video", localSidecarIndex("video.mkv", "video.mkv"))
        assertNull("a file still being written", localSidecarIndex("video.mkv", "video.eng.1000.srt.part"))
        assertNull("no number", localSidecarIndex("video.mkv", "video.eng.srt"))
        assertNull("another video's", localSidecarIndex("video.mkv", "other.eng.1000.srt"))
        assertNull(localSidecarIndex("video.mkv", "video.mkv.part"))
    }

    @Test fun aDownloadsSubtitleFilesAreDescribedByItsOwnStreamList() {
        val names = listOf("video.mkv", "video.eng.1001.srt", "video.eng.1000.srt", "video.fre.1005.srt", "video.und.1006.srt", "poster.jpg")
        assertEquals(
            listOf(
                Sidecar(1000, "/data/dl/abc/video.eng.1000.srt", "External", "eng", false),
                Sidecar(1001, "/data/dl/abc/video.eng.1001.srt", "SDH", "eng", false),
                // Not in the list the download was saved with: the name's language.
                Sidecar(1005, "/data/dl/abc/video.fre.1005.srt", "External", "fre", false),
                Sidecar(1006, "/data/dl/abc/video.und.1006.srt", "External", "", false),
            ),
            localSidecars("/data/dl/abc/video.mkv", names, korra),
        )
    }

    // ---- the media token never goes into a URL that did not need it ----

    @Test fun theSubtitleUrlCarriesNoTokenWhileTheHeaderDoes() {
        val url = "http://box:8097/v1/sources/src1/subtitles/1000"
        assertEquals(url, sidecarSource(url, "http://box:8097/v1/stream/src1?mt=SECRET", tokenInHeader = true))
    }

    @Test fun anOlderServerGetsTheStreamsTokenInTheUrl() {
        val url = "http://box:8097/v1/sources/src1/subtitles/1000"
        assertEquals("$url?mt=SECRET", sidecarSource(url, "http://box:8097/v1/stream/src1?mt=SECRET", tokenInHeader = false))
        assertEquals("$url?mt=SECRET", sidecarSource(url, "http://box:8097/v1/transcode/src1?h=720&start=30.0&mt=SECRET", tokenInHeader = false))
        assertEquals("no token to give", url, sidecarSource(url, "http://box:8097/v1/stream/src1", tokenInHeader = false))
    }

    @Test fun aFileOnTheDeviceIsReadAsItIs() {
        val path = "/data/dl/abc/video.eng.1000.srt"
        assertEquals(path, sidecarSource(path, "/data/dl/abc/video.mkv", tokenInHeader = false))
        // A download that switched to a lower quality streams a transcode: the files are still local.
        assertEquals(path, sidecarSource(path, "http://box:8097/v1/transcode/src1?h=480&mt=SECRET", tokenInHeader = false))
    }

    // ---- adding never shows a track by itself ----

    @Test fun addingAFileDoesNotSelectIt() {
        assertEquals("auto", sidecarFlags(forced = false))
        assertEquals("auto+forced", sidecarFlags(forced = true))
    }

    // ---- what is shown once they are added ----

    private fun plan(pick: TrackPick? = null, savedOff: Boolean = false, saved: Int? = null, subs: List<SubtitleInfo> = korra) =
        sidecarPlan(subs, streamSidecars("http://box:8097", "src1", subs), pick, savedOff, saved)

    @Test fun aSavedSubtitleFileIsShown() = assertEquals(SidecarPlan.Select(1001), plan(saved = 1001))

    @Test fun aSavedTrackOfTheFileStays() = assertEquals(SidecarPlan.Leave, plan(saved = 2))

    @Test fun aSavedOffStays() = assertEquals(SidecarPlan.Leave, plan(savedOff = true))

    @Test fun nothingSavedLeavesItToThePolicy() {
        assertEquals(SidecarPlan.Policy, plan())
        assertEquals("a number the item no longer has", SidecarPlan.Policy, plan(saved = 7))
        assertEquals("a picture subtitle file is not served", SidecarPlan.Policy, plan(saved = 1002))
    }

    @Test fun aFileWithNoSubtitleFilesHasNothingToDecide() =
        assertEquals(SidecarPlan.Leave, plan(saved = 1000, subs = korra.take(1)))

    @Test fun aPickOfASubtitleFileGoesToASubtitleFileAgain() {
        // Reopened (a reconnect, another quality): the same one, also among two alike.
        assertEquals(SidecarPlan.Select(1001), plan(pick = TrackPick(off = false, lang = "eng", kind = "", title = "SDH", sidecar = 1001)))
        val twins = listOf(sub(1, 1000, "subrip", "eng", external = true), sub(2, 1001, "ass", "eng", external = true))
        assertEquals(SidecarPlan.Select(1001), plan(pick = TrackPick(off = false, lang = "eng", kind = "", title = "External", sidecar = 1001), subs = twins))
        // The next episode numbers its own: by title, then the first of that language.
        assertEquals(SidecarPlan.Select(1001), plan(pick = TrackPick(off = false, lang = "eng", kind = "", title = "SDH", sidecar = 1004)))
        assertEquals(SidecarPlan.Select(1000), plan(pick = TrackPick(off = false, lang = "eng", kind = "", title = "Other", sidecar = 1004)))
        // It wins over a track of the file in that language: the viewer chose the file.
        assertEquals(SidecarPlan.Select(1000), plan(pick = TrackPick(off = false, lang = "eng", kind = "", title = "External", sidecar = 1000)))
    }

    @Test fun aPickOfATrackOfTheFileStaysInTheFile() =
        assertEquals(SidecarPlan.Leave, plan(pick = TrackPick(off = false, lang = "eng", kind = "", title = "")))

    @Test fun aPickTheFileLacksGoesToASubtitleFile() {
        val noOwn = listOf(sub(1, 2, "ass", "jpn"), sub(2, 1000, "subrip", "eng", external = true))
        assertEquals(SidecarPlan.Select(1000), plan(pick = TrackPick(off = false, lang = "eng", kind = "", title = "Dialogue"), subs = noOwn))
        assertEquals("no file in that language either", SidecarPlan.Leave, plan(pick = TrackPick(off = false, lang = "ger", kind = "", title = ""), subs = noOwn))
    }

    @Test fun aPickedOffStaysAndWinsOverTheSavedFile() =
        assertEquals(SidecarPlan.Leave, plan(pick = TrackPick(off = true, lang = "", kind = "", title = ""), saved = 1000))

    @Test fun aPickWinsOverTheSavedFile() =
        assertEquals(SidecarPlan.Leave, plan(pick = TrackPick(off = false, lang = "eng", kind = "", title = ""), saved = 1000))

    // ---- opened on no subtitles while the wanted file is still to be added ----

    @Test fun aFileWaitingForItsSubtitleFileOpensOnNone() {
        assertEquals(0, sidForLoad(SidecarPlan.Select(1000), null))
        assertEquals(0, sidForLoad(SidecarPlan.Select(1000), 2))
    }

    @Test fun otherwiseTheFileOpensAsBefore() {
        assertEquals(2, sidForLoad(SidecarPlan.Leave, 2))
        assertNull(sidForLoad(SidecarPlan.Policy, null))
        assertEquals(0, sidForLoad(SidecarPlan.Leave, 0))
    }

    // ---- the policy and the styled subtitles inside a release ----

    @Test fun thePolicyTakesASubtitleFileOnlyWhenTheFileHasNothingForTheViewer() {
        assertTrue("no track of its own was chosen", policyTakesSidecar(null, "eng"))
        assertTrue("only another language inside", policyTakesSidecar("spa", "eng"))
    }

    @Test fun theSubtitlesInsideAReleaseAreNotReplacedByAFileBesideIt() {
        assertFalse(policyTakesSidecar("eng", "eng"))
        assertFalse("en and eng are one language", policyTakesSidecar("en", "eng"))
        assertFalse("untagged: it may well be the viewer's", policyTakesSidecar("", "eng"))
        assertFalse(policyTakesSidecar("und", "eng"))
        assertFalse("no preferred language: nothing to prefer the file for", policyTakesSidecar("spa", ""))
    }

    // ---- what a progress report saves ----

    @Test fun aSubtitleFileIsReportedByTheServersNumber() {
        // mpv's own number for it (subStream) is its place in its own file: 0.
        assertEquals(1 to 1000, reportedStreams(transcoding = false, audioStream = 1, subOn = true, subStream = 0, subOffChosen = false, subSidecar = 1000))
        assertEquals("from a transcode too", null to 1000,
            reportedStreams(transcoding = true, audioStream = 1, subOn = true, subStream = 0, subOffChosen = false, subSidecar = 1000))
    }

    @Test fun thePolicysTrackIsNotSavedOverASavedOneThatWasNotFound() {
        // Saved from the web: the file 1000. Not there (yet): the policy's track 2 shows.
        assertEquals(1 to null, reportedStreams(transcoding = false, audioStream = 1, subOn = true, subStream = 2, subOffChosen = false, savedSub = 1000))
        assertEquals("the saved one, once it shows", 1 to 1000,
            reportedStreams(transcoding = false, audioStream = 1, subOn = true, subStream = 0, subOffChosen = false, subSidecar = 1000, savedSub = 1000))
        assertEquals("a saved track of the file", 1 to 4, reportedStreams(transcoding = false, audioStream = 1, subOn = true, subStream = 4, subOffChosen = false, savedSub = 4))
    }

    @Test fun withNothingSavedTheTrackShowingIsReportedAsBefore() {
        assertEquals(1 to 2, reportedStreams(transcoding = false, audioStream = 1, subOn = true, subStream = 2, subOffChosen = false))
        assertEquals(1 to -1, reportedStreams(transcoding = false, audioStream = 1, subOn = false, subStream = null, subOffChosen = true, savedSub = 1000))
        assertEquals(1 to null, reportedStreams(transcoding = false, audioStream = 1, subOn = false, subStream = null, subOffChosen = false, savedSub = 1000))
    }
}
