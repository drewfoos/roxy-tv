package store

import (
	"context"
	"path/filepath"
	"testing"
)

// A player keeps reporting after the played threshold; only the first report of a
// viewing counts. Starting the item again and finishing it counts once more.
func TestFinishPlayCountsOncePerViewing(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() }) // Windows cannot delete an open database
	ctx := context.Background()
	// The scope and item rows don't matter here; only the counting does.
	if _, err := s.db.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	count := func() int {
		d, err := s.GetUserItemData(ctx, "u", "g")
		if err != nil || d == nil {
			t.Fatalf("user data: %v %v", d, err)
		}
		return d.PlayCount
	}
	for i := 0; i < 5; i++ {
		if err := s.FinishPlay(ctx, "u", "g"); err != nil {
			t.Fatal(err)
		}
	}
	if c := count(); c != 1 {
		t.Fatalf("after one viewing: play count %d, want 1", c)
	}
	if err := s.SetProgress(ctx, "u", "g", 60_000, nil, nil); err != nil { // watching again
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := s.FinishPlay(ctx, "u", "g"); err != nil {
			t.Fatal(err)
		}
	}
	if c := count(); c != 2 {
		t.Fatalf("after a rewatch: play count %d, want 2", c)
	}
}
