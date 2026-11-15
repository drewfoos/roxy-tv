package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/roxy-media/roxy/server/internal/model"
)

// ffprobe's whole output was kept with every file and never read; on the live library it
// was a third of the database. Migration 0047 removes it and gives the space back.
func TestProbeOutputIsRemovedAndItsSpaceGivenBack(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "roxy.db")
	old, err := openUpTo(path, "0046_request_film_scope.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO libraries (id, name, kind, root, created_at) VALUES (1, 'Movies', 'movies', '/m', 'x')`,
		`INSERT INTO items (id, guid, library_id, type, title, date_created, date_modified) VALUES ('m1', 'path:movie:1', 1, 'movie', 'Film', 'x', 'x')`,
	} {
		if _, err := old.db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	big := strings.Repeat(`{"streams":[{"codec_name":"hevc"}]}`, 600) // ~20 kB, as a real probe is
	for i := 0; i < 200; i++ {
		if _, err := old.db.ExecContext(ctx, `INSERT INTO media_sources (id, item_id, name, is_primary, probe_json) VALUES (?, 'm1', 'v', 0, ?)`, "src"+string(rune('a'+i/26))+string(rune('a'+i%26)), big); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := old.db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	old.Close()
	before := fileSize(t, path)

	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var kept, sources int
	if err := st.db.QueryRowContext(ctx, `SELECT count(probe_json), count(*) FROM media_sources`).Scan(&kept, &sources); err != nil {
		t.Fatal(err)
	}
	if kept != 0 || sources != 200 {
		t.Fatalf("after the upgrade %d of %d sources still hold a probe (want 0 of 200)", kept, sources)
	}
	if _, err := st.db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	if after := fileSize(t, path); after > before/4 {
		t.Fatalf("the database is %d bytes after the upgrade, %d before: the freed space was not given back", after, before)
	}

	// And a new probe does not put it back.
	src := &model.MediaSource{ID: "new", ItemID: "m1", Name: "v2", IsPrimary: true}
	if err := st.ReplaceSource(ctx, src, big); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRowContext(ctx, `SELECT count(probe_json) FROM media_sources`).Scan(&kept); err != nil || kept != 0 {
		t.Fatalf("a probe was stored again: %d rows, err %v", kept, err)
	}
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Size()
}

// The query planner chooses indexes by statistics SQLite only gathers when asked. A
// database that never had any gets them at a start; what a run changed is looked at when
// the store closes.
func TestPlannerStatisticsAreGathered(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "roxy.db")
	stats := func(st *Store, table string) (n int) {
		t.Helper()
		// sqlite_stat1 does not exist until something was analysed.
		if err := st.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE name = 'sqlite_stat1'`).Scan(&n); err != nil || n == 0 {
			return 0
		}
		if err := st.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_stat1 WHERE tbl = ?`, table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `INSERT INTO libraries (id, name, kind, root, created_at) VALUES (1, 'Movies', 'movies', '/m', 'x')`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 500; i++ {
		id := "m" + string(rune('a'+i/26)) + string(rune('a'+i%26))
		if _, err := st.db.ExecContext(ctx, `INSERT INTO items (id, guid, library_id, type, title, date_created, date_modified) VALUES (?, ?, 1, 'movie', 'Film', 'x', 'x')`, id, "g:"+id); err != nil {
			t.Fatal(err)
		}
	}
	// As a database written by a build that never asked: no statistics at all.
	if n := stats(st, "items"); n != 0 {
		t.Fatalf("statistics before anything asked for them: %d", n)
	}
	if err := st.db.Close(); err != nil { // not Store.Close: that build did not ask at close either
		t.Fatal(err)
	}

	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := stats(st, "items"); n == 0 {
		t.Fatal("a database without statistics did not get them at start")
	}
	// A run that changes a table a great deal: Close looks at what was read.
	if _, err := st.db.ExecContext(ctx, `DELETE FROM sqlite_stat1`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `ANALYZE sqlite_schema`); err != nil { // the planner forgets the dropped statistics
		t.Fatal(err)
	}
	var c int
	if err := st.db.QueryRowContext(ctx, `SELECT count(*) FROM items WHERE library_id = 1 AND type = 'movie'`).Scan(&c); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", path) // looked at without the store, which would gather them itself
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	db := &Store{db: raw}
	if n := stats(db, "items"); n == 0 {
		t.Fatal("no statistics for a table the run read")
	}
}
