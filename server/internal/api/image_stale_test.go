package api

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/roxy-media/roxy/server/internal/config"
	"github.com/roxy-media/roxy/server/internal/ffmpeg"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/safefs"
	"github.com/roxy-media/roxy/server/internal/store"
)

func colourJPEG(t *testing.T, w, h int, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{c}, image.Point{}, draw.Src)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// An episode matched to the wrong entry has that entry's still, and the client has asked
// for it at 480 px. The match is undone: the still's record goes and the next request
// takes a frame from the file. The 480 px copy of the wrong still was left on disk and
// went on being served at that size, because a copy was only made when none existed.
func TestAResizedCopyOfAReplacedImageIsNotServed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a shell")
	}
	ctx := context.Background()
	base := t.TempDir()
	data, media := filepath.Join(base, "config"), filepath.Join(base, "media")
	for _, d := range []string{data, media} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	safefs.Init(data)
	safefs.SetMediaRoots([]string{media})
	t.Cleanup(func() { safefs.SetMediaRoots(nil) })
	st, err := store.Open(filepath.Join(data, "roxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cfg := &config.Config{DataDir: data}
	lib := &model.Library{Name: "Anime", Kind: model.KindSeries, Root: media}
	if err := st.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertItem(ctx, &model.Item{ID: "ep1", GUID: "g-ep1", LibraryID: lib.ID, Type: model.TypeEpisode, Title: "Wrong title"}); err != nil {
		t.Fatal(err)
	}
	video := filepath.Join(media, "ep.mkv")
	if err := os.WriteFile(video, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceSource(ctx, &model.MediaSource{ID: "src1", ItemID: "ep1", IsPrimary: true, DurationMs: 100_000,
		Parts: []model.MediaPart{{ID: "p1", SourceID: "src1", Path: video}}}, "{}"); err != nil {
		t.Fatal(err)
	}
	// The wrong entry's still: red.
	dir := filepath.Join(cfg.ImagesDir(), "ep1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	thumb := filepath.Join(dir, "thumb.jpg")
	if err := os.WriteFile(thumb, colourJPEG(t, 1280, 720, color.RGBA{255, 0, 0, 255}), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := st.SetItemImage(ctx, store.ItemImage{ItemID: "ep1", Type: "thumb", Path: thumb, Width: 1280, Height: 720, SourceURL: "https://images.example/wrong.jpg"}); err != nil {
		t.Fatal(err)
	}
	// The frame from the file: blue (a stand-in ffmpeg that writes it to its last argument).
	blue := filepath.Join(base, "blue.jpg")
	if err := os.WriteFile(blue, colourJPEG(t, 640, 360, color.RGBA{0, 0, 255, 255}), 0o644); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(base, "ffmpeg")
	if err := os.WriteFile(script, []byte(fmt.Sprintf("#!/bin/sh\nfor a; do last=$a; done\ncp %q \"$last\"\n", blue)), 0o755); err != nil {
		t.Fatal(err)
	}
	s := &Server{store: st, cfg: cfg, ff: &ffmpeg.FFmpeg{FFmpeg: script, FFprobe: script}}
	r := chi.NewRouter()
	r.Get("/v1/items/{id}/images/{type}", s.getItemImage)
	colourAt := func(width string) string {
		t.Helper()
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/items/ep1/images/thumb"+width, nil))
		if rec.Code != 200 {
			t.Fatalf("thumb%s: status %d %s", width, rec.Code, rec.Body)
		}
		img, _, err := image.Decode(bytes.NewReader(rec.Body.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		b := img.Bounds()
		red, _, bl, _ := img.At(b.Dx()/2, b.Dy()/2).RGBA()
		if red > bl {
			return "red"
		}
		return "blue"
	}
	if got := colourAt("?w=480"); got != "red" {
		t.Fatalf("before: the 480 px thumb is %s, want the provider's still (red)", got)
	}
	// The copy is older than whatever replaces the original, as on any real server.
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "thumb.w480.jpg"), old, old); err != nil {
		t.Fatal(err)
	}
	if err := st.ResetEpisodeToFile(ctx, "ep1", "File title"); err != nil {
		t.Fatal(err)
	}
	if got := colourAt(""); got != "blue" {
		t.Fatalf("after: the full-size thumb is %s, want the frame from the file (blue)", got)
	}
	if got := colourAt("?w=480"); got != "blue" {
		t.Errorf("after: the 480 px thumb is still %s: the copy of the dropped still was served", got)
	}
	// And a copy that is up to date is not made again on every request.
	before, err := os.Stat(filepath.Join(dir, "thumb.w480.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	colourAt("?w=480")
	after, err := os.Stat(filepath.Join(dir, "thumb.w480.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("an up-to-date copy was made again")
	}
}

// Deleting a profile removed its picture and left the resized copies of it for good.
func TestDeletingAProfileRemovesItsResizedPictures(t *testing.T) {
	s, st, prof, red, _, dir := avatarFixture(t)
	ctx := context.Background()
	other, err := st.CreateProfile(ctx, prof.UserID, "Other", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []*model.Profile{prof, other} {
		if err := st.UpdateProfile(ctx, p.ID, "", red, "Red"); err != nil {
			t.Fatal(err)
		}
		got, _ := st.GetProfile(ctx, p.ID)
		s.fetchAvatar(ctx, got)
	}
	h := profileRouter(s, prof.UserID)
	for _, p := range []*model.Profile{prof, other} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/me/profiles/"+p.ID+"/avatar?w=160", nil))
		if rec.Code != 200 {
			t.Fatalf("avatar: status %d", rec.Code)
		}
	}
	if files, _ := filepath.Glob(filepath.Join(dir, "*")); len(files) != 4 {
		t.Fatalf("before: %v, want a picture and a 160 px copy for each of two profiles", files)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/v1/me/profiles/"+other.ID, nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: status %d %s", rec.Code, rec.Body)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*"))
	if len(files) != 2 {
		t.Errorf("after: %v, want only the remaining profile's picture and copy", files)
	}
	for _, f := range files {
		if name := filepath.Base(f); len(name) < len(prof.ID) || name[:len(prof.ID)] != prof.ID {
			t.Errorf("left behind or wrongly kept: %s", name)
		}
	}
}
