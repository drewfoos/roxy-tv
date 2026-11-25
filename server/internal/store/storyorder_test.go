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
		_, _ = season, e
		it, err := s.nextEpisodeAfter(ctx, "p", "show", after, true)
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

// Next-up follows the season list: after a season's last episode come the OVAs that came
// out before the next season, in the order they did, then that season. The specials at
// the list's foot (a short, an undated one) never come up; after the last of everything
// nothing does. It went by the seasons' numbers before, past every special no file placed.
func TestNextUpFollowsTheSeasonList(t *testing.T) {
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
	}
	up(&model.Item{ID: "show", GUID: "show", LibraryID: lib.ID, Type: model.TypeSeries, Title: "Show"})
	for _, n := range []int{0, 1, 2} {
		n := n
		up(&model.Item{ID: fmt.Sprintf("s%d", n), GUID: fmt.Sprintf("show:season:%d", n), LibraryID: lib.ID, Type: model.TypeSeason, ParentID: strp("show"), IndexAired: &n})
	}
	ep := func(id string, season, e int, title, date string, minutes float64) {
		t.Helper()
		sn, en := season, e
		ms := int64(minutes * 60 * 1000)
		up(&model.Item{ID: id, GUID: "g-" + id, LibraryID: lib.ID, Type: model.TypeEpisode, ParentID: strp(fmt.Sprintf("s%d", season)), SeasonAired: &sn, IndexAired: &en, Title: title, RuntimeMs: &ms})
		if err := s.ReplaceSource(ctx, &model.MediaSource{ID: "src-" + id, ItemID: id, IsPrimary: true, Parts: []model.MediaPart{{ID: "p-" + id, Path: "/m/" + id}}}, "{}"); err != nil {
			t.Fatal(err)
		}
		if date != "" {
			if err := s.SetAirDateIfUnset(ctx, id, date); err != nil {
				t.Fatal(err)
			}
		}
	}
	ep("s1e1", 1, 1, "One", "2012-01-06", 24)
	ep("s1e12", 1, 12, "Twelve", "2012-03-23", 24)
	ep("s2e1", 2, 1, "Thirteen", "2013-07-07", 24)
	ep("s2e2", 2, 2, "Fourteen", "2013-07-14", 24)
	ep("short", 0, 2, "Going to the Beach", "2012-03-21", 3.5)
	ep("ova1", 0, 8, "Show OVA 1", "2012-09-20", 20.4)
	ep("ova2", 0, 9, "Show OVA 2", "2013-05-21", 20.4)
	ep("after", 0, 10, "Show OVA 3", "2015-03-10", 20.7)
	ep("undated", 0, 11, "Somewhere in Between", "", 22)
	// An episode the library has no file of (announced, not out yet) is never what is next.
	three := 3
	up(&model.Item{ID: "s2e3", GUID: "g-s2e3", LibraryID: lib.ID, Type: model.TypeEpisode, ParentID: strp("s2"), SeasonAired: func() *int { n := 2; return &n }(), IndexAired: &three, Title: "Fifteen", IsVirtual: true})
	if err := s.SetAirDateIfUnset(ctx, "s2e3", "2013-07-21"); err != nil {
		t.Fatal(err)
	}
	next := func(after string) string {
		t.Helper()
		it, err := s.nextEpisodeAfter(ctx, "p", "show", after, true)
		if err != nil {
			t.Fatal(err)
		}
		if it == nil {
			return "<none>"
		}
		return it.ID
	}
	for _, c := range [][2]string{{"s1e1", "s1e12"}, {"s1e12", "ova1"}, {"ova1", "ova2"}, {"ova2", "s2e1"}, {"s2e1", "s2e2"}, {"s2e2", "after"}, {"after", "<none>"}} {
		if got := next(c[0]); got != c[1] {
			t.Errorf("after %s: %s, want %s", c[0], got, c[1])
		}
	}
	// Season 2 begun with the OVAs passed over: what is next is the rest of season 2.
	for _, id := range []string{"s1e1", "s1e12", "s2e1"} {
		if err := s.MarkPlayed(ctx, "p", "g-"+id, true); err != nil {
			t.Fatal(err)
		}
	}
	if got := next("s2e1"); got != "s2e2" {
		t.Errorf("with the OVAs passed over, after S2E1: %s, want S2E2", got)
	}
	// One of the extras just watched: the first thing not watched yet, from the top.
	if got := next("short"); got != "ova1" {
		t.Errorf("after a short at the list's foot: %s, want the first unwatched", got)
	}
}

func strp(s string) *string { return &s }
