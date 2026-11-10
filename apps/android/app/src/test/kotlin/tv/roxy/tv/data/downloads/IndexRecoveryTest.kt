package tv.roxy.tv.data.downloads

import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import tv.roxy.tv.data.Prefs
import java.io.File

/** The app's downloads store with a downloads index that cannot be read. */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = android.app.Application::class)
class IndexRecoveryTest {
    private val context = ApplicationProvider.getApplicationContext<android.app.Application>()

    // The finding: the store started empty, with the video still on the disk: not listed,
    // not playable offline, and gone with the next delete of anything.
    @Test fun aDamagedIndexDoesNotForgetAFinishedDownload() {
        val root = (context.getExternalFilesDir("downloads") ?: File(context.filesDir, "downloads"))
        File(root, "m1").mkdirs()
        File(root, "m1/video.mkv").writeBytes(ByteArray(2048))
        File(context.filesDir, "downloads.json").writeText("{\"entries\":{\"m1\":{\"itemId\":\"m1\",\"sour")
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined)

        val d = Downloads(context, Prefs(context, scope), scope)

        assertEquals(setOf("m1"), d.index.value.entries.keys)
        assertNotNull("plays offline", d.localFile("m1"))
        assertEquals("on the Downloads page", listOf("m1"), d.titles().map { it.id })
        assertEquals(2048L, d.titles().single().bytes)
        assertTrue(File(context.filesDir, "downloads.json.bad").exists())
        scope.cancel()
    }
}
