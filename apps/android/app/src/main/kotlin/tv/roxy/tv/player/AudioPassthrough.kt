package tv.roxy.tv.player

import android.content.Context
import android.media.AudioAttributes
import android.media.AudioDeviceInfo
import android.media.AudioFormat
import android.media.AudioManager
import android.media.AudioTrack
import android.os.Build
import timber.log.Timber

/**
 * Which compressed audio formats the HDMI output (a soundbar or receiver on ARC/eARC, or
 * the TV behind a streaming box) takes as-is. Those are sent through untouched (mpv
 * `audio-spdif`) instead of being decoded here, so the soundbar does the surround. With
 * no HDMI audio output (a phone, a TV on its own speakers) the list is empty and every
 * format is decoded on the device as before.
 */
object AudioPassthrough {
    /** [mpv] is the name `audio-spdif` takes. */
    data class Format(val mpv: String, val label: String, val encoding: Int, val channelMask: Int)

    private val candidates = listOf(
        Format("ac3", "Dolby Digital", AudioFormat.ENCODING_AC3, AudioFormat.CHANNEL_OUT_5POINT1),
        Format("eac3", "Dolby Digital Plus", AudioFormat.ENCODING_E_AC3, AudioFormat.CHANNEL_OUT_5POINT1),
        Format("dts", "DTS", AudioFormat.ENCODING_DTS, AudioFormat.CHANNEL_OUT_5POINT1),
        Format("dts-hd", "DTS-HD", AudioFormat.ENCODING_DTS_HD, AudioFormat.CHANNEL_OUT_7POINT1_SURROUND),
        Format("truehd", "Dolby TrueHD", AudioFormat.ENCODING_DOLBY_TRUEHD, AudioFormat.CHANNEL_OUT_7POINT1_SURROUND),
    )

    private val hdmiTypes: Set<Int> = buildSet {
        add(AudioDeviceInfo.TYPE_HDMI)
        add(AudioDeviceInfo.TYPE_HDMI_ARC)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) add(AudioDeviceInfo.TYPE_HDMI_EARC)
    }

    /** The formats the connected HDMI audio output accepts; empty without one. */
    fun detect(context: Context): List<Format> {
        val am = context.getSystemService(AudioManager::class.java) ?: return emptyList()
        val hdmi = am.getDevices(AudioManager.GET_DEVICES_OUTPUTS).filter { it.type in hdmiTypes }
        if (hdmi.isEmpty()) return emptyList()
        // A device's encoding list is what the soundbar's EDID advertises, and when it gives
        // one it is the truth: the TCL also answers "direct playback supported" for E-AC3,
        // which its soundbar does not take, and the picture then waits on audio that never
        // starts. Only an empty list ("not reported") falls back to asking the audio system.
        val advertised = hdmi.flatMap { it.encodings.toList() }.toSet()
        if (advertised.isNotEmpty()) {
            val found = candidates.filter { it.encoding in advertised }
            Timber.i("audio passthrough: hdmi=%s advertised=%s -> %s", hdmi.map { it.type }, advertised, found.map { it.mpv })
            return found
        }
        val attrs = AudioAttributes.Builder().setUsage(AudioAttributes.USAGE_MEDIA).setContentType(AudioAttributes.CONTENT_TYPE_MOVIE).build()
        val found = candidates.filter { f ->
            (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q && runCatching {
                AudioTrack.isDirectPlaybackSupported(
                    AudioFormat.Builder().setEncoding(f.encoding).setSampleRate(48_000).setChannelMask(f.channelMask).build(),
                    attrs,
                )
            }.getOrDefault(false))
        }
        Timber.i("audio passthrough: hdmi=%s advertised=%s -> %s", hdmi.map { it.type }, advertised, found.map { it.mpv })
        return found
    }

    /** The formats to pass through for a [setting] ("auto" | "off"). */
    fun forSetting(context: Context, setting: String): List<Format> = if (setting == "off") emptyList() else detect(context)

    /** "Auto (Dolby Digital, Dolby Digital Plus)" or "Auto (none found)". */
    fun autoLabel(found: List<Format>): String =
        if (found.isEmpty()) "Auto (no soundbar found)" else "Auto (" + found.joinToString(", ") { it.label } + ")"
}
