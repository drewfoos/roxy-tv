package library

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/roxy-media/roxy/server/internal/model"
)

func TestIgnorePatterns(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".ignore", "# library-wide\n*.sample.mkv\nBonus/\n")
	write("Show/.ignore", "/Season 03\n**/Recap*.mkv\n!Recap 1.mkv\n")
	c := ignoreCache{}
	cases := []struct {
		rel  string
		dir  bool
		want bool
	}{
		{"Film/Film.sample.mkv", false, true}, // root .ignore, any depth
		{"Film/Film.mkv", false, false},
		{"Show/Bonus", true, true}, // folders only
		{"Show/Bonus.mkv", false, false},
		{"Show/Season 03", true, true}, // anchored to Show/
		{"Other/Season 03", true, false},
		{"Show/Season 01/Recap 2.mkv", false, true},  // **
		{"Show/Season 01/Recap 1.mkv", false, false}, // ! brings it back
		{"Show/Season 01/Show - S01E01.mkv", false, false},
	}
	for _, cs := range cases {
		if got := c.ignored(root, filepath.Join(root, filepath.FromSlash(cs.rel)), cs.dir); got != cs.want {
			t.Errorf("%s: %v, want %v", cs.rel, got, cs.want)
		}
	}
}

// A scan honours the patterns: ignored files are not added, an empty .ignore still skips
// its whole folder, and one at the library root works (it used to be read only below it).
func TestScanHonoursIgnorePatterns(t *testing.T) {
	ctx := context.Background()
	l, st, lib, root, _ := moveFixture(t, model.KindMovies)
	for _, rel := range []string{"A (2001)/A (2001).mkv", "B (2002)/B (2002).sample.mkv", "C (2003)/C (2003).mkv", "D (2004)/D (2004).mkv"} {
		writeSized(t, filepath.Join(root, filepath.FromSlash(rel)), 10+len(rel))
	}
	if err := os.WriteFile(filepath.Join(root, ".ignore"), []byte("*.sample.mkv\n/C (2003)/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "D (2004)", ".ignore"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := l.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	m, err := st.ListPartPaths(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for p := range m {
		r, _ := filepath.Rel(root, p)
		got = append(got, filepath.ToSlash(r))
	}
	sort.Strings(got)
	if strings.Join(got, ",") != "A (2001)/A (2001).mkv" {
		t.Fatalf("scanned: %v", got)
	}
}
