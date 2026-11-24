package music

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/roxy-media/roxy/server/internal/safefs"
)

func newArtStore(t *testing.T) *ArtStore {
	t.Helper()
	data := t.TempDir()
	safefs.Init(data)
	return NewArtStore(filepath.Join(data, "cache", "music", "art"))
}

// The same picture is kept once, under an address made from its bytes; what is not a
// picture is refused.
func TestArtStorePut(t *testing.T) {
	a := newArtStore(t)
	pic := testPicture(t, 1)
	h1, err := a.Put(pic)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := a.Put(append([]byte(nil), pic...))
	if err != nil || h1 != h2 {
		t.Fatalf("the same bytes got two addresses: %q %q (%v)", h1, h2, err)
	}
	if !ValidArtHash(h1) || !a.Has(h1) {
		t.Fatalf("hash %q", h1)
	}
	if got, _ := os.ReadFile(a.Path(h1)); string(got) != string(pic) {
		t.Error("the stored picture is not the one given")
	}
	other, _ := a.Put(testPicture(t, 2))
	if other == h1 {
		t.Error("two pictures share an address")
	}
	for name, data := range map[string][]byte{
		"nothing":    nil,
		"text":       []byte("<svg xmlns='http://www.w3.org/2000/svg'><script>alert(1)</script></svg>"),
		"half a PNG": pic[:20],
		"too large":  make([]byte, maxArtBytes+1),
	} {
		if _, err := a.Put(data); !errors.Is(err, ErrNotAPicture) {
			t.Errorf("%s was kept as a picture: %v", name, err)
		}
	}
	for _, bad := range []string{"", "abc", "../../etc/passwd", h1 + "0", "ZZ" + h1[2:]} {
		if ValidArtHash(bad) || a.Has(bad) {
			t.Errorf("%q passed as a hash", bad)
		}
	}
}

// Pictures nothing shows any more are removed with their resized copies; a store most of
// which looks unused is left alone (the library's drive is not there).
func TestArtStoreSweep(t *testing.T) {
	a := newArtStore(t)
	var hashes []string
	for i := 0; i < 30; i++ {
		h, err := a.Put(testPicture(t, uint8(i+1)))
		if err != nil {
			t.Fatal(err)
		}
		hashes = append(hashes, h)
		if err := os.WriteFile(a.Path(h)+".w160.jpg", []byte("copy"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	keep := map[string]bool{}
	for _, h := range hashes[:25] {
		keep[h] = true
	}
	n, err := a.Sweep(keep)
	if err != nil || n != 10 { // 5 pictures and their 5 copies
		t.Fatalf("swept %d files: %v", n, err)
	}
	for i, h := range hashes {
		_, e1 := os.Stat(a.Path(h))
		_, e2 := os.Stat(a.Path(h) + ".w160.jpg")
		if kept := e1 == nil && e2 == nil; kept != (i < 25) {
			t.Errorf("picture %d: kept=%v", i, kept)
		}
	}
	// Nothing in use at all: not believed.
	if n, err := a.Sweep(map[string]bool{}); err == nil || n != 0 {
		t.Errorf("an empty library emptied the store: %d %v", n, err)
	}
	if !a.Has(hashes[0]) {
		t.Error("pictures went although the sweep refused")
	}
	// A store that does not exist yet is nothing to sweep.
	if n, err := NewArtStore(filepath.Join(t.TempDir(), "none")).Sweep(nil); n != 0 || err != nil {
		t.Errorf("%d %v", n, err)
	}
}

func TestPickFolderArt(t *testing.T) {
	names := []string{"01 - Song.mp3", "Folder.JPG", "cover.png", "back.jpg", "cover.txt", "artist.webp"}
	if got := pickFolderArt(names); got != "cover.png" {
		t.Errorf("album picture: %q", got)
	}
	if got := pickFolderArt(names, "artist"); got != "artist.webp" {
		t.Errorf("artist picture: %q", got)
	}
	if got := pickFolderArt([]string{"back.jpg", "cover.txt"}); got != "" {
		t.Errorf("%q", got)
	}
}
