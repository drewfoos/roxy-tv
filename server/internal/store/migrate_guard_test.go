package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
)

// openRespaced builds a database up to just before migration 0034 and rewrites the items
// table's type CHECK as sql (as a hand-rebuilt or imported database might have it).
func openRespaced(t *testing.T, from, to string) string {
	t.Helper()
	ctx := context.Background()
	p := filepath.Join(t.TempDir(), "t.db")
	old, err := openUpTo(p, "0033_sidecar_language_codes.sql")
	if err != nil {
		t.Fatal(err)
	}
	var v int
	if err := old.db.QueryRowContext(ctx, `PRAGMA schema_version`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`PRAGMA writable_schema = ON`,
		fmt.Sprintf(`UPDATE sqlite_master SET sql = replace(sql, '%s', '%s') WHERE name = 'items'`, from, to),
		`PRAGMA writable_schema = OFF`, fmt.Sprintf(`PRAGMA schema_version = %d`, v+1)} {
		if _, err := old.db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	old.Close()
	return p
}

// Found by an audit: migration 0034 widens the items table's type CHECK by replacing its
// exact text; on a table written even slightly differently it did nothing, yet was
// recorded as applied, and every extra was refused from then on. Opening now finds the
// CHECK by pattern and widens it.
func TestExtrasAcceptedWhenItemsTableTextDiffers(t *testing.T) {
	ctx := context.Background()
	p := openRespaced(t, `CHECK (type IN (''movie'', ''series'', ''season'', ''episode''))`, `CHECK (type IN (''movie'',''series'',''season'',''episode''))`)
	s, err := Open(p)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO libraries (id, name, kind, root, created_at) VALUES (7, 'x', 'movies', '/x', 'x')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO items (id, guid, library_id, type, title, date_created, date_modified) VALUES ('e','e',7,'extra','t','x','x')`); err != nil {
		t.Fatalf("an extra was refused: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO items (id, guid, library_id, type, title, date_created, date_modified) VALUES ('b','b',7,'bogus','t','x','x')`); err == nil {
		t.Fatal("the CHECK no longer refuses an unknown type")
	}
	if probs, err := s.CheckIntegrity(ctx); err != nil || len(probs) > 0 {
		// The extra above has no file and no title: those two are expected, nothing else.
		for _, p := range probs {
			if p != "1 films, episodes or extras with no file" && p != "1 extras with no title above them" {
				t.Fatalf("integrity: %v %v", probs, err)
			}
		}
	}
	// A second open finds it already widened and changes nothing.
	s.Close()
	s2, err := Open(p)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	s2.Close()
}

// A table with no type CHECK to widen fails to open, rather than running without extras.
func TestOpenFailsWithoutAnItemsTypeCheck(t *testing.T) {
	p := openRespaced(t, `CHECK (type IN (''movie'', ''series'', ''season'', ''episode''))`, `CHECK (length(type) > 0)`)
	if s, err := Open(p); err == nil {
		s.Close()
		t.Fatal("opened a database whose items table cannot take extras")
	}
}

// Found by an audit: versions were ranked by picture height, labels by width, so a
// 720x576 DVD became the primary version over a 1280x536 widescreen 720p encode, and the
// apps played the one labelled lower. Ranked by width now (migration 0040 re-ranks).
func TestVersionsRankedByWidth(t *testing.T) {
	ctx := context.Background()
	p := filepath.Join(t.TempDir(), "t.db")
	old, err := openUpTo(p, "0039_watched_mark_time.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO libraries (id, name, kind, root, created_at) VALUES (2, 'Movies', 'movies', '/m', 'x')`,
		`INSERT INTO items (id, guid, library_id, type, title, date_created, date_modified) VALUES ('m1', 'g', 2, 'movie', 'Film', 'x', 'x')`,
		`INSERT INTO media_sources (id, item_id, name, is_primary) VALUES ('dvd', 'm1', 'Film - DVD.mkv', 1), ('hd', 'm1', 'Film - 720p.mkv', 0)`,
		`INSERT INTO media_parts (id, source_id, part_index, path, size, mtime) VALUES ('p1','dvd',0,'/m/a.mkv',1,'x'),('p2','hd',0,'/m/b.mkv',1,'x')`,
		`INSERT INTO media_streams (source_id, stream_index, type, codec, width, height) VALUES ('dvd',0,'video','mpeg2video',720,576),('hd',0,'video','h264',1280,536)`,
	} {
		if _, err := old.db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	old.Close()
	s, err := Open(p) // 0040 re-ranks
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	primary := func() string {
		srcs, _ := s.SourcesForItem(ctx, "m1")
		for _, x := range srcs {
			if x.IsPrimary {
				return x.ID
			}
		}
		return ""
	}
	if got := primary(); got != "hd" {
		t.Fatalf("after migration 0040 the primary version is %q, want the 1280-wide hd", got)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE media_sources SET is_primary = CASE id WHEN 'dvd' THEN 1 ELSE 0 END`); err != nil {
		t.Fatal(err)
	}
	if err := s.RankVersions(ctx, "m1"); err != nil {
		t.Fatal(err)
	}
	if got := primary(); got != "hd" {
		t.Fatalf("RankVersions chose %q, want the 1280-wide hd", got)
	}
}
