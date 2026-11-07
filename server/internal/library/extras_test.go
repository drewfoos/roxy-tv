package library

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

func TestExtraOf(t *testing.T) {
	cases := []struct {
		rel, kind, owner, base string
		ok                     bool
	}{
		{"Film (2020)/Film (2020)-trailer.mkv", "trailer", "Film (2020)", "Film (2020)", true},
		{"Film (2020)/Film (2020).featurette2.mkv", "featurette", "Film (2020)", "Film (2020)", true},
		{"Film (2020)/Film (2020)_deletedscene.mkv", "deleted", "Film (2020)", "Film (2020)", true},
		{"Film (2020)/Trailers/Teaser.mkv", "trailer", "Film (2020)", "", true},
		{"Film (2020)/Behind The Scenes/Making of.mkv", "behindthescenes", "Film (2020)", "", true},
		{"Show/Season 01/Extras/Clean OP.mkv", "extra", "Show/Season 01", "", true},
		{"Show/Featurettes/Interview.mkv", "featurette", "Show", "", true},
		{"Film (2020)/Film (2020).mkv", "", "", "", false},
		{"Trailers/Thing.mkv", "", "", "", false},               // a folder at the root is a title
		{"Film (2020)/trailer.mkv", "", "", "", false},          // nothing before the suffix
		{"Show/Season 01/Show - S01E01 - The Interview.mkv", "", "", "", false},
		{"Scenes From a Marriage (1973)/Scenes From a Marriage (1973).mkv", "", "", "", false},
	}
	for _, c := range cases {
		kind, owner, base, ok := ExtraOf(c.rel)
		if kind != c.kind || owner != c.owner || base != c.base || ok != c.ok {
			t.Errorf("%s: %q %q %q %v", c.rel, kind, owner, base, ok)
		}
	}
}

// A film's trailer (suffix) and a featurette (folder) are extras of the film, not a
// second version of it (the trailer used to be taken for one) and not titles of their
// own; the film keeps one version.
func TestScanMakesExtras(t *testing.T) {
	ctx := context.Background()
	l, st, lib, root, _ := moveFixture(t, model.KindMovies)
	film := filepath.Join(root, "Film (2020)", "Film (2020).mkv")
	writeSized(t, film, 1000)
	writeSized(t, filepath.Join(root, "Film (2020)", "Film (2020)-trailer.mkv"), 100)
	writeSized(t, filepath.Join(root, "Film (2020)", "Featurettes", "Making Of.mkv"), 200)
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	f := itemAt(t, st, film)
	src, err := st.SourcesForItem(ctx, f.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(src) != 1 {
		t.Fatalf("the film has %d versions, want 1", len(src))
	}
	extras, err := st.Extras(ctx, f.ID)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]string{}
	for _, e := range extras {
		kinds[e.Title] = e.ExtraType
	}
	if len(extras) != 2 || kinds["Trailer"] != "trailer" || kinds["Making Of"] != "featurette" {
		t.Fatalf("extras: %v", kinds)
	}
	items, _, err := st.ListItems(ctx, store.ItemQuery{LibraryID: lib.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("library lists %d items, want only the film", len(items))
	}
	// A second scan leaves them alone.
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	if again, _ := st.Extras(ctx, f.ID); len(again) != 2 {
		t.Fatalf("after a rescan: %d extras", len(again))
	}
}
