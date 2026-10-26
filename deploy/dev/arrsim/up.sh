#!/bin/bash
# Start the arr test bench: real Sonarr + Radarr (Docker) wired to arrsim, a fake
# indexer and download client (no internet downloads). Run as root in WSL / Linux.
#   MEDIA=/mnt/c/.../roxy/testdata/out deploy/dev/arrsim/up.sh
set -e
HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/../../.." && pwd)"
export ARRSIM_HOME="${ARRSIM_HOME:-/srv/arrsim}"
export MEDIA="${MEDIA:-$ROOT/testdata/out}"
mkdir -p "$ARRSIM_HOME"/{sonarr,radarr,downloads}
chown -R 1000:1000 "$ARRSIM_HOME"

# A 25-minute, tiny test video every fake download copies in (long enough that the arr
# apps do not treat it as a sample).
if [ ! -f "$ARRSIM_HOME/sample.mkv" ]; then
  /usr/lib/jellyfin-ffmpeg/ffmpeg -hide_banner -loglevel error -f lavfi -i "testsrc2=s=320x180:r=10:d=1500" \
    -f lavfi -i "sine=f=440:d=1500" -c:v libx264 -preset ultrafast -crf 45 -g 300 -c:a aac -b:a 16k \
    -shortest "$ARRSIM_HOME/sample.mkv"
fi

docker compose -f "$HERE/docker-compose.yml" up -d

# arrsim itself (built from the server module), restarted with the apps' keys.
cd "$ROOT/server"
export PATH=$PATH:/usr/local/go/bin
go build -buildvcs=false -o "$ARRSIM_HOME/arrsim" ./cmd/arrsim
chown -R 1000:1000 "$ARRSIM_HOME/downloads"
# arrsim runs as uid 1000 like the containers, so Sonarr / Radarr may move what it writes.
# Its downloads live in memory: restarting it drops whatever was still "downloading".
run_arrsim() {
  pkill -x arrsim || true
  nohup runuser -u "$(id -un 1000)" -- "$ARRSIM_HOME/arrsim" -listen :8099 -downloads "$ARRSIM_HOME/downloads" \
    -report-dir /downloads -video "$ARRSIM_HOME/sample.mkv" "$@" >>"$ARRSIM_HOME/arrsim.log" 2>&1 &
  sleep 1
}
run_arrsim
python3 "$HERE/setup.py" --home "$ARRSIM_HOME" | tee "$ARRSIM_HOME/setup.out"
SK=$(sed -n 's/^SONARR_KEY=//p' "$ARRSIM_HOME/setup.out")
RK=$(sed -n 's/^RADARR_KEY=//p' "$ARRSIM_HOME/setup.out")
run_arrsim -sonarr http://localhost:8989 -sonarr-key "$SK" -radarr http://localhost:7878 -radarr-key "$RK"
echo "arrsim bench up: Sonarr http://localhost:8989  Radarr http://localhost:7878  arrsim :8099 (log $ARRSIM_HOME/arrsim.log)"
