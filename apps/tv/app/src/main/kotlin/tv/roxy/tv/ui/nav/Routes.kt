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
    // Admin: the Sonarr / Radarr connection Discover requests go to.
    @Serializable data object ArrSettings : Route
    @Serializable data object Webhooks : Route
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
}
