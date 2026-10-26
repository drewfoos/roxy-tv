package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/roxy-media/roxy/server/internal/model"
)

// A file named with an absolute number ("Show - 03.mkv") is stored under the GUID
// abs0003 while the provider placeholder for the same episode is s01e0003, so the two
// rows never meet by GUID. The shadow cleanup must drop the placeholder and leave every
// other row alone.
func TestDeleteShadowedPlaceholders(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	lib := &model.Library{Name: "Anime", Kind: "series", Root: t.TempDir()}
	if err := s.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	n := func(i int) *int { return &i }
	series := &model.Item{ID: "series1", GUID: "path:series:x", LibraryID: lib.ID, Type: model.TypeSeries, Title: "Show"}
	season := &model.Item{ID: "season1", GUID: "path:series:x:season:1", LibraryID: lib.ID, Type: model.TypeSeason, ParentID: &series.ID, Title: "Season 1", IndexAired: n(1)}
	real3 := &model.Item{ID: "real3", GUID: "path:series:x:abs0003", LibraryID: lib.ID, Type: model.TypeEpisode, ParentID: &season.ID, Title: "Three", IndexAbsolute: n(3)}
	real4 := &model.Item{ID: "real4", GUID: "path:series:x:abs0004", LibraryID: lib.ID, Type: model.TypeEpisode, ParentID: &season.ID, Title: "Four", IndexAired: n(4), SeasonAired: n(1), IndexAbsolute: n(4)}
	for _, it := range []*model.Item{series, season, real3, real4} {
		if err := s.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	for _, ep := range []int{1, 3, 4} {
		it := &model.Item{ID: "ph" + string(rune('0'+ep)), GUID: "path:series:x:s01e000" + string(rune('0'+ep)), LibraryID: lib.ID, Type: model.TypeEpisode, ParentID: &season.ID, Title: "Episode", IndexAired: n(ep), SeasonAired: n(1)}
		if _, _, err := s.EnsureVirtualItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	dropped, err := s.DeleteShadowedPlaceholders(ctx, series.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dropped != 2 {
		t.Fatalf("placeholders 3 (shadowed by an absolute-numbered file) and 4 should go, got %d", dropped)
	}
	eps, err := s.Children(ctx, season.ID, model.TypeEpisode)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, e := range eps {
		got[e.ID] = true
	}
	if len(eps) != 3 || !got["real3"] || !got["real4"] || !got["ph1"] {
		t.Fatalf("want real3, real4 and the E1 placeholder, got %v", got)
	}
	// Running it again, or for the whole library, changes nothing more.
	if again, _ := s.DeleteShadowedPlaceholders(ctx, ""); again != 0 {
		t.Fatalf("second pass dropped %d", again)
	}
}
