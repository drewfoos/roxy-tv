package tv.roxy.tv.player

import tv.roxy.tv.api.MediaStream

/** What the server says of a file's picture before it is opened: how it is encoded for light, and its size. */
internal data class KnownPicture(val gamma: String, val width: Int, val height: Int)

/**
 * The picture of the file about to be opened, when the server's facts say it is an HDR10
 * (also HDR10+) or HLG one; null otherwise. Told by how the picture is encoded for light,
 * which for Dolby Vision is the picture underneath: profiles 7 and 8 carry an HDR10 or HLG
 * one and play as that, profile 5 has none and is left to mpv. The name of the range alone
 * says so too, for a server that sends nothing more.
 */
internal fun knownPicture(streams: List<MediaStream>?): KnownPicture? {
    val video = streams?.firstOrNull { it.type == "video" } ?: return null
    if (video.dvProfile == 5) return null
    val gamma = when {
        video.colorTransfer == "smpte2084" -> "pq"
        video.colorTransfer == "arib-std-b67" -> "hlg"
        video.rangeType == "HDR10" || video.rangeType == "HDR10+" -> "pq"
        video.rangeType == "HLG" -> "hlg"
        else -> return null
    }
    if (video.width <= 0 || video.height <= 0) return null
    return KnownPicture(gamma, video.width, video.height)
}

/**
 * Whether a picture of [gamma] (mpv's name: pq, hlg, bt.1886...) goes to the device's own
 * video hardware. [bench]: the `debug.roxy.hdr` switch (`native`: as if the setting were
 * on and the display took HDR; `native-all`: every file). [hdrNative]: the setting.
 * [hdrDisplay]: the display takes HDR10/HLG.
 */
internal fun wantsNativeHdr(gamma: String, bench: String?, hdrNative: Boolean, hdrDisplay: Boolean): Boolean {
    if (bench == "native-all") return true
    return (bench == "native" || (hdrNative && hdrDisplay)) && (gamma == "pq" || gamma == "hlg")
}
