# Roxy Server image. Multi-stage: build the Go binary, then a slim runtime with
# jellyfin-ffmpeg, which bundles its own libva, AMD (radeonsi) and Intel (iHD, i965)
# VAAPI drivers and Quick Sync runtime, and loads NVIDIA's NVENC/CUDA libraries that the
# NVIDIA Container Toolkit mounts in at run time. So one image covers every GPU.
#
# Build (from the repo root):  docker build -f deploy/Dockerfile -t roxy-server:dev .
# Run:                         see deploy/docker-compose.yml

ARG GO_VERSION=1.27
ARG JELLYFIN_FFMPEG_VERSION=8.1.3-1

FROM golang:${GO_VERSION}-bookworm AS build
WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server ./
# Pure Go (modernc.org/sqlite), so a static binary runs on any base image.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/roxy ./cmd/roxy

FROM debian:bookworm-slim AS runtime
ARG JELLYFIN_FFMPEG_VERSION
ARG TARGETARCH
ENV DEBIAN_FRONTEND=noninteractive

# Asset naming verified against the v8.1.3-1 release: jellyfin-ffmpeg8_8.1.3-1-bookworm_{amd64,arm64}.deb
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates curl wget gosu tzdata \
  && curl -fsSL -o /tmp/jellyfin-ffmpeg.deb \
      "https://github.com/jellyfin/jellyfin-ffmpeg/releases/download/v${JELLYFIN_FFMPEG_VERSION}/jellyfin-ffmpeg8_${JELLYFIN_FFMPEG_VERSION}-bookworm_${TARGETARCH:-amd64}.deb" \
  && apt-get install -y --no-install-recommends /tmp/jellyfin-ffmpeg.deb \
  && apt-get purge -y curl && apt-get autoremove -y \
  && rm -rf /tmp/*.deb /var/lib/apt/lists/*

COPY --from=build /out/roxy /usr/local/bin/roxy
COPY deploy/entrypoint.sh /entrypoint.sh
RUN sed -i 's/\r$//' /entrypoint.sh && chmod +x /entrypoint.sh && mkdir -p /config /fonts

# /config holds roxy.db and the caches (ROXY_DATA_DIR). Media is mounted wherever you
# like (TRaSH: /data) and listed in ROXY_MEDIA_ROOTS.
ENV ROXY_DATA_DIR=/config \
    ROXY_FFMPEG_PATH=/usr/lib/jellyfin-ffmpeg/ffmpeg \
    ROXY_FFPROBE_PATH=/usr/lib/jellyfin-ffmpeg/ffprobe \
    ROXY_FONTS_DIR=/fonts \
    ROXY_LISTEN=:8096 \
    ROXY_HWACCEL=auto \
    NVIDIA_DRIVER_CAPABILITIES=compute,video,utility \
    PUID=1000 PGID=1000

WORKDIR /config
VOLUME ["/config"]
EXPOSE 8096 8096/udp
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8096/healthz || exit 1

ENTRYPOINT ["/entrypoint.sh"]
CMD ["roxy", "serve"]
