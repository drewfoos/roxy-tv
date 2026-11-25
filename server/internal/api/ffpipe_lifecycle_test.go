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

	"github.com/roxy-media/roxy/server/internal/auth"
	"github.com/roxy-media/roxy/server/internal/config"
	"github.com/roxy-media/roxy/server/internal/ffmpeg"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/safefs"
	"github.com/roxy-media/roxy/server/internal/store"
	"github.com/roxy-media/roxy/server/internal/transcode"
)

// pipeWorld serves /transcode and /remux over a real HTTP server (real connections, so
// disconnects and stalls behave as with a player), with a fake ffmpeg that streams zeros
// forever and writes its process id to a file, and its arguments to another. Sources:
// "src" (240 lines) and "wide" (a 3840 x 1604 film).
type pipeWorld struct {
	srv  *httptest.Server
	pids string
	args string
	base string
	s    *Server
}

func newPipeWorld(t *testing.T) *pipeWorld { return newPipeWorldWith(t, "software", "") }

// newPipeWorldWith: [encoder] is the server's encoder setting, and [gpu] is shell put
// before the fake ffmpeg's stream part (how a hardware encoder behaves, see gpuScript).
func newPipeWorldWith(t *testing.T, encoder, gpu string) *pipeWorld {
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
	wide := filepath.Join(media, "Wide.mkv")
	if err := os.WriteFile(wide, []byte("not really a film"), 0o644); err != nil {
		t.Fatal(err)
	}
	pids, args := filepath.Join(base, "pids"), filepath.Join(base, "args")
	fake := filepath.Join(base, "ffmpeg")
	// The encoder check ("-filters", read whole) is answered at once; anything else is a
	// stream: zeros, forever.
	script := "#!/bin/sh\ncase \"$*\" in *-filters*) exit 0;; esac\n" + strings.ReplaceAll(strings.ReplaceAll(gpu, "BASE", base), "ARGS", args) + "echo \"$*\" >> '" + args + "'\necho $$ >> '" + pids + "'\nexec cat /dev/zero\n"
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
	if err := st.UpsertItem(ctx, &model.Item{ID: "w", GUID: "gw", LibraryID: lib.ID, Type: model.TypeMovie, Title: "Wide"}); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceSource(ctx, &model.MediaSource{ID: "wide", ItemID: "w", Container: "mkv", IsPrimary: true,
		Parts:   []model.MediaPart{{ID: "pw", SourceID: "wide", Path: wide}},
		Streams: []model.MediaStream{{SourceID: "wide", Index: 0, Type: model.StreamVideo, Codec: "hevc", Width: 3840, Height: 1604, FPS: 23.976}, {SourceID: "wide", Index: 1, Type: model.StreamAudio, Codec: "aac", Channels: 2}}}, "{}"); err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: &config.Config{DataDir: data}, store: st, ff: &ffmpeg.FFmpeg{FFmpeg: fake},
		transcoder: transcode.NewDetector(fake, encoder, ""), conv: newConversions()}
	r := chi.NewRouter()
	// A request may say which signed-in device it comes from (openAs).
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			if d := req.Header.Get("X-Test-Device"); d != "" {
				req = req.WithContext(auth.WithPrincipal(req.Context(), &model.Principal{UserID: "u", DeviceID: d}))
			}
			next.ServeHTTP(rw, req)
		})
	})
	r.Get("/v1/sources/{id}/transcode", s.transcodeStream)
	r.Get("/v1/sources/{id}/remux", s.remuxStream)
	w := &pipeWorld{srv: httptest.NewServer(r), pids: pids, args: args, base: base, s: s}
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

// get asks for path as a device ("" = not signed in: the address is all that is known).
func (w *pipeWorld) get(t *testing.T, device, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequest("GET", w.srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if device != "" {
		req.Header.Set("X-Test-Device", device)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// open starts a stream and returns its response with the first bytes read.
func (w *pipeWorld) open(t *testing.T, path string) *http.Response { return w.openAs(t, "", path) }

// openAs is open for a signed-in device.
func (w *pipeWorld) openAs(t *testing.T, device, path string) *http.Response {
	t.Helper()
	resp := w.get(t, device, path)
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("%s: %d %s", path, resp.StatusCode, b)
	}
	// Closed with the test at the latest: a stream left open by a test that failed would
	// keep its handler writing, and the test server's own closing waiting on that.
	t.Cleanup(func() { resp.Body.Close() })
	if _, err := io.ReadFull(resp.Body, make([]byte, 64<<10)); err != nil {
		t.Fatal(err)
	}
	return resp
}

// lastArgs is the command line the newest fake ffmpeg was started with.
func (w *pipeWorld) lastArgs(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(w.args)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	return lines[len(lines)-1]
}

// A quality is a class: "1440p" of a film wider than 16:9 fits 2560 pixels across as well
// as 1440 down. The handler has to tell the encoder the file's width for that; by height
// alone a 3840 x 1604 film came out 3448 x 1440, still 4K across.
func TestTranscodeFitsAWideFilmToItsQualityClass(t *testing.T) {
	w := newPipeWorld(t)
	for path, want := range map[string]string{
		"/v1/sources/wide/transcode?web=1&height=1440&bitrate=16000": "scale=-2:1070,",
		"/v1/sources/wide/transcode?height=1080&bitrate=8000":        "scale=-2:802,",
		"/v1/sources/src/transcode?height=120&bitrate=300":           "scale=-2:120,",
	} {
		resp := w.open(t, path)
		resp.Body.Close()
		if got := w.lastArgs(t); !strings.Contains(got, want) {
			t.Errorf("%s: want %q in: %s", path, want, got)
		}
		waitUntil(t, "ffmpeg to stop", 5*time.Second, func() bool { return len(w.running()) == 0 })
	}
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
		open = append(open, w.openAs(t, "device"+strconv.Itoa(i), "/v1/sources/src/transcode"))
	}
	waitUntil(t, "every transcode running", 5*time.Second, func() bool { return len(w.running()) == cap(transcodeSlots) })
	resp := w.get(t, "one-more", "/v1/sources/src/transcode")
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("a transcode past the slot count: %d, want 503", resp.StatusCode)
	}
	if n := len(w.running()); n != cap(transcodeSlots) {
		t.Fatalf("%d ffmpeg running after a refused request, want %d", n, cap(transcodeSlots))
	}
	open[0].Body.Close()
	waitUntil(t, "a slot to come back", 10*time.Second, func() bool { return len(transcodeSlots) < cap(transcodeSlots) })
	open[0] = w.openAs(t, "device0", "/v1/sources/src/transcode")
	for _, r := range open {
		r.Body.Close()
	}
	waitUntil(t, "every ffmpeg to stop", 10*time.Second, func() bool { return len(w.running()) == 0 })
	waitUntil(t, "every slot to be free", 5*time.Second, func() bool { return len(transcodeSlots) == 0 })
}

// One device could take every place the server has: four conversions asked for by one
// player, and nobody else could convert anything. A device has half of a kind's places
// at most, and its next does not get one more: it ends that device's oldest, which its
// player has left behind.
func TestOneDeviceCannotTakeEveryConversion(t *testing.T) {
	for _, kind := range []string{"transcode", "remux"} {
		t.Run(kind, func(t *testing.T) {
			w := newPipeWorld(t)
			slots := transcodeSlots
			if kind == "remux" {
				slots = remuxSlots
			}
			share := clientShare(slots)
			if share != cap(slots)/2 || share < 2 {
				t.Fatalf("a device's share of %d places is %d, want half", cap(slots), share)
			}
			path := "/v1/sources/src/" + kind
			var own []*http.Response
			for i := 0; i < share; i++ {
				own = append(own, w.openAs(t, "tv", path))
			}
			waitUntil(t, "the device's share running", 5*time.Second, func() bool { return len(w.running()) == share })
			// A seek, a change of quality: the next one, with the others still open.
			own = append(own, w.openAs(t, "tv", path))
			waitUntil(t, "the oldest to be ended", 5*time.Second, func() bool { return len(w.running()) == share && len(slots) == share })
			if n := w.s.conv.count(kind, "device|tv"); n != share {
				t.Fatalf("the device has %d conversions, want %d", n, share)
			}
			// The one ended is the oldest: its stream is over, the next oldest's goes on.
			done := make(chan error, 1)
			go func() { _, err := io.Copy(io.Discard, own[0].Body); done <- err }()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("the oldest stream was not ended")
			}
			if _, err := io.ReadFull(own[1].Body, make([]byte, 64<<10)); err != nil {
				t.Fatalf("a newer stream was ended too: %v", err)
			}
			// Other devices fill the rest of the server's places...
			for i := 0; len(slots) < cap(slots); i++ {
				w.openAs(t, "other"+strconv.Itoa(i), path)
			}
			// ...and the device's next still starts: it only ever trades one of its own.
			w.openAs(t, "tv", path)
			waitUntil(t, "the trade", 5*time.Second, func() bool { return len(w.running()) == cap(slots) && len(slots) == cap(slots) })
			if n := w.s.conv.count(kind, "device|tv"); n != share {
				t.Fatalf("the device has %d conversions after one more, want %d", n, share)
			}
			// While somebody with none is told the server is busy, as before.
			resp := w.get(t, "latecomer", path)
			resp.Body.Close()
			if resp.StatusCode != http.StatusServiceUnavailable {
				t.Fatalf("a device with none, the server full: %d, want 503", resp.StatusCode)
			}
		})
	}
}

// Conversions are counted as they start, each client for itself: a loop that asks for
// one after another does not get an ffmpeg per turn. (The limit for JSON calls allows
// twenty a second, and a song's stream is not counted by it at all.)
func TestConversionStartsAreCounted(t *testing.T) {
	w := newPipeWorld(t)
	w.s.conv.starts = newRateLimiter(0.001, 3) // three, then next to none
	for _, kind := range []string{"transcode", "remux", "transcode"} {
		resp := w.openAs(t, "phone", "/v1/sources/src/"+kind)
		resp.Body.Close()
	}
	started := len(strings.Split(strings.TrimSpace(readFile(t, w.args)), "\n"))
	resp := w.get(t, "phone", "/v1/sources/src/transcode")
	resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests || resp.Header.Get("Retry-After") == "" {
		t.Fatalf("a fourth start at once: %d (Retry-After %q), want 429 with a time to wait", resp.StatusCode, resp.Header.Get("Retry-After"))
	}
	if n := len(strings.Split(strings.TrimSpace(readFile(t, w.args)), "\n")); n != started {
		t.Fatalf("a refused start ran ffmpeg (%d runs, %d before)", n, started)
	}
	// Somebody else is not counted with them.
	other := w.openAs(t, "tv", "/v1/sources/src/transcode")
	other.Body.Close()
	waitUntil(t, "every ffmpeg to stop", 10*time.Second, func() bool { return len(w.running()) == 0 })
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// gpuScript makes the fake ffmpeg's VAAPI encoder a GPU that can leave: once the file
// "gone" exists every use of it fails at once, as a device the kernel has given up on
// does; while "dying" exists a stream gets its first 200 KB and then the GPU goes. The
// film "Wide" is one it cannot encode at any time. Test encodes (the encoder check) pass
// while it is there and are not listed in the args file.
const gpuScript = `case "$*" in *h264_vaapi*)
  case "$*" in *Wide.mkv*) echo "$*" >> 'ARGS'; echo 'No usable encoding profile found.' >&2; exit 1;; esac
  if [ -f 'BASE/gone' ]; then
    case "$*" in *testsrc2*) ;; *) echo "$*" >> 'ARGS';; esac
    echo 'No VA display found for device /dev/dri/renderD128.' >&2; exit 1
  fi
  if [ -f 'BASE/dying' ]; then
    case "$*" in *testsrc2*) exit 0;; esac
    echo "$*" >> 'ARGS'; head -c 200000 /dev/zero; : > 'BASE/gone'
    echo 'Failed to sync to output buffer completion' >&2; exit 1
  fi;;
esac
case "$*" in *testsrc2*) exit 0;; esac
`

func (w *pipeWorld) allArgs(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(w.args)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

func (w *pipeWorld) mark(t *testing.T, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(w.base, name), nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

// The GPU left while the server ran (seen with an RX 550 on Linux: the kernel failed to
// power the card down in the middle of an encode). Every conversion asked the hardware encoder, which
// failed at once: an empty answer, and players that asked again without end. Now the one
// that finds out is answered by the CPU encoder in the same answer, and those after it
// go to the CPU straight away.
func TestAConversionGoesToTheCPUWhenTheHardwareEncoderIsGone(t *testing.T) {
	w := newPipeWorldWith(t, "vaapi", gpuScript)
	const path = "/v1/sources/src/transcode?height=120&bitrate=300"
	resp := w.open(t, path)
	resp.Body.Close()
	if got := w.lastArgs(t); !strings.Contains(got, "h264_vaapi") {
		t.Fatalf("with the GPU there, the conversion did not use it: %s", got)
	}
	waitUntil(t, "ffmpeg to stop", 5*time.Second, func() bool { return len(w.running()) == 0 })

	w.mark(t, "gone")
	resp = w.open(t, path) // 200 and a stream, though the GPU is gone
	resp.Body.Close()
	lines := w.allArgs(t)
	if len(lines) != 3 || !strings.Contains(lines[1], "h264_vaapi") || !strings.Contains(lines[2], "libx264") {
		t.Fatalf("want the GPU tried, then the CPU, in one answer; ffmpeg was run with:\n%s", strings.Join(lines, "\n"))
	}
	if strings.Contains(lines[2], "vaapi") {
		t.Errorf("the CPU conversion still asks for the GPU device: %s", lines[2])
	}
	waitUntil(t, "ffmpeg to stop", 5*time.Second, func() bool { return len(w.running()) == 0 })
	if e := w.s.transcoder.Encoder(); e != transcode.Software {
		t.Errorf("the server still says its encoder is %s", e)
	}

	resp = w.open(t, path)
	resp.Body.Close()
	if lines := w.allArgs(t); len(lines) != 4 || !strings.Contains(lines[3], "libx264") {
		t.Errorf("the next conversion should go to the CPU at once; ffmpeg was run with:\n%s", strings.Join(lines, "\n"))
	}
	waitUntil(t, "ffmpeg to stop", 5*time.Second, func() bool { return len(w.running()) == 0 })
}

// The GPU goes in the middle of a stream: that stream ends (the player has part of it and
// asks again from there), and the answer to its next request comes from the CPU.
func TestAStreamCutByALostGPUIsFollowedByTheCPU(t *testing.T) {
	w := newPipeWorldWith(t, "vaapi", gpuScript)
	const path = "/v1/sources/src/transcode?height=120&bitrate=300"
	w.mark(t, "dying")
	resp, err := http.Get(w.srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if len(body) != 200000 {
		t.Fatalf("the cut stream gave %d bytes, want the 200000 the GPU made (nothing may be added to a stream the player has part of)", len(body))
	}
	waitUntil(t, "the server to leave the GPU", 5*time.Second, func() bool { return w.s.transcoder.Encoder() == transcode.Software })
	resp = w.open(t, path)
	resp.Body.Close()
	if got := w.lastArgs(t); !strings.Contains(got, "libx264") || strings.Contains(got, "vaapi") {
		t.Errorf("after the cut, the player's next request was not answered by the CPU: %s", got)
	}
	waitUntil(t, "ffmpeg to stop", 5*time.Second, func() bool { return len(w.running()) == 0 })
}

// A file the GPU cannot encode, on a GPU that is fine: that conversion is answered by the
// CPU, and the GPU stays the server's encoder for everything else.
func TestAFileTheGPUCannotTakeIsConvertedByTheCPU(t *testing.T) {
	w := newPipeWorldWith(t, "vaapi", gpuScript)
	resp := w.open(t, "/v1/sources/wide/transcode?height=1080&bitrate=8000")
	resp.Body.Close()
	lines := w.allArgs(t)
	if len(lines) != 2 || !strings.Contains(lines[0], "h264_vaapi") || !strings.Contains(lines[1], "libx264") || strings.Contains(lines[1], "vaapi") {
		t.Fatalf("want the GPU tried, then the CPU; ffmpeg was run with:\n%s", strings.Join(lines, "\n"))
	}
	waitUntil(t, "ffmpeg to stop", 5*time.Second, func() bool { return len(w.running()) == 0 })
	if e := w.s.transcoder.Encoder(); e != transcode.VAAPI {
		t.Errorf("one file it cannot take cost the server its GPU: encoder is now %s", e)
	}
	resp = w.open(t, "/v1/sources/src/transcode?height=120&bitrate=300")
	resp.Body.Close()
	if got := w.lastArgs(t); !strings.Contains(got, "h264_vaapi") {
		t.Errorf("another file should still go to the GPU: %s", got)
	}
	waitUntil(t, "ffmpeg to stop", 5*time.Second, func() bool { return len(w.running()) == 0 })
}
