package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// A phone replays what it had already sent when it died before its queue's clean-up reached
// disk. Every replay must be harmless: a watched mark or a finish sent twice counts one
// play, a position sent twice is the same position, and an old mark replayed after a later
// rewatch does not undo the rewatch.
func TestReplayedReportsAreHarmless(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	if _, err := s.db.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil { // watch data alone, as the other offline tests
		t.Fatal(err)
	}
	const user = "p"
	get := func(guid string) *UserItemData {
		t.Helper()
		d, err := s.GetUserItemData(ctx, user, guid)
		if err != nil || d == nil {
			t.Fatalf("no data for %s: %v", guid, err)
		}
		return d
	}
	t0 := time.Now().Add(-time.Hour)

	// A watched mark, sent and replayed: one play.
	for i := 0; i < 2; i++ {
		if _, err := s.MarkPlayedAsOf(ctx, user, "g-mark", t0); err != nil {
			t.Fatal(err)
		}
	}
	if d := get("g-mark"); !d.Played || d.PlayCount != 1 {
		t.Fatalf("a replayed watched mark counted twice: %+v", d)
	}

	// A finish (a report at the end), sent and replayed: one play.
	for i := 0; i < 2; i++ {
		if _, err := s.FinishPlayAsOf(ctx, user, "g-finish", t0); err != nil {
			t.Fatal(err)
		}
	}
	if d := get("g-finish"); !d.Played || d.PlayCount != 1 {
		t.Fatalf("a replayed finish counted twice: %+v", d)
	}

	// A position, sent and replayed: the same position.
	for i := 0; i < 2; i++ {
		if _, err := s.SetProgressMin(ctx, user, "g-pos", &t0, 600_000, 30_000, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	if d := get("g-pos"); d.Played || d.PositionMs != 600_000 {
		t.Fatalf("a replayed position changed it: %+v", d)
	}

	// Watched, then rewatched part-way later; the old mark replayed after that is dropped.
	if _, err := s.MarkPlayedAsOf(ctx, user, "g-re", t0); err != nil {
		t.Fatal(err)
	}
	t1 := t0.Add(10 * time.Minute)
	if _, err := s.SetProgressMin(ctx, user, "g-re", &t1, 600_000, 30_000, nil, nil); err != nil {
		t.Fatal(err)
	}
	applied, err := s.MarkPlayedAsOf(ctx, user, "g-re", t0)
	if err != nil {
		t.Fatal(err)
	}
	if d := get("g-re"); applied || d.Played || d.PositionMs != 600_000 || d.PlayCount != 1 {
		t.Fatalf("an old mark replayed after a rewatch undid it (applied=%v): %+v", applied, d)
	}
}
