#!/usr/bin/env bash
# Tests for publish-apk.sh, against a made-up build tree and data folder (no Gradle, no
# real APK). Run: tools/publish-apk-test.sh
set -uo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
work="$(mktemp -d)"
trap 'rm -rf -- "$work"' EXIT
fails=0
fail() { echo "FAIL: $*"; fails=$((fails + 1)); }

tree="$work/android"
out="$tree/app/build/outputs/apk/perf"
data="$work/data"
tv="$data/apps/tv"
mkdir -p "$tree/tools" "$out/baselineProfiles/0" "$out/baselineProfiles/1"
cp "$here/publish-apk.sh" "$tree/tools/"

# gradle <code> <name>: what build.gradle.kts says the version is.
gradle() { printf 'android {\n    defaultConfig {\n        versionCode = %s\n        versionName = "%s"\n    }\n}\n' "$1" "$2" > "$tree/app/build.gradle.kts"; }
# build <code> <name> <content>: what a Gradle build of that version leaves behind.
build() {
  printf '%s' "$3" > "$out/app-perf.apk"
  printf '%s' "$3 profile 31" > "$out/baselineProfiles/0/app-perf.dm"
  printf '%s' "$3 profile 28" > "$out/baselineProfiles/1/app-perf.dm"
  cat > "$out/output-metadata.json" <<META
{"applicationId": "tv.roxy.tv.debug", "variantName": "perf",
 "elements": [{"type": "SINGLE", "versionCode": $1, "versionName": "$2", "outputFile": "app-perf.apk"}],
 "baselineProfiles": [
  {"minApi": 28, "maxApi": 30, "baselineProfiles": ["baselineProfiles/1/app-perf.dm"]},
  {"minApi": 31, "maxApi": 2147483647, "baselineProfiles": ["baselineProfiles/0/app-perf.dm"]}]}
META
}
publish() { bash "$tree/tools/publish-apk.sh" -d "$data" "$@" > "$work/out.log" 2>&1; }
field() { python3 -c 'import json,sys; v=json.load(open(sys.argv[1]))["tv.roxy.tv.debug"]; print(eval(sys.argv[2], {"v": v}))' "$tv/latest.json" "$1"; }
sha() { sha256sum "$1" | cut -d' ' -f1; }

# 1. A build is published whole: the APK, both profiles, and latest.json naming them.
gradle 10 1.0.0; build 10 1.0.0 "build ten"
publish -n "first" || fail "a matching build was refused: $(cat "$work/out.log")"
[ "$(cat "$tv/roxy-tv-1.0.0-perf.apk" 2>/dev/null)" = "build ten" ] || fail "the APK was not published"
[ "$(field 'v["versionCode"]')" = 10 ] && [ "$(field 'v["file"]')" = roxy-tv-1.0.0-perf.apk ] && [ "$(field 'v["notes"]')" = first ] || fail "latest.json does not describe the build"
[ "$(field 'v["sha256"]')" = "$(sha "$tv/roxy-tv-1.0.0-perf.apk")" ] || fail "latest.json's sha256 is not the published APK's"
[ "$(field 'sorted(p["file"] for p in v["profiles"])')" = "['roxy-tv-1.0.0-perf-api28.dm', 'roxy-tv-1.0.0-perf-api31.dm']" ] || fail "profiles: $(field 'v["profiles"]')"
[ "$(field '[p["sha256"] for p in v["profiles"] if p["minApi"] == 28][0]')" = "$(sha "$tv/roxy-tv-1.0.0-perf-api28.dm")" ] || fail "a profile's sha256 is not the published file's"

# 2. The version was bumped but the app not rebuilt: the APK on disk is still build 10.
#    Publishing it as 1.0.1 would offer devices an update they already have.
gradle 11 1.0.1
before="$(sha "$tv/latest.json")"
if publish; then fail "an APK built as 10 was published as 11"; fi
grep -q "Build again" "$work/out.log" || fail "the refusal does not say what to do: $(cat "$work/out.log")"
[ -e "$tv/roxy-tv-1.0.1-perf.apk" ] && fail "the stale APK was copied in under the new version's name"
[ "$(sha "$tv/latest.json")" = "$before" ] || fail "latest.json changed although nothing was published"

# 3. Publishing over a file a device is downloading (the same version, built again) must
#    not change that file under it, nor may latest.json ever be seen half-written: both
#    are replaced, not rewritten in place. A hard link stands in for the open download.
gradle 10 1.0.0; build 10 1.0.0 "build ten, rebuilt"
ln "$tv/roxy-tv-1.0.0-perf.apk" "$work/being-downloaded.apk"
ln "$tv/latest.json" "$work/being-read.json"
old_json="$(sha "$work/being-read.json")"
publish || fail "republishing the same version failed: $(cat "$work/out.log")"
[ "$(cat "$tv/roxy-tv-1.0.0-perf.apk")" = "build ten, rebuilt" ] || fail "the rebuilt APK was not published"
[ "$(cat "$work/being-downloaded.apk")" = "build ten" ] || fail "the APK was rewritten in place: a download in progress got a mix of two builds"
[ "$(sha "$work/being-read.json")" = "$old_json" ] || fail "latest.json was rewritten in place"
[ -z "$(find "$tv" -name '.publishing-*')" ] || fail "work files left behind: $(ls -A "$tv")"

# 4. A publish that fails part-way leaves devices pointed at the previous build.
gradle 12 1.0.2; build 12 1.0.2 "build twelve"
cp "$tv/latest.json" "$work/good.json"
printf '{ not json' > "$tv/latest.json"
if publish; then fail "published over a latest.json it could not read"; fi
[ "$(cat "$tv/latest.json")" = '{ not json' ] || fail "an unreadable latest.json was overwritten"
[ -z "$(find "$tv" -name '.publishing-*')" ] || fail "work files left behind after a failure: $(ls -A "$tv")"
cp "$work/good.json" "$tv/latest.json"

# 5. Old builds do not pile up: the newest few stay, and so does whatever latest.json
#    names for another app id, and anything that is not a published build.
rm -rf -- "$tv"; mkdir -p "$tv"
printf 'release build' > "$tv/roxy-tv-0.1.0-release.apk"
printf '{"tv.roxy.tv": {"versionCode": 1, "versionName": "0.1.0", "file": "roxy-tv-0.1.0-release.apk", "profiles": []}}' > "$tv/latest.json"
printf 'mine' > "$tv/notes.txt"
touch -d @1000000000 "$tv/roxy-tv-0.1.0-release.apk" "$tv/notes.txt"
for i in 1 2 3 4 5 6 7; do
  gradle "$((20 + i))" "2.0.$i"; build "$((20 + i))" "2.0.$i" "build $i"
  publish -k 3 || fail "publishing 2.0.$i failed: $(cat "$work/out.log")"
  touch -d "@$((1700000000 + i * 60))" "$tv"/roxy-tv-2.0.$i-perf*
done
gradle 28 2.0.8; build 28 2.0.8 "build 8"
publish -k 3 || fail "publishing 2.0.8 failed: $(cat "$work/out.log")"
want="latest.json notes.txt roxy-tv-0.1.0-release.apk"
for i in 6 7 8; do want="$want roxy-tv-2.0.$i-perf-api28.dm roxy-tv-2.0.$i-perf-api31.dm roxy-tv-2.0.$i-perf.apk"; done
got="$(ls "$tv" | sort | tr '\n' ' ' | sed 's/ $//')"
[ "$got" = "$(echo $want | tr ' ' '\n' | sort | tr '\n' ' ' | sed 's/ $//')" ] || fail "after 8 publishes keeping 3, the folder holds: $got"
[ "$(field 'v["file"]')" = roxy-tv-2.0.8-perf.apk ] || fail "latest.json names $(field 'v["file"]')"
# -k 0 keeps everything.
gradle 29 2.0.9; build 29 2.0.9 "build 9"
publish -k 0 || fail "publishing 2.0.9 failed"
[ -e "$tv/roxy-tv-2.0.6-perf.apk" ] && [ -e "$tv/roxy-tv-2.0.9-perf.apk" ] || fail "-k 0 removed a build"

if [ "$fails" -gt 0 ]; then echo "$fails check(s) failed"; exit 1; fi
echo "publish-apk.sh: all checks passed"
