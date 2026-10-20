#!/bin/sh
# Create the roxy user with the host's PUID/PGID so /config and /data ownership matches,
# add it to whatever groups own the /dev/dri nodes (video, render; the GIDs differ per
# host, so they are read from the devices rather than named), then drop privileges.
# NVIDIA needs nothing here: the toolkit's /dev/nvidia* nodes are world-accessible.
set -e

PUID="${PUID:-1000}"
PGID="${PGID:-1000}"

if ! getent group roxy >/dev/null; then groupadd -g "$PGID" roxy; fi
if ! id roxy >/dev/null 2>&1; then useradd -u "$PUID" -g "$PGID" -M -s /usr/sbin/nologin roxy; fi

for dev in /dev/dri/*; do
  [ -e "$dev" ] || continue
  gid="$(stat -c %g "$dev")"
  if ! getent group "$gid" >/dev/null; then groupadd -g "$gid" "dri$gid"; fi
  usermod -aG "$gid" roxy
done

chown -R roxy:roxy /config 2>/dev/null || true

exec gosu roxy "$@"
