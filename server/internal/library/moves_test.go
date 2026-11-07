package library

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

// durations gives test files a duration by name (the prober would read it).
var durations = map[string]int64{}

// moveFixture is a store, a library of the given kind, a Library whose scan ingests with
// the real item resolution but no prober, and a profile to keep watch data for.
func moveFixture(t *testing.T, kind model.LibraryKind) (*Library, *store.Store, model.Library, string, string) {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	root := t.TempDir()
	lib := &model.Library{Name: "L", Kind: kind, Root: root}
	if err := st.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	l := New(st, nil, nil)
	// AddFile without the prober: the same item resolution, a source with the file's
	// size (and a duration from durations, when set), and the same departure check for a
	// file new to the library.
	l.ingestOne = func(ctx context.Context, lib model.Library, path string) error {
		prior, err := st.GetPartByPath(ctx, path)
		if err != nil {
			return err
		}
		it, err := l.resolveForIngest(ctx, lib, path, nil)
		if err != nil {
			return err
		}
		fi, err := os.Stat(path)
		if err != nil {
			return err
		}
		if err := st.ReplaceSource(ctx, &model.MediaSource{ID: stableID("source", path), ItemID: it.ID, IsPrimary: true, Size: fi.Size(),
			Parts: []model.MediaPart{{ID: stableID("part", path), Path: path, Size: fi.Size(), ModTime: fi.ModTime().UTC(), DurationMs: durations[filepath.Base(path)]}}}, "{}"); err != nil {
			return err
		}
		if prior == nil {
			l.settleDepartures(ctx)
		}
		return nil
	}
	scope, err := st.DefaultProfileID(ctx, mustUser(t, st))
	if err != nil {
		t.Fatal(err)
	}
	return l, st, *lib, root, scope
}

func writeSized(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func itemAt(t *testing.T, st *store.Store, path string) *model.Item {
	t.Helper()
	it, err := st.ItemForPath(context.Background(), path)
	if err != nil || it == nil {
		t.Fatalf("item at %s: %v %v", path, it, err)
	}
	return it
}

// A movie's folder renamed: the next scan sees the old path gone and a new one with the
// same file, and the watch history follows it to its new item. It used to stay on the
// old identity, so the film showed as never watched.
func TestScanCarriesHistoryAcrossAMovedMovie(t *testing.T) {
	ctx := context.Background()
	l, st, lib, root, scope := moveFixture(t, model.KindMovies)
	old := filepath.Join(root, "Film (2020)", "Film (2020).mkv")
	writeSized(t, old, 1234)
	writeSized(t, filepath.Join(root, "Other (2019)", "Other (2019).mkv"), 999)
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	before := itemAt(t, st, old)
	if err := st.SetProgress(ctx, scope, before.GUID, 600_000, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.SetFavorite(ctx, scope, before.GUID, true); err != nil {
		t.Fatal(err)
	}

	moved := filepath.Join(root, "Film (2020) {tmdb-42}", "Film (2020).mkv")
	if err := os.MkdirAll(filepath.Dir(moved), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(old, moved); err != nil {
		t.Fatal(err)
	}
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	after := itemAt(t, st, moved)
	if after.GUID == before.GUID {
		t.Fatalf("expected a new identity for the moved file (got %s both times)", after.GUID)
	}
	d, err := st.GetUserItemData(ctx, scope, after.GUID)
	if err != nil || d == nil || d.PositionMs != 600_000 || !d.IsFavorite {
		t.Fatalf("watch history did not follow the move: %+v %v", d, err)
	}
	if old, _ := st.GetUserItemData(ctx, scope, before.GUID); old != nil && old.PositionMs != 0 {
		t.Fatalf("history left on the old identity too: %+v", old)
	}
}

// A series folder renamed: every episode's history follows, and the series' own (its
// watchlist mark) follows to the new series once the old one has no files left.
func TestScanCarriesHistoryAcrossARenamedSeries(t *testing.T) {
	ctx := context.Background()
	l, st, lib, root, scope := moveFixture(t, model.KindSeries)
	var olds []string
	for i, n := range []int{101, 202, 303} {
		p := filepath.Join(root, "Show", "Season 01", "Show - S01E0"+string(rune('1'+i))+".mkv")
		writeSized(t, p, n)
		olds = append(olds, p)
	}
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	ep1 := itemAt(t, st, olds[0])
	if err := st.MarkPlayed(ctx, scope, ep1.GUID, true); err != nil {
		t.Fatal(err)
	}
	series, err := st.GetItem(ctx, *mustParent(t, st, ep1).ParentID)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetFavorite(ctx, scope, series.GUID, true); err != nil {
		t.Fatal(err)
	}

	if err := os.Rename(filepath.Join(root, "Show"), filepath.Join(root, "Show (2021)")); err != nil {
		t.Fatal(err)
	}
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	newEp1 := itemAt(t, st, filepath.Join(root, "Show (2021)", "Season 01", "Show - S01E01.mkv"))
	if newEp1.GUID == ep1.GUID {
		t.Fatal("expected the renamed series to be a new identity")
	}
	if d, _ := st.GetUserItemData(ctx, scope, newEp1.GUID); d == nil || !d.Played {
		t.Fatalf("episode watched mark did not follow: %+v", d)
	}
	newSeries, _ := st.GetItem(ctx, *mustParent(t, st, newEp1).ParentID)
	if d, _ := st.GetUserItemData(ctx, scope, newSeries.GUID); d == nil || !d.IsFavorite {
		t.Fatalf("series watchlist mark did not follow: %+v", d)
	}
}

// Ambiguous: two files of the same size gone and two of that size new. Nothing is
// guessed (one might be paired with the wrong film).
func TestScanDoesNotGuessBetweenSameSizedMoves(t *testing.T) {
	ctx := context.Background()
	l, st, lib, root, scope := moveFixture(t, model.KindMovies)
	a, b := filepath.Join(root, "A (2001)", "A (2001).mkv"), filepath.Join(root, "B (2002)", "B (2002).mkv")
	writeSized(t, a, 500)
	writeSized(t, b, 500)
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	ia := itemAt(t, st, a)
	if err := st.SetProgress(ctx, scope, ia.GUID, 1_000, nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{a, b} {
		dst := filepath.Join(root, "moved", filepath.Base(filepath.Dir(p)), filepath.Base(p))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(p, dst); err != nil {
			t.Fatal(err)
		}
	}
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"A (2001)/A (2001).mkv", "B (2002)/B (2002).mkv"} {
		if d, _ := st.GetUserItemData(ctx, scope, itemAt(t, st, filepath.Join(root, "moved", p)).GUID); d != nil && d.PositionMs != 0 {
			t.Fatalf("%s got history from a guess: %+v", p, d)
		}
	}
}

func mustParent(t *testing.T, st *store.Store, it *model.Item) *model.Item {
	t.Helper()
	if it.ParentID == nil {
		t.Fatalf("%s has no parent", it.Title)
	}
	p, err := st.GetItem(context.Background(), *it.ParentID)
	if err != nil || p == nil {
		t.Fatal(p, err)
	}
	return p
}

// What a webhook rename hands over (old path's item, new path), and the guard: an old item
// that still has a file keeps its data. Here one of a film's two versions moves to another
// folder; the film still has the other, so its history stays where it is.
func TestCarryMovesExplicitAndStillInUse(t *testing.T) {
	ctx := context.Background()
	l, st, lib, root, scope := moveFixture(t, model.KindMovies)
	v1, v2 := filepath.Join(root, "Film (2020)", "Film (2020) - 1080p.mkv"), filepath.Join(root, "Film (2020)", "Film (2020) - 2160p.mkv")
	writeSized(t, v1, 100)
	writeSized(t, v2, 200)
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	film := itemAt(t, st, v1)
	if err := st.SetProgress(ctx, scope, film.GUID, 42_000, nil, nil); err != nil {
		t.Fatal(err)
	}
	// The 2160p version moves to a folder of its own (a new item).
	moved := filepath.Join(root, "Film 4K (2020)", "Film (2020) - 2160p.mkv")
	writeSized(t, moved, 200)
	if err := os.Remove(v2); err != nil {
		t.Fatal(err)
	}
	if err := l.ingestOne(ctx, lib, moved); err != nil {
		t.Fatal(err)
	}
	part, _ := st.GetPartByPath(ctx, v2)
	if err := st.DeleteSource(ctx, part.SourceID); err != nil {
		t.Fatal(err)
	}
	chain, _ := st.ItemChain(ctx, film.ID)
	l.CarryMoves(ctx, []Move{{OldPath: v2, OldChain: chain, NewPath: moved}})
	if d, _ := st.GetUserItemData(ctx, scope, film.GUID); d == nil || d.PositionMs != 42_000 {
		t.Fatalf("the film still has its 1080p file and must keep its history: %+v", d)
	}
	if d, _ := st.GetUserItemData(ctx, scope, itemAt(t, st, moved).GUID); d != nil && d.PositionMs != 0 {
		t.Fatalf("history copied to the moved version's item: %+v", d)
	}

	// Now the 1080p goes too (a rename of the whole film): its history follows.
	renamed := filepath.Join(root, "Film (2020) {tmdb-7}", "Film (2020) - 1080p.mkv")
	writeSized(t, renamed, 100)
	if err := os.Remove(v1); err != nil {
		t.Fatal(err)
	}
	if err := l.ingestOne(ctx, lib, renamed); err != nil {
		t.Fatal(err)
	}
	part, _ = st.GetPartByPath(ctx, v1)
	if err := st.DeleteSource(ctx, part.SourceID); err != nil {
		t.Fatal(err)
	}
	l.CarryMoves(ctx, []Move{{OldPath: v1, OldChain: chain, NewPath: renamed}})
	if d, _ := st.GetUserItemData(ctx, scope, itemAt(t, st, renamed).GUID); d == nil || d.PositionMs != 42_000 {
		t.Fatalf("history did not follow the renamed film: %+v", d)
	}
}

// twoLibraries2 is moveFixture with a second library in the same store, scanned with the
// same fake ingest.
func addLibrary(t *testing.T, st *store.Store, kind model.LibraryKind) model.Library {
	t.Helper()
	lib := &model.Library{Name: "L2", Kind: kind, Root: t.TempDir()}
	if err := st.CreateLibrary(context.Background(), lib); err != nil {
		t.Fatal(err)
	}
	return *lib
}

// A film moved from one library to another (Anime to Anime Movies). The two libraries are
// scanned separately, in either order, so the move is never seen within one scan: the
// departure noted by the source's scan meets the file the destination's scan added.
func TestHistoryFollowsAFilmToAnotherLibrary(t *testing.T) {
	for _, destFirst := range []bool{false, true} {
		ctx := context.Background()
		l, st, src, root, scope := moveFixture(t, model.KindSeries)
		dst := addLibrary(t, st, model.KindMovies)
		old := filepath.Join(root, "Film (2020)", "Film (2020).mkv")
		writeSized(t, old, 4321)
		writeSized(t, filepath.Join(root, "Show", "Show - S01E01.mkv"), 10)
		if err := l.Scan(ctx, src); err != nil {
			t.Fatal(err)
		}
		before := itemAt(t, st, old)
		if before.Type != model.TypeMovie {
			t.Fatalf("expected a movie, got %s", before.Type)
		}
		if err := st.SetProgress(ctx, scope, before.GUID, 777_000, nil, nil); err != nil {
			t.Fatal(err)
		}
		moved := filepath.Join(dst.Root, "Film (2020)", "Film (2020).mkv")
		writeSized(t, moved, 4321)
		if err := os.Remove(old); err != nil {
			t.Fatal(err)
		}
		order := []model.Library{src, dst}
		if destFirst {
			order = []model.Library{dst, src}
		}
		for _, lib := range order {
			if err := l.Scan(ctx, lib); err != nil {
				t.Fatal(err)
			}
		}
		after := itemAt(t, st, moved)
		if d, _ := st.GetUserItemData(ctx, scope, after.GUID); d == nil || d.PositionMs != 777_000 {
			t.Fatalf("destination scanned first=%v: history did not follow: %+v", destFirst, d)
		}
	}
}

// Moved and re-encoded (a remux: other size, same name and length) follows too; a file of
// the same name and length under another show (generic "Episode 01" names) does not.
func TestHistoryFollowsAMovedRemux(t *testing.T) {
	ctx := context.Background()
	l, st, lib, root, scope := moveFixture(t, model.KindSeries)
	durations["Episode 01.mkv"], durations["Episode 01.mp4"] = 1_420_000, 1_420_500
	t.Cleanup(func() { delete(durations, "Episode 01.mkv"); delete(durations, "Episode 01.mp4") })
	old := filepath.Join(root, "Show", "Season 01", "Episode 01.mkv")
	writeSized(t, old, 800)
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	ep := itemAt(t, st, old)
	if err := st.SetProgress(ctx, scope, ep.GUID, 300_000, nil, nil); err != nil {
		t.Fatal(err)
	}
	// Remuxed to .mp4 (another size) in a renamed folder, and another show's Episode 01
	// of the same length arrives in the same scan.
	if err := os.Remove(old); err != nil {
		t.Fatal(err)
	}
	remux := filepath.Join(root, "Show (2019)", "Season 01", "Episode 01.mp4")
	writeSized(t, remux, 650)
	other := filepath.Join(root, "Other Show", "Season 01", "Episode 01.mp4")
	writeSized(t, other, 640)
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	if d, _ := st.GetUserItemData(ctx, scope, itemAt(t, st, remux).GUID); d == nil || d.PositionMs != 300_000 {
		t.Fatalf("history did not follow the remux: %+v", d)
	}
	if d, _ := st.GetUserItemData(ctx, scope, itemAt(t, st, other).GUID); d != nil && d.PositionMs != 0 {
		t.Fatalf("another show's Episode 01 got the history: %+v", d)
	}
}

// Renamed, re-encoded and moved all at once: nothing in the file ties it to the old one
// until the metadata lookup matches it to the same provider id. Then the film (and, for a
// series, the episode with the same season and episode number, and the series) follows.
func TestHistoryFollowsByProviderID(t *testing.T) {
	ctx := context.Background()
	l, st, lib, root, scope := moveFixture(t, model.KindMovies)
	old := filepath.Join(root, "Old Name (2010)", "old.name.2010.720p.mkv")
	writeSized(t, old, 900)
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	film := itemAt(t, st, old)
	if err := st.SetProviderIDs(ctx, film.ID, map[string]string{"tmdb": "555"}); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkPlayed(ctx, scope, film.GUID, true); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(old); err != nil {
		t.Fatal(err)
	}
	fresh := filepath.Join(root, "Proper Title (2010)", "Proper.Title.2010.2160p.mkv")
	writeSized(t, fresh, 5000)
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	nw := itemAt(t, st, fresh)
	if d, _ := st.GetUserItemData(ctx, scope, nw.GUID); d != nil && d.Played {
		t.Fatal("carried before anything tied the two together")
	}
	// The metadata lookup matches it.
	if err := st.SetProviderIDs(ctx, nw.ID, map[string]string{"tmdb": "555"}); err != nil {
		t.Fatal(err)
	}
	if n, err := st.SettleByProvider(ctx, nw.ID); err != nil || n != 1 {
		t.Fatalf("settle by provider: %d %v", n, err)
	}
	if d, _ := st.GetUserItemData(ctx, scope, nw.GUID); d == nil || !d.Played {
		t.Fatalf("history did not follow by provider id: %+v", d)
	}
}

func TestHistoryFollowsAnEpisodeByProviderID(t *testing.T) {
	ctx := context.Background()
	l, st, lib, root, scope := moveFixture(t, model.KindSeries)
	old := filepath.Join(root, "Show JP", "Show JP - S01E02.mkv")
	writeSized(t, old, 111)
	writeSized(t, filepath.Join(root, "Show JP", "Show JP - S01E01.mkv"), 112)
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	ep := itemAt(t, st, old)
	series := mustParent(t, st, mustParent(t, st, ep))
	if err := st.SetProviderIDs(ctx, series.ID, map[string]string{"tvdb": "999"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetProgress(ctx, scope, ep.GUID, 50_000, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.SetFavorite(ctx, scope, series.GUID, true); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "Show JP")); err != nil {
		t.Fatal(err)
	}
	e1, e2 := filepath.Join(root, "Show EN", "Show EN - S01E01.mkv"), filepath.Join(root, "Show EN", "Show EN - S01E02.mkv")
	writeSized(t, e1, 2001)
	writeSized(t, e2, 2002)
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	newEp := itemAt(t, st, e2)
	newSeries := mustParent(t, st, mustParent(t, st, newEp))
	if err := st.SetProviderIDs(ctx, newSeries.ID, map[string]string{"tvdb": "999"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SettleByProvider(ctx, newSeries.ID); err != nil {
		t.Fatal(err)
	}
	if d, _ := st.GetUserItemData(ctx, scope, newEp.GUID); d == nil || d.PositionMs != 50_000 {
		t.Fatalf("episode 2's history did not follow: %+v", d)
	}
	if d, _ := st.GetUserItemData(ctx, scope, itemAt(t, st, e1).GUID); d != nil && d.PositionMs != 0 {
		t.Fatalf("episode 1 got episode 2's history: %+v", d)
	}
	if d, _ := st.GetUserItemData(ctx, scope, newSeries.GUID); d == nil || !d.IsFavorite {
		t.Fatalf("the series' watchlist mark did not follow: %+v", d)
	}
}

// A show inside a grouping folder ("Kids/Show/Season 01/...") is the show, not "Kids", and
// a daily show's episode named by its date gets the date as its identity and air date.
func TestResolveNestedShowAndDatedEpisode(t *testing.T) {
	ctx := context.Background()
	l, st, lib, root, _ := moveFixture(t, model.KindSeries)
	nested := filepath.Join(root, "Kids", "Bluey", "Season 01", "Bluey - S01E02.mkv")
	daily := filepath.Join(root, "The Daily Show", "The Daily Show 2024.03.15.mkv")
	writeSized(t, nested, 10)
	writeSized(t, daily, 11)
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	ep := itemAt(t, st, nested)
	series := mustParent(t, st, mustParent(t, st, ep))
	if series.Title != "Bluey" || ep.IndexAired == nil || *ep.IndexAired != 2 {
		t.Fatalf("nested: series %q episode %v", series.Title, ep.IndexAired)
	}
	d := itemAt(t, st, daily)
	if !strings.HasSuffix(d.GUID, ":date:2024-03-15") || !strings.HasPrefix(d.Premiered, "2024-03-15") {
		t.Fatalf("dated: guid %s premiered %q", d.GUID, d.Premiered)
	}
}
