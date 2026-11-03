#!/usr/bin/env bash
# Deploy Roxy Server on pookiebox from the git checkout (~/roxy-src).
#
# The web app is built into server/internal/web/dist (git-ignored, embedded by go:embed),
# then ~/roxy's docker compose rebuilds the image from this checkout. Live data stays in
# ~/roxy/config; nothing here touches it.
#
#   deploy/pookiebox-deploy.sh            # build the web app, rebuild and restart Roxy
#   deploy/pookiebox-deploy.sh --pull     # git pull first
set -euo pipefail

src="$(cd "$(dirname "$0")/.." && pwd)"
live="${ROXY_LIVE_DIR:-$HOME/roxy}"

if [ "${1:-}" = "--pull" ]; then
  git -C "$src" pull --ff-only
fi

echo "== web app"
cd "$src/apps/web"
[ -d node_modules ] || npm ci --silent
npm run -s build

echo "== server tests"
cd "$src/server"
go vet ./...
go test ./... >/tmp/roxy-deploy-tests.log 2>&1 || { tail -40 /tmp/roxy-deploy-tests.log; echo "tests failed; not deploying" >&2; exit 1; }

echo "== rebuild and restart ($live, built from $src @ $(git -C "$src" rev-parse --short HEAD))"
cd "$live"
docker compose up -d --build
for _ in $(seq 1 30); do
  if curl -fsS http://127.0.0.1:8097/healthz >/dev/null 2>&1; then
    echo "Roxy is up."
    exit 0
  fi
  sleep 2
done
echo "Roxy did not answer /healthz within a minute: docker logs --tail 50 roxy" >&2
exit 1
