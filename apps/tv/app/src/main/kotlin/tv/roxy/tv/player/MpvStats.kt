package tv.roxy.tv.player

import android.os.Process
import android.os.SystemClock

/** Snapshot of the mpv properties the spike measures. All nullable: mpv returns null before a file is loaded. */
data class MpvStats(
    val hwdecCurrent: String?,
    val videoCodec: String?,
    val videoFormat: String?,
    val pixelFormat: String?,
    val width: Int?,
    val height: Int?,
    val containerFps: Double?,
    val estimatedVfFps: Double?,
    val displayFps: Double?,
    val frameDropCount: Int?,
    val decoderFrameDropCount: Int?,
    val voDelayedFrameCount: Int?,
    val mistimedFrameCount: Int?,
    val avSync: Double?,
    val timePos: Double?,
    val duration: Double?,
    val paused: Boolean?,
    val pausedForCache: Boolean?,
    val cacheDuration: Double?,
    val cacheSpeedBytes: Int?,
    val subCodec: String?,
    val subTitle: String?,
    val subLang: String?,
    val audioCodec: String?,
    val audioParams: String?,
    val audioChannels: String?,
    val vo: String?,
    val ao: String?,
    val videoOutParams: String?,
    val gamma: String?,
    val primaries: String?,
    val fontLog: List<String>,
) {
    fun lines(cpuPercent: Double, cores: Int): List<String> = listOf(
        "video: ${videoCodec ?: "-"} ${width ?: 0}x${height ?: 0} ${pixelFormat ?: ""}  hwdec=${hwdecCurrent ?: "none (software)"}",
        "fps: container=${fmt(containerFps)} output=${fmt(estimatedVfFps)} display=${fmt(displayFps)}  avsync=${fmt(avSync, 3)}",
        "drops: vo=${frameDropCount ?: 0} decoder=${decoderFrameDropCount ?: 0} delayed=${voDelayedFrameCount ?: 0} mistimed=${mistimedFrameCount ?: 0}",
        "pos: ${fmtTime(timePos)} / ${fmtTime(duration)}  ${if (paused == true) "PAUSED" else ""}${if (pausedForCache == true) " BUFFERING" else ""}  cache=${fmt(cacheDuration)}s @${(cacheSpeedBytes ?: 0) / 1024} KiB/s",
        "sub: ${subCodec ?: "-"} ${subTitle ?: ""} ${subLang ?: ""}   audio: ${audioCodec ?: "-"} ${audioParams ?: ""} ch=${audioChannels ?: "-"}",
        "out: vo=${vo ?: "-"} ao=${ao ?: "-"} matrix=${videoOutParams ?: "-"} gamma=${gamma ?: "-"} primaries=${primaries ?: "-"}",
        "cpu: ${"%.0f".format(cpuPercent)}% of one core ($cores cores)",
    )

    private fun fmt(d: Double?, digits: Int = 2) = if (d == null) "-" else "%.${digits}f".format(d)
    private fun fmtTime(d: Double?): String {
        if (d == null) return "-"
        val s = d.toLong()
        return "%d:%02d:%02d".format(s / 3600, (s % 3600) / 60, s % 60)
    }
}

/** Process CPU usage sampler: percent of one core used by this process since the last sample. */
class CpuSampler {
    private var lastCpuMs = Process.getElapsedCpuTime()
    private var lastWallMs = SystemClock.elapsedRealtime()

    fun sample(): Double {
        val cpu = Process.getElapsedCpuTime()
        val wall = SystemClock.elapsedRealtime()
        val dCpu = (cpu - lastCpuMs).toDouble()
        val dWall = (wall - lastWallMs).toDouble().coerceAtLeast(1.0)
        lastCpuMs = cpu
        lastWallMs = wall
        return dCpu / dWall * 100.0
    }
}
