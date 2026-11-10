package metadata

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/roxy-media/roxy/server/internal/config"
	"github.com/roxy-media/roxy/server/internal/safefs"
)

// A new logo drops the resized copies of the old one. Logos are cached as .png, and only
// .jpg variants used to be removed, so the old logo kept being served at those widths.
func TestNewImageDropsPNGAndJPEGVariants(t *testing.T) {
	f := newFixture(t, false)
	data := t.TempDir()
	safefs.Init(data)
	f.svc.cfg = &config.Config{DataDir: data}

	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 8, 4))); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "logo.png")
	if err := os.WriteFile(src, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(f.svc.cfg.ImagesDir(), f.series.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := []string{"logo.w160.png", "logo.w320.jpg"}
	kept := []string{"backdrop.w160.jpg", "logo.png.bak", "logo.wide.png", "logos.w160.png"}
	for _, n := range append(append([]string{}, stale...), kept...) {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	f.svc.fetchImageOpt(f.ctx, f.series.ID, "logo", "file://"+src, true)
	if _, err := os.Stat(filepath.Join(dir, "logo.png")); err != nil {
		t.Fatalf("the new logo was not saved: %v", err)
	}
	for _, n := range stale {
		if _, err := os.Stat(filepath.Join(dir, n)); err == nil {
			t.Errorf("stale variant %s survived a new logo", n)
		}
	}
	for _, n := range kept {
		if _, err := os.Stat(filepath.Join(dir, n)); err != nil {
			t.Errorf("%s should not have been removed", n)
		}
	}
}

// Picking, by hand, the image a title already has pins it: nothing needs downloading,
// but the next refresh must not swap it for the provider's new favourite. It used to
// report success and leave the image unpinned.
func TestPickingTheCurrentImagePinsIt(t *testing.T) {
	f := newFixture(t, false)
	data := t.TempDir()
	safefs.Init(data)
	f.svc.cfg = &config.Config{DataDir: data}

	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 8, 4))); err != nil {
		t.Fatal(err)
	}
	art := t.TempDir()
	first, second := filepath.Join(art, "a.png"), filepath.Join(art, "b.png")
	for _, p := range []string{first, second} {
		if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A refresh saved the provider's poster.
	f.svc.fetchImage(f.ctx, f.series.ID, "poster", "file://"+first)
	if img, _ := f.st.GetItemImage(f.ctx, f.series.ID, "poster"); img == nil || img.Locked {
		t.Fatalf("after a refresh: %+v, want the poster saved and not pinned", img)
	}
	// The admin picks that same poster.
	if err := f.svc.SetImage(f.ctx, f.series.ID, "poster", "file://"+first); err != nil {
		t.Fatal(err)
	}
	if img, _ := f.st.GetItemImage(f.ctx, f.series.ID, "poster"); img == nil || !img.Locked {
		t.Fatalf("after picking it by hand: %+v, want it pinned", img)
	}
	// A later refresh offers another poster: the pick stays.
	f.svc.fetchImage(f.ctx, f.series.ID, "poster", "file://"+second)
	if img, _ := f.st.GetItemImage(f.ctx, f.series.ID, "poster"); img == nil || img.SourceURL != "file://"+first {
		t.Errorf("a refresh replaced the picked poster: %+v", img)
	}
}
