package library

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSkipFileReason(t *testing.T) {
	skip := []string{"sample.mkv", "Sample.mp4", "show.s01e01-sample.mkv", "movie.sample.mkv", "x_sample.avi", "sample-group.mkv", ".hidden.mkv"}
	keep := []string{"Shomin Sample - S01E01.mkv", "Shomin Sample (2015).mkv", "Samples of Life - 01.mkv", "Frieren - S01E01.mkv"}
	for _, n := range skip {
		if SkipFileReason(n) == "" {
			t.Errorf("%q should be skipped", n)
		}
	}
	for _, n := range keep {
		if r := SkipFileReason(n); r != "" {
			t.Errorf("%q should be scanned, got %q", n, r)
		}
	}
}

func TestSkipDirReason(t *testing.T) {
	root := t.TempDir()
	mk := func(name string) string {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	for _, n := range []string{"downloads", "Incomplete", "Featurettes", "@eaDir", "#recycle", "_UNPACK_Frieren", ".grab"} {
		if SkipDirReason(mk(n)) == "" {
			t.Errorf("%q should be skipped", n)
		}
	}
	for _, n := range []string{"Frieren (2023)", "Season 01", "Specials", "Shomin Sample (2015)", "Downloaded Memories (2020)"} {
		if r := SkipDirReason(mk(n)); r != "" {
			t.Errorf("%q should be scanned, got %q", n, r)
		}
	}
	marked := mk("Private")
	if err := os.WriteFile(filepath.Join(marked, ".ignore"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if SkipDirReason(marked) == "" {
		t.Error("a folder with .ignore should be skipped")
	}
}
