package library

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/roxy-media/roxy/server/internal/config"
	"github.com/roxy-media/roxy/server/internal/safefs"
	"github.com/roxy-media/roxy/server/internal/store"
	"github.com/roxy-media/roxy/server/internal/trickplay"
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

// The same goes for everything else kept under the source's id: the fonts taken out of
// the old file, the note that its seek previews failed, and a thumb that is a frame of it.
// A still from a provider is about the episode, not the file, and stays.
func TestDropStaleCachesTakesFontsFailureNoteAndFrameThumb(t *testing.T) {
	l, st, _, paths := recordedLibrary(t)
	data := t.TempDir()
	safefs.Init(data)
	l.cfg = &config.Config{DataDir: data}
	ctx := context.Background()
	part, err := st.GetPartByPath(ctx, paths[0])
	if err != nil || part == nil {
		t.Fatalf("part: %v", err)
	}
	other, err := st.GetPartByPath(ctx, paths[1])
	if err != nil || other == nil {
		t.Fatalf("part: %v", err)
	}
	item := func(sourceID string) string {
		src, err := st.GetSource(ctx, sourceID)
		if err != nil || src == nil {
			t.Fatalf("source %s: %v", sourceID, err)
		}
		return src.ItemID
	}
	fonts := filepath.Join(l.cfg.AttachmentsDir(), part.SourceID)
	note := trickplay.FailureNote(l.cfg, part.SourceID)
	frame := filepath.Join(l.cfg.ImagesDir(), item(part.SourceID), "thumb.jpg")
	still := filepath.Join(l.cfg.ImagesDir(), item(other.SourceID), "thumb.jpg")
	seed := func() {
		for _, f := range []string{filepath.Join(fonts, "old.ttf"), note, frame, still} {
			if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(f, []byte("old"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		for _, img := range []store.ItemImage{
			{ItemID: item(part.SourceID), Type: "thumb", Path: frame, SourceURL: "frame://" + part.SourceID},
			{ItemID: item(other.SourceID), Type: "thumb", Path: still, SourceURL: "https://images.example/still.jpg"},
		} {
			if err := st.SetItemImage(ctx, img); err != nil {
				t.Fatal(err)
			}
		}
	}
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	thumb := func(sourceID string) bool {
		img, err := st.GetItemImage(ctx, item(sourceID), "thumb")
		if err != nil {
			t.Fatal(err)
		}
		return img != nil
	}

	seed()
	l.dropStaleCaches(ctx, paths[0], part.SourceID, part.Size, part.ModTime)
	if !exists(fonts) || !exists(note) || !exists(frame) || !thumb(part.SourceID) {
		t.Fatal("caches of an unchanged file were dropped")
	}
	l.dropStaleCaches(ctx, paths[0], part.SourceID, part.Size+1, part.ModTime)
	if exists(fonts) {
		t.Error("the old file's fonts are still there")
	}
	if exists(note) {
		t.Error("the note that the old file's previews failed is still there")
	}
	if thumb(part.SourceID) || exists(frame) {
		t.Error("the thumb is still a frame of the old file")
	}
	// The other file changes too: its thumb came from a provider and is kept.
	l.dropStaleCaches(ctx, paths[1], other.SourceID, other.Size+1, other.ModTime)
	if !thumb(other.SourceID) || !exists(still) {
		t.Error("a provider's still was dropped with the file's caches")
	}
}
