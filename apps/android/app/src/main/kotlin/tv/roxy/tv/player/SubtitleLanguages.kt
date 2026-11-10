package tv.roxy.tv.player

import tv.roxy.tv.api.SubtitleInfo

// The server's two lists (transcode.matroskaSubtitles, model.IsTextSubtitleCodec): keep in step.
private val MATROSKA_SUBTITLES = setOf("subrip", "srt", "ass", "ssa", "webvtt", "vtt", "hdmv_pgs_subtitle", "dvd_subtitle", "dvb_subtitle", "hdmv_text_subtitle")
private val TEXT_SUBTITLES = setOf("ass", "ssa", "subrip", "srt", "webvtt", "vtt", "mov_text", "text", "microdvd", "sami")

/**
 * Does the server's transcode of a file in [container] carry this subtitle track? It copies
 * whatever Matroska can hold (everything, from a Matroska source), turns other text formats
 * into SRT, and leaves out a track it can do neither with (teletext in a TS file): the
 * tracks after that one then sit one place earlier than in the file.
 */
internal fun transcodeKeepsSubtitle(codec: String, container: String?): Boolean {
    val c = container.orEmpty().lowercase()
    val k = codec.lowercase()
    return "mkv" in c || "matroska" in c || "webm" in c || k in MATROSKA_SUBTITLES || k in TEXT_SUBTITLES
}

/**
 * mpv's subtitle track id → the language the server sniffed from the track's text, for the
 * tracks whose container tag is wrong or missing. mpv numbers the tracks of what it opened
 * 1..N: the file's own subtitle tracks in order, or, of a transcode, only those the
 * transcode kept ([transcoding], [container]). One map for both used to put a language on
 * the wrong track of a transcode that had dropped one. Subtitle files beside the video
 * (external) are not in the stream: they get their language when they are added.
 */
internal fun languageOverrides(subs: List<SubtitleInfo>, transcoding: Boolean, container: String?): Map<Int, String> {
    val own = subs.filter { !it.isExternal }
    val inStream = if (transcoding) own.filter { transcodeKeepsSubtitle(it.codec, container) } else own
    return inStream.withIndex().filter { it.value.detectedLanguage != null }.associate { (i, s) -> i + 1 to s.effectiveLanguage }
}
