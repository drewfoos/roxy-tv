package tv.roxy.tv.diagnostics

import android.content.Context
import android.media.MediaCodecInfo
import android.media.MediaCodecList
import android.os.Build
import android.view.Display
import android.view.WindowManager

/**
 * One-shot description of the TV: SoC, display refresh rate, and which video decoders
 * exist in hardware (and whether they claim 10-bit profiles). This is the data the spike
 * note needs, and it is what the real app's capability profile will be built from.
 */
object DeviceReport {

    data class Decoder(
        val mime: String,
        val name: String,
        val hardware: Boolean,
        val tenBit: Boolean,
        val maxWidth: Int,
        val maxHeight: Int,
    )

    fun soc(): String = buildString {
        append(Build.MANUFACTURER).append(' ').append(Build.MODEL)
        append(" | board=").append(Build.BOARD)
        append(" hw=").append(Build.HARDWARE)
        if (Build.VERSION.SDK_INT >= 31) {
            append(" soc=").append(Build.SOC_MANUFACTURER).append(' ').append(Build.SOC_MODEL)
        }
        append(" | Android ").append(Build.VERSION.RELEASE).append(" (API ").append(Build.VERSION.SDK_INT).append(')')
        append(" | abi=").append(Build.SUPPORTED_ABIS.joinToString(","))
        append(" | cores=").append(Runtime.getRuntime().availableProcessors())
    }

    @Suppress("DEPRECATION")
    fun display(context: Context): String {
        val display: Display? = if (Build.VERSION.SDK_INT >= 30) context.display
        else (context.getSystemService(Context.WINDOW_SERVICE) as WindowManager).defaultDisplay
        if (display == null) return "display: unknown"
        val mode = display.mode
        val modes = display.supportedModes.joinToString(" ") { "${it.physicalWidth}x${it.physicalHeight}@${"%.2f".format(it.refreshRate)}" }
        val hdr = if (Build.VERSION.SDK_INT >= 24) display.hdrCapabilities?.supportedHdrTypes?.joinToString(",") { hdrName(it) } else null
        return "display: ${mode.physicalWidth}x${mode.physicalHeight}@${"%.2f".format(mode.refreshRate)} modes=[$modes] hdr=[${hdr ?: "none"}]"
    }

    fun refreshRate(context: Context): Float {
        @Suppress("DEPRECATION")
        val display: Display? = if (Build.VERSION.SDK_INT >= 30) context.display
        else (context.getSystemService(Context.WINDOW_SERVICE) as WindowManager).defaultDisplay
        return display?.mode?.refreshRate ?: 60f
    }

    private fun hdrName(type: Int): String = when (type) {
        Display.HdrCapabilities.HDR_TYPE_DOLBY_VISION -> "DV"
        Display.HdrCapabilities.HDR_TYPE_HDR10 -> "HDR10"
        Display.HdrCapabilities.HDR_TYPE_HLG -> "HLG"
        Display.HdrCapabilities.HDR_TYPE_HDR10_PLUS -> "HDR10+"
        else -> "type$type"
    }

    private val interestingMimes = listOf(
        "video/avc", "video/hevc", "video/av01", "video/x-vnd.on2.vp9", "video/dolby-vision",
    )

    fun decoders(): List<Decoder> {
        val list = MediaCodecList(MediaCodecList.ALL_CODECS).codecInfos
        val out = mutableListOf<Decoder>()
        for (info in list) {
            if (info.isEncoder) continue
            for (mime in info.supportedTypes) {
                if (mime.lowercase() !in interestingMimes) continue
                val caps = runCatching { info.getCapabilitiesForType(mime) }.getOrNull() ?: continue
                val tenBit = caps.profileLevels.any { pl -> isTenBitProfile(mime, pl.profile) }
                val video = caps.videoCapabilities
                val hw = if (Build.VERSION.SDK_INT >= 29) info.isHardwareAccelerated else !info.name.startsWith("OMX.google.") && !info.name.startsWith("c2.android.")
                out += Decoder(
                    mime = mime,
                    name = info.name,
                    hardware = hw,
                    tenBit = tenBit,
                    maxWidth = video?.supportedWidths?.upper ?: 0,
                    maxHeight = video?.supportedHeights?.upper ?: 0,
                )
            }
        }
        return out.sortedWith(compareBy({ it.mime }, { !it.hardware }, { it.name }))
    }

    private fun isTenBitProfile(mime: String, profile: Int): Boolean = when (mime.lowercase()) {
        "video/avc" -> profile == MediaCodecInfo.CodecProfileLevel.AVCProfileHigh10
        "video/hevc" -> profile == MediaCodecInfo.CodecProfileLevel.HEVCProfileMain10 ||
            profile == MediaCodecInfo.CodecProfileLevel.HEVCProfileMain10HDR10 ||
            profile == MediaCodecInfo.CodecProfileLevel.HEVCProfileMain10HDR10Plus
        "video/av01" -> profile == MediaCodecInfo.CodecProfileLevel.AV1ProfileMain10 ||
            profile == MediaCodecInfo.CodecProfileLevel.AV1ProfileMain10HDR10 ||
            profile == MediaCodecInfo.CodecProfileLevel.AV1ProfileMain10HDR10Plus
        "video/x-vnd.on2.vp9" -> profile == MediaCodecInfo.CodecProfileLevel.VP9Profile2 ||
            profile == MediaCodecInfo.CodecProfileLevel.VP9Profile2HDR
        else -> false
    }

    fun decodersSummary(): String = decoders().joinToString("\n") {
        val kind = if (it.hardware) "HW" else "SW"
        val bits = if (it.tenBit) "10-bit" else "8-bit"
        "${it.mime.removePrefix("video/")}: ${it.name} [$kind, $bits, max ${it.maxWidth}x${it.maxHeight}]"
    }

    fun full(context: Context): String = soc() + "\n" + display(context) + "\n" + decodersSummary()
}
