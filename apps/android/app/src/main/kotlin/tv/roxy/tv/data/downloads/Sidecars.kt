package tv.roxy.tv.data.downloads

import tv.roxy.tv.api.Item

/**
 * One external subtitle file of a download: the server's stream number and the name it is
 * saved under beside the video, `video.<lang>.<index>.<ext>`. The player counts on that
 * name (mpv's `sub-auto` loads what starts with the video's), so the scheme stays as it is.
 */
internal data class Sidecar(val index: Int, val fileName: String)

/** The external subtitle files of [item]'s primary source, in the server's order. */
internal fun sidecarsOf(item: Item): List<Sidecar> {
    val src = item.primarySource ?: return emptyList()
    return src.streams.filter { it.type == "subtitle" && it.isExternal }.map { s ->
        val ext = when (s.codec.lowercase()) { "ass", "ssa" -> "ass"; "subrip", "srt" -> "srt"; "webvtt", "vtt" -> "vtt"; else -> s.codec.lowercase().ifEmpty { "srt" } }
        val lang = (s.detectedLanguage ?: s.language ?: "und").lowercase().filter { it.isLetterOrDigit() }.ifEmpty { "und" }
        Sidecar(s.index, "video.$lang.${s.index}.${ext.filter { it.isLetterOrDigit() }.ifEmpty { "srt" }}")
    }
}

/** What became of one try at a sidecar. */
internal enum class SidecarFetch {
    /** The file is beside the video. */
    Saved,
    /** The server has nothing to give for it (the subtitle was removed, or is empty): asking again will not help. */
    Gone,
    /** The server answered, but not with the file (an error on its side, signed out, asked to wait): try again later. */
    Refused,
    /** No answer at all: the server is away, so the other sidecars are not tried either. */
    NoAnswer,
}

/**
 * What an answer with status [code] and [bodyLength] bytes means for a sidecar. A 4xx will
 * not get better by asking again, except the ones that are about the moment and not the file
 * (401 signed out, 408 took too long, 429 slow down).
 */
internal fun sidecarAnswer(code: Int, bodyLength: Int): SidecarFetch = when {
    code in 200..299 -> if (bodyLength > 0) SidecarFetch.Saved else SidecarFetch.Gone
    code in 400..499 && code != 401 && code != 408 && code != 429 -> SidecarFetch.Gone
    else -> SidecarFetch.Refused
}
