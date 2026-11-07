package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/roxy-media/roxy/server/internal/model"
)

// A sort title survives a re-ingest that supplies none (the scanner never does), and is
// still replaced by one that does. A rescan used to clear it, so "Monogatari 01" sorted
// under its display title again.
func TestRescanKeepsSortTitle(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	lib := &model.Library{Name: "Anime", Kind: model.KindSeries, Root: t.TempDir()}
	if err := s.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	upsert := func(sortTitle string) {
		t.Helper()
		it := &model.Item{ID: "s1", GUID: "series:bakemonogatari", LibraryID: lib.ID, Type: model.TypeSeries, Title: "Bakemonogatari", SortTitle: sortTitle}
		if err := s.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	sortTitle := func() string {
		t.Helper()
		var v *string
		if err := s.db.QueryRowContext(ctx, `SELECT sort_title FROM items WHERE id = 's1'`).Scan(&v); err != nil {
			t.Fatal(err)
		}
		if v == nil {
			return "<null>"
		}
		return *v
	}
	upsert("")
	// Set by hand (an edit, or a future metadata source).
	if _, err := s.db.ExecContext(ctx, `UPDATE items SET sort_title = 'Monogatari 01' WHERE id = 's1'`); err != nil {
		t.Fatal(err)
	}
	upsert("") // a rescan
	if got := sortTitle(); got != "Monogatari 01" {
		t.Fatalf("after a rescan: %q", got)
	}
	upsert("Monogatari 01a") // one that does supply a sort title still replaces it
	if got := sortTitle(); got != "Monogatari 01a" {
		t.Fatalf("after an upsert with a sort title: %q", got)
	}
}

// UpdateItemMetadata: a nil sort title (online providers) keeps the stored one, a value
// sets it, "" clears it.
func TestMetadataSortTitle(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	lib := &model.Library{Name: "Anime", Kind: model.KindSeries, Root: t.TempDir()}
	if err := s.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertItem(ctx, &model.Item{ID: "s1", GUID: "g", LibraryID: lib.ID, Type: model.TypeSeries, Title: "Bakemonogatari"}); err != nil {
		t.Fatal(err)
	}
	sortTitle := func() string {
		t.Helper()
		it, err := s.GetItem(ctx, "s1")
		if err != nil || it == nil {
			t.Fatal(it, err)
		}
		return it.SortTitle
	}
	str := func(v string) *string { return &v }
	steps := []struct {
		set  *string
		want string
	}{{str("Monogatari 01"), "Monogatari 01"}, {nil, "Monogatari 01"}, {str(""), ""}}
	for i, st := range steps {
		if err := s.UpdateItemMetadata(ctx, "s1", ItemMetadata{Title: "Bakemonogatari", SortTitle: st.set}); err != nil {
			t.Fatal(err)
		}
		if got := sortTitle(); got != st.want {
			t.Fatalf("step %d: %q, want %q", i, got, st.want)
		}
	}
}
