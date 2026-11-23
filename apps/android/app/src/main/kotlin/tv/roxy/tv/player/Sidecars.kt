package tv.roxy.tv.player

import tv.roxy.tv.api.SubtitleInfo
import tv.roxy.tv.data.Languages

/**
 * A subtitle file that sits beside the video (the server lists it as an external stream).
 * It is not in the stream mpv opens: the player adds it once the file has loaded.
 * [streamIndex] is the server's number for it (what an item's saved subtitle names),
 * [source] where mpv reads it: the server's URL (never with the media token in it, see
 * [sidecarSource]) or, for a download, the file's path.
 */
internal data class Sidecar(val streamIndex: Int, val source: String, val title: String, val lang: String, val forced: Boolean)

/** mpv names a track it is given no title for after its file ("1000", "eng.1000.srt"). */
private const val SIDECAR_TITLE = "External"

private fun sidecarOf(s: SubtitleInfo, source: String) =
    Sidecar(s.index, source, MpvTracks.hearingTitle(s.title, s.isHearingImpaired).ifBlank { SIDECAR_TITLE }, s.effectiveLanguage.lowercase(), s.isForced)

/**
 * The subtitle files beside a streamed source, from the server's list. Text formats only:
 * the server does not serve an image subtitle (a .sup or .sub file) as a file.
 */
internal fun streamSidecars(baseUrl: String, sourceId: String, subs: List<SubtitleInfo>): List<Sidecar> =
    subs.filter { it.isExternal && it.codec.lowercase() in TEXT_SUBTITLES }
        .map { sidecarOf(it, "$baseUrl/v1/sources/$sourceId/subtitles/${it.index}") }

/**
 * The server's stream number in the name of a subtitle file saved beside a downloaded
 * video: `video.mkv` has `video.<language>.<number>.<ext>`, a name mpv does not match to
 * the video by itself. Null for any other file (the video, a file still being written).
 */
internal fun localSidecarIndex(videoName: String, fileName: String): Int? {
    val base = videoName.substringBeforeLast('.')
    if (fileName == videoName || !fileName.startsWith("$base.")) return null
    val parts = fileName.removePrefix("$base.").split('.')
    if (parts.size < 2 || parts.last().isEmpty()) return null
    return parts[parts.size - 2].takeIf { it.isNotEmpty() && it.length <= 9 && it.all(Char::isDigit) }?.toInt()
}

/**
 * The subtitle files saved beside a downloaded video ([names]: what its folder holds),
 * described as the download's own list of streams describes them; a file that list does
 * not know keeps the language in its name.
 */
internal fun localSidecars(videoPath: String, names: List<String>, subs: List<SubtitleInfo>): List<Sidecar> {
    val dir = videoPath.substringBeforeLast('/', "")
    val video = videoPath.substringAfterLast('/')
    return names.mapNotNull { name ->
        val index = localSidecarIndex(video, name) ?: return@mapNotNull null
        val path = if (dir.isEmpty()) name else "$dir/$name"
        subs.firstOrNull { it.isExternal && it.index == index }?.let { return@mapNotNull sidecarOf(it, path) }
        val parts = name.removePrefix(video.substringBeforeLast('.') + ".").split('.')
        val lang = parts.getOrNull(parts.size - 3)?.lowercase()?.takeIf { it != "und" }.orEmpty()
        Sidecar(index, path, SIDECAR_TITLE, lang, forced = false)
    }.sortedBy { it.streamIndex }.distinctBy { it.streamIndex }
}

/**
 * Where mpv reads a subtitle file from, for the load that is playing. The media token
 * normally travels as a header, set when the stream was opened, and the URL carries none:
 * the player's native side writes every URL it opens to the device log. Only when the
 * stream itself had to carry its token in the URL (an older server, [tokenInHeader] false)
 * does the subtitle's URL carry the same one. A file on the device needs none.
 */
internal fun sidecarSource(source: String, streamUrl: String?, tokenInHeader: Boolean): String {
    if (tokenInHeader || !source.startsWith("http")) return source
    val token = streamUrl?.substringAfter('?', "")?.split('&')?.firstOrNull { it.startsWith("mt=") && it.length > 3 } ?: return source
    return "$source?$token"
}

/** mpv's `sub-add` flags: listed, never shown by the add itself (the player chooses, see [sidecarPlan]). */
internal fun sidecarFlags(forced: Boolean): String = if (forced) "auto+forced" else "auto"

/** What happens to the subtitles once a file's subtitle files have been added. */
internal sealed interface SidecarPlan {
    /** This one (the server's stream number) is shown. */
    data class Select(val streamIndex: Int) : SidecarPlan
    /** Nothing was chosen: the language policy looks again, now that they are there. */
    data object Policy : SidecarPlan
    /** A track of the file itself, or none, was chosen: they are only listed. */
    data object Leave : SidecarPlan
}

/**
 * Decides it as the file's own tracks are decided (see [subtitleStart]): the viewer's pick
 * in this session, then the item's saved choice, then the policy. [subs] is the server's
 * list of the item's subtitle streams, [sidecars] those of them that can be added.
 *
 * A pick is carried by language and kind. One made on an added file goes to an added file
 * again (the same number first, then the same title); one made on a track of the file
 * goes to an added file only where the file has no such track. A saved number that names
 * an added file is used on a transcode too: it is the server's number, not the stream's.
 */
internal fun sidecarPlan(subs: List<SubtitleInfo>, sidecars: List<Sidecar>, pick: TrackPick?, savedOff: Boolean, savedIndex: Int?): SidecarPlan {
    if (sidecars.isEmpty()) return SidecarPlan.Leave
    if (pick != null) {
        if (pick.off) return SidecarPlan.Leave
        val same = sidecars.filter { it.lang == pick.lang && MpvTracks.subtitleKind(it.title, it.forced) == pick.kind }
        val t = same.firstOrNull { it.streamIndex == pick.sidecar } ?: same.firstOrNull { it.title == pick.title } ?: same.firstOrNull()
            ?: return SidecarPlan.Leave
        val inFile = subs.any { !it.isExternal && it.effectiveLanguage == pick.lang && MpvTracks.subtitleKind(it.title, it.isForced) == pick.kind }
        return if (pick.sidecar != null || !inFile) SidecarPlan.Select(t.streamIndex) else SidecarPlan.Leave
    }
    if (savedOff) return SidecarPlan.Leave
    if (savedIndex != null && savedIndex > 0) {
        if (sidecars.any { it.streamIndex == savedIndex }) return SidecarPlan.Select(savedIndex)
        if (subs.any { !it.isExternal && it.index == savedIndex }) return SidecarPlan.Leave
    }
    return SidecarPlan.Policy
}

/**
 * The subtitle id a file is opened on when its subtitles will come from an added file:
 * none (0), so that no other track shows until the file is there. Else [preload] (see [preloadSid]).
 */
internal fun sidForLoad(plan: SidecarPlan, preload: Int?): Int? = if (plan is SidecarPlan.Select) 0 else preload

/**
 * May the language policy show an added subtitle file instead of its choice among the
 * file's own tracks ([ownLang]: that track's language, "" untagged, null when it chose
 * none)? Only when the file has nothing for the viewer: no track, or one tagged with
 * another language than [viewerLang]. A track in the viewer's language, or an untagged
 * one, stays: the styled subtitles inside a release are not replaced by a plain file
 * beside it (the viewer can still pick that file).
 */
internal fun policyTakesSidecar(ownLang: String?, viewerLang: String): Boolean {
    if (ownLang == null) return true
    if (ownLang.isEmpty() || ownLang == "und" || viewerLang.isEmpty()) return false
    return Languages.canonical(ownLang) != Languages.canonical(viewerLang)
}

/**
 * What the subtitle layer over a native-HDR picture asks the server for (see
 * [SubtitleOverlay]): the shown track as a file of its own, and, for a styled track, where
 * the video file's fonts are. [query] and [header] carry the stream's token the way the
 * stream does.
 */
internal data class OverlayFiles(
    val subtitle: String,
    /** The address a font's number is added to; null when the track needs none or the file is not the server's. */
    val fonts: String?,
    val sourceId: String?,
    val query: String,
    val header: String?,
)

private val STREAM_URL = Regex("^(.*)/v1/stream/([^/?]+)")
private val PICTURE_SUBTITLES = setOf("hdmv_pgs_subtitle", "dvd_subtitle", "dvb_subtitle", "dvb_teletext", "xsub")
private val STYLED_SUBTITLES = setOf("ass", "ssa")

/** A subtitle of pictures (PGS, VobSub): the server has those as files only for text, so the layer cannot draw them. */
internal fun isPictureSubtitle(codec: String): Boolean = codec.lowercase() in PICTURE_SUBTITLES

/**
 * The files for the subtitle being shown, or null when the layer has nothing it can draw:
 * a track inside a file on the device (only the server has tracks as files of their own),
 * a picture track, or a track that cannot be named. An added subtitle file ([external]) is asked for where it
 * was added from; a track inside the video by its stream number.
 */
internal fun overlayFiles(
    streamUrl: String?,
    tokenInHeader: Boolean,
    external: Boolean,
    externalFile: String?,
    streamIndex: Int?,
    codec: String,
): OverlayFiles? {
    val url = streamUrl ?: return null
    if (isPictureSubtitle(codec)) return null
    val token = url.substringAfter('?', "").split('&').firstOrNull { it.startsWith("mt=") && it.length > 3 }
    val header = if (tokenInHeader && token != null) "X-Roxy-Media-Token: ${token.removePrefix("mt=")}" else null
    val query = if (!tokenInHeader && token != null) "?$token" else ""
    val stream = STREAM_URL.find(url)
    val base = stream?.groupValues?.get(1)
    val sourceId = stream?.groupValues?.get(2)
    val subtitle = if (external) {
        externalFile?.takeIf { it.startsWith("http") } ?: return null
    } else {
        if (base == null || streamIndex == null) return null
        "$base/v1/sources/$sourceId/subtitles/$streamIndex$query"
    }
    val fonts = if (codec.lowercase() in STYLED_SUBTITLES && base != null) "$base/v1/sources/$sourceId/attachments/" else null
    return OverlayFiles(subtitle, fonts, sourceId, query, header)
}

/** Whether an attached file is a font (films also attach covers and the like). */
internal fun isFontAttachment(filename: String, mime: String?): Boolean {
    val m = mime.orEmpty().lowercase()
    if (m.startsWith("font/") || "truetype" in m || "opentype" in m || m == "application/x-font-ttf" || m == "application/font-sfnt" || m == "application/vnd.ms-opentype") return true
    return filename.substringAfterLast('.', "").lowercase() in setOf("ttf", "otf", "ttc", "otc", "woff", "woff2")
}

/**
 * One second of silence as a WAV file (8-bit mono PCM): what the subtitle layer's mpv holds,
 * paused, so that it has a file to draw subtitles for.
 */
internal fun silenceWav(): ByteArray {
    val rate = 8000
    val data = ByteArray(rate) { 0x80.toByte() }
    val header = java.nio.ByteBuffer.allocate(44).order(java.nio.ByteOrder.LITTLE_ENDIAN).apply {
        put("RIFF".toByteArray()); putInt(36 + data.size); put("WAVE".toByteArray())
        put("fmt ".toByteArray()); putInt(16); putShort(1); putShort(1); putInt(rate); putInt(rate); putShort(1); putShort(8)
        put("data".toByteArray()); putInt(data.size)
    }.array()
    return header + data
}
