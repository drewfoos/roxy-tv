package tv.roxy.tv.tv

import android.content.ContentProvider
import android.content.ContentValues
import android.database.Cursor
import android.net.Uri
import android.os.ParcelFileDescriptor
import java.io.File
import java.io.FileNotFoundException

/**
 * Serves the thumbnails behind the launcher's "Continue watching" row (Android TV's
 * Watch Next store) as `content://<applicationId>.images/items/<id>/thumb`. The launcher
 * has no network, so [WatchNext] writes a small JPEG into the app's cache first and the
 * row points here. Read-only, images only, and the id is checked against the cache
 * folder so a crafted URI cannot reach another file.
 */
class ImageProvider : ContentProvider() {
    override fun onCreate(): Boolean = true

    override fun getType(uri: Uri): String = "image/jpeg"

    override fun openFile(uri: Uri, mode: String): ParcelFileDescriptor {
        if (mode != "r") throw FileNotFoundException("read-only")
        val ctx = context ?: throw FileNotFoundException()
        val parts = uri.pathSegments
        // items / <id> / thumb
        if (parts.size != 3 || parts[0] != "items" || parts[2] != "thumb") throw FileNotFoundException(uri.toString())
        val file = WatchNext.thumbFile(ctx, parts[1])
        if (!file.isFile || file.canonicalFile.parentFile != WatchNext.thumbDir(ctx).canonicalFile) throw FileNotFoundException(uri.toString())
        return ParcelFileDescriptor.open(file, ParcelFileDescriptor.MODE_READ_ONLY)
    }

    override fun query(uri: Uri, projection: Array<out String>?, selection: String?, selectionArgs: Array<out String>?, sortOrder: String?): Cursor? = null
    override fun insert(uri: Uri, values: ContentValues?): Uri? = null
    override fun delete(uri: Uri, selection: String?, selectionArgs: Array<out String>?): Int = 0
    override fun update(uri: Uri, values: ContentValues?, selection: String?, selectionArgs: Array<out String>?): Int = 0
}

internal fun File.safeChild(name: String): File = File(this, name)
