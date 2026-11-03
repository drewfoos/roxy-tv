package safefs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// A sidecar symlinked out of the media folder (the audit's /config/roxy.db case) is
// refused; a symlink that stays inside a media folder, and a media folder that is itself
// a symlink, are fine.
func TestMediaPathRefusesSymlinkEscape(t *testing.T) {
	base := t.TempDir()
	media := filepath.Join(base, "media")
	config := filepath.Join(base, "config")
	for _, d := range []string{filepath.Join(media, "Show"), config} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	secret := filepath.Join(config, "roxy.db")
	ep := filepath.Join(media, "Show", "ep.mkv")
	for _, f := range []string{secret, ep} {
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	evil := filepath.Join(media, "Show", "ep.en.srt")
	if err := os.Symlink(secret, evil); err != nil {
		t.Fatal(err)
	}
	evilDir := filepath.Join(media, "Show", "linked")
	if err := os.Symlink(config, evilDir); err != nil {
		t.Fatal(err)
	}
	inner := filepath.Join(media, "Show", "ep2.mkv")
	if err := os.Symlink(ep, inner); err != nil {
		t.Fatal(err)
	}
	mediaLink := filepath.Join(base, "media-link")
	if err := os.Symlink(media, mediaLink); err != nil {
		t.Fatal(err)
	}

	SetMediaRoots([]string{mediaLink})
	defer SetMediaRoots(nil)

	for _, p := range []string{ep, inner, filepath.Join(mediaLink, "Show", "ep.mkv")} {
		if _, err := MediaPath(p); err != nil {
			t.Errorf("%s is inside the media folder: %v", p, err)
		}
	}
	for _, p := range []string{evil, filepath.Join(evilDir, "roxy.db"), secret, filepath.Join(media, "Show", "..", "..", "config", "roxy.db")} {
		if _, err := MediaPath(p); !errors.Is(err, ErrEscapes) {
			t.Errorf("%s must be refused with ErrEscapes, got %v", p, err)
		}
	}
	// The media folder itself is not a file in it.
	if _, err := MediaPath(media); err == nil {
		t.Errorf("the media root itself must not count as inside")
	}
	// No media folders known: nothing is allowed.
	SetMediaRoots(nil)
	if _, err := MediaPath(ep); err == nil {
		t.Errorf("with no media roots every path must be refused")
	}
	// ResolveUnder with an explicit root (scan time).
	if got, err := ResolveUnder(inner, media); err != nil || got != ep {
		t.Errorf("ResolveUnder(inner) = %q, %v; want %q", got, err, ep)
	}
	if _, err := ResolveUnder(evil, media); err == nil {
		t.Errorf("ResolveUnder(evil) must fail")
	}
}

func TestNoEscape(t *testing.T) {
	base := t.TempDir()
	media := filepath.Join(base, "media")
	if err := os.MkdirAll(media, 0o755); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(media, "poster.jpg")
	secret := filepath.Join(base, "roxy.db")
	for _, f := range []string{plain, secret} {
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	inLink := filepath.Join(media, "folder.jpg")
	outLink := filepath.Join(media, "fanart.jpg")
	if err := os.Symlink(plain, inLink); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, outLink); err != nil {
		t.Fatal(err)
	}
	SetMediaRoots([]string{media})
	defer SetMediaRoots(nil)
	if err := NoEscape(plain); err != nil {
		t.Errorf("plain file: %v", err)
	}
	if err := NoEscape(inLink); err != nil {
		t.Errorf("symlink inside media: %v", err)
	}
	if err := NoEscape(outLink); !errors.Is(err, ErrEscapes) {
		t.Errorf("symlink out of media must be refused, got %v", err)
	}
	if err := NoEscape(filepath.Join(media, "missing.jpg")); err == nil {
		t.Errorf("a missing file is an error")
	}
}
