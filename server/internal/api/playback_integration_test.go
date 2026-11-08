package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/roxy-media/roxy/server/internal/auth"
	"github.com/roxy-media/roxy/server/internal/config"
	"github.com/roxy-media/roxy/server/internal/ffmpeg"
	"github.com/roxy-media/roxy/server/internal/library"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/safefs"
	"github.com/roxy-media/roxy/server/internal/store"
	"github.com/roxy-media/roxy/server/internal/trickplay"
)

// needFFmpeg skips a test that needs ffmpeg and ffprobe, or fails it when
// ROXY_REQUIRE_INTEGRATION is set (CI's integration job), so a green job means it ran.
func needFFmpeg(t *testing.T) *ffmpeg.FFmpeg {
	t.Helper()
	ff, err := ffmpeg.New("", "")
	if err == nil {
		if _, err = exec.LookPath(ff.FFprobe); err == nil {
			return ff
		}
	}
	if os.Getenv("ROXY_REQUIRE_INTEGRATION") != "" {
		t.Fatalf("ROXY_REQUIRE_INTEGRATION is set but ffmpeg is not usable: %v", err)
	}
	t.Skip("needs ffmpeg and ffprobe")
	return nil
}

// TestPlaybackOverHTTP plays a real file through the whole router, as a player does, and
// checks what comes back is right, not only that it was allowed (TestMedia... covers the
// refusals): the direct stream byte for byte, byte ranges (a span, an open end, a suffix,
// one out of range), HEAD, a sidecar and an embedded subtitle's text, a remux and a
// transcode that ffprobe reads as the right streams, a browser transcode, seek previews,
// and the same stream with a media token instead of the sign-in. Needs ffmpeg.
func TestPlaybackOverHTTP(t *testing.T) {
	ff := needFFmpeg(t)
	ctx := context.Background()
	base := t.TempDir()
	data, media := filepath.Join(base, "config"), filepath.Join(base, "media")
	cfg := &config.Config{DataDir: data, HWAccel: "none"}
	dir := filepath.Join(media, "Film (2020)")
	for _, d := range []string{dir, data} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	embedded := filepath.Join(base, "embedded.srt")
	if err := os.WriteFile(embedded, []byte("1\n00:00:00,500 --> 00:00:02,500\nembedded line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	video := filepath.Join(dir, "Film (2020).mkv")
	out, err := exec.Command(ff.FFmpeg, "-v", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=duration=25:size=320x240:rate=10",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=25",
		"-i", embedded,
		"-map", "0:v", "-map", "1:a", "-map", "2:s",
		"-c:v", "libx264", "-preset", "ultrafast", "-g", "10", "-c:a", "aac", "-c:s", "srt",
		"-metadata:s:s:0", "language=eng", "-f", "matroska", video).CombinedOutput() // no -shortest: the subtitle ends at 2.5 s
	if err != nil {
		t.Fatalf("making the video: %v %s", err, out)
	}
	// A clip shorter than one preview interval: no thumbnails can be made of it.
	short := filepath.Join(media, "Short (2021)", "Short (2021).mkv")
	if err := os.MkdirAll(filepath.Dir(short), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(ff.FFmpeg, "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc=duration=3:size=160x120:rate=10",
		"-c:v", "libx264", "-preset", "ultrafast", "-f", "matroska", short).CombinedOutput(); err != nil {
		t.Fatalf("making the short clip: %v %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(dir, "Film (2020).en.srt"), []byte("1\n00:00:01,000 --> 00:00:03,000\nhello sidecar\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	whole, err := os.ReadFile(video)
	if err != nil {
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
	lib := &model.Library{Name: "Movies", Kind: model.KindMovies, Root: media}
	if err := st.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	lb := library.New(st, ff, cfg)
	// A trailer: an extra of the film, with no picture of its own.
	trailer := filepath.Join(dir, "Film (2020)-trailer.mkv")
	if out, err := exec.Command(ff.FFmpeg, "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc=duration=8:size=320x240:rate=10",
		"-c:v", "libx264", "-preset", "ultrafast", "-f", "matroska", trailer).CombinedOutput(); err != nil {
		t.Fatalf("making the trailer: %v %s", err, out)
	}
	for _, p := range []string{video, short, trailer} {
		if err := lb.AddFile(ctx, *lib, p); err != nil { // the real prober
			t.Fatal(err)
		}
	}
	part, err := st.GetPartByPath(ctx, video)
	if err != nil || part == nil {
		t.Fatal(part, err)
	}
	src, err := st.GetSource(ctx, part.SourceID)
	if err != nil || src == nil {
		t.Fatal(src, err)
	}
	sidecar, inner := -1, -1
	for _, s := range src.Streams {
		if s.Type == model.StreamSubtitle {
			if s.IsExternal {
				sidecar = s.Index
			} else {
				inner = s.Index
			}
		}
	}
	if sidecar < 0 || inner < 0 {
		t.Fatalf("the source's subtitles: %+v", src.Streams)
	}

	authSvc := auth.New(st)
	u := &model.User{ID: "u1", Name: "alice", Role: model.RoleBasic}
	if err := st.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	dev, tok, err := authSvc.IssueToken(ctx, u, auth.ClientInfo{Client: "test", DeviceID: "phone"})
	if err != nil {
		t.Fatal(err)
	}
	trick := trickplay.New(st, ff, cfg)
	tctx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	trick.Start(tctx)
	h := NewRouter(cfg, st, lb, ff, authSvc, nil, trick, nil)
	do := func(method, path string, hdr map[string]string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, nil)
		r.RemoteAddr = "192.168.1.50:5000"
		r.Header.Set("Authorization", "Bearer "+tok)
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}
	streamPath := "/v1/stream/" + src.ID
	size := len(whole)

	// Direct play: the whole file, byte for byte.
	rec := do(http.MethodGet, streamPath, nil)
	if rec.Code != 200 || !bytes.Equal(rec.Body.Bytes(), whole) {
		t.Fatalf("direct stream: %d, %d bytes (file %d)", rec.Code, rec.Body.Len(), size)
	}
	if rec.Header().Get("Accept-Ranges") != "bytes" || rec.Header().Get("Content-Type") == "" {
		t.Errorf("direct stream headers: %v", rec.Header())
	}
	// Byte ranges, as a player seeking asks for them.
	for _, c := range []struct {
		rng        string
		from, to   int // inclusive
		wantStatus int
	}{
		{"bytes=100-199", 100, 199, 206},
		{fmt.Sprintf("bytes=%d-", size-10), size - 10, size - 1, 206},
		{"bytes=-50", size - 50, size - 1, 206},
		{fmt.Sprintf("bytes=%d-%d", size+10, size+20), 0, 0, 416},
	} {
		rec := do(http.MethodGet, streamPath, map[string]string{"Range": c.rng})
		if rec.Code != c.wantStatus {
			t.Errorf("Range %s: %d, want %d", c.rng, rec.Code, c.wantStatus)
			continue
		}
		if c.wantStatus != 206 {
			continue
		}
		if !bytes.Equal(rec.Body.Bytes(), whole[c.from:c.to+1]) {
			t.Errorf("Range %s: wrong bytes (%d of them)", c.rng, rec.Body.Len())
		}
		if want := fmt.Sprintf("bytes %d-%d/%d", c.from, c.to, size); rec.Header().Get("Content-Range") != want {
			t.Errorf("Range %s: Content-Range %q, want %q", c.rng, rec.Header().Get("Content-Range"), want)
		}
	}
	// HEAD: the size, no body.
	rec = do(http.MethodHead, streamPath, nil)
	if rec.Code != 200 || rec.Body.Len() != 0 || rec.Header().Get("Content-Length") != strconv.Itoa(size) {
		t.Errorf("HEAD: %d, body %d, length %q", rec.Code, rec.Body.Len(), rec.Header().Get("Content-Length"))
	}

	// Compression: JSON is gzipped for a client that accepts it; media never is. A player
	// that sends Accept-Encoding: gzip still gets the file's own bytes, and its ranges.
	gz := map[string]string{"Accept-Encoding": "gzip"}
	if rec := do(http.MethodGet, streamPath, gz); rec.Code != 200 || rec.Header().Get("Content-Encoding") != "" || !bytes.Equal(rec.Body.Bytes(), whole) {
		t.Errorf("stream asked with gzip: %d, encoding %q, %d bytes", rec.Code, rec.Header().Get("Content-Encoding"), rec.Body.Len())
	}
	if rec := do(http.MethodGet, streamPath, map[string]string{"Accept-Encoding": "gzip", "Range": "bytes=100-199"}); rec.Code != 206 || rec.Header().Get("Content-Encoding") != "" || !bytes.Equal(rec.Body.Bytes(), whole[100:200]) {
		t.Errorf("range asked with gzip: %d, encoding %q", rec.Code, rec.Header().Get("Content-Encoding"))
	}
	if rec := do(http.MethodGet, "/v1/items?type=movie&recursive=1", gz); rec.Code != 200 || rec.Header().Get("Content-Encoding") != "gzip" {
		t.Errorf("JSON asked with gzip: %d, encoding %q", rec.Code, rec.Header().Get("Content-Encoding"))
	} else if zr, err := gzip.NewReader(rec.Body); err != nil {
		t.Errorf("JSON gzip: %v", err)
	} else {
		var list struct{ Items []model.Item }
		if err := json.NewDecoder(zr).Decode(&list); err != nil || len(list.Items) == 0 {
			t.Errorf("gzipped JSON: %v, %d items", err, len(list.Items))
		}
	}

	// Subtitles: the sidecar's text, and the embedded track's (extracted by ffmpeg).
	for idx, want := range map[int]string{sidecar: "hello sidecar", inner: "embedded line"} {
		rec := do(http.MethodGet, fmt.Sprintf("/v1/sources/%s/subtitles/%d", src.ID, idx), nil)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), want) {
			t.Errorf("subtitle %d: %d %q, want it to contain %q", idx, rec.Code, rec.Body.String(), want)
		}
	}

	// Remux and transcode: what comes back is a file ffprobe reads, with the right streams.
	probeOut := func(name string, body []byte) map[string]map[string]any {
		t.Helper()
		p := filepath.Join(base, name)
		if err := os.WriteFile(p, body, 0o644); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command(ff.FFprobe, "-v", "error", "-show_streams", "-of", "json", p).Output()
		if err != nil {
			t.Fatalf("%s: ffprobe cannot read the output (%d bytes): %v", name, len(body), err)
		}
		var res struct {
			Streams []map[string]any `json:"streams"`
		}
		if err := json.Unmarshal(out, &res); err != nil {
			t.Fatal(err)
		}
		byType := map[string]map[string]any{}
		for _, s := range res.Streams {
			if ty, _ := s["codec_type"].(string); byType[ty] == nil {
				byType[ty] = s
			}
		}
		return byType
	}
	get := func(path string) []byte {
		t.Helper()
		rec := do(http.MethodGet, path, nil)
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body.String())
		}
		return rec.Body.Bytes()
	}
	remux := probeOut("remux.mkv", get("/v1/sources/"+src.ID+"/remux"))
	if remux["video"]["codec_name"] != "h264" || remux["audio"] == nil {
		t.Errorf("remux streams: %v", remux)
	}
	tr := probeOut("transcode.mkv", get("/v1/sources/"+src.ID+"/transcode?height=120&bitrate=300"))
	if tr["video"]["codec_name"] != "h264" || tr["audio"]["codec_name"] != "aac" {
		t.Errorf("transcode streams: %v", tr)
	} else if hgt, _ := tr["video"]["height"].(float64); hgt <= 0 || hgt > 120 {
		t.Errorf("transcode height %v, want at most 120", hgt)
	}
	web := probeOut("web.mkv", get("/v1/sources/"+src.ID+"/transcode?web=1&height=120"))
	if web["video"] == nil || web["audio"] == nil || web["subtitle"] != nil {
		t.Errorf("browser transcode streams (video and one audio, no subtitles): %v", web)
	}

	// Seek previews: asked for, generated, then a manifest and a JPEG sheet.
	manifest := "/v1/sources/" + src.ID + "/trickplay"
	for deadline := time.Now().Add(60 * time.Second); ; time.Sleep(200 * time.Millisecond) {
		rec := do(http.MethodGet, manifest, nil)
		if rec.Code == 200 {
			var m struct{ Count int }
			if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil || m.Count < 1 {
				t.Fatalf("trickplay manifest: %v %s", err, rec.Body.String())
			}
			break
		}
		if rec.Code != 202 || time.Now().After(deadline) {
			t.Fatalf("trickplay manifest: %d %s", rec.Code, rec.Body.String())
		}
	}
	sheet := do(http.MethodGet, manifest+"/0", nil)
	if sheet.Code != 200 || !bytes.HasPrefix(sheet.Body.Bytes(), []byte{0xff, 0xd8}) {
		t.Errorf("trickplay sheet: %d, %d bytes", sheet.Code, sheet.Body.Len())
	}
	// The short clip: "generating" until the attempt fails, then "none" (404), not
	// "generating" for the hour before the next attempt.
	shortPart, err := st.GetPartByPath(ctx, short)
	if err != nil || shortPart == nil {
		t.Fatal(shortPart, err)
	}
	for deadline := time.Now().Add(60 * time.Second); ; time.Sleep(200 * time.Millisecond) {
		rec := do(http.MethodGet, "/v1/sources/"+shortPart.SourceID+"/trickplay", nil)
		if rec.Code == 404 {
			break
		}
		if rec.Code != 202 || time.Now().After(deadline) {
			t.Fatalf("trickplay of a clip too short for any: %d %s", rec.Code, rec.Body.String())
		}
	}

	// The trailer's row has a thumbnail: offered by the item, made from a frame on request.
	extra, err := st.ItemForPath(ctx, trailer)
	if err != nil || extra == nil || extra.Type != model.TypeExtra {
		t.Fatalf("the trailer's item: %+v %v", extra, err)
	}
	listed := do(http.MethodGet, "/v1/items/"+extra.ID, nil)
	var asListed struct{ Images map[string]string }
	if err := json.Unmarshal(listed.Body.Bytes(), &asListed); err != nil || asListed.Images["thumb"] == "" {
		t.Fatalf("the trailer offers no thumb: %s", listed.Body.String())
	}
	thumb := do(http.MethodGet, "/v1/items/"+extra.ID+"/images/thumb", nil)
	if thumb.Code != 200 || !bytes.HasPrefix(thumb.Body.Bytes(), []byte{0xff, 0xd8}) {
		t.Errorf("the trailer's thumb: %d, %d bytes", thumb.Code, thumb.Body.Len())
	}

	// A player's way in: a media token, with no sign-in; the stream and a range still work.
	mt, _, err := authSvc.NewMediaToken(ctx, src.ID, u.ID, dev.ID)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, streamPath+"?mt="+mt, nil)
	r.Header.Set("Range", "bytes=0-99")
	r.RemoteAddr = "192.168.1.50:5000"
	mrec := httptest.NewRecorder()
	h.ServeHTTP(mrec, r)
	body, _ := io.ReadAll(mrec.Body)
	if mrec.Code != 206 || !bytes.Equal(body, whole[:100]) {
		t.Errorf("stream with a media token: %d, %d bytes", mrec.Code, len(body))
	}
}
