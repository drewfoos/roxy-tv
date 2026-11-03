package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/roxy-media/roxy/server/internal/config"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/safefs"
	"github.com/roxy-media/roxy/server/internal/store"
)

func TestAttachmentMime(t *testing.T) {
	for _, c := range []struct{ declared, file, want string }{
		{"text/html", "x.ttf", "application/octet-stream"},
		{"image/svg+xml", "x.svg", "application/octet-stream"},
		{"application/xhtml+xml", "x.ttf", "application/octet-stream"},
		{"font/ttf", "x.ttf", "font/ttf"},
		{"FONT/OTF; x=1", "x.otf", "font/otf"},
		{"application/x-truetype-font", "x.ttf", "application/x-truetype-font"},
		{"application/vnd.ms-opentype", "x.otf", "application/vnd.ms-opentype"},
		{"application/x-font-ttf", "x.ttf", "application/x-font-ttf"},
		{"application/x-font-ttf\r\nX-Evil: 1", "x.ttf", "application/octet-stream"},
		{"font/", "x.ttf", "application/octet-stream"},
		{"", "x.ttf", "font/ttf"},
		{"", "x.html", "application/octet-stream"},
		{"", "x.svg", "application/octet-stream"},
	} {
		if got := attachmentMime(c.declared, c.file); got != c.want {
			t.Errorf("attachmentMime(%q, %q) = %q, want %q", c.declared, c.file, got, c.want)
		}
	}
}

func TestContentDisposition(t *testing.T) {
	for name, want := range map[string]string{
		"ep.mkv":                   `inline; filename=ep.mkv`,
		`a"b.mkv`:                  `inline; filename="a\"b.mkv"`,
		"x.mkv\r\nSet-Cookie: a=b": `inline; filename="x.mkvSet-Cookie: a=b"`,
		"Frieren – 01.mkv":         `inline; filename*=utf-8''Frieren%20%E2%80%93%2001.mkv`,
	} {
		if got := contentDisposition("inline", name); got != want {
			t.Errorf("contentDisposition(%q) = %q, want %q", name, got, want)
		}
	}
}

// streamFixture: a library with one source whose attachments and sidecars are hostile
// (an HTML "font", a sidecar symlinked to the database), plus their honest counterparts.
func streamFixture(t *testing.T) (*Server, http.Handler, string) {
	t.Helper()
	base := t.TempDir()
	data := filepath.Join(base, "config")
	media := filepath.Join(base, "media")
	show := filepath.Join(media, "Show")
	attDir := filepath.Join(data, "cache", "attachments", "src1")
	for _, d := range []string{show, attDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	db := filepath.Join(data, "roxy.db")
	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	video := filepath.Join(show, "ep.mkv")
	write(video, "not really a video")
	write(filepath.Join(show, "ep.en.srt"), "1\n00:00:01,000 --> 00:00:02,000\nhello\n")
	if err := os.Symlink(db, filepath.Join(show, "ep.de.srt")); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(attDir, "evil.html"), "<script>alert(document.domain)</script>")
	write(filepath.Join(attDir, "Font.ttf"), "\x00\x01\x00\x00font")

	safefs.Init(data)
	safefs.SetMediaRoots([]string{media})
	t.Cleanup(func() { safefs.SetMediaRoots(nil) })

	ctx := context.Background()
	lib := &model.Library{Name: "Anime", Kind: model.KindSeries, Root: media}
	if err := st.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	item := &model.Item{ID: "item1", GUID: "g1", LibraryID: lib.ID, Type: model.TypeMovie, Title: "Show"}
	if err := st.UpsertItem(ctx, item); err != nil {
		t.Fatal(err)
	}
	src := &model.MediaSource{
		ID: "src1", ItemID: item.ID, Name: "ep.mkv", Container: "mkv", IsPrimary: true,
		Parts: []model.MediaPart{{ID: "part1", SourceID: "src1", Path: video}},
		Streams: []model.MediaStream{
			{SourceID: "src1", Index: 5, Type: model.StreamSubtitle, Codec: "subrip", IsTextFormat: true, IsExternal: true, ExternalPath: filepath.Join(show, "ep.en.srt")},
			{SourceID: "src1", Index: 6, Type: model.StreamSubtitle, Codec: "subrip", IsTextFormat: true, IsExternal: true, ExternalPath: filepath.Join(show, "ep.de.srt")},
		},
		Attachments: []model.Attachment{
			{SourceID: "src1", Index: 3, Filename: "evil.html", Mime: "text/html", ExtractedPath: filepath.Join(attDir, "evil.html")},
			{SourceID: "src1", Index: 4, Filename: "Font.ttf", Mime: "application/x-truetype-font", ExtractedPath: filepath.Join(attDir, "Font.ttf")},
			{SourceID: "src1", Index: 7, Filename: "db.ttf", Mime: "font/ttf", ExtractedPath: db},
		},
	}
	if err := st.ReplaceSource(ctx, src, "{}"); err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: &config.Config{DataDir: data}, store: st}
	r := chi.NewRouter()
	r.Get("/v1/sources/{id}/attachments/{index}", s.getAttachment)
	r.Get("/v1/sources/{id}/subtitles/{index}", s.getSubtitle)
	r.Get("/v1/stream/{id}", s.stream)
	return s, r, video
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestAttachmentNeverServedAsHTML(t *testing.T) {
	_, h, _ := streamFixture(t)
	rec := get(h, "/v1/sources/src1/attachments/3")
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	hd := rec.Header()
	if ct := hd.Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("an HTML attachment must be octet-stream, got %q", ct)
	}
	if hd.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("missing nosniff")
	}
	if cd := hd.Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment;") {
		t.Errorf("Content-Disposition %q", cd)
	}
	if csp := hd.Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'none'") || !strings.Contains(csp, "sandbox") {
		t.Errorf("CSP %q", csp)
	}
	font := get(h, "/v1/sources/src1/attachments/4")
	if font.Code != 200 || font.Header().Get("Content-Type") != "application/x-truetype-font" || font.Body.String() != "\x00\x01\x00\x00font" {
		t.Errorf("font: %d %q %q", font.Code, font.Header().Get("Content-Type"), font.Body.String())
	}
	// An attachment row pointing outside the attachment cache (at the database) is refused.
	if rec := get(h, "/v1/sources/src1/attachments/7"); rec.Code == 200 {
		t.Errorf("attachment outside the cache was served (%d bytes)", rec.Body.Len())
	}
}

func TestSidecarSymlinkToDatabaseRefused(t *testing.T) {
	_, h, _ := streamFixture(t)
	ok := get(h, "/v1/sources/src1/subtitles/5")
	if ok.Code != 200 || !strings.Contains(ok.Body.String(), "hello") {
		t.Fatalf("a real sidecar must be served: %d %q", ok.Code, ok.Body.String())
	}
	bad := get(h, "/v1/sources/src1/subtitles/6")
	if bad.Code != http.StatusForbidden {
		t.Fatalf("a sidecar symlinked to roxy.db must be 403, got %d %q", bad.Code, bad.Body.String())
	}
}

func TestStreamPartBoundsAndSymlink(t *testing.T) {
	_, h, video := streamFixture(t)
	if rec := get(h, "/v1/stream/src1"); rec.Code != 200 {
		t.Fatalf("stream: %d %s", rec.Code, rec.Body.String())
	}
	for _, q := range []string{"-1", "1", "99999"} {
		if rec := get(h, "/v1/stream/src1?part="+q); rec.Code != 404 {
			t.Errorf("part=%s: want 404, got %d", q, rec.Code)
		}
	}
	// The video replaced by a symlink out of the media folder after it was scanned.
	if err := os.Remove(video); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", video); err != nil {
		t.Fatal(err)
	}
	if rec := get(h, "/v1/stream/src1"); rec.Code != http.StatusForbidden {
		t.Errorf("a video symlinked out of media must be 403, got %d", rec.Code)
	}
}
