# Building libmpv-roxy (custom libmpv-android AAR)

Output: `app/libs/libmpv-roxy-1.0.0-pl7.387-p2.aar` (22,728,545 bytes,
sha256 `B5C81686B9BF29632083AD31E03EE820427D378E78C97A0C9BA8F747344B889D`, built 2026-11-23 in the
container, see "Environment"), with Roxy's patches (see "Patches"). Earlier ones are listed under
"History" at the end.

It stands in for `dev.jdtech.mpv:libmpv:1.0.0`: same package `dev.jdtech.mpv`, and the MPVLib API is
that one's plus `attachOsdSurface` / `detachOsdSurface` (wrapper.patch). Two pins changed:

| dep        | 1.0.0    | roxy        | why |
|------------|----------|-------------|-----|
| libplacebo | 7.360.1  | **7.387.0** | GL sync-object leak fix c93aa134 (vo=gpu-next purple/frozen after 15-20 min), AV1 film-grain GLES fix a701ba4 |
| libass     | 0.17.4   | **0.17.5**  | latest release |

## Patches
`tools/libmpv-patches/<dep>/*.patch` are applied to the dependency's sources by the build's own
`patch.sh` (copied to `buildscripts/patches/` first). Two more are applied to libmpv-android itself:
`recipe.patch`, the change to its build scripts (the pins above and the ABI list, the same text as
in "Steps" below), and `wrapper.patch`, an addition to its JNI wrapper. `LibmpvPatchTest` checks the
shipped AAR for each patch. To change one: edit the sources under
`~/build/libmpv/libmpv-android/buildscripts/deps/<dep>`, write `git diff` over the patch file, and
build (the build resets the sources and applies the files).

| patch | what it fixes |
|-------|---------------|
| `ffmpeg/0001-mediacodec-end-of-stream.patch` | FFmpeg's MediaCodec decoder answered "send more" (EAGAIN) after the last packet when a dequeue brought something other than a frame. mpv hands the end of its input over once, so it then waited for ever: the video never ended. Seen on the TCL Google TV (MediaTek `c2.mtk.hevc.decoder`), which reports an output format change with the last frame of an HDR stream: the film froze on its last picture, never closed, and a seek into its last seconds spun for good. Same code in FFmpeg master on 2026-11-23 (not fixed upstream). |
| `mpv/0001-mediacodec-embed-subtitle-surface.patch` | `vo=mediacodec_embed` (the decoder fills the surface itself: what native HDR uses) could show no subtitles. With `--android-osd-wid=<Surface>` it draws mpv's own subtitles, of every kind mpv can show (text, styled with the file's fonts, pictures), on a second surface laid over the video's: the way `vo_dmabuf_wayland` does it (`osd_render` + `mp_draw_sub_overlay`), written to the surface's buffers with `ANativeWindow_lock`, only where something changed. The option is told to the VO at once and waited for, so the surface given before can be released when the set returns. Two small changes outside the VO: a redraw reaches a VO that keeps no frame but has such a layer (`VO_CAP_OSD_LAYER`; subtitles switched while paused), and a track switched on while paused is drawn when its packets arrive, not only when playback goes on. |
| `wrapper.patch` | `MPVLib.attachOsdSurface(Surface)` / `detachOsdSurface()`: hand that surface to mpv and take it back (they set `android-osd-wid`). |

Everything else stays at the 1.0.0 pins: mpv 0.41.0, FFmpeg 8.1, dav1d 1.5.3, fontconfig 2.17.1, harfbuzz 14.1.0,
freetype 2.14.3, fribidi 1.0.16, libxml2 2.15.2, mbedtls 3.6.6, lua 5.2.4, libunibreak 6.1, NDK 29.0.14206865.
ABIs: arm64-v8a and armeabi-v7a only, matching the app's abiFilters.

## Environment
In a container (how the home server builds it since 2026-11-23; it needs only docker and git):
`tools/libmpv-build/build.sh` builds the image (`Dockerfile` beside it: Ubuntu 24.04 with the
packages below), clones libmpv-android v1.0.0 into `~/build/libmpv`, applies `recipe.patch`, copies
the patches and runs the steps below as the calling user. About 6 minutes on 16 cores, downloads
included. The rest of this section is the laptop's setup, which the container copies.

WSL2 Ubuntu-24.04 on the laptop. Build in the Linux filesystem (`~/build`), not `/mnt/c`.

```sh
# as root: wsl -d Ubuntu-24.04 -u root
apt-get update
apt-get install -y openjdk-21-jdk-headless autoconf automake pkg-config libtool ninja-build nasm \
  python3-pip python3-setuptools python3-jinja2 gperf unzip wget git
pip3 install --break-system-packages meson     # got 1.12.1; libplacebo 7.387 needs >= 1.3.0
```
libplacebo's other build deps (glad, jinja, markupsafe, Vulkan-Headers, fast_float) come in as git
submodules, so nothing else is needed. Vulkan is disabled in the libmpv-android build anyway.

### WSL clock gotcha (important)
The Windows clock on this laptop runs ~2h ahead of NTP (the W32Time service is stopped). WSL's
systemd-timesyncd and the Hyper-V host time sync then fight each other, so the VM clock jumps back
and forth by ~2h. meson then aborts with `ERROR: Clock skew detected ... coredata.dat has a time
stamp 7259s in the future` (the first two attempts died at fribidi and then harfbuzz). Fix: stop
timesyncd for the duration of the build, then start it again:
```sh
sudo systemctl stop systemd-timesyncd     # before building
sudo systemctl start systemd-timesyncd    # after
```
(Fixing the Windows clock itself, by starting W32Time and resyncing, would also fix it.)

## Steps (as user drewfoos)
```sh
mkdir -p ~/build && cd ~/build
git clone --depth 1 --branch v1.0.0 https://github.com/jarnedemeulemeester/libmpv-android
cd libmpv-android            # tag v1.0.0 = fcf6745703dc1265bca88f12fee8fc355ddf251e
git apply <<'PATCH'
--- a/buildscripts/include/depinfo.sh
+++ b/buildscripts/include/depinfo.sh
@@ -10,14 +10,14 @@ v_cmake=4.1.2
 
 v_lua=5.2.4
 v_libunibreak=6_1
-v_libass=0.17.4
+v_libass=0.17.5
 v_harfbuzz=14.1.0
 v_fribidi=1.0.16
 v_freetype=2-14-3
 v_libxml2=2.15.2
 v_fontconfig=2.17.1
 v_mbedtls=3.6.6
-v_libplacebo=7.360.1
+v_libplacebo=7.387.0
 v_dav1d=1.5.3
 v_ffmpeg=8.1
 v_mpv=0.41.0
--- a/libmpv/build.gradle.kts
+++ b/libmpv/build.gradle.kts
@@ -15,6 +15,9 @@ android {
 
     defaultConfig {
         minSdk = 26
+        ndk {
+            abiFilters += listOf("arm64-v8a", "armeabi-v7a")
+        }
         consumerProguardFiles("proguard-rules.pro")
         externalNativeBuild {
             cmake {
PATCH

cd buildscripts
IN_CI=1 ./download.sh        # Android cmdline-tools/SDK/NDK r29/cmake into buildscripts/sdk, then dep sources (~5 min)
./patch.sh                   # there is no patches/ dir at v1.0.0, so this does nothing
./build.sh --clean --arch arm64  mpv
./build.sh --clean --arch armv7l mpv
./build.sh -n                # gradle assembleRelease
# -> ~/build/libmpv-android/libmpv/build/outputs/aar/libmpv-release.aar
```
The abiFilters patch is needed: without it Gradle also builds the JNI wrapper for x86/x86_64,
and there is no libmpv.so for those ABIs.

Build time on the laptop (12 threads): ~17 min for both ABIs plus the AAR (1037 s; the Gradle step was 3m47s),
plus ~5 min of downloads.

## Copy to the app and verify
```powershell
Copy-Item \wsl.localhost\<distro>\home\<you>\build\libmpv-android\libmpv\build\outputs\aar\libmpv-release.aar `
  <repo>\apps\android\app\libs\libmpv-roxy-1.0.0-pl7.387-p2.aar
```
```sh
unzip -q libmpv-release.aar -d x
strings x/jni/arm64-v8a/libmpv.so | grep -E '^mpv v|^v7\.|^n8\.'
#   mpv v0.41.0 / v7.387.0 (v7.387.0) / n8.1
grep LIBASS_VERSION buildscripts/prefix/arm64-v8a/include/ass/ass.h   # 0x01705000 = 0.17.5
```
libass doesn't embed a version string. You can check it at runtime with `ass_library_version()`, or in
the build prefix `lib/pkgconfig/libass.pc` (Version: 0.17.5).

API check (Windows): `javap -p -constants -cp classes.jar dev.jdtech.mpv.MPVLib 'dev.jdtech.mpv.MPVLib$*'`
on the old (Gradle cache 1.0.0) and new classes.jar gave 132 lines each with no diff for the first
build; since p2 the new one has `attachOsdSurface`, `detachOsdSurface` and their two native functions
more.

## History
- 2026-11-01, `libmpv-roxy-1.0.0-pl7.387.aar` (22,726,494 bytes, sha256
  `382AF08EF09F5ACFD6C2EA366E60546D4A44B340A7F7F5D4F2AD9CBC3D63A23B`): built on the laptop, no
  patches. App 0.5.2 to 0.6.55.
- 2026-11-23, `...-p1.aar` (22,726,737 bytes, sha256
  `E3D289F5D2813F6085677535AA9ECDE3E8299CAE9DF4C5DD70D5EA6F1EEC1DFA`): that AAR with
  `libavcodec.so` of both ABIs taken from the first container build (ffmpeg/0001). App 0.6.56.
- 2026-11-23, `...-p2.aar`: the container build's AAR whole, with ffmpeg/0001, mpv/0001 and
  wrapper.patch. App 0.6.57. Compared with the first one before shipping (`readelf --dyn-syms`, both
  ABIs, every library): the same exported and imported symbols and the same needed libraries, but
  for what the patches add (`av_gettime_relative` in libavcodec, `ANativeWindow_lock` /
  `ANativeWindow_unlockAndPost` in libmpv, the two JNI functions in libplayer).
