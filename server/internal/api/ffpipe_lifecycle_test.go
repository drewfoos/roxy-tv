package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/roxy-media/roxy/server/internal/config"
	"github.com/roxy-media/roxy/server/internal/ffmpeg"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/safefs"
	"github.com/roxy-media/roxy/server/internal/store"
	"github.com/roxy-media/roxy/server/internal/transcode"
)

// pipeWorld serves /transcode and /remux over a real HTTP server (real connections, so
// disconnects and stalls behave as with a player), with a fake ffmpeg that streams zeros
// forever and writes its process id to a file.
type pipeWorld struct {
	srv  *httptest.Server
	pids string
}

func newPipeWorld(t *testing.T) *pipeWorld {
	t.Helper()
	base := t.TempDir()
	data, media := filepath.Join(base, "config"), filepath.Join(base, "media")
	for _, d := range []string{data, media} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	video := filepath.Join(media, "Film.mkv")
	if err := os.WriteFile(video, []byte("not really a video"), 0o644); err != nil {
		t.Fatal(err)
	}
	pids := filepath.Join(base, "pids")
	fake := filepath.Join(base, "ffmpeg")
	// The encoder check ("-filters", read whole) is answered at once; anything else is a
	// stream: zeros, forever.
	script := "#!/bin/sh\ncase \"$*\" in *-filters*) exit 0;; esac\necho $$ >> '" + pids + "'\nexec cat /dev/zero\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	safefs.Init(data)
	safefs.SetMediaRoots([]string{media})
	t.Cleanup(func() { safefs.SetMediaRoots(nil) })
	st, err := store.Open(filepath.Join(data, "roxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	lib := &model.Library{Name: "Movies", Kind: model.KindMovies, Root: media}
	if err := st.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertItem(ctx, &model.Item{ID: "i", GUID: "g", LibraryID: lib.ID, Type: model.TypeMovie, Title: "Film"}); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceSource(ctx, &model.MediaSource{ID: "src", ItemID: "i", Container: "mkv", IsPrimary: true,
		Parts:   []model.MediaPart{{ID: "p", SourceID: "src", Path: video}},
		Streams: []model.MediaStream{{SourceID: "src", Index: 0, Type: model.StreamVideo, Codec: "h264", Height: 240}, {SourceID: "src", Index: 1, Type: model.StreamAudio, Codec: "aac", Channels: 2}}}, "{}"); err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: &config.Config{DataDir: data}, store: st, ff: &ffmpeg.FFmpeg{FFmpeg: fake},
		transcoder: transcode.NewDetector(fake, "software", "")}
	r := chi.NewRouter()
	r.Get("/v1/sources/{id}/transcode", s.transcodeStream)
	r.Get("/v1/sources/{id}/remux", s.remuxStream)
	w := &pipeWorld{srv: httptest.NewServer(r), pids: pids}
	t.Cleanup(w.srv.Close)
	return w
}

// running lists the fake ffmpeg processes still alive.
func (w *pipeWorld) running() []int {
	b, _ := os.ReadFile(w.pids)
	var out []int
	for _, l := range strings.Fields(string(b)) {
		pid, _ := strconv.Atoi(l)
		if pid > 0 && syscall.Kill(pid, 0) == nil {
			// A zombie still answers kill 0; its /proc state says Z.
			if st, err := os.ReadFile("/proc/" + l + "/stat"); err == nil && strings.Contains(string(st), ") Z ") {
				continue
			}
			out = append(out, pid)
		}
	}
	return out
}

func waitUntil(t *testing.T, what string, within time.Duration, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(within); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timed out after %v waiting for %s", within, what)
}

// open starts a stream and returns its response with the first bytes read.
func (w *pipeWorld) open(t *testing.T, path string) *http.Response {
	t.Helper()
	resp, err := http.Get(w.srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("%s: %d %s", path, resp.StatusCode, b)
	}
	if _, err := io.ReadFull(resp.Body, make([]byte, 64<<10)); err != nil {
		t.Fatal(err)
	}
	return resp
}

// The player going away (it closed the connection: stopped, seeked, the app closed)
// stops ffmpeg at once and gives the slot back, for transcodes and remuxes alike.
func TestStreamDisconnectStopsFFmpeg(t *testing.T) {
	for _, path := range []string{"/v1/sources/src/transcode", "/v1/sources/src/remux"} {
		t.Run(path, func(t *testing.T) {
			w := newPipeWorld(t)
			resp := w.open(t, path)
			waitUntil(t, "ffmpeg to be running", 5*time.Second, func() bool { return len(w.running()) == 1 })
			resp.Body.Close()
			waitUntil(t, "ffmpeg to stop after the player left", 10*time.Second, func() bool { return len(w.running()) == 0 })
			waitUntil(t, "the slots to be free", 5*time.Second, func() bool { return len(transcodeSlots) == 0 && len(remuxSlots) == 0 })
		})
	}
}

// A player that keeps the connection open but stops reading (paused with its buffer
// full, a phone app put away) used to hold ffmpeg and its slot for as long as the
// connection lived: four of those and no one could transcode. Past pipeStall the stream
// ends, ffmpeg stops and the slot is free again.
func TestStalledPlayerReleasesTheSlot(t *testing.T) {
	old := pipeStall
	pipeStall = 500 * time.Millisecond
	t.Cleanup(func() { pipeStall = old })
	w := newPipeWorld(t)
	resp := w.open(t, "/v1/sources/src/transcode") // then never read again, connection kept
	defer resp.Body.Close()
	waitUntil(t, "ffmpeg to be running", 5*time.Second, func() bool { return len(w.running()) == 1 })
	waitUntil(t, "a stalled player's ffmpeg to stop", 30*time.Second, func() bool { return len(w.running()) == 0 })
	waitUntil(t, "its slot to be free", 5*time.Second, func() bool { return len(transcodeSlots) == 0 })
}

// Concurrent transcodes: up to the slot count run, the next is told the server is busy
// (503) rather than queued or started, and a slot given back is usable again. Every
// ffmpeg ends with its player.
func TestConcurrentTranscodesAreBounded(t *testing.T) {
	w := newPipeWorld(t)
	var open []*http.Response
	for i := 0; i < cap(transcodeSlots); i++ {
		open = append(open, w.open(t, "/v1/sources/src/transcode"))
	}
	waitUntil(t, "every transcode running", 5*time.Second, func() bool { return len(w.running()) == cap(transcodeSlots) })
	resp, err := http.Get(w.srv.URL + "/v1/sources/src/transcode")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("a transcode past the slot count: %d, want 503", resp.StatusCode)
	}
	if n := len(w.running()); n != cap(transcodeSlots) {
		t.Fatalf("%d ffmpeg running after a refused request, want %d", n, cap(transcodeSlots))
	}
	open[0].Body.Close()
	waitUntil(t, "a slot to come back", 10*time.Second, func() bool { return len(transcodeSlots) < cap(transcodeSlots) })
	open[0] = w.open(t, "/v1/sources/src/transcode")
	for _, r := range open {
		r.Body.Close()
	}
	waitUntil(t, "every ffmpeg to stop", 10*time.Second, func() bool { return len(w.running()) == 0 })
	waitUntil(t, "every slot to be free", 5*time.Second, func() bool { return len(transcodeSlots) == 0 })
}
