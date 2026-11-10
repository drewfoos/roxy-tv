package tv.roxy.tv.data.downloads

import android.content.Context
import android.os.SystemClock
import android.provider.Settings

/**
 * The device's clocks as the downloads system reads them (tests give their own).
 * [wallMs] is the date and time, which a person, the network or a dead battery can move at
 * any moment. [elapsedMs] counts the milliseconds since the device started, asleep or not,
 * and nothing can set it; it starts again at every boot, so [bootCount] says which boot a
 * reading belongs to (null when the device does not tell).
 */
class DeviceClock(val wallMs: () -> Long, val elapsedMs: () -> Long, val bootCount: () -> Int?) {
    companion object {
        fun system(context: Context): DeviceClock {
            // One boot per process: read once, when first needed.
            val boot by lazy {
                runCatching { Settings.Global.getInt(context.contentResolver, Settings.Global.BOOT_COUNT, -1) }.getOrDefault(-1).takeIf { it >= 0 }
            }
            return DeviceClock(System::currentTimeMillis, SystemClock::elapsedRealtime) { boot }
        }
    }
}

/**
 * How long ago [p] happened. Within the boot it was recorded in, that is the difference of
 * two readings of the time since boot: right whatever was done to the date meanwhile (a
 * clock set by hand, a time zone fix, a phone that starts in 1970 until it finds the
 * network). After a restart, and for an entry from before those readings were stored, only
 * the dates are left to compare, as before.
 */
fun pendingAgeMs(p: PendingProgress, wallNow: Long, elapsedNow: Long, bootCount: Int?): Long {
    val then = p.elapsedAt
    if (then != null && p.bootCount != null && p.bootCount == bootCount && elapsedNow >= then) return elapsedNow - then
    return (wallNow - p.at).coerceAtLeast(0)
}
