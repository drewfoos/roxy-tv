package tv.roxy.tv.ui.phone

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.width
import androidx.compose.material3.Text
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.getBoundsInRoot
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.height
import androidx.compose.ui.unit.sp
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * The season selector on a series page, when the entry is a film with a long name
 * ("Rascal Does Not Dream of a Dreaming Girl"). The name took the whole row and squeezed
 * "Download all" into a column of single letters (or off the screen, making the row several
 * lines tall). The name gives way instead: one line, cut with an ellipsis.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = android.app.Application::class, qualifiers = "w412dp-h915dp-xxhdpi")
class SeasonRowTest {
    @get:Rule val compose = createComposeRule()

    private fun show(label: String) = compose.setContent {
        Box(Modifier.width(412.dp)) {
            SeasonRow(label, onOpen = {}) { Text("Download all", fontSize = 13.sp) }
        }
    }

    @Test fun aLongNameLeavesTheDownloadButtonWhole() {
        show("Rascal Does Not Dream of a Sister Venturing Out and a Knapsack Kid")
        val button = compose.onNodeWithText("Download all").getBoundsInRoot()
        assertTrue("the button stays on one line (was ${button.height})", button.height < 28.dp)
        assertTrue("and on the screen (ends at ${button.right})", button.right <= 412.dp && button.left > 200.dp)
        val name = compose.onNodeWithText("Rascal", substring = true, useUnmergedTree = true).getBoundsInRoot()
        assertTrue("the name is one line (was ${name.height})", name.height < 30.dp)
        assertTrue("and ends before the button", name.right <= button.left)
    }

    @Test fun aShortNameSitsAtTheStartAndTheButtonAtTheEnd() {
        show("Season 1")
        val name = compose.onNodeWithText("Season 1", useUnmergedTree = true).getBoundsInRoot()
        val button = compose.onNodeWithText("Download all").getBoundsInRoot()
        assertTrue("the name keeps its own width", name.right < 140.dp)
        assertTrue("the button is at the end", button.right > 380.dp)
    }

    // While the season list is being read the row is there already, as a placeholder of
    // the height it will have: the name arriving moves nothing below it.
    @Test fun theRowKeepsItsHeightWhenTheNameArrives() {
        var label by mutableStateOf<String?>(null)
        compose.setContent { Box(Modifier.width(412.dp)) { SeasonRow(label, onOpen = {}) { } } }
        compose.onNodeWithTag("season-name-pending").assertExists()
        val before = compose.onRoot().getBoundsInRoot().height
        label = "Season 1"
        compose.waitForIdle()
        compose.onNodeWithTag("season-name-pending").assertDoesNotExist()
        assertEquals(before, compose.onRoot().getBoundsInRoot().height)
    }
}
