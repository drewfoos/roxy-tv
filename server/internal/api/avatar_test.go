package api

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/roxy-media/roxy/server/internal/config"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/safefs"
	"github.com/roxy-media/roxy/server/internal/store"
)

func solid(c color.Color) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for x := 0; x < 8; x++ {
		for y := 0; y < 8; y++ {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

func avatarFixture(t *testing.T) (*Server, *store.Store, *model.Profile, string, string, string) {
	t.Helper()
	data := t.TempDir()
	safefs.Init(data)
	st, err := store.Open(filepath.Join(data, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	u := &model.User{ID: "u1", Name: "drew"}
	if err := st.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	prof, err := st.CreateProfile(ctx, u.ID, "Drew", "", "")
	if err != nil {
		t.Fatal(err)
	}
	pics := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/red.png":
			_, _ = w.Write(solid(color.RGBA{255, 0, 0, 255}))
		case "/blue.png":
			_, _ = w.Write(solid(color.RGBA{0, 0, 255, 255}))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(pics.Close)
	s := &Server{cfg: &config.Config{DataDir: data}, store: st}
	return s, st, prof, pics.URL + "/red.png", pics.URL + "/blue.png", filepath.Join(s.cfg.ImagesDir(), "profiles")
}

func blueish(t *testing.T, path string) bool {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	img, err := jpeg.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	r, _, b, _ := img.At(4, 4).RGBA()
	return b > r
}

// Two picture changes overlap: red is chosen, then blue; blue's download finishes first,
// red's late. The profile must keep blue, as recorded and as the file it serves.
func TestAvatarLateDownloadDoesNotWin(t *testing.T) {
	s, st, prof, red, blue, dir := avatarFixture(t)
	ctx := context.Background()
	if err := st.UpdateProfile(ctx, prof.ID, "", red, "Red"); err != nil {
		t.Fatal(err)
	}
	asRed, _ := st.GetProfile(ctx, prof.ID) // request A, about to download red
	if err := st.UpdateProfile(ctx, prof.ID, "", blue, "Blue"); err != nil {
		t.Fatal(err)
	}
	asBlue, _ := st.GetProfile(ctx, prof.ID)
	s.fetchAvatar(ctx, asBlue) // request B finishes first
	s.fetchAvatar(ctx, asRed)  // request A finishes late

	got, _ := st.GetProfile(ctx, prof.ID)
	if got.AvatarURL != blue || got.AvatarPath == "" {
		t.Fatalf("profile: url %s path %q", got.AvatarURL, got.AvatarPath)
	}
	if !blueish(t, got.AvatarPath) {
		t.Fatal("the profile's picture file is red: the late download overwrote it")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*"))
	if len(files) != 1 || files[0] != got.AvatarPath {
		t.Fatalf("files left: %v (want only %s)", files, got.AvatarPath)
	}
}

// Changing the picture later replaces the old file once the new one is in.
func TestAvatarChangeRemovesOldFile(t *testing.T) {
	s, st, prof, red, blue, dir := avatarFixture(t)
	ctx := context.Background()
	_ = st.UpdateProfile(ctx, prof.ID, "", red, "Red")
	p, _ := st.GetProfile(ctx, prof.ID)
	s.fetchAvatar(ctx, p)
	if p.AvatarPath == "" || blueish(t, p.AvatarPath) {
		t.Fatal("red not cached")
	}
	_ = os.WriteFile(filepath.Join(dir, filepath.Base(p.AvatarPath[:len(p.AvatarPath)-4])+".w64.jpg"), []byte("x"), 0o644) // a resized variant
	_ = st.UpdateProfile(ctx, prof.ID, "", blue, "Blue")
	p, _ = st.GetProfile(ctx, prof.ID)
	s.fetchAvatar(ctx, p)
	files, _ := filepath.Glob(filepath.Join(dir, "*"))
	if len(files) != 1 || files[0] != p.AvatarPath || !blueish(t, p.AvatarPath) {
		t.Fatalf("files: %v, path %s", files, p.AvatarPath)
	}
}
