package trickplay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/roxy-media/roxy/server/internal/config"
	"github.com/roxy-media/roxy/server/internal/ffmpeg"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/safefs"
	"github.com/roxy-media/roxy/server/internal/store"
)

// The list ffmpeg's metadata filter prints: a line per frame, a line per key after it.
func TestReadTimes(t *testing.T) {
	got, err := readTimes(strings.NewReader("frame:0    pts:0       pts_time:10.427\nroxy=1\n" +
		"frame:1    pts:1       pts_time:0\nroxy=1\n" +
		"frame:2    pts:2       pts_time:nonsense\nroxy=1\n" +
		"frame:3    pts:3       pts_time:20.854\nroxy=1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 || got[0] != 10.427 || got[1] != 0 || !math.IsNaN(got[2]) || got[3] != 20.854 {
		t.Fatalf("times: %v (the third must be NaN, so the count still matches the pictures)", got)
	}
}

// A tile shows the last frame at or before its middle, whatever order the frames were
// written in: the decoder hands keyframes out of order.
func TestPickTilesGoesByTimeNotByOrderWritten(t *testing.T) {
	frames := []frame{{"b", 10.4}, {"a", 0}, {"d", 31}, {"c", 20.8}}
	got := pickTiles(frames, 5, 10)
	// middles 5, 15, 25, 35, 45: a (0), b (10.4), c (20.8), d (31), d
	if want := []int{1, 0, 3, 2, 2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("picks %v, want %v", got, want)
	}
	// No frame early enough: the earliest there is.
	if got := pickTiles([]frame{{"late", 7}, {"later", 12}}, 2, 10); !reflect.DeepEqual(got, []int{0, 1}) {
		t.Fatalf("picks with no early frame: %v", got)
	}
	if pickTiles(nil, 3, 10) != nil || pickTiles(frames, 0, 10) != nil {
		t.Fatal("no frames or no tiles: nothing to pick")
	}
}

// Tiles whose frame is from before their own interval's reach (or after their middle)
// get a frame fetched by seeking.
func TestStaleTiles(t *testing.T) {
	frames := []frame{{"a", 0}, {"b", 30.1}}
	picks := pickTiles(frames, 5, 10) // middles 5, 15, 25, 35, 45
	// 0: frame at 0, fine. 1: 0 is 15 s before the middle. 2: 25 s before. 3: 30.1, fine. 4: 14.9 s before.
	if got := staleTiles(frames, picks, 10); !reflect.DeepEqual(got, []int{1, 2, 4}) {
		t.Fatalf("stale tiles %v", got)
	}
	late := []frame{{"late", 7}}
	if got := staleTiles(late, pickTiles(late, 1, 10), 10); !reflect.DeepEqual(got, []int{0}) {
		t.Fatalf("a tile whose only frame is after its middle: %v", got)
	}
}

func writeColour(t *testing.T, path string, c color.Color, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(c), image.Point{}, draw.Src)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// tileColour reads the middle of tile n of a sheet as "red", "green", "blue", "black"
// or the numbers.
func tileColour(t *testing.T, sheet string, n, w, h int) string {
	t.Helper()
	f, err := os.Open(sheet)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := jpeg.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := img.At((n%Columns)*w+w/2, (n/Columns)*h+h/2).RGBA()
	r, g, b = r>>8, g>>8, b>>8
	switch {
	case r > 150 && g < 100 && b < 100:
		return "red"
	case g > 90 && r < 80 && b < 80:
		return "green"
	case b > 150 && r < 100 && g < 100:
		return "blue"
	case r < 40 && g < 40 && b < 40:
		return "black"
	}
	return fmt.Sprintf("rgb(%d,%d,%d)", r, g, b)
}

func setup(t *testing.T) (*config.Config, string) {
	t.Helper()
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
	return &config.Config{DataDir: data}, media
}

// Sheets are full size with the unused tiles black, a second sheet starts after
// Columns x Rows tiles, and a frame that cannot be read leaves its tile black.
func TestWriteSheets(t *testing.T) {
	cfg, _ := setup(t)
	dir := filepath.Join(cfg.TrickplayDir(), "work")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	red, green, broken := filepath.Join(dir, "r.jpg"), filepath.Join(dir, "g.jpg"), filepath.Join(dir, "x.jpg")
	writeColour(t, red, color.RGBA{255, 0, 0, 255}, 32, 18)
	writeColour(t, green, color.RGBA{0, 160, 0, 255}, 32, 18)
	if err := os.WriteFile(broken, []byte("not a picture"), 0o644); err != nil {
		t.Fatal(err)
	}
	frames := []frame{{red, 0}, {green, 10}, {broken, 20}}
	picks := make([]int, Columns*Rows+2) // 102 tiles: red everywhere, but
	picks[1], picks[2] = 1, 2            // tile 1 green, tile 2 unreadable,
	picks[Columns*Rows+1] = 1            // and the last tile of the second sheet green
	w, h, sheets, err := writeSheets(dir, frames, picks)
	if err != nil {
		t.Fatal(err)
	}
	if w != 32 || h != 18 || sheets != 2 {
		t.Fatalf("tile %dx%d on %d sheets, want 32x18 on 2", w, h, sheets)
	}
	for _, name := range []string{"sheet_001.jpg", "sheet_002.jpg"} {
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		c, err := jpeg.DecodeConfig(f)
		f.Close()
		if err != nil || c.Width != 32*Columns || c.Height != 18*Rows {
			t.Fatalf("%s: %dx%d %v, want a full sheet", name, c.Width, c.Height, err)
		}
	}
	first, second := filepath.Join(dir, "sheet_001.jpg"), filepath.Join(dir, "sheet_002.jpg")
	for n, want := range map[int]string{0: "red", 1: "green", 2: "black", 3: "red", Columns*Rows - 1: "red"} {
		if got := tileColour(t, first, n, 32, 18); got != want {
			t.Errorf("sheet 1 tile %d is %s, want %s", n, got, want)
		}
	}
	for n, want := range map[int]string{0: "red", 1: "green", 2: "black", Columns*Rows - 1: "black"} {
		if got := tileColour(t, second, n, 32, 18); got != want {
			t.Errorf("sheet 2 tile %d is %s, want %s", n, got, want)
		}
	}
}

// service makes a Service over a real store holding one film whose file is at path.
func service(t *testing.T, cfg *config.Config, ff *ffmpeg.FFmpeg, path string, durationMs int64) (*Service, string) {
	t.Helper()
	st, err := store.Open(filepath.Join(cfg.DataDir, "roxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	lib := &model.Library{Name: "Movies", Kind: model.KindMovies, Root: filepath.Dir(path)}
	if err := st.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertItem(ctx, &model.Item{ID: "film", GUID: "g:film", LibraryID: lib.ID, Type: model.TypeMovie, Title: "Film"}); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceSource(ctx, &model.MediaSource{ID: "src", ItemID: "film", IsPrimary: true, DurationMs: durationMs,
		Parts: []model.MediaPart{{ID: "part", Path: path}}}, "{}"); err != nil {
		t.Fatal(err)
	}
	return New(st, ff, cfg), "src"
}

// fakeFFmpeg is a stand-in for ffmpeg that runs a shell script (in the folder the
// generator works in).
func fakeFFmpeg(t *testing.T, script string) *ffmpeg.FFmpeg {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("needs a shell")
	}
	p := filepath.Join(t.TempDir(), "ffmpeg")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return &ffmpeg.FFmpeg{FFmpeg: p, FFprobe: p}
}

func entries(t *testing.T, dir string) []string {
	t.Helper()
	list, _ := os.ReadDir(dir)
	var names []string
	for _, e := range list {
		names = append(names, e.Name())
	}
	return names
}

// A file ffmpeg cannot make previews of is tried three times (a failure may be this
// machine's: a full disk), then left alone across restarts until the file changes, the
// generator does, or someone asks again (Library > Refresh > Trickplay). Before, only
// memory knew, and every restart decoded such a file again.
func TestAFileThatKeepsFailingIsLeftAlone(t *testing.T) {
	cfg, media := setup(t)
	video := filepath.Join(media, "Film.mkv")
	if err := os.WriteFile(video, []byte("not a video"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, id := service(t, cfg, fakeFFmpeg(t, "echo 'Invalid data found when processing input' >&2; exit 1"), video, 30_000)
	ctx := context.Background()
	restart := func() { // what a restart forgets
		s.mu.Lock()
		s.failed = map[string]time.Time{}
		s.pending = map[string]bool{}
		for len(s.queue) > 0 {
			<-s.queue
		}
		s.mu.Unlock()
	}
	for try := 1; try <= failureAttempts; try++ {
		if !s.Enqueue(id) {
			t.Fatalf("try %d: the file was not queued", try)
		}
		restart()
		s.attempt(ctx, id)
		if !s.Failed(id) {
			t.Fatalf("try %d: the failure is not reported", try)
		}
		if s.Enqueue(id) {
			t.Fatalf("try %d: queued again within the hour", try)
		}
		restart()
	}
	if s.Enqueue(id) {
		t.Fatalf("queued again after %d failures and a restart", failureAttempts)
	}
	if !s.Failed(id) {
		t.Fatal("after a restart the API must say there are no previews for this file, not that they are being made")
	}
	if left := entries(t, cfg.TrickplayDir()); !reflect.DeepEqual(left, []string{id + ".failed"}) {
		t.Fatalf("the trickplay folder holds %v, want only the note", left)
	}
	var note failure
	b, _ := os.ReadFile(s.failurePath(id))
	if err := json.Unmarshal(b, &note); err != nil || note.Attempts != failureAttempts || !strings.Contains(note.Reason, "Invalid data") {
		t.Fatalf("the note: %s %v", b, err)
	}

	// A note from another generator does not count.
	note.Version = generatorVersion - 1
	b, _ = json.Marshal(note)
	if err := os.WriteFile(s.failurePath(id), b, 0o644); err != nil {
		t.Fatal(err)
	}
	if !s.Enqueue(id) {
		t.Fatal("a note from an older generator still stops the file")
	}
	restart()

	// Nor one about the file as it was.
	for try := 1; try <= failureAttempts; try++ {
		s.attempt(ctx, id)
		restart()
	}
	if s.Enqueue(id) {
		t.Fatal("the note was not written again")
	}
	if err := os.WriteFile(video, []byte("another file under the same name"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !s.Enqueue(id) {
		t.Fatal("the file changed, but its old failure still stops it")
	}
	restart()

	// Nor an old one.
	for try := 1; try <= failureAttempts; try++ {
		s.attempt(ctx, id)
		restart()
	}
	b, _ = os.ReadFile(s.failurePath(id))
	json.Unmarshal(b, &note)
	note.At = time.Now().Add(-failureLife - time.Hour).UTC().Format(time.RFC3339)
	b, _ = json.Marshal(note)
	if err := os.WriteFile(s.failurePath(id), b, 0o644); err != nil {
		t.Fatal(err)
	}
	if !s.Enqueue(id) {
		t.Fatal("a failure from long ago still stops the file")
	}
	restart()

	// And asking again clears it.
	for try := 1; try <= failureAttempts; try++ {
		s.attempt(ctx, id)
		restart()
	}
	s.Regenerate(id)
	if !s.Pending(id) {
		t.Fatal("Regenerate did not queue a file that had failed before")
	}
}

// A failure that is not the file's own is not held against it: the file being away (a
// drive not mounted) or the server stopping in the middle.
func TestOnlyTheFilesOwnFailuresAreNoted(t *testing.T) {
	cfg, media := setup(t)
	video := filepath.Join(media, "Film.mkv")
	if err := os.WriteFile(video, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, id := service(t, cfg, fakeFFmpeg(t, "exit 1"), video, 30_000)
	stopped, cancel := context.WithCancel(context.Background())
	cancel()
	s.attempt(stopped, id)
	if s.Failed(id) {
		t.Fatal("a generation cut short by the server stopping counts as the file's failure")
	}
	if err := os.Remove(video); err != nil {
		t.Fatal(err)
	}
	for try := 0; try < failureAttempts+1; try++ {
		s.attempt(context.Background(), id)
	}
	if _, err := os.Stat(s.failurePath(id)); err == nil {
		t.Fatal("a file that is not there was noted as one that cannot be decoded")
	}
	if left := entries(t, cfg.TrickplayDir()); len(left) != 0 {
		t.Fatalf("left behind: %v", left)
	}
}

// Whatever goes wrong, the work folder goes. Here the finished previews cannot be put
// in place (the old ones cannot be removed): that way out, and two others, used to
// leave "<source>.tmp" behind for good.
func TestAFailedGenerationLeavesNoWorkFolder(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root removes read-only folders")
	}
	cfg, media := setup(t)
	video := filepath.Join(media, "Film.mkv")
	if err := os.WriteFile(video, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	pic := filepath.Join(t.TempDir(), "pic.jpg")
	writeColour(t, pic, color.RGBA{255, 0, 0, 255}, 32, 18)
	// "ffmpeg" writes a good picture under the names both the old and the new generator
	// look for.
	s, id := service(t, cfg, fakeFFmpeg(t, "cp '"+pic+"' sheet_001.jpg; cp '"+pic+"' kf_0000001.jpg; printf 'frame:0    pts:0       pts_time:0\\nroxy=1\\n' > times.txt"), video, 30_000)
	locked := filepath.Join(cfg.TrickplayDir(), id, "locked")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	if err := s.generate(context.Background(), id); err == nil {
		t.Fatal("the previews were put in place over a folder that cannot be removed")
	}
	if left := entries(t, cfg.TrickplayDir()); !reflect.DeepEqual(left, []string{id}) {
		t.Fatalf("the trickplay folder holds %v, want only the old folder", left)
	}
}

// needFFmpeg skips a test that needs ffmpeg with libx264 and libx265, or fails it when
// ROXY_REQUIRE_INTEGRATION is set (CI's integration job), so a green job means it ran.
func needFFmpeg(t *testing.T) *ffmpeg.FFmpeg {
	t.Helper()
	ff, err := ffmpeg.New("", "")
	if err == nil {
		var out []byte
		if out, err = exec.Command(ff.FFmpeg, "-v", "error", "-encoders").Output(); err == nil {
			if strings.Contains(string(out), "libx265") && strings.Contains(string(out), "libx264") {
				return ff
			}
			err = fmt.Errorf("this ffmpeg has no libx264 or no libx265")
		}
	}
	if os.Getenv("ROXY_REQUIRE_INTEGRATION") != "" {
		t.Fatalf("ROXY_REQUIRE_INTEGRATION is set but ffmpeg is not usable: %v", err)
	}
	t.Skip("needs ffmpeg with libx264 and libx265")
	return nil
}

// An HEVC video with a keyframe every 250 frames, as many encodes have in still shots:
// ten seconds each of red, green and blue. Decoding keyframes alone, the decoder hands
// the second out before the first and drops the third; the old filter chain made no
// sheet of this at all (and of a whole episode, sheets with tiles a keyframe off). The
// tiles must be red, green, blue.
func TestPreviewsOfHEVCFollowTheVideosTime(t *testing.T) {
	ff := needFFmpeg(t)
	cfg, media := setup(t)
	video := filepath.Join(media, "Colours.mkv")
	if out, err := exec.Command(ff.FFmpeg, "-v", "error", "-y",
		"-f", "lavfi", "-i", "color=red:s=320x240:d=10:r=24",
		"-f", "lavfi", "-i", "color=green:s=320x240:d=10:r=24",
		"-f", "lavfi", "-i", "color=blue:s=320x240:d=10:r=24",
		"-filter_complex", "[0:v][1:v][2:v]concat=n=3:v=1,noise=alls=20:allf=t",
		"-c:v", "libx265", "-preset", "ultrafast", "-x265-params", "keyint=250:min-keyint=250:scenecut=0:log-level=error",
		"-f", "matroska", video).CombinedOutput(); err != nil {
		t.Fatalf("making the video: %v %s", err, out)
	}
	s, id := service(t, cfg, ff, video, 30_000)
	if err := s.generate(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	m := s.Manifest(id)
	if m == nil || m.Count != 3 || m.Sheets != 1 || m.Width != 320 || m.Height != 240 || m.IntervalMs != IntervalMs {
		t.Fatalf("manifest: %+v", m)
	}
	for n, want := range []string{"red", "green", "blue", "black"} {
		if got := tileColour(t, s.SheetPath(id, 0), n, m.Width, m.Height); got != want {
			t.Errorf("tile %d (%d s on) is %s, want %s", n, n*10, got, want)
		}
	}
	if left := entries(t, cfg.TrickplayDir()); !reflect.DeepEqual(left, []string{id}) {
		t.Errorf("the trickplay folder holds %v, want only the source's folder", left)
	}
	if left := entries(t, filepath.Join(cfg.TrickplayDir(), id)); !reflect.DeepEqual(left, []string{"manifest.json", "sheet_001.jpg"}) {
		t.Errorf("the source's folder holds %v", left)
	}
}

// A video with pixels that are not square (a DVD: 720x480 shown as 16:9) gets previews
// in the shape it is shown in, not the squeezed shape it is stored in.
func TestPreviewsKeepTheShapeTheVideoIsShownIn(t *testing.T) {
	ff := needFFmpeg(t)
	cfg, media := setup(t)
	video := filepath.Join(media, "DVD.mkv")
	if out, err := exec.Command(ff.FFmpeg, "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc=duration=12:size=720x480:rate=10",
		"-vf", "setsar=32/27", "-c:v", "libx264", "-preset", "ultrafast", "-f", "matroska", video).CombinedOutput(); err != nil {
		t.Fatalf("making the video: %v %s", err, out)
	}
	s, id := service(t, cfg, ff, video, 12_000)
	if err := s.generate(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	m := s.Manifest(id)
	if m == nil || m.Count != 2 || m.Width != 320 || m.Height != 180 {
		t.Fatalf("manifest: %+v, want two 320x180 tiles (16:9)", m)
	}
}

// oldManifest writes sheets as a generator from before manifests had a version left them.
func oldManifest(t *testing.T, cfg *config.Config, id string) {
	t.Helper()
	dir := filepath.Join(cfg.TrickplayDir(), id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeColour(t, filepath.Join(dir, "sheet_001.jpg"), color.White, TileWidth*Columns, 180*Rows)
	old := `{"intervalMs":10000,"width":320,"height":180,"columns":10,"rows":10,"count":3,"sheets":1,"generatedAt":"2026-10-27T08:00:00Z"}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Sheets an older generator made (tiles of HEVC a keyframe off, squeezed pictures of
// videos with pixels that are not square) stayed for good: nothing but Library > Refresh
// > Trickplay, one title at a time, made them again. They are queued again now, behind
// the files that have none, and served as they are until then.
func TestSheetsOfAnOlderGeneratorAreQueuedBehindFilesWithNone(t *testing.T) {
	cfg, media := setup(t)
	video := filepath.Join(media, "Film.mkv")
	if err := os.WriteFile(video, []byte("a video"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, old := service(t, cfg, fakeFFmpeg(t, "exit 1"), video, 30_000)
	ctx := context.Background()
	const missing = "new"
	if err := s.store.ReplaceSource(ctx, &model.MediaSource{ID: missing, ItemID: "film", DurationMs: 30_000,
		Parts: []model.MediaPart{{ID: "part2", Path: filepath.Join(media, "Film - 4K.mkv")}}}, "{}"); err != nil {
		t.Fatal(err)
	}
	oldManifest(t, cfg, old)

	if m := s.Manifest(old); m == nil || m.Count != 3 {
		t.Fatalf("the old sheets are not served while they wait: %+v", m)
	}
	if !s.Enqueue(old) { // first, so that plain first-come order would hand it out first
		t.Fatal("sheets of an older generator were not queued to be made again")
	}
	if !s.Enqueue(missing) {
		t.Fatal("the file with no sheets was not queued")
	}
	if s.Enqueue(old) || s.Enqueue(missing) {
		t.Error("queued twice")
	}
	var order []string
	for range 2 {
		id, ok := s.next(ctx)
		if !ok {
			t.Fatal("nothing to work on")
		}
		order = append(order, id)
	}
	if want := []string{missing, old}; !reflect.DeepEqual(order, want) {
		t.Errorf("worked on in the order %v, want %v: a file with no sheets goes first", order, want)
	}

	// A remake that fails leaves the old sheets where they are.
	s.attempt(ctx, old)
	if m := s.Manifest(old); m == nil || m.Count != 3 {
		t.Errorf("a failed remake took the old sheets away: %+v", m)
	}
	if _, err := os.Stat(s.SheetPath(old, 0)); err != nil {
		t.Errorf("a failed remake took the old sheet away: %v", err)
	}

	stopped, cancel := context.WithCancel(ctx)
	cancel()
	if id, ok := s.next(stopped); ok {
		t.Errorf("next handed out %q after the server was stopped", id)
	}
}

// The sweep at startup counts both kinds, and sheets this generator made are left alone.
func TestTheSweepRemakesOnlySheetsOfAnOlderGenerator(t *testing.T) {
	cfg, media := setup(t)
	video := filepath.Join(media, "Film.mkv")
	if err := os.WriteFile(video, []byte("a video"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, id := service(t, cfg, fakeFFmpeg(t, "exit 1"), video, 30_000)
	ctx := context.Background()
	oldManifest(t, cfg, id)
	s.SweepMissing(ctx)
	if len(s.redo) != 1 || len(s.queue) != 0 {
		t.Fatalf("after the sweep: %d to remake, %d with none; want 1 and 0", len(s.redo), len(s.queue))
	}
	<-s.redo
	s.mu.Lock()
	delete(s.pending, id)
	s.mu.Unlock()

	// The same sheets, marked as this generator's.
	b, err := os.ReadFile(s.manifestPath(id))
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	m.Version = manifestVersion
	b, _ = json.Marshal(m)
	if err := os.WriteFile(s.manifestPath(id), b, 0o644); err != nil {
		t.Fatal(err)
	}
	s.SweepMissing(ctx)
	if s.Enqueue(id) || len(s.redo) != 0 || len(s.queue) != 0 {
		t.Errorf("sheets this generator made were queued again (%d to remake, %d with none)", len(s.redo), len(s.queue))
	}
	// Asked for twice and made in between: the second turn does nothing (here ffmpeg
	// would fail, and the failure would be noted).
	s.attempt(ctx, id)
	if s.Failed(id) {
		t.Error("a file that has this generator's sheets was generated again")
	}
}

// Sheets made by this generator say so, and making them again replaces an older
// generator's sheets (real ffmpeg).
func TestARemakeReplacesTheOldSheets(t *testing.T) {
	ff := needFFmpeg(t)
	cfg, media := setup(t)
	video := filepath.Join(media, "Clip.mkv")
	if out, err := exec.Command(ff.FFmpeg, "-v", "error", "-y", "-f", "lavfi", "-i", "color=red:s=320x240:d=25:r=10",
		"-c:v", "libx264", "-preset", "ultrafast", "-g", "10", "-f", "matroska", video).CombinedOutput(); err != nil {
		t.Fatalf("making the video: %v %s", err, out)
	}
	s, id := service(t, cfg, ff, video, 25_000)
	oldManifest(t, cfg, id)
	if !s.Enqueue(id) {
		t.Fatal("the old sheets were not queued")
	}
	got, ok := s.next(context.Background())
	if !ok || got != id {
		t.Fatalf("next = %q, %v", got, ok)
	}
	s.mu.Lock()
	delete(s.pending, id)
	s.mu.Unlock()
	s.attempt(context.Background(), id)
	m := s.Manifest(id)
	if m == nil || m.Version != manifestVersion || m.Height != 240 || m.Generated == "2026-10-27T08:00:00Z" {
		t.Fatalf("after the remake the manifest is %+v", m)
	}
	if c := tileColour(t, s.SheetPath(id, 0), 0, m.Width, m.Height); c != "red" {
		t.Errorf("the sheet is still the old one: its first tile is %s", c)
	}
	if s.Enqueue(id) {
		t.Error("queued again after the remake")
	}
}
