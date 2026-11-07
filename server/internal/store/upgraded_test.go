package store

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestUpgradedByNewerBuild checks a database that an older release created (and recorded
// watch history in) after this build has opened it and scanned the library again, forced:
// see scripts/upgrade-from-release.sh, which the CI job "Upgrade from older releases"
// runs. CheckIntegrity must find nothing, and every piece of watch history the old
// release recorded must still belong to an item that has a file: an identity that
// changed (an old release's merged collection films, a trailer taken for a version) must
// have carried its history along. Skipped unless ROXY_UPGRADED_DB names such a database.
func TestUpgradedByNewerBuild(t *testing.T) {
	path := os.Getenv("ROXY_UPGRADED_DB")
	if path == "" {
		if os.Getenv("ROXY_REQUIRE_INTEGRATION") != "" {
			t.Fatal("ROXY_REQUIRE_INTEGRATION is set but ROXY_UPGRADED_DB is not")
		}
		t.Skip("set ROXY_UPGRADED_DB (scripts/upgrade-from-release.sh)")
	}
	ctx := context.Background()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	probs, err := s.CheckIntegrity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range probs {
		lost, _ := s.LostWatchData(ctx)
		t.Errorf("after the upgrade: %s (lost: %s)", p, lost)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT u.item_guid, COALESCE(i.id, ''), COALESCE(i.title, '') FROM user_item_data u
		LEFT JOIN items i ON i.guid = u.item_guid WHERE u.position_ms > 0 OR u.played = 1 OR u.is_favorite = 1`)
	if err != nil {
		t.Fatal(err)
	}
	type row struct{ guid, id, title string }
	var hist []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.guid, &r.id, &r.title); err != nil {
			t.Fatal(err)
		}
		hist = append(hist, r)
	}
	rows.Close()
	if len(hist) == 0 {
		t.Fatal("no watch history to check: the old release's history was not recorded")
	}
	for _, r := range hist {
		if r.id == "" {
			t.Errorf("history for %s belongs to no item", r.guid)
			continue
		}
		if has, err := s.HasMediaUnder(ctx, r.id); err != nil || !has {
			t.Errorf("history for %q (%s) belongs to an item with no file", r.title, r.guid)
		}
	}
	t.Logf("%d pieces of watch history checked", len(hist))
	if exp := os.Getenv("ROXY_UPGRADE_EXPECT"); exp != "" {
		checkExpected(t, s, exp)
	}
}

// checkExpected compares the tester profile's history with where it must be, file by
// file (ROXY_UPGRADE_EXPECT, written by the script): history on the right item, and none
// on a film an old release had merged with another.
func checkExpected(t *testing.T, s *Store, file string) {
	ctx := context.Background()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 5 {
			t.Fatalf("bad expectation %q", line)
		}
		var guid string
		if err := s.db.QueryRowContext(ctx, `SELECT i.guid FROM media_parts mp JOIN media_sources ms ON ms.id = mp.source_id
			JOIN items i ON i.id = ms.item_id WHERE mp.path LIKE ?`, f[1]).Scan(&guid); err != nil {
			t.Errorf("%s: no item has this file: %v", f[1], err)
			continue
		}
		if f[0] == "top" {
			for {
				var parent string
				if s.db.QueryRowContext(ctx, `SELECT p.guid FROM items i JOIN items p ON p.id = i.parent_id WHERE i.guid = ?`, guid).Scan(&parent) != nil {
					break
				}
				guid = parent
			}
		}
		var pos int64
		var played, fav int
		err := s.db.QueryRowContext(ctx, `SELECT position_ms, played, is_favorite FROM user_item_data WHERE scope_id = 'p-up' AND item_guid = ?`, guid).Scan(&pos, &played, &fav)
		if err != nil {
			pos, played, fav = 0, 0, 0
		}
		want := f[2] + "/" + f[3] + "/" + f[4]
		got := strconv.FormatInt(pos, 10) + "/" + strconv.Itoa(played) + "/" + strconv.Itoa(fav)
		if got != want {
			t.Errorf("%s (%s %s): position/watched/favourite = %s, want %s", f[1], f[0], guid, got, want)
		}
	}
}
