package store

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
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

// A replayed offline report (made at `at`, sent later) applies only if nothing newer was
// recorded for the item; the check is part of the write.
func TestProgressAsOf(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	if _, err := s.db.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	pos := func(g string) (int64, bool) {
		d, err := s.GetUserItemData(ctx, "u", g)
		if err != nil || d == nil {
			t.Fatalf("user data %s: %v %v", g, d, err)
		}
		return d.PositionMs, d.Played
	}
	hourAgo := time.Now().Add(-time.Hour)

	// Nothing recorded yet: an old report is the best there is.
	if ok, err := s.SetProgressAsOf(ctx, "u", "new", hourAgo, 90_000, nil, nil); err != nil || !ok {
		t.Fatalf("first report not applied: %v %v", ok, err)
	}
	// Watched further live since: the older report must not wind it back.
	if err := s.SetProgress(ctx, "u", "g", 600_000, nil, nil); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.SetProgressAsOf(ctx, "u", "g", hourAgo, 1_000, nil, nil); err != nil || ok {
		t.Fatalf("older report applied: %v %v", ok, err)
	}
	if p, _ := pos("g"); p != 600_000 {
		t.Fatalf("wound back to %d", p)
	}
	if ok, err := s.FinishPlayAsOf(ctx, "u", "g", hourAgo); err != nil || ok {
		t.Fatalf("older finish applied: %v %v", ok, err)
	}
	// Made after the latest activity: applied.
	if ok, err := s.SetProgressAsOf(ctx, "u", "g", time.Now().Add(time.Minute), 700_000, nil, nil); err != nil || !ok {
		t.Fatalf("newer report not applied: %v %v", ok, err)
	}
	if ok, err := s.FinishPlayAsOf(ctx, "u", "g", time.Now().Add(2*time.Minute)); err != nil || !ok {
		t.Fatalf("newer finish not applied: %v %v", ok, err)
	}
	if _, played := pos("g"); !played {
		t.Fatal("finish not recorded")
	}
}

// A live report and an older replayed one racing for the same item: whichever lands
// first, the live position must win (a separate check-then-write could lose it).
func TestProgressAsOfRacesLiveReport(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	if _, err := s.db.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	for i := 0; i < 50; i++ {
		g := fmt.Sprintf("g%d", i)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); _ = s.SetProgress(ctx, "u", g, 600_000, nil, nil) }()
		go func() { defer wg.Done(); _, _ = s.SetProgressAsOf(ctx, "u", g, old, 1_000, nil, nil) }()
		wg.Wait()
		if d, err := s.GetUserItemData(ctx, "u", g); err != nil || d == nil || d.PositionMs != 600_000 {
			t.Fatalf("round %d: live position lost: %+v %v", i, d, err)
		}
	}
}

// A watched mark made offline and replayed later: applied when nothing newer happened,
// dropped when the item was rewatched since (it used to flip it back to watched and lose
// the newer position).
func TestMarkPlayedAsOf(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	if _, err := s.db.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	get := func(g string) (int64, bool, int) {
		d, err := s.GetUserItemData(ctx, "u", g)
		if err != nil || d == nil {
			t.Fatalf("user data %s: %v %v", g, d, err)
		}
		return d.PositionMs, d.Played, d.PlayCount
	}
	hourAgo := time.Now().Add(-time.Hour)

	// Nothing recorded: the old mark is the best there is.
	if ok, err := s.MarkPlayedAsOf(ctx, "u", "new", hourAgo); err != nil || !ok {
		t.Fatalf("first mark not applied: %v %v", ok, err)
	}
	if _, played, n := get("new"); !played || n != 1 {
		t.Fatalf("first mark: played %v count %d", played, n)
	}
	// Finished offline an hour ago, rewatched part way on the TV since: kept as the TV left it.
	if err := s.SetProgress(ctx, "u", "g", 600_000, nil, nil); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.MarkPlayedAsOf(ctx, "u", "g", hourAgo); err != nil || ok {
		t.Fatalf("older mark applied: %v %v", ok, err)
	}
	if p, played, _ := get("g"); p != 600_000 || played {
		t.Fatalf("rewatch lost: position %d played %v", p, played)
	}
	// Made after the latest activity: applied.
	if ok, err := s.MarkPlayedAsOf(ctx, "u", "g", time.Now().Add(time.Minute)); err != nil || !ok {
		t.Fatalf("newer mark not applied: %v %v", ok, err)
	}
	if p, played, n := get("g"); p != 0 || !played || n != 1 {
		t.Fatalf("newer mark: position %d played %v count %d", p, played, n)
	}
}

// Offline reports from two devices reach the server late and in the wrong order. Each
// records its own time as last_played_at, so the one made later still wins even when it
// arrives second (with the arrival time recorded it looked older and was dropped). A time
// in the future is taken as now.
func TestOfflineReportsRecordTheirOwnTime(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	if _, err := s.db.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	get := func(g string) (int64, bool, string) {
		t.Helper()
		var pos int64
		var played int
		var last string
		if err := s.db.QueryRowContext(ctx, `SELECT position_ms, played, COALESCE(last_played_at, '') FROM user_item_data WHERE scope_id = 'u' AND item_guid = ?`, g).Scan(&pos, &played, &last); err != nil {
			t.Fatal(err)
		}
		return pos, played == 1, last
	}
	stamp := func(at time.Time) string { return at.UTC().Format(TimeFormat) }
	base := time.Now().Add(-time.Hour)
	phone, tablet := base, base.Add(10*time.Minute) // the tablet watched further, later

	// Positions: the phone's (older) arrives first, then the tablet's.
	if ok, err := s.SetProgressAsOf(ctx, "u", "g", phone, 100_000, nil, nil); err != nil || !ok {
		t.Fatalf("phone report: %v %v", ok, err)
	}
	if _, _, last := get("g"); last != stamp(phone) {
		t.Fatalf("last_played_at %s, want the report's own time %s", last, stamp(phone))
	}
	if ok, err := s.SetProgressAsOf(ctx, "u", "g", tablet, 900_000, nil, nil); err != nil || !ok {
		t.Fatalf("newer tablet report dropped: %v %v", ok, err)
	}
	if pos, _, last := get("g"); pos != 900_000 || last != stamp(tablet) {
		t.Fatalf("after the tablet: position %d at %s", pos, last)
	}
	// And the other way round: an older one arriving last is still dropped.
	if ok, err := s.SetProgressAsOf(ctx, "u", "g", phone.Add(time.Minute), 200_000, nil, nil); err != nil || ok {
		t.Fatalf("older report applied: %v %v", ok, err)
	}

	// Finish and watched marks record their own time too.
	if ok, err := s.FinishPlayAsOf(ctx, "u", "f", phone); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if ok, err := s.SetProgressAsOf(ctx, "u", "f", tablet, 300_000, nil, nil); err != nil || !ok {
		t.Fatalf("rewatch made after an offline finish dropped: %v %v", ok, err)
	}
	if ok, err := s.MarkPlayedAsOf(ctx, "u", "m", phone); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if _, played, last := get("m"); !played || last != stamp(phone) {
		t.Fatalf("watched mark: played %v at %s", played, last)
	}
	if ok, err := s.SetProgressAsOf(ctx, "u", "m", tablet, 300_000, nil, nil); err != nil || !ok {
		t.Fatalf("rewatch made after an offline mark dropped: %v %v", ok, err)
	}

	// A clock running fast: recorded as now, so a report made a moment later still applies.
	if ok, err := s.SetProgressAsOf(ctx, "u", "c", time.Now().Add(24*time.Hour), 100_000, nil, nil); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if _, _, last := get("c"); last > Now() {
		t.Fatalf("future time stored: %s", last)
	}
	if ok, err := s.SetProgressAsOf(ctx, "u", "c", time.Now().Add(time.Second), 200_000, nil, nil); err != nil || !ok {
		t.Fatalf("report after a fast clock's dropped: %v %v", ok, err)
	}
}
