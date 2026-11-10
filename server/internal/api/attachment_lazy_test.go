package api

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/roxy-media/roxy/server/internal/ffmpeg"
	"github.com/roxy-media/roxy/server/internal/model"
)

// fontScript stands in for ffmpeg dumping attachments: it notes the call in calls and
// writes "font-<stream>" into each file a "-dump_attachment:<stream> <file>" pair names,
// except for the streams in skip (ffmpeg writes nothing for a stream it has nothing for,
// and still exits 0).
func fontScript(calls, skip string) string {
	return `#!/bin/sh
echo x >> '` + calls + `'
while [ $# -gt 0 ]; do
	case "$1" in -dump_attachment:*)
		n="${1#-dump_attachment:}"
		shift
		case " ` + skip + ` " in *" $n "*) ;; *) printf 'font-%s' "$n" > "$1"; sleep 0.05 ;; esac
	esac
	shift
done
`
}

// fontSource stores a video with three font attachments, none extracted yet.
func fontSource(t *testing.T, s *Server, video, id string) {
	t.Helper()
	ctx := context.Background()
	lib, err := s.store.ListLibraries(ctx)
	if err != nil || len(lib) == 0 {
		t.Fatalf("libraries: %v", err)
	}
	path := filepath.Join(filepath.Dir(video), id+".mkv")
	if err := os.WriteFile(path, []byte("not really a video"), 0o644); err != nil {
		t.Fatal(err)
	}
	item := &model.Item{ID: "item-" + id, GUID: "g-" + id, LibraryID: lib[0].ID, Type: model.TypeMovie, Title: "Film " + id}
	if err := s.store.UpsertItem(ctx, item); err != nil {
		t.Fatal(err)
	}
	src := &model.MediaSource{
		ID: id, ItemID: item.ID, Name: id + ".mkv", Container: "mkv", IsPrimary: true,
		Parts: []model.MediaPart{{ID: "part-" + id, SourceID: id, Path: path}},
		Attachments: []model.Attachment{
			{SourceID: id, Index: 3, Filename: "A.ttf", Mime: "font/ttf"},
			{SourceID: id, Index: 4, Filename: "B.otf", Mime: "font/otf"},
			{SourceID: id, Index: 5, Filename: "C.ttf", Mime: "font/ttf"},
		},
	}
	if err := s.store.ReplaceSource(ctx, src, "{}"); err != nil {
		t.Fatal(err)
	}
}

// One of a file's three fonts does not come out of it. The other two are served and
// recorded all the same (the whole request used to depend on the extraction reporting
// no error), a request for the third is "not found" rather than a server error, and
// asking for it again does not run ffmpeg over the video again.
func TestFontThatCannotBeExtractedDoesNotTakeTheOthersWithIt(t *testing.T) {
	s, h, video := streamFixture(t)
	calls := filepath.Join(t.TempDir(), "calls")
	script := filepath.Join(t.TempDir(), "ffmpeg")
	if err := os.WriteFile(script, []byte(fontScript(calls, "4")), 0o755); err != nil {
		t.Fatal(err)
	}
	s.ff = &ffmpeg.FFmpeg{FFmpeg: script}
	fontSource(t, s, video, "src3")
	ran := func() int {
		b, _ := os.ReadFile(calls)
		return strings.Count(string(b), "x")
	}

	// The font that is not there is asked for first: the others are extracted by that run.
	for i := 1; i <= 3; i++ {
		if rec := get(h, "/v1/sources/src3/attachments/4"); rec.Code != 404 {
			t.Fatalf("request %d for the font that does not come out: %d %s, want 404", i, rec.Code, rec.Body.String())
		}
	}
	if n := ran(); n != 1 {
		t.Fatalf("ffmpeg ran %d times for three requests for a font it cannot produce, want 1", n)
	}
	stored, err := s.store.GetSource(context.Background(), "src3")
	if err != nil || stored == nil {
		t.Fatalf("source: %v", err)
	}
	dir := filepath.Join(s.cfg.AttachmentsDir(), "src3")
	for _, a := range stored.Attachments {
		want := filepath.Join(dir, a.Filename)
		if a.Index == 4 {
			want = ""
		}
		if a.ExtractedPath != want {
			t.Errorf("%s is recorded at %q, want %q", a.Filename, a.ExtractedPath, want)
		}
	}
	for p, want := range map[string]string{"/v1/sources/src3/attachments/3": "font-3", "/v1/sources/src3/attachments/5": "font-5"} {
		if rec := get(h, p); rec.Code != 200 || rec.Body.String() != want {
			t.Errorf("%s: %d %q, want 200 %q", p, rec.Code, rec.Body.String(), want)
		}
	}
	if n := ran(); n != 1 {
		t.Fatalf("ffmpeg ran %d times in all, want 1", n)
	}
}

// Fonts that were not extracted when the file was read are extracted by the first request
// for one: once, however many fonts are asked for together, and recorded so that later
// requests find them. Each request used to run ffmpeg over the whole video.
func TestUnextractedFontsAreExtractedOnce(t *testing.T) {
	s, h, video := streamFixture(t)
	ctx := context.Background()
	calls := filepath.Join(t.TempDir(), "calls")
	script := filepath.Join(t.TempDir(), "ffmpeg")
	if err := os.WriteFile(script, []byte(fontScript(calls, "")), 0o755); err != nil {
		t.Fatal(err)
	}
	s.ff = &ffmpeg.FFmpeg{FFmpeg: script}

	lib, err := s.store.ListLibraries(ctx)
	if err != nil || len(lib) == 0 {
		t.Fatalf("libraries: %v", err)
	}
	video2 := filepath.Join(filepath.Dir(video), "ep2.mkv")
	if err := os.WriteFile(video2, []byte("not really a video"), 0o644); err != nil {
		t.Fatal(err)
	}
	item := &model.Item{ID: "item2", GUID: "g2", LibraryID: lib[0].ID, Type: model.TypeMovie, Title: "Show 2"}
	if err := s.store.UpsertItem(ctx, item); err != nil {
		t.Fatal(err)
	}
	src := &model.MediaSource{
		ID: "src2", ItemID: item.ID, Name: "ep2.mkv", Container: "mkv", IsPrimary: true,
		Parts: []model.MediaPart{{ID: "part2", SourceID: "src2", Path: video2}},
		Attachments: []model.Attachment{
			{SourceID: "src2", Index: 3, Filename: "A.ttf", Mime: "font/ttf"},
			{SourceID: "src2", Index: 4, Filename: "B.otf", Mime: "font/otf"},
		},
	}
	if err := s.store.ReplaceSource(ctx, src, "{}"); err != nil {
		t.Fatal(err)
	}

	paths := []string{"/v1/sources/src2/attachments/3", "/v1/sources/src2/attachments/4", "/v1/sources/src2/attachments/3", "/v1/sources/src2/attachments/4"}
	bodies := make([]string, len(paths))
	codes := make([]int, len(paths))
	var wg sync.WaitGroup
	for i, p := range paths {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := get(h, p)
			codes[i], bodies[i] = rec.Code, rec.Body.String()
		}()
	}
	wg.Wait()
	for i, p := range paths {
		want := "font-3"
		if strings.HasSuffix(p, "/4") {
			want = "font-4"
		}
		if codes[i] != 200 || bodies[i] != want {
			t.Errorf("%s: %d %q, want 200 %q", p, codes[i], bodies[i], want)
		}
	}
	ran := func() int {
		b, _ := os.ReadFile(calls)
		return strings.Count(string(b), "x")
	}
	if n := ran(); n != 1 {
		t.Fatalf("ffmpeg ran %d times for four requests, want 1", n)
	}

	stored, err := s.store.GetSource(ctx, "src2")
	if err != nil || stored == nil {
		t.Fatalf("source: %v", err)
	}
	dir := filepath.Join(s.cfg.AttachmentsDir(), "src2")
	for _, a := range stored.Attachments {
		if a.ExtractedPath != filepath.Join(dir, a.Filename) || a.ExtractedAt == nil {
			t.Errorf("%s is not recorded as extracted: %+v", a.Filename, a)
		}
	}
	// Recorded: a later request does not look at the folder or run anything.
	s.ff = &ffmpeg.FFmpeg{FFmpeg: filepath.Join(t.TempDir(), "gone")}
	if rec := get(h, "/v1/sources/src2/attachments/4"); rec.Code != 200 || rec.Body.String() != "font-4" {
		t.Fatalf("a later request: %d %q", rec.Code, rec.Body.String())
	}
	if n := ran(); n != 1 {
		t.Fatalf("ffmpeg ran %d times in all, want 1", n)
	}
}
