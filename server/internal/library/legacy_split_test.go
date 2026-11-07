package library

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/roxy-media/roxy/server/internal/config"
	"github.com/roxy-media/roxy/server/internal/ffmpeg"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/safefs"
	"github.com/roxy-media/roxy/server/internal/store"
)

// TestLegacyMergedFilmsSplitByScan is TestLegacyMergedFilmsSplit through the whole
// pipeline: real (tiny) videos, a database in the shape an older release left it (two
// films of a collection folder on one item, which holds the watch history), and the real
// forced scan, which probes the files, moves sources and prunes. Afterwards:
//
//   - the first film keeps the merged item (same id and identity) and all its history;
//   - the second film is an item of its own, holding its own media source and no history;
//   - each film has exactly its own file, probed (a video stream, a duration), so plays;
//   - two versions of one film merged the same way stay one item with both;
//   - no item is left without media, and CheckIntegrity is clean;
//   - a second forced scan changes no identity and no history.
//
// Needs ffmpeg and ffprobe (see TestLibraryLifecycle for running it in a container).
func TestLegacyMergedFilmsSplitByScan(t *testing.T) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		skipUnlessRequired(t, "ffmpeg/ffprobe not installed")
	}
	ff, err := ffmpeg.New("", "")
	if err != nil {
		skipUnlessRequired(t, err.Error())
	}
	ctx := context.Background()
	data := t.TempDir()
	safefs.Init(data)
	st, err := store.Open(filepath.Join(data, "roxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	root := filepath.Join(t.TempDir(), "Movies")
	lib := &model.Library{Name: "Movies", Kind: model.KindMovies, Root: root}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	safefs.SetMediaRoots([]string{root})
	l := New(st, ff, &config.Config{DataDir: data})
	u := &model.User{ID: "u1", Name: "drew"}
	if err := st.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	scope, err := st.DefaultProfileID(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}

	n := 0
	video := func(path string) {
		t.Helper()
		n++
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command("ffmpeg", "-v", "error", "-y",
			"-f", "lavfi", "-i", fmt.Sprintf("testsrc=duration=%d:size=320x240:rate=10", 1+n),
			"-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=%d:duration=%d", 200+n*10, 1+n),
			"-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac", "-shortest", "-f", "matroska", path).CombinedOutput()
		if err != nil {
			t.Fatalf("making %s: %v %s", path, err, out)
		}
	}
	// legacy records what an older release did: one item for the folder, every file in
	// it a source of that item (no streams: the old probe data does not matter, a forced
	// scan re-probes).
	legacy := func(folder string, files ...string) *model.Item {
		t.Helper()
		it := &model.Item{ID: stableID("item", folder), GUID: guidFor(nil, "movie", root, folder), LibraryID: lib.ID, Type: model.TypeMovie, Title: folder}
		if err := st.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			path := filepath.Join(root, folder, f)
			video(path)
			src := &model.MediaSource{ID: stableID("source", path), ItemID: it.ID, Name: f,
				Parts: []model.MediaPart{{ID: stableID("part", path), Path: path}}}
			if err := st.ReplaceSource(ctx, src, "{}"); err != nil {
				t.Fatal(err)
			}
		}
		return it
	}
	filmOne := filepath.Join(root, "Box", "Film One (2001).mkv")
	filmTwo := filepath.Join(root, "Box", "Film Two (2002).mkv")
	box := legacy("Box", "Film One (2001).mkv", "Film Two (2002).mkv")
	reze := legacy("Reze (2025)", "Reze (2025) - 1080p.mkv", "Reze (2025) - 2160p.mkv")
	// The merged item's history: a position, and on the watchlist.
	if err := st.SetProgress(ctx, scope, box.GUID, 600_000, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.SetFavorite(ctx, scope, box.GUID, true); err != nil {
		t.Fatal(err)
	}

	itemOf := func(path string) *model.Item {
		t.Helper()
		it, err := st.ItemForPath(ctx, path)
		if err != nil || it == nil {
			t.Fatalf("%s: no item (%v)", filepath.Base(path), err)
		}
		return it
	}
	// ownsExactly: the item's sources are exactly these files, each probed and on disk.
	ownsExactly := func(step string, it *model.Item, paths ...string) {
		t.Helper()
		srcs, err := st.SourcesForItem(ctx, it.ID)
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]bool{}
		for _, s := range srcs {
			video := false
			for _, x := range s.Streams {
				video = video || x.Type == model.StreamVideo
			}
			if !video || s.DurationMs <= 0 || len(s.Parts) != 1 {
				t.Fatalf("%s: %q's source %s is not playable: video=%v duration=%d parts=%d", step, it.Title, s.Name, video, s.DurationMs, len(s.Parts))
			}
			if _, err := os.Stat(s.Parts[0].Path); err != nil {
				t.Fatalf("%s: %q's file is missing: %v", step, it.Title, err)
			}
			got[s.Parts[0].Path] = true
		}
		if len(got) != len(paths) {
			t.Fatalf("%s: %q has %d files, want %d (%v)", step, it.Title, len(got), len(paths), got)
		}
		for _, p := range paths {
			if !got[p] {
				t.Fatalf("%s: %q does not hold %s (%v)", step, it.Title, filepath.Base(p), got)
			}
		}
	}
	history := func(it *model.Item) (int64, bool) {
		d, _ := st.GetUserItemData(ctx, scope, it.GUID)
		if d == nil {
			return 0, false
		}
		return d.PositionMs, d.IsFavorite
	}
	clean := func(step string) {
		t.Helper()
		probs, err := st.CheckIntegrity(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(probs) > 0 {
			lost, _ := st.LostWatchData(ctx)
			t.Fatalf("%s: %v (lost: %s)", step, probs, lost)
		}
		// Every film left is one with files: the split leaves no empty item behind.
		yes := true
		all, total, err := st.ListItems(ctx, store.ItemQuery{LibraryID: lib.ID, Types: []model.ItemType{model.TypeMovie}})
		if err != nil {
			t.Fatal(err)
		}
		with, _, err := st.ListItems(ctx, store.ItemQuery{LibraryID: lib.ID, Types: []model.ItemType{model.TypeMovie}, HasMedia: &yes})
		if err != nil {
			t.Fatal(err)
		}
		if total != 3 || len(with) != 3 {
			names := []string{}
			for _, it := range all {
				names = append(names, it.Title)
			}
			t.Fatalf("%s: want 3 films, all with files; got %d, %d with files: %v", step, total, len(with), names)
		}
	}
	type ident struct{ id, guid string }
	identities := func() map[string]ident {
		out := map[string]ident{}
		for _, p := range []string{filmOne, filmTwo, filepath.Join(root, "Reze (2025)", "Reze (2025) - 1080p.mkv"), filepath.Join(root, "Reze (2025)", "Reze (2025) - 2160p.mkv")} {
			it := itemOf(p)
			out[p] = ident{it.ID, it.GUID}
		}
		return out
	}

	// The upgrade: a forced scan, as `roxy scan --force` or the admin page's button.
	if err := l.ScanAll(ctx, true); err != nil {
		t.Fatal(err)
	}
	one, two := itemOf(filmOne), itemOf(filmTwo)
	if one.ID != box.ID || one.GUID != box.GUID {
		t.Fatalf("Film One left the merged item: %s %s, want %s %s", one.ID, one.GUID, box.ID, box.GUID)
	}
	if one.Title != "Film One" {
		t.Fatalf("Film One's item is called %q", one.Title)
	}
	if two.ID == box.ID || two.GUID == box.GUID || two.Title != "Film Two" {
		t.Fatalf("Film Two stayed merged: %s %s %q", two.ID, two.GUID, two.Title)
	}
	ownsExactly("split", one, filmOne)
	ownsExactly("split", two, filmTwo)
	if pos, fav := history(one); pos != 600_000 || !fav {
		t.Fatalf("Film One's history: position %d, watchlist %v; want 600000, true", pos, fav)
	}
	if pos, fav := history(two); pos != 0 || fav {
		t.Fatalf("Film Two was given Film One's history: position %d, watchlist %v", pos, fav)
	}
	r := itemOf(filepath.Join(root, "Reze (2025)", "Reze (2025) - 1080p.mkv"))
	if r.ID != reze.ID {
		t.Fatalf("two versions of one film were split: %s, want %s", r.ID, reze.ID)
	}
	ownsExactly("split", r, filepath.Join(root, "Reze (2025)", "Reze (2025) - 1080p.mkv"), filepath.Join(root, "Reze (2025)", "Reze (2025) - 2160p.mkv"))
	clean("after the split")

	// Again, forced: nothing moves now that the merge is undone.
	before := identities()
	if err := l.ScanAll(ctx, true); err != nil {
		t.Fatal(err)
	}
	for p, was := range before {
		if now := identities()[p]; now != was {
			t.Fatalf("second forced scan moved %s: %+v -> %+v", filepath.Base(p), was, now)
		}
	}
	one, two = itemOf(filmOne), itemOf(filmTwo)
	ownsExactly("second scan", one, filmOne)
	ownsExactly("second scan", two, filmTwo)
	if pos, fav := history(one); pos != 600_000 || !fav {
		t.Fatalf("after the second scan, Film One's history: %d %v", pos, fav)
	}
	if pos, fav := history(two); pos != 0 || fav {
		t.Fatalf("after the second scan, Film Two has history: %d %v", pos, fav)
	}
	clean("after the second scan")
}
