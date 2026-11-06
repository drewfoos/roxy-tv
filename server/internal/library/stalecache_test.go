package library

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/roxy-media/roxy/server/internal/config"
	"github.com/roxy-media/roxy/server/internal/safefs"
)

// A file replaced in place keeps its source id, so its cached subtitles and trickplay
// must be dropped when the size or mtime changes, and kept when nothing changed.
func TestDropStaleCachesOnChangedFile(t *testing.T) {
	l, st, _, paths := recordedLibrary(t)
	data := t.TempDir()
	safefs.Init(data)
	l.cfg = &config.Config{DataDir: data}
	ctx := context.Background()
	path := paths[0]
	part, err := st.GetPartByPath(ctx, path)
	if err != nil || part == nil {
		t.Fatalf("part: %v", err)
	}
	const sourceID = "src1"
	subs := filepath.Join(l.cfg.SubtitlesDir(), sourceID)
	trick := filepath.Join(l.cfg.TrickplayDir(), sourceID)
	seed := func() {
		for _, d := range []string{subs, trick} {
			if err := os.MkdirAll(d, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(d, "f"), []byte("old"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	exists := func(d string) bool { _, err := os.Stat(d); return err == nil }

	seed()
	l.dropStaleCaches(ctx, path, sourceID, part.Size, part.ModTime)
	if !exists(subs) || !exists(trick) {
		t.Fatal("caches of an unchanged file were dropped")
	}
	l.dropStaleCaches(ctx, path, sourceID, part.Size+1, part.ModTime)
	if exists(subs) || exists(trick) {
		t.Fatal("caches survived a size change")
	}
	seed()
	l.dropStaleCaches(ctx, path, sourceID, part.Size, part.ModTime.Add(time.Minute))
	if exists(subs) || exists(trick) {
		t.Fatal("caches survived an mtime change")
	}
}
