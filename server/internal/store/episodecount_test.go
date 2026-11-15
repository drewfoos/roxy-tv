package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/roxy-media/roxy/server/internal/model"
)

// A series' count is its episodes in the library: in season folders (specials too) and
// straight under it, without the placeholders for missing episodes; a film has none.
func TestEpisodeCountsFor(t *testing.T) {
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
	p := func(v string) *string { return &v }
	items := []*model.Item{
		{ID: "show", Type: model.TypeSeries},
		{ID: "s1", Type: model.TypeSeason, ParentID: p("show")},
		{ID: "s0", Type: model.TypeSeason, ParentID: p("show")},
		{ID: "e1", Type: model.TypeEpisode, ParentID: p("s1")},
		{ID: "e2", Type: model.TypeEpisode, ParentID: p("s1")},
		{ID: "missing", Type: model.TypeEpisode, ParentID: p("s1"), IsVirtual: true},
		{ID: "special", Type: model.TypeEpisode, ParentID: p("s0")},
		{ID: "flat", Type: model.TypeEpisode, ParentID: p("show")},
		{ID: "empty", Type: model.TypeSeries},
		{ID: "film", Type: model.TypeMovie},
	}
	for _, it := range items {
		it.GUID, it.LibraryID, it.Title = "g:"+it.ID, lib.ID, it.ID
		if err := s.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.EpisodeCountsFor(ctx, []string{"show", "empty", "film"})
	if err != nil {
		t.Fatal(err)
	}
	if got["show"] != 4 {
		t.Errorf("show: %d episodes, want 4", got["show"])
	}
	if _, ok := got["empty"]; ok {
		t.Errorf("a series with no episodes was given a count: %v", got)
	}
	if _, ok := got["film"]; ok {
		t.Errorf("a film was given a count: %v", got)
	}
}
