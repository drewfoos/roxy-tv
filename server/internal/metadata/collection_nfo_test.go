package metadata

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/roxy-media/roxy/server/internal/model"
)

// Found by an audit: a collection folder's movie.nfo was applied to every film in the
// folder, now that each is an item of its own (each took its provider ids and sort
// title). Only a film's own folder's movie.nfo is its; local_test.go covers that one.
func TestCollectionFolderMovieNFOIsNotEveryFilms(t *testing.T) {
	f := newFixture(t, false)
	box := filepath.Join(f.lib.Root, "Box")
	if err := os.MkdirAll(box, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(box, "movie.nfo"), `<movie><title>Film One</title><sorttitle>One</sorttitle><uniqueid type="tmdb">111</uniqueid></movie>`)
	for _, n := range []string{"Film One (2001)", "Film Two (2002)"} {
		id := n
		f.upsert(&model.Item{ID: id, GUID: "g:" + id, LibraryID: f.lib.ID, Type: model.TypeMovie, Title: n})
		p := filepath.Join(box, n+".mkv")
		writeFile(t, p, "x")
		if err := f.st.ReplaceSource(f.ctx, &model.MediaSource{ID: "s" + id, ItemID: id, IsPrimary: true, Parts: []model.MediaPart{{ID: "p" + id, Path: p}}}, "{}"); err != nil {
			t.Fatal(err)
		}
	}
	two, _ := f.st.GetItem(f.ctx, "Film Two (2002)")
	if _, err := f.svc.applyLocal(f.ctx, two); err != nil {
		t.Fatal(err)
	}
	two, _ = f.st.GetItem(f.ctx, "Film Two (2002)")
	if two.ProviderIDs["tmdb"] == "111" || two.SortTitle == "One" {
		t.Fatalf("Film Two took the folder's movie.nfo: ids %v sort %q title %q", two.ProviderIDs, two.SortTitle, two.Title)
	}
}
