package tv.roxy.tv.player

/**
 * mpv track helpers with no libmpv in them (MpvPlayer cannot be loaded in a unit test:
 * the libmpv classes it implements need a newer JVM than the tests run on).
 */
internal object MpvTracks {
    // Signs & songs tracks go by many names: "Signs/Songs", "Forced", "[FOR DUB]" (FMA:B),
    // "English [ForcedNarrative]" (Netflix rips), "Dubtitles".
    val signsRegex = Regex("""(?i)\b(signs?|songs?|s&s|signs/songs|forced\w*|for dub|dubtitles?)\b""")
    val fullRegex = Regex("""(?i)\b(full|dialog(ue)?|complete)\b""")

    /** "signs" / "full" / "" from a subtitle track's title (and forced flag): carries a viewer's pick to the next episode. */
    fun subtitleKind(title: String, forced: Boolean): String = when {
        signsRegex.containsMatchIn(title) || forced -> "signs"
        fullRegex.containsMatchIn(title) -> "full"
        else -> ""
    }

    /** An mpv `aid` / `sid` value: null = auto (mpv's alang/slang choice), 0 or less = none. */
    private fun trackOption(id: Int?): String = when {
        id == null -> "auto"
        id <= 0 -> "no"
        else -> id.toString()
    }

    /**
     * The `aid` and `sid` to set before a loadfile, BOTH always. mpv keeps them as options
     * for later files, by number: a track set while one episode played (the saved audio,
     * a viewer's pick) opened the next one on that number, ignoring alang/slang, and a
     * file with another track order played the wrong language.
     */
    fun loadTrackOptions(sid: Int?, aid: Int?): List<Pair<String, String>> =
        listOf("aid" to trackOption(aid), "sid" to trackOption(sid))
}
