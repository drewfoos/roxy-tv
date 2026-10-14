package tv.roxy.tv.ui.nav

import kotlinx.serialization.Serializable

/**
 * Typed destinations for Navigation Compose. The phone shows Home, SeriesLibrary,
 * MovieLibrary and Settings as bottom-bar tabs; the TV reaches Settings from the hero.
 * Series and Movie are detail pages pushed on top.
 */
sealed interface Route {
    @Serializable data object Home : Route
    @Serializable data object SeriesLibrary : Route
    @Serializable data object MovieLibrary : Route
    @Serializable data object Settings : Route
    @Serializable data class Series(val id: String, val title: String) : Route
    @Serializable data class Movie(val id: String, val title: String) : Route
}
