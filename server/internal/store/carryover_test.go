package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/roxy-media/roxy/server/internal/model"
)

// CarryChain: data only on the old item moves; data on both is merged (the more recently
// played one's position and choices, plays added up, watchlist if either); hand-picked
// artwork moves over the new item's fetched one; another profile's data is untouched.
func TestCarryChain(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	if _, err := s.db.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	lib := &model.Library{Name: "M", Kind: model.KindMovies, Root: t.TempDir()}
	if err := s.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"old", "new"} {
		if err := s.UpsertItem(ctx, &model.Item{ID: id, GUID: "g-" + id, LibraryID: lib.ID, Type: model.TypeMovie, Title: id}); err != nil {
			t.Fatal(err)
		}
	}
	one, two := 1, 2
	// Profile p1: only on the old item.
	if err := s.SetProgress(ctx, "p1", "g-old", 100_000, &one, &two); err != nil {
		t.Fatal(err)
	}
	// Profile p2: on both; the old one is the more recent viewing, the new one is on the watchlist.
	if _, err := s.SetProgressAsOf(ctx, "p2", "g-new", time.Now().Add(-time.Hour), 5_000, nil, &one); err != nil {
		t.Fatal(err)
	}
	if err := s.SetFavorite(ctx, "p2", "g-new", true); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishPlay(ctx, "p2", "g-old"); err != nil { // watched to the end, just now
		t.Fatal(err)
	}
	// Artwork: the old item's poster was picked by hand, the new one fetched its own.
	if _, err := s.db.ExecContext(ctx, `INSERT INTO item_images (item_id, type, path, locked, fetched_at) VALUES ('old', 'poster', '/picked.jpg', 1, 'x'), ('new', 'poster', '/fetched.jpg', 0, 'x')`); err != nil {
		t.Fatal(err)
	}

	chain, err := s.ItemChain(ctx, "old")
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := s.CarryChain(ctx, chain, "new"); err != nil || !ok {
		t.Fatal(ok, err)
	}
	p1, _ := s.GetUserItemData(ctx, "p1", "g-new")
	if p1 == nil || p1.PositionMs != 100_000 || p1.AudioStreamIndex == nil || *p1.AudioStreamIndex != 1 {
		t.Fatalf("p1 not moved: %+v", p1)
	}
	p2, _ := s.GetUserItemData(ctx, "p2", "g-new")
	if p2 == nil || !p2.Played || p2.PlayCount != 1 || !p2.IsFavorite || p2.SubtitleStreamIndex == nil || *p2.SubtitleStreamIndex != 1 {
		t.Fatalf("p2 merge: %+v", p2)
	}
	for _, scope := range []string{"p1", "p2"} {
		if d, _ := s.GetUserItemData(ctx, scope, "g-old"); d != nil && (d.PositionMs != 0 || d.Played || d.IsFavorite) {
			t.Fatalf("%s left on the old item: %+v", scope, d)
		}
	}
	var path string
	var locked int
	if err := s.db.QueryRowContext(ctx, `SELECT path, locked FROM item_images WHERE item_id = 'new' AND type = 'poster'`).Scan(&path, &locked); err != nil || path != "/picked.jpg" || locked != 1 {
		t.Fatalf("artwork: %q %d %v", path, locked, err)
	}
	// A client still holding the old id (a phone's download) is sent to the new item once
	// the old one is gone.
	if _, err := s.db.ExecContext(ctx, `DELETE FROM items WHERE id = 'old'`); err != nil {
		t.Fatal(err)
	}
	if it, err := s.GetItemOrMoved(ctx, "old"); err != nil || it == nil || it.ID != "new" {
		t.Fatalf("redirect: %+v %v", it, err)
	}
	if it, _ := s.GetItemOrMoved(ctx, "nothing"); it != nil {
		t.Fatalf("unknown id resolved to %s", it.ID)
	}
}

// Two files of exactly the same size arrive, one of them the moved file: the size alone is
// ambiguous, so its name and length decide. It used to give up (the name rule ran only
// when no size matched), leaving the history behind.
func TestDepartureSameSizeDecidedByName(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	if _, err := s.db.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	lib := &model.Library{Name: "Anime", Kind: model.KindSeries, Root: "/m"}
	if err := s.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	add := func(id, guid, title, path string, parent *string, typ model.ItemType, size, dur int64) {
		t.Helper()
		if err := s.UpsertItem(ctx, &model.Item{ID: id, GUID: guid, LibraryID: lib.ID, Type: typ, Title: title, ParentID: parent}); err != nil {
			t.Fatal(err)
		}
		if path != "" {
			if err := s.ReplaceSource(ctx, &model.MediaSource{ID: "src-" + id, ItemID: id, IsPrimary: true,
				Parts: []model.MediaPart{{ID: "p-" + id, Path: path, Size: size, DurationMs: dur}}}, "{}"); err != nil {
				t.Fatal(err)
			}
		}
	}
	strp := func(v string) *string { return &v }
	add("old", "path:show:old", "Show A", "", nil, model.TypeSeries, 0, 0)
	add("olde2", "old:e2", "Episode 2", "/m/Show A/Show A - S01E02.mkv", strp("old"), model.TypeEpisode, 5000, 3000)
	if err := s.SetProgress(ctx, "p", "old:e2", 1500, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordDeparture(ctx, "/m/Show A/Show A - S01E02.mkv"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSource(ctx, "src-olde2"); err != nil {
		t.Fatal(err)
	}
	// Arrivals: the moved episode, and another film of exactly the same size.
	add("new", "tvdb:show:1", "Show A", "", nil, model.TypeSeries, 0, 0)
	add("newe2", "tvdb:e2", "Episode 2", "/m/Show A {tvdb-1}/Show A - S01E02.mkv", strp("new"), model.TypeEpisode, 5000, 3000)
	add("film", "film", "Other Film", "/m/Other/Other Film.mkv", nil, model.TypeMovie, 5000, 3000)
	if n, err := s.SettleDepartures(ctx); err != nil || n != 1 {
		t.Fatalf("settle: %d %v", n, err)
	}
	if d, _ := s.GetUserItemData(ctx, "p", "tvdb:e2"); d == nil || d.PositionMs != 1500 {
		t.Fatalf("history did not follow the moved episode: %+v", d)
	}
	if d, _ := s.GetUserItemData(ctx, "p", "film"); d != nil && d.PositionMs != 0 {
		t.Fatalf("the same-sized film got it: %+v", d)
	}
}
