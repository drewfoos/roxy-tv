package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/roxy-media/roxy/server/internal/model"
)

// An extra with a file and no picture offers a thumb (made from a frame when asked):
// apps only ask for the images an item lists, so extras' rows had no thumbnail. An extra
// with a picture keeps its own; one with no file offers none; episodes are unchanged.
func TestExtrasOfferAFrameThumb(t *testing.T) {
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
	show := "show"
	for _, it := range []*model.Item{
		{ID: "show", GUID: "g-show", LibraryID: lib.ID, Type: model.TypeSeries, Title: "Show"},
		{ID: "op", GUID: "extra:op", LibraryID: lib.ID, Type: model.TypeExtra, Title: "Opening", ParentID: &show},
		{ID: "pic", GUID: "extra:pic", LibraryID: lib.ID, Type: model.TypeExtra, Title: "Trailer", ParentID: &show},
		{ID: "gone", GUID: "extra:gone", LibraryID: lib.ID, Type: model.TypeExtra, Title: "Gone", ParentID: &show},
	} {
		if err := s.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"op", "pic"} {
		if err := s.ReplaceSource(ctx, &model.MediaSource{ID: "s-" + id, ItemID: id, Parts: []model.MediaPart{{ID: "p-" + id, Path: "/a/Show/extras/" + id + ".mkv"}}}, "{}"); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetItemImage(ctx, ItemImage{ItemID: "pic", Type: "thumb", Path: "/x.jpg", FetchedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	extras, err := s.Children(ctx, "show", model.TypeExtra)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, x := range extras {
		got[x.ID] = x.Images["thumb"]
	}
	if got["op"] != ImageURL("op", "thumb") {
		t.Errorf("an extra with a file and no picture offers %q, want the frame thumb", got["op"])
	}
	if got["pic"] == "" || got["pic"] == ImageURL("pic", "thumb") {
		t.Errorf("an extra with its own picture: %q, want its stored image's URL", got["pic"])
	}
	if got["gone"] != "" {
		t.Errorf("an extra with no file offers %q", got["gone"])
	}
}

// EpisodesOf reads a long show whole (ListItems answers 100 at a time by default; callers
// that took that saw the first 100 of SpongeBob's 716).
func TestEpisodesOfReadsEveryEpisode(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	lib := &model.Library{Name: "TV", Kind: model.KindSeries, Root: "/t"}
	if err := s.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	series := "series"
	if err := s.UpsertItem(ctx, &model.Item{ID: series, GUID: "g-series", LibraryID: lib.ID, Type: model.TypeSeries, Title: "Long"}); err != nil {
		t.Fatal(err)
	}
	const seasons, perSeason = 3, 250
	for n := 1; n <= seasons; n++ {
		sid := fmt.Sprintf("season%d", n)
		if err := s.UpsertItem(ctx, &model.Item{ID: sid, GUID: "g-" + sid, LibraryID: lib.ID, Type: model.TypeSeason, ParentID: &series}); err != nil {
			t.Fatal(err)
		}
		for e := 1; e <= perSeason; e++ {
			id := fmt.Sprintf("%s-e%d", sid, e)
			if err := s.UpsertItem(ctx, &model.Item{ID: id, GUID: "g-" + id, LibraryID: lib.ID, Type: model.TypeEpisode, ParentID: &sid}); err != nil {
				t.Fatal(err)
			}
		}
	}
	eps, err := s.EpisodesOf(ctx, series)
	if err != nil || len(eps) != seasons*perSeason {
		t.Fatalf("%d episodes read, want %d (%v)", len(eps), seasons*perSeason, err)
	}
}

// Pages of a list whose items tie on the sort (episodes with no numbers, a scan's batch
// created together): every item once, none repeated or skipped at a page boundary.
func TestListItemsPagesWithTies(t *testing.T) {
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
	show, season := "show", "season"
	for _, it := range []*model.Item{
		{ID: show, GUID: "g-show", LibraryID: lib.ID, Type: model.TypeSeries, Title: "Show"},
		{ID: season, GUID: "g-season", LibraryID: lib.ID, Type: model.TypeSeason, ParentID: &show},
	} {
		if err := s.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	// Inserted in an order unlike their ids, all tied on season/index.
	for i := 249; i >= 0; i-- {
		id := fmt.Sprintf("e%03d", (i*97)%250)
		if err := s.UpsertItem(ctx, &model.Item{ID: id, GUID: "g-" + id, LibraryID: lib.ID, Type: model.TypeEpisode, ParentID: &season, Title: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, sortBy := range []string{"index", "title", ""} {
		seen := map[string]int{}
		for off := 0; off < 250; off += 37 {
			page, _, err := s.ListItems(ctx, ItemQuery{Types: []model.ItemType{model.TypeEpisode}, ParentID: season, SortBy: sortBy, Limit: 37, Offset: off})
			if err != nil {
				t.Fatal(err)
			}
			for _, it := range page {
				seen[it.ID]++
			}
		}
		if len(seen) != 250 {
			t.Errorf("sort %q: %d distinct items over the pages, want 250", sortBy, len(seen))
		}
		for id, n := range seen {
			if n != 1 {
				t.Errorf("sort %q: %s came %d times", sortBy, id, n)
			}
		}
	}
}
