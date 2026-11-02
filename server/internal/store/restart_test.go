package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/roxy-media/roxy/server/internal/model"
)

func openTest(t *testing.T) (*Store, *model.Library, context.Context) {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	lib := &model.Library{Name: "TV", Kind: "series", Root: t.TempDir()}
	if err := s.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	return s, lib, ctx
}

func ip(i int) *int { return &i }

// Only real, numbered, unfilled episodes count, and with since only newer ones.
func TestCountUnrefreshedEpisodes(t *testing.T) {
	s, lib, ctx := openTest(t)
	series := &model.Item{ID: "sr", GUID: "g:sr", LibraryID: lib.ID, Type: model.TypeSeries, Title: "S"}
	season := &model.Item{ID: "se", GUID: "g:se", LibraryID: lib.ID, Type: model.TypeSeason, ParentID: &series.ID, Title: "Season 1", IndexAired: ip(1)}
	for _, it := range []*model.Item{series, season,
		{ID: "real", GUID: "g:1", LibraryID: lib.ID, Type: model.TypeEpisode, ParentID: &season.ID, Title: "E1", IndexAired: ip(1)},
		{ID: "unnumbered", GUID: "g:file", LibraryID: lib.ID, Type: model.TypeEpisode, ParentID: &season.ID, Title: "Weird name"},
	} {
		if err := s.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := s.EnsureVirtualItem(ctx, &model.Item{ID: "ph", GUID: "g:2", LibraryID: lib.ID, Type: model.TypeEpisode, ParentID: &season.ID, Title: "E2", IndexAired: ip(2)}); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.CountUnrefreshedEpisodes(ctx, series.ID, nil); n != 1 {
		t.Errorf("count = %d, want 1 (placeholder and unnumbered file excluded)", n)
	}
	later := time.Now().Add(time.Second)
	if n, _ := s.CountUnrefreshedEpisodes(ctx, series.ID, &later); n != 0 {
		t.Errorf("count since a later attempt = %d, want 0", n)
	}
	if at, _ := s.MetadataAttemptedAt(ctx, series.ID); at != nil {
		t.Errorf("attempted = %v before any attempt", at)
	}
	if err := s.SetMetadataAttempted(ctx, series.ID, later); err != nil {
		t.Fatal(err)
	}
	if at, _ := s.MetadataAttemptedAt(ctx, series.ID); at == nil || !at.Equal(later.UTC().Truncate(time.Millisecond)) {
		t.Errorf("attempted = %v, want %v", at, later)
	}
}

// Migration 0023, SyncChildLibrary and EnsureVirtualItem: seasons and episodes take
// their series' library.
func TestChildLibraryRepair(t *testing.T) {
	s, lib, ctx := openTest(t)
	movies := &model.Library{Name: "Movies", Kind: "movies", Root: t.TempDir()}
	if err := s.CreateLibrary(ctx, movies); err != nil {
		t.Fatal(err)
	}
	series := &model.Item{ID: "sr", GUID: "g:sr", LibraryID: lib.ID, Type: model.TypeSeries, Title: "S"}
	if err := s.UpsertItem(ctx, series); err != nil {
		t.Fatal(err)
	}
	vs := &model.Item{ID: "vs", GUID: "g:vs", LibraryID: movies.ID, Type: model.TypeSeason, ParentID: &series.ID, Title: "Season 2", IndexAired: ip(2)}
	ve := &model.Item{ID: "ve", GUID: "g:ve", LibraryID: movies.ID, Type: model.TypeEpisode, ParentID: &vs.ID, Title: "E1", IndexAired: ip(1)}
	for _, it := range []*model.Item{vs, ve} {
		if _, _, err := s.EnsureVirtualItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	check := func(when string) {
		t.Helper()
		for _, id := range []string{"vs", "ve"} {
			if it, _ := s.GetItem(ctx, id); it.LibraryID != lib.ID {
				t.Errorf("%s: %s library = %d, want %d", when, id, it.LibraryID, lib.ID)
			}
		}
	}
	body, err := migrationFS.ReadFile("migrations/0023_child_library.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, string(body)); err != nil {
		t.Fatal(err)
	}
	check("migration")

	if _, err := s.db.ExecContext(ctx, `UPDATE items SET library_id = ? WHERE id IN ('vs','ve')`, movies.ID); err != nil {
		t.Fatal(err)
	}
	if n, err := s.SyncChildLibrary(ctx, series.ID); err != nil || n != 2 {
		t.Fatalf("SyncChildLibrary = %d, %v", n, err)
	}
	check("sync")
	if _, err := s.db.ExecContext(ctx, `UPDATE items SET library_id = ? WHERE id = 've'`, movies.ID); err != nil {
		t.Fatal(err)
	}
	if got, _, err := s.EnsureVirtualItem(ctx, &model.Item{ID: "x", GUID: "g:ve", LibraryID: lib.ID, Type: model.TypeEpisode, ParentID: &vs.ID, Title: "E1"}); err != nil || got.LibraryID != lib.ID {
		t.Fatalf("EnsureVirtualItem = %+v, %v", got, err)
	}
	check("ensure")
}

// Detection markers come back with the season's episodes and go when the file is
// re-probed (it changed).
func TestSegmentDetectionMarks(t *testing.T) {
	s, lib, ctx := openTest(t)
	series := &model.Item{ID: "sr", GUID: "g:sr", LibraryID: lib.ID, Type: model.TypeSeries, Title: "S"}
	season := &model.Item{ID: "se", GUID: "g:se", LibraryID: lib.ID, Type: model.TypeSeason, ParentID: &series.ID, Title: "Season 1", IndexAired: ip(1)}
	ep := &model.Item{ID: "ep", GUID: "g:ep", LibraryID: lib.ID, Type: model.TypeEpisode, ParentID: &season.ID, Title: "E1", IndexAired: ip(1)}
	for _, it := range []*model.Item{series, season, ep} {
		if err := s.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	src := &model.MediaSource{ID: "src", ItemID: ep.ID, IsPrimary: true, DurationMs: 1400000, Parts: []model.MediaPart{{ID: "p", Index: 0, Path: "/x/e1.mkv", ModTime: time.Now()}}}
	if err := s.ReplaceSource(ctx, src, ""); err != nil {
		t.Fatal(err)
	}
	get := func() SeasonEpisode {
		t.Helper()
		eps, err := s.SeasonEpisodes(ctx, season.ID)
		if err != nil || len(eps) != 1 {
			t.Fatalf("SeasonEpisodes = %v, %v", eps, err)
		}
		return eps[0]
	}
	if e := get(); e.DetectVersion != 0 || e.AniSkipAsked {
		t.Fatalf("fresh source = %+v", e)
	}
	if err := s.MarkDetected(ctx, src.ID, 3); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkAniSkipAsked(ctx, src.ID, "999"); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkDetected(ctx, src.ID, 3); err != nil { // keeps the AniSkip mark
		t.Fatal(err)
	}
	if e := get(); e.DetectVersion != 3 || !e.AniSkipAsked || e.AniSkipMal != "999" {
		t.Fatalf("marked source = %+v", e)
	}
	if err := s.ReplaceSource(ctx, src, ""); err != nil {
		t.Fatal(err)
	}
	if e := get(); e.DetectVersion != 0 || e.AniSkipAsked {
		t.Errorf("re-probed source keeps its marks: %+v", e)
	}
}
