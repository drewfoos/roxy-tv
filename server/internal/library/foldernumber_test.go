package library

import (
	"path/filepath"
	"testing"
)

func TestFolderTitleNumber(t *testing.T) {
	for _, tc := range []struct {
		stem, dir string
		want      int
		ok        bool
	}{
		{"Arlong Park 07", "[One Pace][69-95] Arlong Park", 7, true},
		{"Wano 57", "[One Pace][909-1057] Wano", 57, true},
		{"Little Garden 01", "[One Pace][115-129] Little Garden", 1, true},
		{"If You Could Go Anywhere... The Adventures of the Straw Hats 01", "[One Pace][543-560 cover stories] If You Could Go Anywhere... The Adventures of the Straw Hats", 1, true},
		// Titles that merely end in a number are not episodes.
		{"Mob Psycho 100", "Mob Psycho 100", 0, false},
		{"Blade Runner 2049", "Blade Runner 2049 (2017)", 0, false},
		{"Psych 2 Lassie Come Home", "Psych 2 Lassie Come Home (2020)", 0, false},
		{"Arlong Park 07", "Season 01", 0, false},
		{"Other Show 03", "[One Pace][69-95] Arlong Park", 0, false},
	} {
		got, ok := FolderTitleNumber(tc.stem, tc.dir)
		if ok != tc.ok || got != tc.want {
			t.Errorf("%q in %q = %d,%v; want %d,%v", tc.stem, tc.dir, got, ok, tc.want, tc.ok)
		}
	}
}

// Two arcs' "07" episodes get their numbers but stay separate items (path GUIDs).
func TestFolderTitleNumberKeepsArcsApart(t *testing.T) {
	l, _, lib, ctx := testLibrary(t)
	a, err := l.resolveItem(ctx, lib, filepath.Join(lib.Root, "One Pace", "[One Pace][69-95] Arlong Park", "Arlong Park 07.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := l.resolveItem(ctx, lib, filepath.Join(lib.Root, "One Pace", "[One Pace][909-1057] Wano", "Wano 07.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID || a.GUID == b.GUID {
		t.Fatalf("arcs collided: %s / %s", a.GUID, b.GUID)
	}
	if a.IndexAired == nil || *a.IndexAired != 7 || b.IndexAired == nil || *b.IndexAired != 7 {
		t.Fatalf("index: %v %v", a.IndexAired, b.IndexAired)
	}
}
