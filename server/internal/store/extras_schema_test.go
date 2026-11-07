package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// After migration 0034 the items table takes type 'extra' (and still refuses anything
// else), keeps every column added before it, and passes SQLite's integrity check.
func TestExtrasMigration(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	var ic string
	if err := s.db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&ic); err != nil || ic != "ok" {
		t.Fatalf("integrity: %q %v", ic, err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO libraries (id, name, kind, root, created_at) VALUES (7, 'x', 'movies', '/x', 'x')`); err != nil {
		t.Fatal(err)
	}
	ins := func(id, typ string) error {
		_, err := s.db.ExecContext(ctx, `INSERT INTO items (id, guid, library_id, type, title, date_created, date_modified, airs_after_season, extra_type)
			VALUES (?, ?, 7, ?, 't', 'x', 'x', 1, 'trailer')`, id, id, typ)
		return err
	}
	if err := ins("e", "extra"); err != nil {
		t.Fatalf("extra refused: %v", err)
	}
	if err := ins("b", "bogus"); err == nil || !strings.Contains(err.Error(), "CHECK") {
		t.Fatalf("bogus type accepted: %v", err)
	}
}
