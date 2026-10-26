package tv.roxy.tv.ui.nav

import kotlinx.serialization.Serializable

/**
 * Typed destinations for Navigation Compose. The phone shows Home, SeriesLibrary,
 * MovieLibrary and Settings as bottom-bar tabs; the TV reaches Settings from the hero.
 * Series and Movie are detail pages pushed on top.
 */
sealed interface Route {
    @Serializable data object Home : Route
    // Discover: TMDB browsing and requests ("our own request app"); a title page per TMDB id.
    @Serializable data object Discover : Route
    @Serializable data class DiscoverDetail(val type: String, val tmdbId: Int) : Route
    @Serializable data class Person(val tmdbId: Int, val name: String) : Route
    // Admin: the Sonarr / Radarr connection Discover requests go to.
    @Serializable data object ArrSettings : Route
    @Serializable data object FanartSettings : Route
    @Serializable data object TranscodingSettings : Route
    // Admin: Library (scan / refresh, unmatched titles), Identify and Edit details for one title.
    @Serializable data object LibrarySettings : Route
    @Serializable data object ChangePassword : Route
    @Serializable data object LibraryRefreshPick : Route
    @Serializable data object AddMediaFolder : Route
    @Serializable data class Identify(val id: String, val title: String, val type: String) : Route
    @Serializable data class EditMetadata(val id: String, val title: String) : Route
    @Serializable data class DiscoverMoreDetails(val type: String, val tmdbId: Int, val title: String) : Route
    @Serializable data object Webhooks : Route
    @Serializable data class Legal(val doc: String) : Route
    @Serializable data object Watchlist : Route
    @Serializable data object Browse : Route
    @Serializable data object Account : Route
    @Serializable data object Search : Route
    // Phone: the Downloads page (from Account, and from Home when the server is away) and one downloaded show.
    @Serializable data object Downloads : Route
    @Serializable data class DownloadedTitle(val id: String) : Route
    // TV-only: the D-pad screens keep Settings as a page and have no tabs.
    @Serializable data object SeriesLibrary : Route
    @Serializable data object MovieLibrary : Route
    @Serializable data object Settings : Route
    @Serializable data class Series(val id: String, val title: String) : Route
    @Serializable data class Movie(val id: String, val title: String) : Route
    // Phone: the "More details" page of a series or movie.
    @Serializable data class Details(val id: String, val title: String) : Route
    // Phone: one episode, playing inline at the top of its page.
    @Serializable data class Episode(val id: String, val title: String) : Route
}
