package store

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// TestRestoreDrill opens a backup with this build, as a restore would: every pending
// migration is applied, no table loses rows, SQLite's checks pass, CheckIntegrity finds
// nothing, and no watch history ends up shown by no item that was shown before (a real
// library keeps the history of files deleted for good; that much may already be there).
//
// Give it a backup archive as roxy-backup.sh writes them (roxy.db is extracted from it,
// so the archive itself is what is tested), or a copy of a database file (it is changed):
//
//	ROXY_RESTORE_TARBALL=~/backups/roxy/roxy-....tar.gz go test ./internal/store -run RestoreDrill -v
//	ROXY_RESTORE_COPY=/tmp/drill.db go test ./internal/store -run RestoreDrill -v
func TestRestoreDrill(t *testing.T) {
	path := os.Getenv("ROXY_RESTORE_COPY")
	if tb := os.Getenv("ROXY_RESTORE_TARBALL"); tb != "" {
		path = extractDB(t, tb)
	}
	if path == "" {
		t.Skip("set ROXY_RESTORE_TARBALL to a backup archive, or ROXY_RESTORE_COPY to a copy of a database")
	}
	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	before := tableCounts(t, raw)
	lostBefore := lostHistory(t, raw)
	var applied int
	if err := raw.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&applied); err != nil {
		t.Fatalf("schema_migrations: %v", err)
	}
	raw.Close()

	s, err := Open(path) // applies the pending migrations
	if err != nil {
		t.Fatalf("opening the restored database: %v", err)
	}
	defer s.Close()
	after := tableCounts(t, s.db)
	var now int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&now); err != nil {
		t.Fatalf("schema_migrations: %v", err)
	}
	t.Logf("migrations: %d before, %d after", applied, now)
	for tb, n := range before {
		t.Logf("%-22s %6d -> %6d", tb, n, after[tb])
		if a, ok := after[tb]; !ok {
			t.Errorf("table %s is gone", tb)
		} else if a < n {
			t.Errorf("%s lost rows: %d -> %d", tb, n, a)
		}
	}
	probs, err := s.CheckIntegrity(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range probs {
		if strings.Contains(p, "watch history no item shows") {
			continue // compared with what was there before, below
		}
		t.Errorf("after restore: %s", p)
	}
	for g := range lostHistory(t, s.db) {
		if !lostBefore[g] {
			t.Errorf("history for %s is shown by no item since the restore", g)
		}
	}
	t.Logf("history shown by no item: %d (as before)", len(lostBefore))
}

// extractDB writes the roxy.db inside a backup archive to a temporary file.
func extractDB(t *testing.T, archive string) string {
	t.Helper()
	f, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("%s: %v", archive, err)
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			t.Fatalf("%s holds no roxy.db", archive)
		}
		if err != nil {
			t.Fatalf("%s: %v", archive, err)
		}
		if filepath.Clean(h.Name) != "roxy.db" {
			continue
		}
		out := filepath.Join(t.TempDir(), "roxy.db")
		w, err := os.Create(out)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(w, tr); err != nil {
			t.Fatalf("extracting roxy.db: %v", err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		return out
	}
}

// tableCounts is every table's row count.
func tableCounts(t *testing.T, db *sql.DB) map[string]int {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	out := map[string]int{}
	for _, n := range names {
		var c int
		if err := db.QueryRow(`SELECT COUNT(*) FROM "` + n + `"`).Scan(&c); err != nil {
			t.Fatalf("%s: %v", n, err)
		}
		out[n] = c
	}
	return out
}

// lostHistory is the identities whose watch history no item shows.
func lostHistory(t *testing.T, db *sql.DB) map[string]bool {
	t.Helper()
	rows, err := db.Query(`SELECT DISTINCT item_guid FROM user_item_data u
		WHERE (u.position_ms > 0 OR u.played = 1 OR u.is_favorite = 1)
		AND NOT EXISTS (SELECT 1 FROM items i WHERE i.guid = u.item_guid)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			t.Fatal(err)
		}
		out[g] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}
