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

// A watched item is never also part-way: a short peek leaves it watched with no resume
// point; watching past RewatchMinMs makes it "in progress" at the new position.
func TestRewatchClearsWatched(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	if _, err := s.db.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	get := func() *UserItemData {
		d, err := s.GetUserItemData(ctx, "u", "g")
		if err != nil || d == nil {
			t.Fatalf("user data: %v %v", d, err)
		}
		return d
	}
	if err := s.FinishPlay(ctx, "u", "g"); err != nil {
		t.Fatal(err)
	}
	finishedAt := get().LastPlayedAt

	// a peek: opened, a few seconds played, closed
	if err := s.SetProgress(ctx, "u", "g", 5_000, nil, nil); err != nil {
		t.Fatal(err)
	}
	if d := get(); !d.Played || d.PositionMs != 0 || !d.LastPlayedAt.Equal(finishedAt) {
		t.Fatalf("after a peek: played %v position %d last %v, want watched at 0, unchanged", d.Played, d.PositionMs, d.LastPlayedAt)
	}

	// a real rewatch: a minute in
	if err := s.SetProgress(ctx, "u", "g", 60_000, nil, nil); err != nil {
		t.Fatal(err)
	}
	if d := get(); d.Played || d.PositionMs != 60_000 || d.PlayCount != 1 {
		t.Fatalf("after a rewatch: played %v position %d plays %d, want in progress at 60000, 1 play", d.Played, d.PositionMs, d.PlayCount)
	}
	// once in progress, any position is kept, even a short one after seeking back
	if err := s.SetProgress(ctx, "u", "g", 10_000, nil, nil); err != nil {
		t.Fatal(err)
	}
	if d := get(); d.Played || d.PositionMs != 10_000 {
		t.Fatalf("seeking back: played %v position %d, want in progress at 10000", d.Played, d.PositionMs)
	}
	// finishing again: watched, counted
	if err := s.FinishPlay(ctx, "u", "g"); err != nil {
		t.Fatal(err)
	}
	if d := get(); !d.Played || d.PositionMs != 0 || d.PlayCount != 2 {
		t.Fatalf("finished again: played %v position %d plays %d, want watched at 0, 2 plays", d.Played, d.PositionMs, d.PlayCount)
	}
}
