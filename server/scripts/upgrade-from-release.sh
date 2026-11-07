#!/usr/bin/env bash
# Upgrade a database made by an older release, the way a real upgrade happens.
#
#   scripts/upgrade-from-release.sh <checkout of the old release> [work dir]
#
# 1. builds the old release's server from its checkout, and this checkout's;
# 2. makes a small library of real (tiny) videos with ffmpeg, in the shapes that old
#    releases read differently (a film collection folder, a film with versions and a
#    trailer, an extras folder, a multi-episode file, specials, an absolute-numbered
#    anime file, a sidecar subtitle, a show in a grouping folder, a date-named episode);
# 3. has the OLD server scan it, and records watch history in its database (a profile
#    with a position, watched marks and a watchlist entry);
# 4. has THIS server open that database (its migrations run) and rescan, forced;
# 5. runs TestUpgradedByNewerBuild on the result: no broken invariants, and every piece
#    of history still on an item that has a file.
# Needs go, ffmpeg and python3. Run from the server folder of this checkout.
set -euo pipefail

OLD_SRC=$(cd "$1" && pwd)
WORK=${2:-$(mktemp -d)}
NEW_SRC=$(pwd)
mkdir -p "$WORK"
echo "work dir: $WORK"

echo "== building"
(cd "$OLD_SRC/server" && CGO_ENABLED=0 go build -buildvcs=false -o "$WORK/roxy-old" ./cmd/roxy)
(cd "$NEW_SRC" && CGO_ENABLED=0 go build -buildvcs=false -o "$WORK/roxy-new" ./cmd/roxy)

echo "== making the library"
M="$WORK/media"
rm -rf "$M" "$WORK/data"
n=0
video() {
	n=$((n + 1))
	mkdir -p "$(dirname "$1")"
	ffmpeg -v error -y -f lavfi -i "testsrc=duration=$((1 + n % 5)):size=320x240:rate=10" \
		-f lavfi -i "sine=frequency=$((200 + n * 13)):duration=$((1 + n % 5))" \
		-c:v libx264 -preset ultrafast -c:a aac -shortest -f matroska "$1"
}
A="$M/Anime/Show A (2020)/Season 01"
for e in 01 02 03; do video "$A/Show A - S01E$e.mkv"; done
printf '1\n00:00:00,500 --> 00:00:01,500\nhello\n' > "$A/Show A - S01E01.en.srt"
video "$M/Anime/Show A (2020)/Season 01/extras/Show A - Opening.mkv"
video "$M/Anime/Show A (2020)/Specials/Show A - S00E01 - OVA.mkv"
video "$M/Anime/Show B (2019)/Season 01/Show B - S01E01-E02 - Two Parts.mkv"
video "$M/Anime/Show C/[Group] Show C - 05 [1080p].mkv"
# Read differently by newer releases, so these get new identities on upgrade and their
# history must follow: a show inside a grouping folder (old: the show was "Kids"), and a
# daily show's episode named by its date (old: a film, by the year in the date).
video "$M/Anime/Kids/Bluey/Season 01/Bluey - S01E01.mkv"
video "$M/Anime/The Daily Show/The Daily Show 2024.03.15.mkv"
video "$M/Movies/Collection/Film One (2001).mkv"
video "$M/Movies/Collection/Film Two (2002).mkv"
video "$M/Movies/Film Three (2003)/Film Three (2003) - 1080p.mkv"
video "$M/Movies/Film Three (2003)/Film Three (2003) - 720p.mkv"
video "$M/Movies/Film Three (2003)/Film Three (2003)-trailer.mkv"

export ROXY_MEDIA_ROOTS="$M/Anime,$M/Movies" ROXY_DATA_DIR="$WORK/data" ROXY_HWACCEL=none

echo "== the old release scans"
"$WORK/roxy-old" scan

echo "== recording watch history in the old database"
python3 - "$WORK/data/roxy.db" <<'PY'
import sqlite3, sys
db = sqlite3.connect(sys.argv[1])
db.execute("INSERT INTO users (id, name, created_at) VALUES ('u-up', 'tester', '2026-01-01T00:00:00.000Z')")
db.execute("INSERT INTO profiles (id, user_id, name, created_at) VALUES ('p-up', 'u-up', 'tester', '2026-01-01T00:00:00.000Z')")
def guid(like):
    r = db.execute("SELECT i.guid FROM media_parts mp JOIN media_sources ms ON ms.id = mp.source_id JOIN items i ON i.id = ms.item_id WHERE mp.path LIKE ?", (like,)).fetchone()
    if not r:
        sys.exit("not recorded by the old release: " + like)
    return r[0]
def top(like):
    g = guid(like)
    while True:
        r = db.execute("SELECT p.guid FROM items i JOIN items p ON p.id = i.parent_id WHERE i.guid = ?", (g,)).fetchone()
        if not r:
            return g
        g = r[0]
rows = [
    (guid('%Show A - S01E02.mkv'), 1500, 0, 0),
    (guid('%Film One (2001).mkv'), 0, 1, 0),
    (guid('%Film Three (2003) - 1080p.mkv'), 900, 0, 0),
    (guid('%Two Parts.mkv'), 0, 1, 0),
    (top('%Show A - S01E01.mkv'), 0, 0, 1),
    (guid('%Bluey - S01E01.mkv'), 0, 1, 0),
    (guid('%The Daily Show 2024.03.15.mkv'), 2000, 0, 0),
]
for g, pos, played, fav in rows:
    db.execute("INSERT OR REPLACE INTO user_item_data (scope_id, item_guid, position_ms, played, is_favorite, last_played_at) VALUES ('p-up', ?, ?, ?, ?, '2026-01-02T00:00:00.000Z')", (g, pos, played, fav))
db.commit()
print(len(rows), "pieces of history recorded")
PY

# Where each piece must be after the upgrade, by file: level (the file's item, or the top
# of its tree), path pattern, position, watched, favourite. Film Two shared Film One's
# item in old releases and must come out with no history of its own.
cat > "$WORK/expect.tsv" <<'TSV'
item	%Show A - S01E02.mkv	1500	0	0
item	%Film One (2001).mkv	0	1	0
item	%Film Two (2002).mkv	0	0	0
item	%Film Three (2003) - 1080p.mkv	900	0	0
item	%Two Parts.mkv	0	1	0
top	%Show A - S01E01.mkv	0	0	1
item	%Show A - S01E01.mkv	0	0	0
item	%Bluey - S01E01.mkv	0	1	0
item	%The Daily Show 2024.03.15.mkv	2000	0	0
TSV

echo "== this build opens it and rescans, forced"
"$WORK/roxy-new" scan --force

echo "== checking"
ROXY_UPGRADED_DB="$WORK/data/roxy.db" ROXY_UPGRADE_EXPECT="$WORK/expect.tsv" go test -count=1 -v -run TestUpgradedByNewerBuild ./internal/store
