package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/roxy-media/roxy/server/internal/model"
)

// A placeholder whose file arrives starts over from the file, its air date included: the
// date was the provider's for that number, which need not be what the file is. High
// School DxD's "New OVA", filed as S00E10, kept TMDB's special 10's date (2013, a
// broadcast short; the OVA is from 2015), and the date-only fill never replaces a date,
// so the OVA sat a season too early in release order. A date the file's own name gives
// is still set afterwards.
func TestPlaceholderTurnedRealDropsTheProvidersDate(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	lib := &model.Library{Name: "A", Kind: model.KindSeries, Root: "/a"}
	if err := s.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	n := func(v int) *int { return &v }
	show, sp := "show", "sp"
	for _, it := range []*model.Item{
		{ID: show, GUID: "g-show", LibraryID: lib.ID, Type: model.TypeSeries, Title: "Show"},
		{ID: sp, GUID: "g-sp", LibraryID: lib.ID, Type: model.TypeSeason, Title: "Specials", ParentID: &show, IndexAired: n(0)},
	} {
		if err := s.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	for _, g := range []string{"g-e10", "g-e11"} {
		ph := &model.Item{ID: g + "-id", GUID: g, LibraryID: lib.ID, Type: model.TypeEpisode, ParentID: &sp, Title: "Broadcast short", SeasonAired: n(0), IndexAired: n(10), IsVirtual: true}
		if _, _, err := s.EnsureVirtualItem(ctx, ph); err != nil {
			t.Fatal(err)
		}
		if err := s.UpdateItemMetadata(ctx, g+"-id", ItemMetadata{Title: "Broadcast short", Premiered: "2013-06-25"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, g := range []string{"g-e10", "g-e11"} {
		if err := s.UpsertItem(ctx, &model.Item{ID: "new", GUID: g, LibraryID: lib.ID, Type: model.TypeEpisode, ParentID: &sp, Title: "New OVA", SeasonAired: n(0), IndexAired: n(10)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetAirDateIfUnset(ctx, "g-e11-id", "2015-03-10"); err != nil { // its name gave a date
		t.Fatal(err)
	}
	for id, want := range map[string]string{"g-e10-id": "", "g-e11-id": "2015-03-10"} {
		it, err := s.GetItem(ctx, id)
		if err != nil || it == nil {
			t.Fatal(id, err)
		}
		if it.IsVirtual || it.Title != "New OVA" || it.Premiered != want {
			t.Fatalf("%s: virtual=%v title=%q premiered=%q, want real, %q, %q", id, it.IsVirtual, it.Title, it.Premiered, "New OVA", want)
		}
	}
}
