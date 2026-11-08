package tv.roxy.tv.ui.phone

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import tv.roxy.tv.api.Item
import tv.roxy.tv.api.MediaStream

class RowAudioLabelTest {
    private fun episode(vararg langs: String, anime: Boolean = true, type: String = "episode") = Item(
        id = "e", type = type, isAnime = anime,
        mediaSources = listOf(Item.Source(id = "s", isPrimary = true, streams = langs.map { MediaStream(type = "audio", language = it) })),
    )

    // Crunchyroll's row: Dub | Sub, or Sub alone, or Dub alone, for anime.
    @Test fun animeSaysDubAndSub() {
        assertEquals("Dub | Sub", rowAudioLabel(episode("jpn", "eng")))
        assertEquals("Sub", rowAudioLabel(episode("jpn")))
        assertEquals("Dub", rowAudioLabel(episode("eng")))
    }

    // An anime film (the movie page's row) reads the same.
    @Test fun animeFilm() {
        assertEquals("Dub | Sub", rowAudioLabel(Item(id = "m", type = "movie", isAnime = true, audioLanguages = listOf("jpn", "eng"))))
    }

    // Not anime: nothing for one language ("English" on every row says nothing), the
    // languages when there are several.
    @Test fun otherTitles() {
        assertNull(rowAudioLabel(episode("eng", anime = false)))
        assertNull(rowAudioLabel(Item(id = "m", type = "movie", audioLanguages = listOf("eng"))))
        assertEquals("English, French", rowAudioLabel(episode("eng", "fre", anime = false)))
    }

    @Test fun nothingForExtrasOrUnknownAudio() {
        assertNull(rowAudioLabel(episode("jpn", "eng", type = "extra")))
        assertNull(rowAudioLabel(Item(id = "e", type = "episode", isAnime = true)))
        assertNull(rowAudioLabel(episode("und")))
    }

    // A Movies & OVAs entry's format, as the dialog says it.
    @Test fun relatedFormats() {
        assertEquals("OVA", relatedFormatLabel("ova"))
        assertEquals("Special", relatedFormatLabel("SPECIAL"))
        assertEquals("Film", relatedFormatLabel("MOVIE"))
        assertEquals("Short", relatedFormatLabel("TV_SHORT"))
        assertEquals("Music", relatedFormatLabel("MUSIC"))
    }
}
