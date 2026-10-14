package tv.roxy.tv.ui.nav

import androidx.activity.compose.BackHandler
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.Stable
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.remember
import androidx.lifecycle.ViewModelStore
import androidx.lifecycle.ViewModelStoreOwner
import androidx.lifecycle.viewmodel.compose.LocalViewModelStoreOwner

/**
 * Hand-rolled back stack (no androidx.navigation): a list of [Screen]s, each entry owning
 * its own ViewModelStore so screen ViewModels die when the screen is popped. Enough for
 * four screens; swap for navigation-compose when deep links or saved state matter.
 */
sealed interface Screen {
    data object Home : Screen
    data class Series(val id: String, val title: String) : Screen
    data class Movie(val id: String, val title: String) : Screen
    data object Settings : Screen
}

class NavEntry(val screen: Screen) : ViewModelStoreOwner {
    override val viewModelStore: ViewModelStore = ViewModelStore()
}

@Stable
class NavController(root: Screen) {
    val stack = mutableStateListOf(NavEntry(root))

    val current: NavEntry get() = stack.last()

    fun push(screen: Screen) {
        stack.add(NavEntry(screen))
    }

    /** Pops the top screen; false when already at the root. */
    fun pop(): Boolean {
        if (stack.size <= 1) return false
        stack.removeAt(stack.size - 1).viewModelStore.clear()
        return true
    }
}

@Composable
fun rememberNavController(root: Screen = Screen.Home): NavController = remember { NavController(root) }

/** Renders the top of the stack with its own ViewModelStoreOwner and wires the system back button. */
@Composable
fun NavHost(nav: NavController, content: @Composable (Screen) -> Unit) {
    val entry = nav.current
    BackHandler(enabled = nav.stack.size > 1) { nav.pop() }
    CompositionLocalProvider(LocalViewModelStoreOwner provides entry) {
        content(entry.screen)
    }
}
