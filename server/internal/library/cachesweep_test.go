package library

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/roxy-media/roxy/server/internal/config"
	"github.com/roxy-media/roxy/server/internal/safefs"
)

// sweepFixture is a library of four films with a full set of caches each, and returns a
// way to plant a cache entry of a given age.
func sweepFixture(t *testing.T) (*Library, func(dir, name string, age time.Duration) string) {
	t.Helper()
	l, st, _, paths := recordedLibrary(t)
	data := t.TempDir()
	safefs.Init(data)
	l.cfg = &config.Config{DataDir: data}
	plant := func(dir, name string, age time.Duration) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if filepath.Ext(name) == ".failed" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte("{}"), 0o644); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := os.MkdirAll(p, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(p, "f"), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		at := time.Now().Add(-age)
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
		return p
	}
	ctx := context.Background()
	for _, path := range paths {
		part, err := st.GetPartByPath(ctx, path)
		if err != nil || part == nil {
			t.Fatalf("part: %v", err)
		}
		src, err := st.GetSource(ctx, part.SourceID)
		if err != nil || src == nil {
			t.Fatalf("source: %v", err)
		}
		plant(l.cfg.ImagesDir(), src.ItemID, 48*time.Hour)
		for _, dir := range []string{l.cfg.SubtitlesDir(), l.cfg.AttachmentsDir(), l.cfg.TrickplayDir()} {
			plant(dir, src.ID, 48*time.Hour)
		}
	}
	return l, plant
}

func there(p string) bool { _, err := os.Lstat(p); return err == nil }

func count(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

// Nothing removed a title's images or a file's subtitles, fonts and seek previews when
// the title or the file left the library.
func TestSweepCachesRemovesWhatBelongsToNothing(t *testing.T) {
	l, plant := sweepFixture(t)
	cfg := l.cfg
	old := 48 * time.Hour
	gone := []string{
		plant(cfg.ImagesDir(), uuid.NewString(), old),
		plant(cfg.SubtitlesDir(), uuid.NewString(), old),
		plant(cfg.AttachmentsDir(), uuid.NewString(), old),
		plant(cfg.TrickplayDir(), uuid.NewString(), old),
		plant(cfg.TrickplayDir(), uuid.NewString()+".tmp", old),
		plant(cfg.TrickplayDir(), uuid.NewString()+".failed", old),
	}
	kept := map[string]string{
		"profile pictures":                   plant(cfg.ImagesDir(), "profiles", old),
		"a name that is not an id":           plant(cfg.SubtitlesDir(), "notes", old),
		"an entry written a minute ago":      plant(cfg.AttachmentsDir(), uuid.NewString(), time.Minute),
		"previews being made a minute ago":   plant(cfg.TrickplayDir(), uuid.NewString()+".tmp", time.Minute),
		"a cache folder of another kind":     plant(cfg.CacheDir(), uuid.NewString(), old),
		"something outside the cache folder": plant(cfg.DataDir, uuid.NewString(), old),
	}
	n, err := l.SweepCaches(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != len(gone) {
		t.Errorf("removed %d entries, want %d", n, len(gone))
	}
	for _, p := range gone {
		if there(p) {
			t.Errorf("still there: %s", p)
		}
	}
	for what, p := range kept {
		if !there(p) {
			t.Errorf("%s was removed: %s", what, p)
		}
	}
	// What the library's four films own is untouched: 4 + profiles in images, and so on.
	for dir, want := range map[string]int{cfg.ImagesDir(): 5, cfg.SubtitlesDir(): 5, cfg.AttachmentsDir(): 5, cfg.TrickplayDir(): 5} {
		if got := count(t, dir); got != want {
			t.Errorf("%s holds %d entries, want %d", dir, got, want)
		}
	}
}

// A database that cannot be read, or that lists nothing, or a cache most of which seems
// to belong to nothing: each is more likely a fault than a clear-out, and nothing goes.
func TestSweepCachesLeavesEverythingWhenInDoubt(t *testing.T) {
	old := 48 * time.Hour
	t.Run("more than half of a cache would go", func(t *testing.T) {
		l, plant := sweepFixture(t)
		var orphans []string
		for i := 0; i < 5; i++ { // 5 of 9 in subtitles
			orphans = append(orphans, plant(l.cfg.SubtitlesDir(), uuid.NewString(), old))
		}
		lone := plant(l.cfg.TrickplayDir(), uuid.NewString(), old) // 1 of 5: that cache is swept
		n, err := l.SweepCaches(context.Background())
		if err != nil || n != 1 || there(lone) {
			t.Errorf("removed %d (%v), want only the one unowned preview folder", n, err)
		}
		for _, p := range orphans {
			if !there(p) {
				t.Errorf("removed from a cache that was mostly unowned: %s", p)
			}
		}
	})
	t.Run("the database lists nothing", func(t *testing.T) {
		l, plant := sweepFixture(t)
		orphan := plant(l.cfg.TrickplayDir(), uuid.NewString(), old)
		libs, err := l.store.ListLibraries(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, lib := range libs {
			if err := l.store.DeleteLibrary(context.Background(), lib.ID); err != nil {
				t.Fatal(err)
			}
		}
		n, err := l.SweepCaches(context.Background())
		if err != nil || n != 0 || !there(orphan) || count(t, l.cfg.TrickplayDir()) != 5 {
			t.Errorf("removed %d (%v) with an empty database", n, err)
		}
	})
	t.Run("the database cannot be read", func(t *testing.T) {
		l, plant := sweepFixture(t)
		orphan := plant(l.cfg.TrickplayDir(), uuid.NewString(), old)
		l.store.Close()
		n, err := l.SweepCaches(context.Background())
		if err == nil || n != 0 || !there(orphan) {
			t.Errorf("removed %d (err %v) with the database closed", n, err)
		}
	})
}
