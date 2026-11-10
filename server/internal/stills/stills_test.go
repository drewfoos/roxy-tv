package stills

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/roxy-media/roxy/server/internal/config"
	"github.com/roxy-media/roxy/server/internal/ffmpeg"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/safefs"
	"github.com/roxy-media/roxy/server/internal/store"
)

func TestSeekPointStaysInsideTheFile(t *testing.T) {
	for _, c := range []struct {
		durationMs int64
		want       float64
	}{
		{0, 5},             // length unknown: the old five seconds
		{24 * 60_000, 288}, // an episode: a fifth of the way in
		{25_000, 5},        // a fifth would be the first frame: five seconds
		{5_001, 5},
		{5_000, 2.5}, // five seconds is the end: the middle
		{3_000, 1.5}, // a three-second clip had its frame asked for two seconds past its end
	} {
		if got := seekPoint(c.durationMs); got != c.want {
			t.Errorf("seekPoint(%d ms) = %v s, want %v s", c.durationMs, got, c.want)
		}
	}
}

func setup(t *testing.T, ff *ffmpeg.FFmpeg, video string, durationMs int64) (*store.Store, *config.Config) {
	t.Helper()
	data := filepath.Join(t.TempDir(), "config")
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}
	safefs.Init(data)
	safefs.SetMediaRoots([]string{filepath.Dir(video)})
	t.Cleanup(func() { safefs.SetMediaRoots(nil) })
	st, err := store.Open(filepath.Join(data, "roxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	lib := &model.Library{Name: "Movies", Kind: model.KindMovies, Root: filepath.Dir(video)}
	if err := st.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertItem(ctx, &model.Item{ID: "film", GUID: "g:film", LibraryID: lib.ID, Type: model.TypeMovie, Title: "Film"}); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceSource(ctx, &model.MediaSource{ID: "src", ItemID: "film", IsPrimary: true, DurationMs: durationMs,
		Parts: []model.MediaPart{{ID: "part", Path: video}}}, "{}"); err != nil {
		t.Fatal(err)
	}
	return st, &config.Config{DataDir: data}
}

// ffmpeg ends with success and no picture when there is no frame at the point asked for.
// That was not remembered, so every view of the page decoded the file again.
func TestAGrabThatWritesNothingIsNotRepeatedAtEveryView(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a shell")
	}
	dir := t.TempDir()
	video := filepath.Join(dir, "media", "Film.mkv")
	if err := os.MkdirAll(filepath.Dir(video), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(video, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	runs := filepath.Join(dir, "runs")
	script := filepath.Join(dir, "ffmpeg")
	if err := os.WriteFile(script, []byte(fmt.Sprintf("#!/bin/sh\necho run >> %q\nexit 0\n", runs)), 0o755); err != nil {
		t.Fatal(err)
	}
	ff := &ffmpeg.FFmpeg{FFmpeg: script, FFprobe: script}
	st, cfg := setup(t, ff, video, 60_000)
	t.Cleanup(func() { failMu.Lock(); delete(failures, "film"); failMu.Unlock() })

	for view := 1; view <= 3; view++ {
		if img, _ := Grab(context.Background(), ff, st, cfg, "film"); img != nil {
			t.Fatalf("view %d: got a thumb from an ffmpeg that wrote none: %+v", view, img)
		}
	}
	got, err := os.ReadFile(runs)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "run\n" {
		t.Errorf("ffmpeg ran %q, want once for three views", got)
	}
	left, _ := os.ReadDir(filepath.Join(cfg.ImagesDir(), "film"))
	for _, e := range left {
		t.Errorf("left in the item's image folder: %s", e.Name())
	}
}

func needFFmpeg(t *testing.T) *ffmpeg.FFmpeg {
	t.Helper()
	ff, err := ffmpeg.New("", "")
	if err == nil {
		return ff
	}
	if os.Getenv("ROXY_REQUIRE_INTEGRATION") != "" {
		t.Fatalf("ROXY_REQUIRE_INTEGRATION is set but ffmpeg is not usable: %v", err)
	}
	t.Skip("needs ffmpeg")
	return nil
}

func makeVideo(t *testing.T, ff *ffmpeg.FFmpeg, video, source, filter string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(video), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(ff.FFmpeg, "-v", "error", "-y", "-f", "lavfi", "-i", source, "-vf", filter,
		"-c:v", "mpeg4", "-q:v", "5", "-f", "matroska", video).CombinedOutput(); err != nil {
		t.Fatalf("making the video: %v %s", err, out)
	}
}

// A DVD stores 720x480 and is shown 16:9. The thumb was the stored shape (640x426).
func TestAThumbHasTheShapeTheVideoIsShownIn(t *testing.T) {
	ff := needFFmpeg(t)
	video := filepath.Join(t.TempDir(), "media", "DVD.mkv")
	makeVideo(t, ff, video, "testsrc=duration=12:size=720x480:rate=10", "setsar=32/27")
	st, cfg := setup(t, ff, video, 12_000)
	t.Cleanup(func() { failMu.Lock(); delete(failures, "film"); failMu.Unlock() })
	img, err := Grab(context.Background(), ff, st, cfg, "film")
	if err != nil || img == nil {
		t.Fatalf("Grab: %+v, %v", img, err)
	}
	if img.Width != 640 || img.Height != 360 {
		t.Errorf("thumb is %dx%d, want 640x360 (16:9)", img.Width, img.Height)
	}
}

// A clip shorter than five seconds had its frame asked for past its end, and got none.
func TestAShortClipGetsAThumb(t *testing.T) {
	ff := needFFmpeg(t)
	video := filepath.Join(t.TempDir(), "media", "Clip.mkv")
	makeVideo(t, ff, video, "testsrc=duration=3:size=320x240:rate=10", "null")
	st, cfg := setup(t, ff, video, 3_000)
	t.Cleanup(func() { failMu.Lock(); delete(failures, "film"); failMu.Unlock() })
	img, err := Grab(context.Background(), ff, st, cfg, "film")
	if err != nil || img == nil {
		t.Fatalf("Grab on a 3 s clip: %+v, %v", img, err)
	}
}
