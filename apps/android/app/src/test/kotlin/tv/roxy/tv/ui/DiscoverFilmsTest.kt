package tv.roxy.tv.ui

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import tv.roxy.tv.api.DiscoverFilm
import tv.roxy.tv.api.DiscoverFilms

/**
 * The films of an anime series on its Discover page: the app shows them where the server
 * put them and sends, with the main Request, exactly the films the server marked. It
 * decides nothing about a film itself.
 */
class DiscoverFilmsTest {
    private fun film(id: Int, after: Int = -1, tmdb: Int? = id * 10, with: Boolean = false, note: String? = null) =
        DiscoverFilm(anilistId = id, tmdbId = tmdb, title = "Film $id", afterSeason = after, eligible = with, withRequest = with, note = note)

    @Test fun filmsSitWhereTheServerSaid() {
        val p = placeFilms(listOf(film(1, after = 0), film(2, after = 1), film(3, after = 1), film(4), film(5, after = 7)), listOf(0, 1, 2))
        assertEquals(listOf(1), p.before.map { it.anilistId })
        assertEquals(listOf(2, 3), p.after[1]?.map { it.anilistId })
        assertNull(p.after[2])
        assertEquals("a film whose season the page does not list is still shown", listOf(4, 5), p.more.map { it.anilistId })
    }

    @Test fun theButtonSendsOnlyTheFilmsTheServerMarked() {
        val films = DiscoverFilms(
            films = listOf(
                film(1, after = 1, with = true),
                film(2, after = 1).copy(eligible = true), // eligible, but not for this button (already had, or not allowed)
                film(3, note = "Side story"),
                film(4, tmdb = null, with = true), // no TMDB film: nothing to send
            ),
            requestsEnabled = true,
        )
        assertEquals(listOf(10), buttonFilms(films))
        assertEquals(emptyList<Int>(), buttonFilms(null))
    }

    @Test fun theButtonNamesItsFilms() {
        assertEquals("", filmsSuffix(0))
        assertEquals(" + 1 film", filmsSuffix(1))
        assertEquals(" + 3 films", filmsSuffix(3))
    }

    @Test fun aRowSaysWhyAFilmIsOnlyListed() {
        assertEquals("Film  ·  2019  ·  Side story", filmLine(film(1, note = "Side story").copy(year = 2019)))
        assertEquals("Film  ·  Comes with the request", filmLine(film(1, with = true, note = "x")))
        assertEquals("Film", filmLine(film(1)))
    }

    @Test fun aRowsStateComesFromTheServersFields() {
        assertEquals(FilmState.Library, filmState(film(1).copy(libraryItemId = "i", requestStatus = "sent"), true))
        assertEquals(FilmState.Approval, filmState(film(1).copy(requestStatus = "approval"), true))
        assertEquals(FilmState.Requested, filmState(film(1).copy(requestStatus = "sent"), true))
        assertEquals(FilmState.Radarr, filmState(film(1).copy(inRadarr = true), true))
        assertEquals(FilmState.Request, filmState(film(1), true))
        assertEquals(FilmState.Request, filmState(film(1).copy(requestStatus = "failed"), true))
    }

    @Test fun noRequestWithoutATmdbFilmOrWithRequestsOff() {
        assertEquals(FilmState.None, filmState(film(1, tmdb = null), true))
        assertEquals(FilmState.None, filmState(film(1), false))
        assertEquals(FilmState.Failed, filmState(film(1).copy(requestStatus = "failed"), false))
    }

    @Test fun theTvSaysAFilmsStateInWords() {
        assertEquals("In library", filmWord(film(1).copy(libraryItemId = "i")))
        assertEquals("Comes with the request", filmWord(film(1, with = true)))
        assertEquals("Side story", filmWord(film(1, note = "Side story")))
        assertEquals("Requested", filmWord(film(1, with = true).copy(requestStatus = "sent")))
    }

    @Test fun theFilmsArePendingOnlyWhileAskedForAndNotAnswered() {
        assertTrue(filmsPending(wantsFilms = true, answered = false))
        assertFalse(filmsPending(wantsFilms = true, answered = true))
        // Not an anime series (or a film's page): nothing is asked for, nothing to wait for.
        assertFalse(filmsPending(wantsFilms = false, answered = false))
    }

    @Test fun aRequestButtonHasNoWordsUntilItsFilmsAreKnown() {
        // "Request" must not turn into "Request + 2 films" under the viewer's thumb.
        assertNull(requestWords("Request", films = 0, pending = true))
        assertEquals("Request + 2 films", requestWords("Request", films = 2, pending = false))
        assertEquals("Request", requestWords("Request", films = 0, pending = false))
        assertEquals("Request 2 missing seasons + 1 film", requestWords("Request 2 missing seasons", films = 1, pending = false))
    }
}
