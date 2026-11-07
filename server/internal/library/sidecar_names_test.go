package library

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Videos whose names overlap ("1" and "10", "01" and "010", "1" and "1.5") each get only
// their own sidecars; ordinary naming (.en, .forced, .en.hi, no language) keeps working.
// A bare prefix match gave "Show - 1.mkv" the subtitles of "Show - 10.mkv".
func TestSidecarsOfOverlappingNames(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Show")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := []string{
		"Show - 1.mkv", "Show - 1.en.ass", "Show - 1.forced.ass", "Show - 1.srt",
		"Show - 10.mkv", "Show - 10.en.ass", "Show - 10.de.srt",
		"Show - 01.mkv", "Show - 01.en.hi.srt",
		"Show - 010.mkv", "Show - 010.en.ass",
		"Show - 1.5.mkv", "Show - 1.5.en.ass",
		"Show - 1 extra.en.ass", // another name altogether: nobody's
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{
		"Show - 1.mkv":   {"Show - 1.en.ass", "Show - 1.forced.ass", "Show - 1.srt"},
		"Show - 10.mkv":  {"Show - 10.de.srt", "Show - 10.en.ass"},
		"Show - 01.mkv":  {"Show - 01.en.hi.srt"},
		"Show - 010.mkv": {"Show - 010.en.ass"},
		"Show - 1.5.mkv": {"Show - 1.5.en.ass"},
	}
	for video, w := range want {
		var got []string
		for _, p := range sidecarFiles(filepath.Join(dir, video), root, entries) {
			got = append(got, filepath.Base(p))
		}
		sort.Strings(got)
		sort.Strings(w)
		if strings.Join(got, "|") != strings.Join(w, "|") {
			t.Errorf("%s:\n got  %q\n want %q", video, got, w)
		}
	}
	// The same through sidecarSubtitles (what an ingest stores), with languages read.
	streams := sidecarSubtitles(filepath.Join(dir, "Show - 1.mkv"), root, 1)
	if len(streams) != 3 {
		t.Fatalf("Show - 1 streams: %+v", streams)
	}
	for _, st := range streams {
		if strings.Contains(st.ExternalPath, "10.") || strings.Contains(st.ExternalPath, "1.5") {
			t.Errorf("Show - 1 got %s", st.ExternalPath)
		}
	}
}
