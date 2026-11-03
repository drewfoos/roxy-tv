package arrhook

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestResolve(t *testing.T) {
	roots := []string{`D:\media\tv`, `D:\media\movies`}
	existing := map[string]bool{
		filepath.FromSlash(`D:/media/tv/Show (2020)/Season 01/Show - S01E01.mkv`): true,
		filepath.FromSlash(`D:/media/movies/Film (2019)/Film.mkv`):                true,
	}
	exists := func(p string) bool { return existing[p] }

	// Explicit mapping wins.
	got, ok := Resolve("/data/media/tv/Show (2020)/Season 01/Show - S01E01.mkv", []PathMapping{{From: "/data/media", To: `D:\media`}}, roots, exists)
	if !ok || got != filepath.FromSlash(`D:/media/tv/Show (2020)/Season 01/Show - S01E01.mkv`) {
		t.Fatalf("mapping: got %q ok=%v", got, ok)
	}
	// No mapping: the suffix that exists under a root.
	got, ok = Resolve("/srv/stuff/tv/Show (2020)/Season 01/Show - S01E01.mkv", nil, roots, exists)
	if !ok || got != filepath.FromSlash(`D:/media/tv/Show (2020)/Season 01/Show - S01E01.mkv`) {
		t.Fatalf("suffix: got %q ok=%v", got, ok)
	}
	// Already local.
	got, ok = Resolve(`D:\media\movies\Film (2019)\Film.mkv`, nil, roots, exists)
	if !ok || got != filepath.FromSlash(`D:/media/movies/Film (2019)/Film.mkv`) {
		t.Fatalf("local: got %q ok=%v", got, ok)
	}
	// Nothing matches.
	if _, ok = Resolve("/elsewhere/x.mkv", nil, roots, exists); ok {
		t.Fatal("unrelated path resolved")
	}
}

func TestPayloadShapes(t *testing.T) {
	var p payload
	body := `{"EventType":"Download","Series":{"Title":"Show","Path":"/data/tv/Show"},"Episodes":[{"SeasonNumber":1,"EpisodeNumber":3}],"EpisodeFile":{"RelativePath":"Season 01/Show - S01E03.mkv"}}`
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		t.Fatal(err)
	}
	if p.title() != "Show S1 E3" {
		t.Errorf("title = %q", p.title())
	}
	if got := p.absolute(*p.EpisodeFile); got != "/data/tv/Show/Season 01/Show - S01E03.mkv" {
		t.Errorf("absolute = %q", got)
	}
	var m payload
	if err := json.Unmarshal([]byte(`{"eventType":"MovieFileDelete","movie":{"title":"Film","folderPath":"/data/movies/Film"},"movieFile":{"path":"/data/movies/Film/Film.mkv"}}`), &m); err != nil {
		t.Fatal(err)
	}
	if m.title() != "Film" || m.absolute(*m.MovieFile) != "/data/movies/Film/Film.mkv" {
		t.Errorf("movie: %q %q", m.title(), m.absolute(*m.MovieFile))
	}
}

// A webhook must not name a library root (or "/") as a series folder, nor climb out of
// a root with "..".
func TestResolveRefusesRootsAndDotDot(t *testing.T) {
	roots := []string{"/mnt/media/TV"}
	exists := func(string) bool { return true }
	if got, ok := Resolve("/", nil, roots, exists); ok && got == "/mnt/media/TV" {
		t.Fatalf("\"/\" resolved to the root")
	}
	if _, ok := Resolve("/mnt/media/TV/../../etc/passwd", nil, roots, func(p string) bool { return p == "/mnt/media/TV/../../etc/passwd" }); ok {
		t.Fatal("dot-dot escaped the root")
	}
	if !rootOrAbove("/mnt/media/TV", roots) || !rootOrAbove("/mnt/media", roots) || rootOrAbove("/mnt/media/TV/Show", roots) {
		t.Fatal("rootOrAbove")
	}
}
