package tv.roxy.tv.player

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicInteger

/** mpv's track list cache: read on the main thread, invalidated on mpv's event thread. */
class GenerationCacheTest {
    @Test fun aValueBuiltBeforeAChangeIsNotServed() {
        val c = GenerationCache<String>()
        val g = c.generation()
        c.invalidate() // track-list changed while the list was being read
        c.put(g, "old list")
        assertNull(c.get())
    }

    @Test fun aCurrentValueIsServedUntilTheNextChange() {
        val c = GenerationCache<String>()
        c.put(c.generation(), "list")
        assertEquals("list", c.get())
        c.invalidate()
        assertNull(c.get())
    }

    // The audit's race: the counter was checked, the event thread changed it (and cleared
    // the cache), then the stale list was written and served until the next change. Two
    // threads doing exactly that; a served value must always be the current generation's.
    @Test fun aChangeBetweenTheCheckAndTheWriteNeverLeavesAStaleList() {
        val c = GenerationCache<Int>()
        val stop = AtomicBoolean(false)
        val stale = AtomicInteger(0)
        val events = Thread { while (!stop.get()) { c.invalidate(); Thread.yield() } }
        events.start()
        val until = System.nanoTime() + 1_500_000_000L
        while (System.nanoTime() < until) {
            val g = c.generation()
            c.put(g, g) // the "list" is the generation it was read at
            val before = c.generation()
            val v = c.get()
            val after = c.generation()
            if (v != null && before == after && v != before) stale.incrementAndGet()
        }
        stop.set(true); events.join()
        assertTrue("stale values served: ${stale.get()}", stale.get() == 0)
    }
}
