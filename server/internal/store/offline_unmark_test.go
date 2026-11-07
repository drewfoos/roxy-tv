package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// Found by an audit: watched at t0, offline "watched" mark made at t1 on a phone, then unmarked on
// the TV at t2 > t1. The phone replays at t3: the stale mark flipped the item back to
// watched, because unmarking recorded no time (it records marked_at now).
func TestOfflineMarkOlderThanUnmarkIsDropped(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	if _, err := s.db.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	// t0: finished two hours ago (offline report replayed)
	if _, err := s.FinishPlayAsOf(ctx, "p", "g", time.Now().Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	// t2 = now: unmarked on the TV.
	if err := s.MarkPlayed(ctx, "p", "g", false); err != nil {
		t.Fatal(err)
	}
	// t1 = an hour ago: the phone's offline mark, replayed now.
	ok, err := s.MarkPlayedAsOf(ctx, "p", "g", time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	d, _ := s.GetUserItemData(ctx, "p", "g")
	if ok || d.Played {
		t.Fatalf("older offline mark overrode a newer unmark: applied=%v %+v", ok, d)
	}
}
