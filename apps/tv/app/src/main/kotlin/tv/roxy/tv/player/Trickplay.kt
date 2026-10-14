package tv.roxy.tv.player

import android.graphics.BitmapFactory
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import timber.log.Timber
import tv.roxy.tv.api.RoxyApi
import tv.roxy.tv.api.TrickplayManifest

/**
 * Seek-preview thumbnails for one source: the server's manifest plus a small in-memory
 * cache of decoded sheets (a 25-minute episode is 2 sheets of ~600 KB).
 */
class TrickplayPreview(private val api: RoxyApi, private val sourceId: String, val manifest: TrickplayManifest) {
    private val sheets = HashMap<Int, ImageBitmap>()
    private val failed = HashSet<Int>()
    private val lock = Mutex()

    /** Decoded sheet n, fetching it on first use; null while unavailable. */
    suspend fun sheet(n: Int): ImageBitmap? {
        if (n < 0 || n >= manifest.sheets) return null
        sheets[n]?.let { return it }
        if (n in failed) return null
        return lock.withLock {
            sheets[n]?.let { return it }
            withContext(Dispatchers.IO) {
                runCatching {
                    val bytes = api.bytes(api.trickplaySheetUrl(sourceId, n))
                    BitmapFactory.decodeByteArray(bytes, 0, bytes.size)!!.asImageBitmap()
                }.onFailure { Timber.w(it, "trickplay sheet %d failed", n); failed += n }
                    .getOrNull()
                    ?.also { sheets[n] = it }
            }
        }
    }

    /** Cached sheet without fetching (for synchronous draw). */
    fun cached(n: Int): ImageBitmap? = sheets[n]
}
