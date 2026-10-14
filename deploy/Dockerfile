# Roxy Server image. Multi-stage: build Go binary, then a slim runtime with
# jellyfin-ffmpeg (libass, VAAPI/AMF/QSV/NVENC runtime), Noto CJK fonts, VAAPI drivers.
#
# Build:  docker build -f deploy/Dockerfile -t ghcr.io/roxy-media/roxy-server:dev .
# Run:    see deploy/docker-compose.yml

ARG GO_VERSION=1.23
ARG JELLYFIN_FFMPEG_VERSION=8.1.3-1

FROM golang:${GO_VERSION}-bookworm AS build
WORKDIR /src
COPY server/go.mod server/go.sum ./server/
RUN cd server && go mod download
COPY server ./server
RUN cd server && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/roxy ./cmd/roxy

FROM debian:bookworm-slim AS runtime
ARG JELLYFIN_FFMPEG_VERSION
ARG TARGETARCH
ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates curl wget gosu tzdata \
      fonts-noto-cjk fonts-noto-core \
      libva2 libva-drm2 mesa-va-drivers intel-media-va-driver-non-free \
      libdrm2 libgl1 \
  && curl -fsSL -o /tmp/jellyfin-ffmpeg.deb \
      "https://github.com/jellyfin/jellyfin-ffmpeg/releases/download/v${JELLYFIN_FFMPEG_VERSION}/jellyfin-ffmpeg8_${JELLYFIN_FFMPEG_VERSION}-bookworm_${TARGETARCH}.deb" \
  && apt-get install -y --no-install-recommends /tmp/jellyfin-ffmpeg.deb \
  && rm -rf /tmp/*.deb /var/lib/apt/lists/*
# Asset naming verified against the v8.1.3-1 release (2026-09-27):
#   jellyfin-ffmpeg8_8.1.3-1-bookworm_{amd64,arm64}.deb

COPY --from=build /out/roxy /usr/local/bin/roxy
COPY deploy/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh && mkdir -p /config /data /fonts /transcode

ENV ROXY_CONFIG_DIR=/config \
    ROXY_FFMPEG_PATH=/usr/lib/jellyfin-ffmpeg/ffmpeg \
    ROXY_FFPROBE_PATH=/usr/lib/jellyfin-ffmpeg/ffprobe \
    ROXY_FONTS_DIR=/fonts \
    ROXY_TRANSCODE_DIR=/transcode \
    ROXY_LISTEN=:8096 \
    PUID=1000 PGID=1000

VOLUME ["/config", "/data", "/fonts", "/transcode"]
EXPOSE 8096
HEALTHCHECK --interval=30s --timeout=5s --retries=3 CMD wget -qO- http://127.0.0.1:8096/healthz || exit 1

ENTRYPOINT ["/entrypoint.sh"]
CMD ["roxy", "serve"]
