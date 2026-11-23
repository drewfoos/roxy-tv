package tv.roxy.tv.player

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File
import java.util.zip.ZipFile

/**
 * The libmpv the app ships carries Roxy's patches (tools/libmpv-patches), and the app uses
 * what they add as they expect. Checked in the built library and in the source, because a
 * rebuild that leaves a patch out looks the same to every other test: the player needs a
 * real device to run.
 */
class LibmpvPatchTest {
    private val aar: File = Regex("""implementation\(files\("(libs/libmpv-roxy-[^"]+\.aar)"\)\)""")
        .find(File("build.gradle.kts").readText())!!.groupValues[1].let(::File)
    private val abis = listOf("arm64-v8a", "armeabi-v7a")
    private val player = File("src/main/kotlin/tv/roxy/tv/player")

    private fun bytes(entry: String): ByteArray = ZipFile(aar).use { zip -> zip.getInputStream(zip.getEntry(entry)).readBytes() }

    private fun ByteArray.has(words: String): Boolean {
        val want = words.toByteArray(Charsets.US_ASCII)
        return (0..size - want.size).any { i -> want.indices.all { j -> this[i + j] == want[j] } }
    }

    // ffmpeg/0001: the decoder told mpv to send more after the last packet when the TV's HEVC
    // decoder reported a format change with the last frame of an HDR film. mpv has nothing
    // more to send and waited for ever: the picture never ended, the film never closed, and
    // a seek into its last seconds spun for good (0.6.55, the film's last 16 s).
    @Test fun theDecoderEndsItsStreamOnBothProcessorTypes() {
        val patched = abis.filter { bytes("jni/$it/libavcodec.so").has("The codec did not end its stream within") }
        assertEquals("libavcodec.so with the end-of-stream patch", abis, patched)
    }

    // mpv/0001 and wrapper.patch: mpv draws its own subtitles on a second surface while the
    // decoder fills the video's (native HDR). Without them a native-HDR picture has none.
    @Test fun mpvDrawsItsSubtitlesOnASecondSurface() {
        assertEquals("libmpv.so with the option", abis, abis.filter { bytes("jni/$it/libmpv.so").has("android-osd-wid") })
        assertEquals("libplayer.so hands the surface over", abis, abis.filter { bytes("jni/$it/libplayer.so").has("nativeAttachOsdSurface") })
        val jar = File.createTempFile("classes", ".jar").apply { deleteOnExit(); writeBytes(bytes("classes.jar")) }
        val lib = ZipFile(jar).use { it.getInputStream(it.getEntry("dev/jdtech/mpv/MPVLib.class")).readBytes() }
        assertTrue("MPVLib.attachOsdSurface", lib.has("attachOsdSurface"))
        assertTrue("MPVLib.detachOsdSurface", lib.has("detachOsdSurface"))
    }

    // The surface is the page's: mpv must have let go of it before it is destroyed, also
    // when the player goes first (its teardown runs on another thread).
    @Test fun theSubtitleSurfaceIsLetGoBeforeItIsDestroyed() {
        val mpv = File(player, "MpvPlayer.kt").readText()
        val destroy = mpv.substring(mpv.indexOf("    fun destroy() {"))
        val detach = destroy.indexOf("mpv.detachOsdSurface()")
        assertTrue("destroy() lets the surface go before it marks the player destroyed", detach in 0 until destroy.indexOf("destroyed = true"))
        assertTrue("the surface's own end lets it go",
            mpv.contains("override fun surfaceDestroyed(holder: SurfaceHolder) {\n            if (!destroyed) mpv.detachOsdSurface()"))
        assertTrue("the page gives mpv the surface it lays over a native-HDR picture",
            File(player, "PlayerSurface.kt").readText().contains("holder.addCallback(player.subtitleSurface)"))
    }

    // One player: the subtitles of a native-HDR picture were a second mpv's until 0.6.56.
    @Test fun thePlayerIsTheOnlyMpv() {
        val made = File("src/main/kotlin").walkTopDown().filter { it.extension == "kt" }
            .flatMap { f -> Regex("""MPVLib\.create\(""").findAll(f.readText()).map { f.name } }.toList()
        assertEquals(listOf("MpvPlayer.kt"), made)
    }

    @Test fun everyPatchInTheToolsFolderIsNamedHere() {
        val patches = File("../tools/libmpv-patches").walkTopDown().filter { it.extension == "patch" }.map { it.name }.toSet()
        // A new patch gets a check of its own above, then its name here.
        assertEquals(setOf("recipe.patch", "wrapper.patch", "0001-mediacodec-end-of-stream.patch", "0001-mediacodec-embed-subtitle-surface.patch"), patches)
    }
}
