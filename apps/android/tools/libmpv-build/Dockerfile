# Build environment for Roxy's libmpv (apps/android/tools/build-libmpv.md), as on the
# laptop's WSL Ubuntu 24.04. Used by build.sh beside it, which runs it as the calling user.
FROM ubuntu:24.04
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends \
      openjdk-21-jdk-headless autoconf automake pkg-config libtool ninja-build nasm \
      python3-pip python3-setuptools python3-jinja2 gperf unzip wget git curl ca-certificates \
      make gcc g++ xz-utils bzip2 patch file cmake \
    && pip3 install --break-system-packages meson \
    && rm -rf /var/lib/apt/lists/*
