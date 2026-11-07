package library

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/roxy-media/roxy/server/internal/model"
)

// Found by an audit: a legacy merged item whose first file (by path) is already deleted: the
// remaining film should keep the item (and its history), not split off.
func TestLegacySplitWithFirstFileGone(t *testing.T) {
	ctx := context.Background()
	l, st, lib, root, scope := moveFixture(t, model.KindMovies)
	it := &model.Item{ID: "legacy", GUID: guidFor(nil, "movie", root, "Box"), LibraryID: lib.ID, Type: model.TypeMovie, Title: "Box"}
	if err := st.UpsertItem(ctx, it); err != nil {
		t.Fatal(err)
	}
	a := filepath.Join(root, "Box", "Film One (2001).mkv")
	b := filepath.Join(root, "Box", "Film Two (2002).mkv")
	for i, p := range []string{a, b} {
		writeSized(t, p, 100+i)
		fi, _ := os.Stat(p)
		if err := st.ReplaceSource(ctx, &model.MediaSource{ID: stableID("source", p), ItemID: it.ID, Size: fi.Size(),
			Parts: []model.MediaPart{{ID: stableID("part", p), Path: p, Size: fi.Size(), ModTime: fi.ModTime().UTC()}}}, "{}"); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SetProgress(ctx, scope, it.GUID, 600_000, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(a); err != nil {
		t.Fatal(err)
	}
	if err := l.ScanAll(ctx, true); err != nil {
		t.Fatal(err)
	}
	got := itemAt(t, st, b)
	if got.GUID != it.GUID {
		t.Fatalf("the only film left in the merged item was split off: %s -> %s (history stays on the deleted file's identity)", it.GUID, got.GUID)
	}
}

// Found by an audit: an older release merged a film with a loose bonus file in its own
// folder; the split kept the item on the first path, which was the bonus file, so the film
// lost its history. The file named as its folder's film keeps it now.
func TestLegacySplitKeepsTheFilmNotABonus(t *testing.T) {
	ctx := context.Background()
	l, st, lib, root, scope := moveFixture(t, model.KindMovies)
	it := &model.Item{ID: "legacy", GUID: guidFor(nil, "movie", root, "Film (2020)"), LibraryID: lib.ID, Type: model.TypeMovie, Title: "Film"}
	if err := st.UpsertItem(ctx, it); err != nil {
		t.Fatal(err)
	}
	film := filepath.Join(root, "Film (2020)", "Film (2020).mkv")
	bonus := filepath.Join(root, "Film (2020)", "Deleted Scenes.mkv")
	for i, p := range []string{film, bonus} {
		writeSized(t, p, 100+i)
		fi, _ := os.Stat(p)
		if err := st.ReplaceSource(ctx, &model.MediaSource{ID: stableID("source", p), ItemID: it.ID, Size: fi.Size(),
			Parts: []model.MediaPart{{ID: stableID("part", p), Path: p, Size: fi.Size(), ModTime: fi.ModTime().UTC()}}}, "{}"); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SetProgress(ctx, scope, it.GUID, 600_000, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := l.ScanAll(ctx, true); err != nil {
		t.Fatal(err)
	}
	if got := itemAt(t, st, film); got.GUID != it.GUID {
		b := itemAt(t, st, bonus)
		t.Fatalf("the film moved to a new item %s; the merged item (with history) is now %q (bonus kept=%v)", got.GUID, b.Title, b.GUID == it.GUID)
	}
}
