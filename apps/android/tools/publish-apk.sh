#!/usr/bin/env bash
# Publish the built Roxy TV APK to Roxy Server so phones and TVs offer the update.
# Bash twin of publish-apk.ps1, for building on the server itself.
#
# Copies app/build/outputs/apk/<variant>/app-<variant>.apk into <data dir>/apps/tv/ and
# updates latest.json there (keyed by applicationId). The build's compilation profiles
# (baselineProfiles/*/*.dm, one per range of Android versions) go with it: the app
# installs the one for its Android version together with the APK, so an update runs
# compiled from its first launch instead of stuttering until the device compiles it. The server serves it at
# GET /v1/apps/tv/latest and /v1/apps/tv/download; the app compares versionCode with its own.
#
# The default is the perf build (./gradlew assemblePerf): the debug app's id, key and
# sign-in, built the release way. A plain debug build is debuggable, so Android never
# compiles it ahead of time and the TV feels laggy; do not ship that one.
#
#   tools/publish-apk.sh                      # perf build into ~/roxy/config (pookiebox)
#   tools/publish-apk.sh -n "Downloads fix"   # with release notes
#   tools/publish-apk.sh -v release -d /path/to/roxy/config
#   tools/publish-apk.sh -k 10                # keep the 10 newest builds (default 5; 0 = all)
#
# It refuses to publish when the APK on disk was not built from the version build.gradle.kts
# now names (a version bump without a rebuild): devices would be offered "the new version"
# and handed the old build, which they cannot install over itself. Files appear under
# their final names only when complete, and latest.json is replaced last, so a device
# never downloads half a file or is pointed at one that is not there yet. Older builds
# than the newest few are removed (each is ~100 MB and nothing asks for them again).
set -euo pipefail

variant=perf
notes=""
keep=5
data_dir="${ROXY_DATA_DIR:-$HOME/roxy/config}"
while getopts "v:d:n:k:" o; do
  case "$o" in
    v) variant="$OPTARG" ;;
    d) data_dir="$OPTARG" ;;
    n) notes="$OPTARG" ;;
    k) keep="$OPTARG" ;;
    *) echo "usage: $0 [-v perf|debug|release] [-d data dir] [-n notes] [-k builds to keep]" >&2; exit 2 ;;
  esac
done
case "$variant" in perf|debug|release) ;; *) echo "variant must be perf, debug or release" >&2; exit 2 ;; esac
case "$keep" in ''|*[!0-9]*) echo "-k takes a number" >&2; exit 2 ;; esac

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

python3 - "$dir" "$app_id" "$code" "$name" "$file" "$notes" "$apk" "$variant" "$keep" <<'EOF'
import json, os, re, shutil, sys, datetime, hashlib
dir, app_id, code, name, file, notes, apk, variant, keep = sys.argv[1:]
out_dir = os.path.dirname(apk)
path = os.path.join(dir, "latest.json")

def sha(p):
    h = hashlib.sha256()
    with open(p, "rb") as f:
        for block in iter(lambda: f.read(1 << 20), b""):
            h.update(block)
    return h.hexdigest()

# What the APK on disk really is, from the build's output-metadata.json. Without that
# file there is nothing to check against (and no profiles): the APK goes out alone.
meta = None
meta_path = os.path.join(out_dir, "output-metadata.json")
if os.path.exists(meta_path):
    with open(meta_path, encoding="utf-8") as f:
        meta = json.load(f)
    built = [e.get("versionCode") for e in meta.get("elements", [])]
    if built != [int(code)]:
        sys.exit(f"refusing to publish: {apk} was built as versionCode {built}, but build.gradle.kts now says {code} ({name}). Build again, then publish.")
else:
    print("warning: no output-metadata.json beside the APK; cannot check its version, publishing without profiles", file=sys.stderr)

temps = []
def put(src, dest_name):
    """Copy src into the folder under dest_name, appearing only when complete."""
    tmp = os.path.join(dir, ".publishing-" + dest_name)
    temps.append(tmp)
    shutil.copyfile(src, tmp)
    digest = sha(tmp)
    os.replace(tmp, os.path.join(dir, dest_name))
    return digest  # of what was written: the app checks its download against this

try:
    sha256 = put(apk, file)
    profiles = []
    for group in (meta or {}).get("baselineProfiles", []):
        for rel in group.get("baselineProfiles", []):
            src = os.path.join(out_dir, rel)
            if not os.path.isfile(src):
                continue
            dm = f"roxy-tv-{name}-{variant}-api{group['minApi']}.dm"
            profiles.append({"minApi": group["minApi"], "maxApi": group["maxApi"], "file": dm, "sha256": put(src, dm)})
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
        "profiles": profiles,
        "publishedAt": datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
    }
    # Last, and in one step: until now devices are still pointed at the previous build.
    tmp = os.path.join(dir, ".publishing-latest.json")
    temps.append(tmp)
    with open(tmp, "w", encoding="utf-8") as f:
        json.dump(latest, f, indent=4)
        f.flush()
        os.fsync(f.fileno())
    os.replace(tmp, path)
finally:
    for t in temps:
        if os.path.exists(t):
            os.remove(t)

# Older builds: keep what latest.json names (for every app id) and the newest few.
keep = int(keep)
if keep > 0:
    built_file = re.compile(r"^(roxy-tv-.+-(?:perf|debug|release))(?:-api\d+\.dm|\.apk)$")
    builds = {}
    for entry in os.listdir(dir):
        m = built_file.match(entry)
        if m and os.path.isfile(os.path.join(dir, entry)):
            builds.setdefault(m.group(1), []).append(entry)
    named = set()
    for v in latest.values():
        if isinstance(v, dict):
            named.add(v.get("file"))
            named.update(p.get("file") for p in v.get("profiles") or [])
    newest = sorted(builds, key=lambda b: max(os.path.getmtime(os.path.join(dir, e)) for e in builds[b]), reverse=True)
    removed = 0
    for build in newest[keep:]:
        if named.intersection(builds[build]):
            continue
        for entry in builds[build]:
            os.remove(os.path.join(dir, entry))
        removed += 1
    if removed:
        print(f"Removed {removed} older build(s); the newest {keep} are kept (-k to change)")
EOF
echo "Published $app_id $name ($code) -> $dir/$file (+ $(python3 -c 'import json,sys; print(len(json.load(open(sys.argv[1]))[sys.argv[2]]["profiles"]))' "$dir/latest.json" "$app_id") profiles)"
