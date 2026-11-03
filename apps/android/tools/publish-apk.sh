#!/usr/bin/env bash
# Publish the built Roxy TV APK to Roxy Server so phones and TVs offer the update.
# Bash twin of publish-apk.ps1, for building on the server itself.
#
# Copies app/build/outputs/apk/<variant>/app-<variant>.apk into <data dir>/apps/tv/ and
# updates latest.json there (keyed by applicationId). The server serves it at
# GET /v1/apps/tv/latest and /v1/apps/tv/download; the app compares versionCode with its own.
#
# The default is the perf build (./gradlew assemblePerf): the debug app's id, key and
# sign-in, built the release way. A plain debug build is debuggable, so Android never
# compiles it ahead of time and the TV feels laggy; do not ship that one.
#
#   tools/publish-apk.sh                      # perf build into ~/roxy/config (pookiebox)
#   tools/publish-apk.sh -n "Downloads fix"   # with release notes
#   tools/publish-apk.sh -v release -d /path/to/roxy/config
set -euo pipefail

variant=perf
notes=""
data_dir="${ROXY_DATA_DIR:-$HOME/roxy/config}"
while getopts "v:d:n:" o; do
  case "$o" in
    v) variant="$OPTARG" ;;
    d) data_dir="$OPTARG" ;;
    n) notes="$OPTARG" ;;
    *) echo "usage: $0 [-v perf|debug|release] [-d data dir] [-n notes]" >&2; exit 2 ;;
  esac
done
case "$variant" in perf|debug|release) ;; *) echo "variant must be perf, debug or release" >&2; exit 2 ;; esac

root="$(cd "$(dirname "$0")/.." && pwd)"
gradle="$root/app/build.gradle.kts"
code=$(grep -oE 'versionCode *= *[0-9]+' "$gradle" | grep -oE '[0-9]+$')
name=$(grep -oE 'versionName *= *"[^"]+"' "$gradle" | sed -E 's/.*"([^"]+)"/\1/')
app_id="tv.roxy.tv"; [ "$variant" = release ] || app_id="tv.roxy.tv.debug"
apk="$root/app/build/outputs/apk/$variant/app-$variant.apk"
[ -f "$apk" ] || { echo "APK not found: $apk (build first)" >&2; exit 1; }

dir="$data_dir/apps/tv"
mkdir -p "$dir"
file="roxy-tv-$name-$variant.apk"
cp "$apk" "$dir/$file"

python3 - "$dir/latest.json" "$app_id" "$code" "$name" "$file" "$notes" <<'EOF'
import json, os, sys, datetime, hashlib
path, app_id, code, name, file, notes = sys.argv[1:]
with open(os.path.join(os.path.dirname(path), file), "rb") as f:
    sha256 = hashlib.sha256(f.read()).hexdigest()  # the app checks its download against this
latest = {}
if os.path.exists(path):
    with open(path, encoding="utf-8-sig") as f:  # the PowerShell version may have left a BOM
        latest = json.load(f)
latest[app_id] = {
    "versionCode": int(code),
    "versionName": name,
    "file": file,
    "notes": notes,
    "sha256": sha256,
    "publishedAt": datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
}
with open(path, "w", encoding="utf-8") as f:
    json.dump(latest, f, indent=4)
EOF
echo "Published $app_id $name ($code) -> $dir/$file"
