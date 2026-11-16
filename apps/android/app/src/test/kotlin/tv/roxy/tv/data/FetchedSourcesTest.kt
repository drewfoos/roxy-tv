package tv.roxy.tv.data

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import tv.roxy.tv.api.MediaSource
import java.io.File

class FetchedSourcesTest {
    private var clock = 1_000_000L
    private val kept = FetchedSources { clock }
    private val file = MediaSource(id = "a", container = "mkv")

    @Test fun whatWasJustReadIsHandedOverOnce() {
        kept.put(file)
        clock += 300
        assertEquals(file, kept.take("a"))
        assertNull("a second player asks the server", kept.take("a"))
    }

    @Test fun anotherFileGetsNothing() {
        kept.put(file)
        assertNull(kept.take("b"))
        assertEquals(file, kept.take("a"))
    }

    // The next episode is looked up when this one starts and opens when it ends.
    @Test fun anOldReadIsNotUsed() {
        kept.put(file)
        clock += FetchedSources.MAX_AGE_MS + 1
        assertNull(kept.take("a"))
    }

    @Test fun aClockSetBackDoesNotMakeAnOldReadNew() {
        kept.put(file)
        clock -= 60_000
        assertNull(kept.take("a"))
    }

    @Test fun oldReadsAreDroppedWhenANewOneArrives() {
        kept.put(file)
        clock += FetchedSources.MAX_AGE_MS + 1
        kept.put(MediaSource(id = "b"))
        clock -= FetchedSources.MAX_AGE_MS // "a" would look new again if it were still held
        assertNull(kept.take("a"))
    }

    // The wiring, checked in the source (the session needs a real libmpv to run).
    @Test fun thePlayRequestKeepsOnlyWhatTheServerJustGave() {
        val src = File("src/main/kotlin/tv/roxy/tv/ui/Playback.kt").readText()
        assertTrue("kept only when the item came from the server now, and not for a download",
            "if (fresh != null && local == null) app.fetchedSources.put(source.asMediaSource())" in src)
    }

    @Test fun thePlayerAsksTheServerOnlyWithoutIt() {
        val src = File("src/main/kotlin/tv/roxy/tv/player/PlayerSession.kt").readText()
        val took = src.indexOf("app.fetchedSources.take(sourceId)")
        val asked = src.indexOf("app.api.source(sourceId)")
        assertTrue("the kept facts are looked at before the server is asked", took in 0 until asked)
    }

    @Test fun anItemsFileIsHandedOverWhole() {
        val stream = tv.roxy.tv.api.MediaStream(index = 2, type = "subtitle")
        val seg = tv.roxy.tv.api.MediaSegment(type = "intro", startMs = 1000, endMs = 91000)
        val of = tv.roxy.tv.api.Item.Source(id = "a", container = "mkv", size = 5, durationMs = 9, isPrimary = true, streams = listOf(stream), segments = listOf(seg))
        assertEquals(MediaSource(id = "a", container = "mkv", size = 5, durationMs = 9, streams = listOf(stream), segments = listOf(seg)), of.asMediaSource())
    }
}
