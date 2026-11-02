package library

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

func testLibrary(t *testing.T) (*Library, *store.Store, model.Library, context.Context) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	lib := &model.Library{Name: "Anime", Kind: model.KindSeries, Root: t.TempDir(), AnimeMode: true}
	if err := st.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	return New(st, nil, nil), st, *lib, ctx
}

// Absolute-numbered files in different season folders are different episodes; a rescan
// of an existing season 1 file keeps its item (and so its watch state).
func TestAbsoluteNumbersPerSeasonFolder(t *testing.T) {
	l, _, lib, ctx := testLibrary(t)
	resolve := func(rel string) *model.Item {
		t.Helper()
		it, err := l.resolveItem(ctx, lib, filepath.Join(lib.Root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		return it
	}
	s1 := resolve("Show/Season 01/Show - 01.mkv")
	s2 := resolve("Show/Season 02/Show - 01.mkv")
	if s1.ID == s2.ID {
		t.Fatalf("season 1 and season 2 episode 1 share item %s (guid %s)", s1.ID, s1.GUID)
	}
	if s1.GUID[len(s1.GUID)-8:] != ":abs0001" || s2.GUID[len(s2.GUID)-11:] != ":s02abs0001" {
		t.Errorf("guids %q, %q", s1.GUID, s2.GUID)
	}
	if again := resolve("Show/Season 01/Show - 01.mkv"); again.ID != s1.ID {
		t.Errorf("rescan of season 1 file made a new item")
	}
	if again := resolve("Show/Season 02/Show - 01.mkv"); again.ID != s2.ID {
		t.Errorf("rescan of season 2 file made a new item")
	}
}

// Items stored under the old GUID scheme in a later season keep it on a rescan: an
// absolute file in Season 02 stored as abs0005, and a "S3 - 02" name once read as
// absolute 2.
func TestLegacyGUIDsKept(t *testing.T) {
	l, st, lib, ctx := testLibrary(t)
	first, err := l.resolveItem(ctx, lib, filepath.Join(lib.Root, "Show", "Season 02", "Show - 04.mkv"))
	if err != nil {
		t.Fatal(err)
	}
	season2 := *first.ParentID
	seriesGUID := first.GUID[:len(first.GUID)-len(":s02abs0004")]
	n := func(i int) *int { return &i }
	old := &model.Item{ID: "old5", GUID: seriesGUID + ":abs0005", LibraryID: lib.ID, Type: model.TypeEpisode, ParentID: &season2, Title: "Five", IndexAired: n(5), SeasonAired: n(2), IndexAbsolute: n(5)}
	if err := st.UpsertItem(ctx, old); err != nil {
		t.Fatal(err)
	}
	it, err := l.resolveItem(ctx, lib, filepath.Join(lib.Root, "Show", "Season 02", "Show - 05.mkv"))
	if err != nil {
		t.Fatal(err)
	}
	if it.ID != "old5" {
		t.Errorf("season 2 file stored under abs0005 got a new item %s (%s)", it.ID, it.GUID)
	}
	third, err := l.resolveItem(ctx, lib, filepath.Join(lib.Root, "Show", "Season 03", "Show S3 - 01.mkv"))
	if err != nil {
		t.Fatal(err)
	}
	season3 := *third.ParentID
	legacy := &model.Item{ID: "old-s3e2", GUID: seriesGUID + ":abs0002", LibraryID: lib.ID, Type: model.TypeEpisode, ParentID: &season3, Title: "Two", IndexAired: n(2), SeasonAired: n(3), IndexAbsolute: n(2)}
	if err := st.UpsertItem(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	it, err = l.resolveItem(ctx, lib, filepath.Join(lib.Root, "Show", "Season 03", "Show S3 - 02.mkv"))
	if err != nil {
		t.Fatal(err)
	}
	if it.ID != "old-s3e2" || it.IndexAired == nil || *it.IndexAired != 2 || it.IndexAbsolute != nil {
		t.Errorf("S3 - 02: item %s guid %s aired %v abs %v", it.ID, it.GUID, it.IndexAired, it.IndexAbsolute)
	}
	if third.GUID != seriesGUID+":s03e0001" {
		t.Errorf("S3 - 01 guid %q", third.GUID)
	}
}

// A multi-episode file records the last episode it covers.
func TestMultiEpisodeFileEnd(t *testing.T) {
	l, _, lib, ctx := testLibrary(t)
	it, err := l.resolveItem(ctx, lib, filepath.Join(lib.Root, "SpongeBob", "Season 01", "SpongeBob - S01E01-E03 - Help Wanted + Reef Blower + Tea at the Treedome.mkv"))
	if err != nil {
		t.Fatal(err)
	}
	if it.IndexAiredEnd == nil || *it.IndexAiredEnd != 3 {
		t.Errorf("end = %v, want 3", it.IndexAiredEnd)
	}
}
