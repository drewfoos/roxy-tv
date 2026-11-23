#!/bin/bash
# Builds libmpv-android v1.0.0 with Roxy's pins and patches in a container (see
# ../build-libmpv.md). Needs docker and git; nothing is installed on the machine itself.
#
#   tools/libmpv-build/build.sh             the whole recipe: downloads, both ABIs, the AAR
#   tools/libmpv-build/build.sh '<command>' one command in buildscripts/, e.g.
#                                           './build.sh --arch armv7l ffmpeg'
#
# Sources and output: $LIBMPV_WORK (default ~/build/libmpv)/libmpv-android; the AAR lands in
# libmpv/build/outputs/aar/libmpv-release.aar there.
set -euo pipefail
HERE=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
PATCHES=$HERE/../libmpv-patches
W=${LIBMPV_WORK:-$HOME/build/libmpv}
mkdir -p "$W/home"
[ -d "$W/libmpv-android" ] || git clone -q --depth 1 --branch v1.0.0 https://github.com/jarnedemeulemeester/libmpv-android "$W/libmpv-android"
cd "$W/libmpv-android"
# The build scripts as the tag has them, then Roxy's pins and ABI list.
git checkout -q -- buildscripts/include/depinfo.sh libmpv/build.gradle.kts
git apply "$PATCHES/recipe.patch"
# The dependencies' patches, where the build's own patch.sh looks for them.
rm -rf buildscripts/patches && mkdir -p buildscripts/patches
for d in "$PATCHES"/*/; do cp -r "$d" buildscripts/patches/; done
docker build -q -t roxy-libmpv-build "$HERE" >/dev/null
exec docker run --rm -u "$(id -u):$(id -g)" -e HOME=/work/home -e IN_CI=1 -v "$W":/work -w /work/libmpv-android/buildscripts roxy-libmpv-build \
  bash -ec "${*:-./download.sh && ./patch.sh && ./build.sh --clean --arch arm64 mpv && ./build.sh --clean --arch armv7l mpv && ./build.sh -n}"
