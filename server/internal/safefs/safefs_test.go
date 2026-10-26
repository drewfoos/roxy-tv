package safefs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOnlyTheDataFolderIsWritable(t *testing.T) {
	base := t.TempDir()
	data := filepath.Join(base, "data")
	media := filepath.Join(base, "media")
	nested := filepath.Join(data, "media-inside-data")
	for _, d := range []string{data, media, nested} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	Init(data)
	SetMediaRoots([]string{media, nested})

	ok := []string{filepath.Join(data, "cache", "x.jpg"), filepath.Join(data, "a")}
	for _, p := range ok {
		if err := Check(p); err != nil {
			t.Errorf("%s should be writable: %v", p, err)
		}
	}
	bad := []string{
		data, // the folder itself
		media, filepath.Join(media, "Show", "ep.mkv"),
		nested, filepath.Join(nested, "movie.mkv"),
		filepath.Join(data, "..", "media", "x.mkv"),
		base, "/", filepath.Join(base, "elsewhere"),
	}
	for _, p := range bad {
		if err := Check(p); err == nil {
			t.Errorf("%s must be refused", p)
		}
	}
	// A real delete aimed at media is refused and the file survives.
	f := filepath.Join(media, "keep.mkv")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Remove(f); err == nil {
		t.Fatal("Remove in a media folder must fail")
	}
	if err := RemoveAll(media); err == nil {
		t.Fatal("RemoveAll of a media folder must fail")
	}
	if _, err := os.Stat(f); err != nil {
		t.Fatal("the media file was touched")
	}
}
