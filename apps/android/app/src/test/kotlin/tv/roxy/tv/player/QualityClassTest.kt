package tv.roxy.tv.player

import org.junit.Assert.assertEquals
import org.junit.Test
import tv.roxy.tv.api.MediaSource
import tv.roxy.tv.api.MediaStream

/**
 * A film wider than 16:9 has fewer lines than its size's name says: 3840 x 1604 is 4K. The
 * Quality menu went by the lines alone, so that film was "1440p" with a second "1440p"
 * (the conversion) listed under it, and every wide 1080p film "720p" over a "720p".
 */
class QualityClassTest {
    private fun film(width: Int, height: Int) = MediaSource(
        id = "s",
        streams = listOf(
            MediaStream(index = 1, type = "audio", codec = "aac"),
            MediaStream(index = 0, type = "video", codec = "hevc", width = width, height = height),
        ),
    )

    private fun labels(source: MediaSource?) = QualityOption.forSource(QualityOption.classHeight(source?.streams)).map { it.label }

    @Test fun aWide4KFilmIs4KWith1440pUnderIt() {
        assertEquals(2160, QualityOption.classHeight(film(3840, 1604).streams))
        assertEquals(listOf("4K", "1440p", "1080p", "720p", "480p"), labels(film(3840, 1604)))
    }

    @Test fun aWide1080pFilmIs1080p() {
        assertEquals(1080, QualityOption.classHeight(film(1920, 802).streams))
        assertEquals(listOf("1080p", "720p", "480p"), labels(film(1920, 802)))
    }

    @Test fun noFileIsListedUnderItsOwnName() {
        for ((w, h) in listOf(3840 to 1604, 3840 to 2160, 2560 to 1072, 1920 to 802, 1920 to 1038, 1920 to 1080, 1440 to 1080, 1280 to 534, 1280 to 720, 720 to 480)) {
            val names = labels(film(w, h))
            assertEquals("$w x $h: $names", names.size, names.distinct().size)
        }
    }

    @Test fun picturesNoWiderThan16By9GoByTheirHeight() {
        assertEquals(1080, QualityOption.classHeight(1920, 1080))
        assertEquals(1080, QualityOption.classHeight(1440, 1080))
        assertEquals(720, QualityOption.classHeight(960, 720))
    }

    @Test fun anUnknownWidthLeavesTheHeightAndAnUnknownFileNothing() {
        assertEquals(1080, QualityOption.classHeight(0, 1080))
        assertEquals(0, QualityOption.classHeight(1920, 0))
        assertEquals(0, QualityOption.classHeight(null))
        assertEquals(0, QualityOption.classHeight(MediaSource(id = "s").streams))
        assertEquals(listOf("Best", "4K", "1440p", "1080p", "720p", "480p"), labels(null))
    }
}
