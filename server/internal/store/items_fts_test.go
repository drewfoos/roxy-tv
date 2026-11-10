package store

import (
	"testing"

	"github.com/roxy-media/roxy/server/internal/model"
)

// Nothing reads items_fts (search is a LIKE on items), and nothing kept it in step with
// renames and removals, so new items no longer write to it and the upgrade empties it.
func TestItemsWriteNoUnusedSearchIndex(t *testing.T) {
	s, lib, ctx := openTest(t)
	indexed := func() int {
		t.Helper()
		var n int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM items_fts_docsize`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if err := s.UpsertItem(ctx, &model.Item{ID: "m1", GUID: "g:m1", LibraryID: lib.ID, Type: model.TypeMovie, Title: "A Film"}); err != nil {
		t.Fatal(err)
	}
	if n := indexed(); n != 0 {
		t.Errorf("a new item wrote %d rows to the search index nobody reads", n)
	}

	// A database from before: titles in the index. Running the migration again empties it
	// and leaves the table, so an older build can still add items.
	if _, err := s.db.ExecContext(ctx, `INSERT INTO items_fts (rowid, title) SELECT rowid, title FROM items`); err != nil {
		t.Fatal(err)
	}
	if indexed() != 1 {
		t.Fatal("the test could not fill the index")
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version = '0044_empty_items_fts.sql'`); err != nil {
		t.Fatal(err)
	}
	if err := s.migrate(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if n := indexed(); n != 0 {
		t.Errorf("the upgrade left %d rows in the index", n)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO items_fts (rowid, title) SELECT rowid, title FROM items WHERE id = 'm1'`); err != nil {
		t.Errorf("an older build's write to the index fails after the upgrade: %v", err)
	}
}
