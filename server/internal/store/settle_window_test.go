package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/roxy-media/roxy/server/internal/model"
)

// Found by an audit: a SettleDepartures that runs (from another goroutine: a scan of another library,
// a webhook ingest) between RecordDeparture and DeleteSource sees the file still recorded,
// called the departure "settled" and dropped it; the move was then never matched. A
// departure whose own file is still recorded now waits.
func TestSettleBetweenRecordAndDeleteKeepsDeparture(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	s.db.ExecContext(ctx, `PRAGMA foreign_keys = OFF`)
	lib := &model.Library{Name: "M", Kind: model.KindMovies, Root: "/m"}
	if err := s.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	s.UpsertItem(ctx, &model.Item{ID: "old", GUID: "g-old", LibraryID: lib.ID, Type: model.TypeMovie, Title: "Film"})
	s.ReplaceSource(ctx, &model.MediaSource{ID: "s-old", ItemID: "old", Parts: []model.MediaPart{{ID: "p-old", Path: "/m/A/Film.mkv", Size: 4242, DurationMs: 6_000_000}}}, "{}")
	if err := s.SetProgress(ctx, "p", "g-old", 60_000, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordDeparture(ctx, "/m/A/Film.mkv"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SettleDepartures(ctx); err != nil { // concurrent settle lands here
		t.Fatal(err)
	}
	s.DeleteSource(ctx, "s-old")
	s.UpsertItem(ctx, &model.Item{ID: "new", GUID: "g-new", LibraryID: lib.ID, Type: model.TypeMovie, Title: "Film"})
	s.ReplaceSource(ctx, &model.MediaSource{ID: "s-new", ItemID: "new", Parts: []model.MediaPart{{ID: "p-new", Path: "/m/B/Film.mkv", Size: 4242, DurationMs: 6_000_000}}}, "{}")
	if _, err := s.SettleDepartures(ctx); err != nil {
		t.Fatal(err)
	}
	if d, _ := s.GetUserItemData(ctx, "p", "g-new"); d == nil || d.PositionMs != 60_000 {
		t.Fatalf("move not carried: the departure was dropped as settled before its source was deleted: %+v", d)
	}
}

// An extra (a trailer) of exactly the departed film's size is not a match: history is
// never carried to an extra, and the departure used to be dropped anyway, so the film's
// real arrival afterwards found nothing to carry.
func TestExtraIsNotAnArrival(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	s.db.ExecContext(ctx, `PRAGMA foreign_keys = OFF`)
	lib := &model.Library{Name: "M", Kind: model.KindMovies, Root: "/m"}
	if err := s.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	s.UpsertItem(ctx, &model.Item{ID: "old", GUID: "g-old", LibraryID: lib.ID, Type: model.TypeMovie, Title: "Film"})
	s.ReplaceSource(ctx, &model.MediaSource{ID: "s-old", ItemID: "old", Parts: []model.MediaPart{{ID: "p-old", Path: "/m/A/Film.mkv", Size: 4242, DurationMs: 6_000_000}}}, "{}")
	if err := s.SetProgress(ctx, "p", "g-old", 60_000, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordDeparture(ctx, "/m/A/Film.mkv"); err != nil {
		t.Fatal(err)
	}
	s.DeleteSource(ctx, "s-old")
	// Another film's trailer, the same size by chance, arrives first.
	other := "other"
	s.UpsertItem(ctx, &model.Item{ID: "o", GUID: "g-o", LibraryID: lib.ID, Type: model.TypeMovie, Title: "Other"})
	s.ReplaceSource(ctx, &model.MediaSource{ID: "s-o", ItemID: "o", Parts: []model.MediaPart{{ID: "p-o", Path: "/m/O/Other.mkv", Size: 9999}}}, "{}")
	s.UpsertItem(ctx, &model.Item{ID: "tr", GUID: "extra:tr", LibraryID: lib.ID, Type: model.TypeExtra, Title: "Trailer", ParentID: &other})
	s.ReplaceSource(ctx, &model.MediaSource{ID: "s-tr", ItemID: "tr", Parts: []model.MediaPart{{ID: "p-tr", Path: "/m/O/trailers/T.mkv", Size: 4242, DurationMs: 6_000_000}}}, "{}")
	if _, err := s.SettleDepartures(ctx); err != nil {
		t.Fatal(err)
	}
	// Then the film itself, moved.
	s.UpsertItem(ctx, &model.Item{ID: "new", GUID: "g-new", LibraryID: lib.ID, Type: model.TypeMovie, Title: "Film"})
	s.ReplaceSource(ctx, &model.MediaSource{ID: "s-new", ItemID: "new", Parts: []model.MediaPart{{ID: "p-new", Path: "/m/B/Film.mkv", Size: 4242, DurationMs: 6_000_000}}}, "{}")
	if _, err := s.SettleDepartures(ctx); err != nil {
		t.Fatal(err)
	}
	if d, _ := s.GetUserItemData(ctx, "p", "g-new"); d == nil || d.PositionMs != 60_000 {
		t.Fatalf("the film's history did not follow it after a same-sized trailer arrived: %+v", d)
	}
}
