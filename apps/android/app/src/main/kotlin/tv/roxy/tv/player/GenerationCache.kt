package tv.roxy.tv.player

import java.util.concurrent.atomic.AtomicInteger

/**
 * A value built on one thread and invalidated from another (mpv's track list: read on the
 * main thread, changed on mpv's event thread). The value is stored with the generation it
 * was built at and served only while that generation is still current. Checking the
 * generation and then writing the value was a race: the event thread could invalidate in
 * between, and the stale list then stayed cached until the next change.
 */
internal class GenerationCache<T : Any> {
    private val gen = AtomicInteger(0)
    @Volatile private var entry: Pair<Int, T>? = null

    /** The generation to build a new value at: read it BEFORE reading what the value is built from. */
    fun generation(): Int = gen.get()

    /** The cached value, or null when there is none or it was built before the last [invalidate]. */
    fun get(): T? = entry?.takeIf { it.first == gen.get() }?.second

    /** Keep [value], built at generation [builtAt]; a value built before an invalidate is never served. */
    fun put(builtAt: Int, value: T) { entry = builtAt to value }

    /** Whatever was built so far is out of date (any thread). */
    fun invalidate() { gen.incrementAndGet(); entry = null }
}
