package tv.roxy.tv.data.downloads

import org.junit.Assert.assertEquals
import org.junit.Test
import tv.roxy.tv.api.Item
import tv.roxy.tv.api.MediaStream

class SidecarRulesTest {
    // The names are what the player counts on (`video.<lang>.<index>.<ext>`): a change here
    // would leave the files of every download made so far unfound.
    @Test fun theFileNamesStayAsTheyAre() {
        val item = Item(id = "e1", type = "episode", mediaSources = listOf(Item.Source(id = "s1", isPrimary = true, streams = listOf(
            MediaStream(index = 0, type = "video", codec = "hevc"),
            MediaStream(index = 2, type = "subtitle", codec = "ass", language = "eng"), // inside the file: not a sidecar
            MediaStream(index = 1000, type = "subtitle", codec = "subrip", language = "eng", isExternal = true),
            MediaStream(index = 1001, type = "subtitle", codec = "SSA", language = "eng", detectedLanguage = "jpn", isExternal = true),
            MediaStream(index = 1002, type = "subtitle", codec = "webvtt", language = "pt-BR", isExternal = true),
            MediaStream(index = 1003, type = "subtitle", codec = "", isExternal = true),
            MediaStream(index = 1004, type = "subtitle", codec = "mov_text", language = "", isExternal = true),
        ))))
        assertEquals(
            listOf(
                Sidecar(1000, "video.eng.1000.srt"), Sidecar(1001, "video.jpn.1001.ass"), Sidecar(1002, "video.ptbr.1002.vtt"),
                Sidecar(1003, "video.und.1003.srt"), Sidecar(1004, "video.und.1004.movtext"),
            ),
            sidecarsOf(item),
        )
        assertEquals("nothing to play, nothing to fetch", emptyList<Sidecar>(), sidecarsOf(Item(id = "x", type = "movie")))
    }

    @Test fun whatAnAnswerMeansForASidecar() {
        assertEquals(SidecarFetch.Saved, sidecarAnswer(200, 120))
        assertEquals("an empty file is no subtitle", SidecarFetch.Gone, sidecarAnswer(200, 0))
        assertEquals(SidecarFetch.Gone, sidecarAnswer(404, 0))
        assertEquals(SidecarFetch.Gone, sidecarAnswer(410, 0))
        // About the moment, not the file: asked again later.
        for (code in listOf(401, 408, 429, 500, 502, 503)) assertEquals("HTTP $code", SidecarFetch.Refused, sidecarAnswer(code, 0))
    }
}
