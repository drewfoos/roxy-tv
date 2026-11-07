package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/roxy-media/roxy/server/internal/model"
)

// Next-up in story order: after season 2's finale comes the film placed before season 3
// episode 1, then season 3 episode 1; a special with no place never comes up; an OVA
// placed after season 1 comes between the seasons.
func TestNextUpFollowsStoryOrder(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	if _, err := s.db.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	lib := &model.Library{Name: "Anime", Kind: model.KindSeries, Root: t.TempDir()}
	if err := s.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	up := func(it *model.Item) {
		t.Helper()
		if err := s.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
		if it.Type == model.TypeEpisode {
			if err := s.ReplaceSource(ctx, &model.MediaSource{ID: "src-" + it.ID, ItemID: it.ID, IsPrimary: true,
				Parts: []model.MediaPart{{ID: "p-" + it.ID, Path: "/m/" + it.ID}}}, "{}"); err != nil {
				t.Fatal(err)
			}
		}
	}
	up(&model.Item{ID: "show", GUID: "show", LibraryID: lib.ID, Type: model.TypeSeries, Title: "KonoSuba"})
	for _, n := range []int{0, 1, 2, 3} {
		n := n
		up(&model.Item{ID: fmt.Sprintf("s%d", n), GUID: fmt.Sprintf("show:season:%d", n), LibraryID: lib.ID, Type: model.TypeSeason, ParentID: strp("show"), IndexAired: &n})
	}
	ep := func(id string, season, e int) {
		sn, en := season, e
		up(&model.Item{ID: id, GUID: "g-" + id, LibraryID: lib.ID, Type: model.TypeEpisode, ParentID: strp(fmt.Sprintf("s%d", season)), SeasonAired: &sn, IndexAired: &en, Title: id})
	}
	ep("s1e1", 1, 1)
	ep("s2e1", 2, 1)
	ep("s2e10", 2, 10)
	ep("s3e1", 3, 1)
	ep("film", 0, 5)
	ep("ova", 0, 6)
	ep("unplaced", 0, 1)
	three, one := 3, 1
	if err := s.SetSpecialPlacement(ctx, "film", &three, &one, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSpecialPlacement(ctx, "ova", nil, nil, &one); err != nil {
		t.Fatal(err)
	}
	next := func(after string, season, e int) string {
		t.Helper()
		it, err := s.nextEpisodeAfter(ctx, "p", "show", after, season, e, true)
		if err != nil {
			t.Fatal(err)
		}
		if it == nil {
			return "<none>"
		}
		return it.ID
	}
	if got := next("s2e10", 2, 10); got != "film" {
		t.Fatalf("after S2's finale: %s, want the film", got)
	}
	if got := next("film", 0, 5); got != "s3e1" {
		t.Fatalf("after the film: %s, want S3E1", got)
	}
	if got := next("s1e1", 1, 1); got != "ova" {
		t.Fatalf("after season 1: %s, want the OVA", got)
	}
	if got := next("ova", 0, 6); got != "s2e1" {
		t.Fatalf("after the OVA: %s, want S2E1", got)
	}
	if got := next("unplaced", 0, 1); got != "s1e1" {
		t.Fatalf("after an unplaced special: %s, want the first unwatched episode", got)
	}
}

func strp(s string) *string { return &s }
