package metadata

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A One Pace style tvshow.nfo: no provider ids, so it is the metadata.
func TestReadNFOShowWithoutIDs(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "tvshow.nfo")
	writeFile(t, p, `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<tvshow>
  <title>One Pace</title>
  <plot>One Piece, re-cut to match the manga's pacing.</plot>
  <premiered>2013-12-01</premiered>
  <genre>Anime</genre><genre>Adventure</genre>
  <studio>One Pace Team</studio>
  <status>Continuing</status>
</tvshow>
https://onepace.net`)
	d, err := readNFO(p)
	if err != nil || d == nil {
		t.Fatalf("readNFO: %v %v", d, err)
	}
	if ids := d.ids(); len(ids) != 0 {
		t.Errorf("ids = %v, want none", ids)
	}
	m := d.meta()
	if m.Title != "One Pace" || m.Studio != "One Pace Team" || len(m.Genres) != 2 || m.Premiered != "2013-12-01" {
		t.Errorf("meta = %+v", m)
	}
	if m.Year == nil || *m.Year != 2013 {
		t.Errorf("year = %v, want 2013 from premiered", m.Year)
	}
}

func TestReadNFOIDs(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "movie.nfo")
	writeFile(t, p, `<movie><title>X</title><uniqueid type="tmdb" default="true">129</uniqueid><uniqueid type="imdb">tt0245429</uniqueid><tvdbid>77</tvdbid></movie>`)
	d, err := readNFO(p)
	if err != nil {
		t.Fatal(err)
	}
	ids := d.ids()
	if ids["tmdb"] != "129" || ids["imdb"] != "tt0245429" || ids["tvdb"] != "77" {
		t.Errorf("ids = %v", ids)
	}
}

func TestReadNFOEpisodeAndMissing(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "ep.nfo")
	writeFile(t, p, `<episodedetails><title>Romance Dawn</title><season>1</season><episode>1</episode><aired>2013-12-01T00:00:00</aired><plot>Luffy sets out &amp; meets Coby.</plot></episodedetails>`)
	d, err := readNFO(p)
	if err != nil {
		t.Fatal(err)
	}
	m := d.meta()
	if m.Title != "Romance Dawn" || m.Premiered != "2013-12-01" || m.Overview != "Luffy sets out & meets Coby." {
		t.Errorf("meta = %+v", m)
	}
	if d, err := readNFO(filepath.Join(dir, "nope.nfo")); d != nil || err != nil {
		t.Errorf("missing file: %v %v", d, err)
	}
}

// An .nfo's <sorttitle> is the item's sort title: set from it, and cleared when the tag is
// gone (it was parsed and dropped before). Online providers leave it alone (nil).
func TestNFOSortTitle(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "tvshow.nfo")
	writeFile(t, p, `<tvshow><title>Bakemonogatari</title><sorttitle> Monogatari 01 </sorttitle></tvshow>`)
	d, err := readNFO(p)
	if err != nil || d == nil {
		t.Fatal(d, err)
	}
	if m := d.meta(); m.SortTitle == nil || *m.SortTitle != "Monogatari 01" {
		t.Fatalf("sort title = %v", m.SortTitle)
	}
	writeFile(t, p, `<tvshow><title>Bakemonogatari</title></tvshow>`)
	d, _ = readNFO(p)
	if m := d.meta(); m.SortTitle == nil || *m.SortTitle != "" {
		t.Fatalf("an .nfo without <sorttitle> must clear it: %v", m.SortTitle)
	}
}
