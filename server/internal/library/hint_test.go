package library

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/roxy-media/roxy/server/internal/model"
)

// A name the parser cannot number takes Sonarr's season/episode, and a later scan
// (no hint) keeps that episode instead of falling back to the bare filename.
func TestEpisodeHintAndRescanKeepsIt(t *testing.T) {
	l, st, lib, ctx := testLibrary(t)
	path := filepath.Join(lib.Root, "Show (2022)", "Season 02", "[Grp] Weird Name Without Numbers.mkv")
	hint := NewEpisodeHint([][2]int{{2, 3}})
	it, err := l.resolveItemHinted(ctx, lib, path, hint)
	if err != nil {
		t.Fatal(err)
	}
	if it.IndexAired == nil || *it.IndexAired != 3 || it.SeasonAired == nil || *it.SeasonAired != 2 || !strings.HasSuffix(it.GUID, ":s02e0003") {
		t.Fatalf("hinted item: index=%v season=%v guid=%s", it.IndexAired, it.SeasonAired, it.GUID)
	}
	// Attach the file as AddFile would, then rescan without a hint.
	srcID := stableID("source", path)
	src := &model.MediaSource{ID: srcID, ItemID: it.ID, Name: filepath.Base(path), IsPrimary: true,
		Parts: []model.MediaPart{{ID: stableID("part", path), SourceID: srcID, Index: 0, Path: path}}}
	if err := st.ReplaceSource(ctx, src, "{}"); err != nil {
		t.Fatal(err)
	}
	again, err := l.resolveItem(ctx, lib, path)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != it.ID {
		t.Fatalf("rescan made a new item: %s (guid %s) vs %s (guid %s)", again.ID, again.GUID, it.ID, it.GUID)
	}
	if h := l.HintForPath(ctx, path); h == nil || h.Season != 2 || len(h.Episodes) != 1 || h.Episodes[0] != 3 {
		t.Fatalf("HintForPath = %+v", h)
	}
}

// A readable name always wins over the hint: a scan, which has no hint, must reach the
// same item the webhook did.
func TestParsedNameBeatsHint(t *testing.T) {
	l, _, lib, ctx := testLibrary(t)
	path := filepath.Join(lib.Root, "Show (2022)", "Season 01", "Show - S01E05.mkv")
	it, err := l.resolveItemHinted(ctx, lib, path, NewEpisodeHint([][2]int{{1, 9}}))
	if err != nil {
		t.Fatal(err)
	}
	if it.IndexAired == nil || *it.IndexAired != 5 {
		t.Fatalf("index = %v, want 5 from the filename", it.IndexAired)
	}
}

// Absolute-numbered names keep Roxy's own handling; the hint is only for names with no
// number at all.
func TestHintIgnoredForAbsoluteNames(t *testing.T) {
	l, _, lib, ctx := testLibrary(t)
	path := filepath.Join(lib.Root, "Show", "Season 01", "[SubsPlease] Show - 07 (1080p).mkv")
	it, err := l.resolveItemHinted(ctx, lib, path, NewEpisodeHint([][2]int{{1, 2}}))
	if err != nil {
		t.Fatal(err)
	}
	if it.IndexAbsolute == nil || *it.IndexAbsolute != 7 {
		t.Fatalf("absolute = %v, want 7", it.IndexAbsolute)
	}
}

func TestNewEpisodeHint(t *testing.T) {
	if h := NewEpisodeHint(nil); h != nil {
		t.Errorf("empty -> %+v", h)
	}
	if h := NewEpisodeHint([][2]int{{1, 1}, {2, 1}}); h != nil {
		t.Errorf("two seasons -> %+v, want nil", h)
	}
	h := NewEpisodeHint([][2]int{{3, 5}, {3, 4}})
	if h == nil || h.Season != 3 || h.Episodes[0] != 4 || h.Episodes[1] != 5 {
		t.Errorf("multi-episode -> %+v", h)
	}
	var p ParsedName
	p.applyHint(h)
	if *p.Season != 3 || *p.Episode != 4 || p.EpisodeEnd == nil || *p.EpisodeEnd != 5 {
		t.Errorf("applyHint -> %+v", p)
	}
}
