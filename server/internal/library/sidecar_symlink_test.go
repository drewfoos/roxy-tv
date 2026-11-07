package library

import (
	"os"
	"path/filepath"
	"testing"
)

// A sidecar subtitle symlinked out of the library (to the database, say) is never
// recorded; one symlinked to another file inside the library still is.
func TestSidecarSymlinkOutsideLibraryIsSkipped(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "Anime")
	show := filepath.Join(root, "Show")
	if err := os.MkdirAll(show, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(base, "roxy.db")
	video := filepath.Join(show, "Show - S01E01.mkv")
	real := filepath.Join(show, "Show - S01E01.en.srt")
	for _, f := range []string{secret, video, real} {
		if err := os.WriteFile(f, []byte("1\n00:00:01,000 --> 00:00:02,000\nhi\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(secret, filepath.Join(show, "Show - S01E01.de.srt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(show, "Show - S01E01.fr.srt")); err != nil {
		t.Fatal(err)
	}
	got, err := sidecarSubtitles(video, root, 10)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, st := range got {
		names[filepath.Base(st.ExternalPath)] = true
	}
	if names["Show - S01E01.de.srt"] {
		t.Errorf("sidecar symlinked outside the library was recorded: %v", names)
	}
	if !names["Show - S01E01.en.srt"] || !names["Show - S01E01.fr.srt"] {
		t.Errorf("in-library sidecars missing: %v", names)
	}
}
