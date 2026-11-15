package tv.roxy.tv.api

import androidx.test.core.app.ApplicationProvider
import coil3.request.ImageRequest
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.runBlocking
import okhttp3.HttpUrl.Companion.toHttpUrl
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNull
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import tv.roxy.tv.data.Prefs
import tv.roxy.tv.data.Session
import tv.roxy.tv.withServerImageKey

/**
 * Images were kept on disk under the whole address they were asked at, so the same poster
 * was downloaded again at each of a server's addresses (home network, Tailscale). The key
 * is now the server's id and the image's path.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = android.app.Application::class)
class ImageCacheKeyTest {
    private val home = "http://192.168.1.50:8097"
    private val tail = "http://100.64.0.7:8097"
    private val context = ApplicationProvider.getApplicationContext<android.app.Application>()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined)
    private val prefs = Prefs(context, scope)
    private val api = RoxyApi(prefs)

    @After fun stop() = scope.cancel()

    private fun signIn(serverId: String) =
        runBlocking { prefs.saveSession(Session(serverUrl = home, token = "t", profileId = "p", addresses = listOf(home, tail), serverId = serverId)) }

    @Test fun oneServersImageHasOneKeyAtEveryAddress() {
        val a = serverImageKey("$home/v1/items/e1/image?type=poster&w=300".toHttpUrl(), "srv1")
        assertEquals("roxy:srv1:/v1/items/e1/image?type=poster&w=300", a)
        assertEquals(a, serverImageKey("$tail/v1/items/e1/image?type=poster&w=300".toHttpUrl(), "srv1"))
        assertNotEquals("another size is another file", a, serverImageKey("$home/v1/items/e1/image?type=poster&w=600".toHttpUrl(), "srv1"))
        assertNotEquals("another server's image is kept apart", a, serverImageKey("$home/v1/items/e1/image?type=poster&w=300".toHttpUrl(), "srv2"))
        assertEquals("roxy:srv1:/v1/items/e1/image", serverImageKey("$home/v1/items/e1/image".toHttpUrl(), "srv1"))
    }

    @Test fun anAddressCarryingASecretKeepsItsOwnKey() {
        assertNull(serverImageKey("$home/v1/sources/s1/trickplay/0.jpg?mt=abc".toHttpUrl(), "srv1"))
        assertNull(serverImageKey("$home/v1/items/e1/image?token=abc&w=300".toHttpUrl(), "srv1"))
        assertNull("no id known: nothing says whose image it is", serverImageKey("$home/v1/items/e1/image".toHttpUrl(), ""))
    }

    @Test fun onlyTheSignedInServersImagesAreRenamed() {
        signIn("srv1")
        assertEquals(api.imageCacheKey("$home/v1/items/e1/image?w=300"), api.imageCacheKey("$tail/v1/items/e1/image?w=300"))
        assertEquals("roxy:srv1:/v1/items/e1/image?w=300", api.imageCacheKey("$tail/v1/items/e1/image?w=300"))
        assertNull("somebody else's picture", api.imageCacheKey("https://image.example.org/t/p/w500/x.jpg"))
        assertNull(api.imageCacheKey("not an address"))
        signIn("")
        assertNull("an older server gives no id: the address stays the key", api.imageCacheKey("$home/v1/items/e1/image?w=300"))
    }

    @Test fun theImageLoaderAsksUnderThatKey() {
        signIn("srv1")
        fun asked(url: String) = withServerImageKey(ImageRequest.Builder(context).data(url).build(), api::imageCacheKey)
        assertEquals("roxy:srv1:/v1/items/e1/image?w=300", asked("$tail/v1/items/e1/image?w=300").diskCacheKey)
        assertEquals(asked("$home/v1/items/e1/image?w=300").diskCacheKey, asked("$tail/v1/items/e1/image?w=300").diskCacheKey)
        assertNull(asked("https://image.example.org/t/p/w500/x.jpg").diskCacheKey)
        val own = ImageRequest.Builder(context).data("$home/v1/items/e1/image").diskCacheKey("mine").build()
        assertEquals("a key the caller chose stands", "mine", withServerImageKey(own, api::imageCacheKey).diskCacheKey)
    }
}
