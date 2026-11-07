package library

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/roxy-media/roxy/server/internal/model"
)

// Found by an audit: a dated episode, once the metadata pass gave it a number, keeps its
// date GUID on a rescan. The number, read back as a hint, used to change it to sNNeNNNN.
func TestDatedEpisodeKeepsGUIDAfterMetadata(t *testing.T) {
	ctx := context.Background()
	l, st, lib, root, _ := moveFixture(t, model.KindSeries)
	daily := filepath.Join(root, "The Daily Show", "The Daily Show 2024.03.15.mkv")
	writeSized(t, daily, 11)
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	d := itemAt(t, st, daily)
	before := d.GUID
	// What matchDatedEpisodes/placeInSeason do: number it S29E74.
	s29, e74 := 29, 74
	d.SeasonAired, d.IndexAired = &s29, &e74
	if err := st.UpsertItem(ctx, d); err != nil {
		t.Fatal(err)
	}
	it, err := l.resolveItem(ctx, lib, daily)
	if err != nil {
		t.Fatal(err)
	}
	if it.GUID != before {
		t.Fatalf("dated episode identity changed on rescan: %s -> %s", before, it.GUID)
	}
}

// Found by an audit: a loosely named episode ("Show [05]") placed elsewhere by an .nfo
// keeps its GUID.
func TestWeakEpisodeKeepsGUIDAfterNFOPlacement(t *testing.T) {
	ctx := context.Background()
	l, st, lib, root, _ := moveFixture(t, model.KindSeries)
	f := filepath.Join(root, "Show", "[Grp] Show [05][1080p].mkv")
	writeSized(t, f, 11)
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	d := itemAt(t, st, f)
	before := d.GUID
	s2, e5 := 2, 5
	d.SeasonAired, d.IndexAired = &s2, &e5
	if err := st.UpsertItem(ctx, d); err != nil {
		t.Fatal(err)
	}
	it, err := l.resolveItem(ctx, lib, f)
	if err != nil {
		t.Fatal(err)
	}
	if it.GUID != before {
		t.Fatalf("weak-numbered episode identity changed on rescan: %s -> %s", before, it.GUID)
	}
}
