package tv.roxy.tv.ui

import tv.roxy.tv.api.DiscoverFilm
import tv.roxy.tv.api.DiscoverFilms

/*
 * The films of an anime series on its Discover page (phone and TV). Everything that is a
 * rule (where a film sits, whether it comes with a request, what can be requested) is the
 * server's and arrives in the data; what is here only arranges and words it. The web app
 * has the same in pages/discoverFilms.ts.
 */

/** Films by the place the server gave them. */
class PlacedFilms(
    /** Before the first season. */
    val before: List<DiscoverFilm>,
    /** After the season with that number. */
    val after: Map<Int, List<DiscoverFilm>>,
    /** Apart from the seasons: "More films". */
    val more: List<DiscoverFilm>,
)

/** A film whose season the page does not list goes under "More films". */
fun placeFilms(films: List<DiscoverFilm>, seasonNumbers: List<Int>): PlacedFilms {
    val before = films.filter { it.afterSeason == 0 }
    val after = films.filter { it.afterSeason > 0 && it.afterSeason in seasonNumbers }.groupBy { it.afterSeason }
    val more = films.filter { it.afterSeason < 0 || (it.afterSeason > 0 && it.afterSeason !in seasonNumbers) }
    return PlacedFilms(before, after, more)
}

/** The TMDB ids the page's main Request sends: exactly the films the server marked for it. */
fun buttonFilms(films: DiscoverFilms?): List<Int> = films?.films.orEmpty().filter { it.withRequest }.mapNotNull { it.tmdbId }

/** " + 3 films" for the main button's label; nothing without films. */
fun filmsSuffix(n: Int): String = when {
    n <= 0 -> ""
    n == 1 -> " + 1 film"
    else -> " + $n films"
}

enum class FilmState { Library, Requested, Approval, Failed, Radarr, Request, None }

/** What a film's row shows at its end. */
fun filmState(f: DiscoverFilm, requestsEnabled: Boolean): FilmState {
    val canRequest = f.tmdbId != null && requestsEnabled
    return when {
        !f.libraryItemId.isNullOrEmpty() -> FilmState.Library
        f.requestStatus == "approval" -> FilmState.Approval
        f.requestStatus == "failed" || f.requestStatus == "declined" -> if (canRequest) FilmState.Request else FilmState.Failed
        !f.requestStatus.isNullOrEmpty() -> FilmState.Requested
        f.inRadarr -> FilmState.Radarr
        canRequest -> FilmState.Request
        else -> FilmState.None
    }
}

/** The line under a film's title: "Film · 2019 · Side story". */
fun filmLine(f: DiscoverFilm): String =
    listOfNotNull("Film", f.year?.takeIf { it > 0 }?.toString(), (if (f.withRequest) "Comes with the request" else f.note)?.takeIf { it.isNotBlank() }).joinToString("  ·  ")

/** What OK does on a film's entry on the TV page. */
enum class FilmAction { OpenLibrary, Request, OpenPage, None }

/** The film in the library opens; one that can be requested is requested; any other opens its own page when TMDB's film is known. */
fun filmAction(f: DiscoverFilm, requestsEnabled: Boolean): FilmAction = when (filmState(f, requestsEnabled)) {
    FilmState.Library -> FilmAction.OpenLibrary
    FilmState.Request -> FilmAction.Request
    else -> if (f.tmdbId != null) FilmAction.OpenPage else FilmAction.None
}

/** What the focused entry says OK will do. */
fun filmActionWord(a: FilmAction): String = when (a) {
    FilmAction.OpenLibrary -> "Open in library"
    FilmAction.Request -> "Request"
    FilmAction.OpenPage -> "Open"
    FilmAction.None -> ""
}

/** A film's state in words, shown under its name on the TV page. */
fun filmWord(f: DiscoverFilm): String = when (filmState(f, requestsEnabled = false)) {
    FilmState.Library -> "In library"
    FilmState.Approval -> "Waiting for approval"
    FilmState.Requested -> "Requested"
    FilmState.Failed -> "Request failed"
    FilmState.Radarr -> "In Radarr"
    else -> if (f.withRequest) "Comes with the request" else f.note.orEmpty()
}
