package tv.roxy.tv.ui

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The library search compares text with the accents off. "Pokemon" typed on the TV found
 * nothing, because the title is stored as "Pokémon" and the keyboard has no é.
 */
class SearchTextTest {
    @Test fun plainLettersFindAnAccentedTitle() {
        assertTrue(matchesSearch("Pokemon", "Pokémon"))
        assertTrue(matchesSearch("pokemon the movie", "Pokémon the Movie 2000"))
        assertTrue(matchesSearch("amelie", "Le Fabuleux Destin d'Amélie Poulain"))
    }

    @Test fun anAccentedQueryStillFindsItsTitleAndThePlainOne() {
        assertTrue(matchesSearch("Pokémon", "Pokémon"))
        assertTrue(matchesSearch("Pokémon", "Pokemon Heroes"))
    }

    @Test fun composedAndDecomposedSpellingsAreTheSame() {
        // "é" as one character and as "e" + a combining accent.
        assertEquals(searchFold("Pokémon"), searchFold("Pokémon"))
        assertEquals("pokemon", searchFold("Pokémon"))
    }

    @Test fun caseIsIgnoredAndGenresCount() {
        assertTrue(matchesSearch("JUJUTSU", "Jujutsu Kaisen"))
        assertTrue(matchesSearch("comedy", "Pokémon", listOf("Action", "Comedy")))
    }

    @Test fun otherTitlesAreNotMatched() {
        assertFalse(matchesSearch("pokemon", "Digimon Adventure", listOf("Adventure")))
        assertFalse(matchesSearch("   ", "Pokémon"))
    }
}
