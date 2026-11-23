package tv.roxy.tv.player

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File
import java.util.zip.ZipFile

/**
 * The libmpv the app ships carries Roxy's patches (tools/libmpv-patches). Checked in the
 * built library itself, because a rebuild that leaves a patch out looks the same to every
 * other test: the player needs a real device to run.
 */
class LibmpvPatchTest {
    private val aar: File = Regex("""implementation\(files\("(libs/libmpv-roxy-[^"]+\.aar)"\)\)""")
        .find(File("build.gradle.kts").readText())!!.groupValues[1].let(::File)

    private fun has(entry: String, words: String): Boolean = ZipFile(aar).use { zip ->
        val bytes = zip.getInputStream(zip.getEntry(entry)).readBytes()
        val want = words.toByteArray(Charsets.US_ASCII)
        (0..bytes.size - want.size).any { i -> want.indices.all { j -> bytes[i + j] == want[j] } }
    }

    // ffmpeg/0001: the decoder told mpv to send more after the last packet when the TV's HEVC
    // decoder reported a format change with the last frame of an HDR film. mpv has nothing
    // more to send and waited for ever: the picture never ended, the film never closed, and
    // a seek into its last seconds spun for good (0.6.55, the film's last 16 s).
    @Test fun theDecoderEndsItsStreamOnBothProcessorTypes() {
        val abis = listOf("arm64-v8a", "armeabi-v7a")
        val patched = abis.filter { has("jni/$it/libavcodec.so", "The codec did not end its stream within") }
        assertEquals("libavcodec.so with the end-of-stream patch", abis, patched)
    }

    @Test fun everyPatchInTheToolsFolderIsNamedHere() {
        val patches = File("../tools/libmpv-patches").walkTopDown().filter { it.extension == "patch" }.map { it.name }.toSet()
        assertTrue("the patches exist", patches.isNotEmpty())
        // A new patch gets a check of its own above, then its name here.
        assertEquals(setOf("recipe.patch", "0001-mediacodec-end-of-stream.patch"), patches)
    }
}
