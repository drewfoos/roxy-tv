package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/roxy-media/roxy/server/internal/model"
)

// addShow stores a series [show] with the given seasons (in that order, which is the
// order the rows are read back in) of eps real episodes each: ids <show>-s<N>e<M>,
// guids g-<id>.
func addShow(t *testing.T, s *Store, lib *model.Library, ctx context.Context, show string, seasons []int, eps int) {
	t.Helper()
	if _, err := s.db.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	up := func(it *model.Item) {
		t.Helper()
		if err := s.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	up(&model.Item{ID: show, GUID: "g-" + show, LibraryID: lib.ID, Type: model.TypeSeries, Title: show})
	for _, n := range seasons {
		up(&model.Item{ID: fmt.Sprintf("%s-s%d", show, n), GUID: fmt.Sprintf("g-%s-s%d", show, n), LibraryID: lib.ID, Type: model.TypeSeason, ParentID: strp(show), IndexAired: ip(n)})
	}
	for _, n := range seasons {
		for e := 1; e <= eps; e++ {
			id := fmt.Sprintf("%s-s%de%d", show, n, e)
			up(&model.Item{ID: id, GUID: "g-" + id, LibraryID: lib.ID, Type: model.TypeEpisode, ParentID: strp(fmt.Sprintf("%s-s%d", show, n)), SeasonAired: ip(n), IndexAired: ip(e), Title: id})
			if _, err := s.db.ExecContext(ctx, `INSERT INTO media_sources (id, item_id) VALUES (?, ?)`, "src-"+id, id); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// "Mark all as watched" on a series is not a viewing: nothing of it comes up in Continue
// Watching. Each episode used to get a time of its own, the one written last looked like
// the episode finished most recently, and the rewatch rule then offered the one after it:
// an episode the profile had just marked watched.
func TestMarkAllWatchedIsNotARewatch(t *testing.T) {
	for _, seasons := range [][]int{{1, 2}, {2, 1}, {1, 2, 0}} {
		t.Run(fmt.Sprint(seasons), func(t *testing.T) {
			s, lib, ctx := openTest(t)
			addShow(t, s, lib, ctx, "show", seasons, 12)
			if err := s.MarkPlayedUnder(ctx, "u", "show", true); err != nil {
				t.Fatal(err)
			}
			if ids := continueIDs(t, s, ctx); len(ids) != 0 {
				t.Fatalf("whole series marked watched: Continue Watching has %v, want nothing", ids)
			}
			// One time for the whole mark.
			var stamps int
			if err := s.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT marked_at) FROM user_item_data WHERE scope_id = 'u'`).Scan(&stamps); err != nil {
				t.Fatal(err)
			}
			if stamps != 1 {
				t.Errorf("the mark left %d different times, want 1", stamps)
			}
		})
	}
}

// A real rewatch after a "mark all" still brings up the next episode, and marking the
// series watched again takes it away.
func TestRewatchAfterMarkAll(t *testing.T) {
	s, lib, ctx := openTest(t)
	addShow(t, s, lib, ctx, "show", []int{1}, 6)
	if err := s.MarkPlayedUnder(ctx, "u", "show", true); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	// Watching E3 again to the end (position past the rewatch minimum, then finished).
	if err := s.SetProgress(ctx, "u", "g-show-s1e3", 600_000, nil, nil); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := s.FinishPlay(ctx, "u", "g-show-s1e3"); err != nil {
		t.Fatal(err)
	}
	if ids := continueIDs(t, s, ctx); len(ids) != 1 || ids[0] != "show-s1e4" {
		t.Fatalf("after rewatching E3: %v, want [show-s1e4]", ids)
	}
	time.Sleep(2 * time.Millisecond)
	if err := s.MarkPlayedUnder(ctx, "u", "show", true); err != nil {
		t.Fatal(err)
	}
	if ids := continueIDs(t, s, ctx); len(ids) != 0 {
		t.Fatalf("series marked watched again: %v, want nothing", ids)
	}
}

// One long series marked watched does not hide another show's next episode: the latest
// finished episode is picked per series, where a limit over all finished episodes used to
// be filled by the long series alone.
func TestContinueWatchingLongSeriesDoesNotHideOthers(t *testing.T) {
	s, lib, ctx := openTest(t)
	addShow(t, s, lib, ctx, "alya", []int{1}, 12)
	addShow(t, s, lib, ctx, "long", []int{1}, 320)
	if err := s.FinishPlay(ctx, "u", "g-alya-s1e3"); err != nil {
		t.Fatal(err)
	}
	if ids := continueIDs(t, s, ctx); len(ids) != 1 || ids[0] != "alya-s1e4" {
		t.Fatalf("after Alya E3: %v, want [alya-s1e4]", ids)
	}
	time.Sleep(2 * time.Millisecond)
	if err := s.MarkPlayedUnder(ctx, "u", "long", true); err != nil {
		t.Fatal(err)
	}
	if ids := continueIDs(t, s, ctx); len(ids) != 1 || ids[0] != "alya-s1e4" {
		t.Fatalf("after marking a 320-episode series watched: %v, want [alya-s1e4]", ids)
	}
}

// A half-watched episode whose file went and which is now a "missing" placeholder (same
// GUID, so the same watch state) is not offered to resume, in Continue Watching or in
// History, and does not hide an older half-watched episode of the same show.
func TestPlaceholderIsNotResumable(t *testing.T) {
	s, lib, ctx := openTest(t)
	addShow(t, s, lib, ctx, "show", []int{1}, 6)
	if err := s.SetProgress(ctx, "u", "g-show-s1e2", 600_000, nil, nil); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := s.SetProgress(ctx, "u", "g-show-s1e4", 600_000, nil, nil); err != nil {
		t.Fatal(err)
	}
	if ids := continueIDs(t, s, ctx); len(ids) != 1 || ids[0] != "show-s1e4" {
		t.Fatalf("E4 half watched: %v, want [show-s1e4]", ids)
	}
	// E4's file goes; the metadata refresh puts a placeholder in its place.
	if err := s.DeleteItem(ctx, "show-s1e4"); err != nil {
		t.Fatal(err)
	}
	ph := &model.Item{ID: "ph-e4", GUID: "g-show-s1e4", LibraryID: lib.ID, Type: model.TypeEpisode, ParentID: strp("show-s1"), SeasonAired: ip(1), IndexAired: ip(4), Title: "Episode 4"}
	if _, made, err := s.EnsureVirtualItem(ctx, ph); err != nil || !made {
		t.Fatalf("placeholder: made=%v err=%v", made, err)
	}
	if ids := continueIDs(t, s, ctx); len(ids) != 1 || ids[0] != "show-s1e2" {
		t.Errorf("Continue Watching with E4 a placeholder: %v, want [show-s1e2]", ids)
	}
	hist, err := s.HistoryItems(ctx, "u", 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range hist {
		if it.IsVirtual {
			t.Errorf("History lists the placeholder %s", it.ID)
		}
	}
}

// A peek (10 s) at an unwatched item keeps no resume point whatever the item's length.
// The minimum of a short item (10 minutes or less, or of unknown length) is exactly
// RewatchMinMs, which the store read as "no minimum": those peeks landed in Continue
// Watching.
func TestPeekAtShortItemKeepsNothing(t *testing.T) {
	s, _, ctx := openTest(t)
	if _, err := s.db.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		guid    string
		runtime int64
	}{{"short-4min", 240_000}, {"mid-8min", 480_000}, {"unknown", 0}, {"ep-24min", 1_440_000}} {
		if _, err := s.SetProgressMin(ctx, "u", c.guid, nil, 10_000, MinResumeMs(c.runtime), nil, nil); err != nil {
			t.Fatal(err)
		}
		d, err := s.GetUserItemData(ctx, "u", c.guid)
		if err != nil || d == nil {
			t.Fatalf("%s: %v %v", c.guid, d, err)
		}
		if d.PositionMs != 0 || !d.LastPlayedAt.IsZero() {
			t.Errorf("%s: a 10 s peek kept position %d, last played %v; want none", c.guid, d.PositionMs, d.LastPlayedAt)
		}
		// Past the minimum it is a resume point.
		if _, err := s.SetProgressMin(ctx, "u", c.guid, nil, 130_000, MinResumeMs(c.runtime), nil, nil); err != nil {
			t.Fatal(err)
		}
		if d, _ := s.GetUserItemData(ctx, "u", c.guid); d.PositionMs != 130_000 {
			t.Errorf("%s: position past the minimum = %d, want 130000", c.guid, d.PositionMs)
		}
	}
	// The callers without a minimum keep every position of an unwatched item.
	if err := s.SetProgress(ctx, "u", "old-rule", 10_000, nil, nil); err != nil {
		t.Fatal(err)
	}
	if d, _ := s.GetUserItemData(ctx, "u", "old-rule"); d == nil || d.PositionMs != 10_000 {
		t.Errorf("SetProgress at 10 s: %+v, want position 10000", d)
	}
}
