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

// Fonts that were not extracted when the file was read are extracted by the first request
// for one: once, however many fonts are asked for together, and recorded so that later
// requests find them. Each request used to run ffmpeg over the whole video.
func TestUnextractedFontsAreExtractedOnce(t *testing.T) {
	s, h, video := streamFixture(t)
	ctx := context.Background()
	calls := filepath.Join(t.TempDir(), "calls")
	script := filepath.Join(t.TempDir(), "ffmpeg")
	body := "#!/bin/sh\necho x >> '" + calls + "'\nprintf font-a > A.ttf\nsleep 0.05\nprintf font-b > B.otf\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
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
		want := "font-a"
		if strings.HasSuffix(p, "/4") {
			want = "font-b"
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
	if rec := get(h, "/v1/sources/src2/attachments/4"); rec.Code != 200 || rec.Body.String() != "font-b" {
		t.Fatalf("a later request: %d %q", rec.Code, rec.Body.String())
	}
	if n := ran(); n != 1 {
		t.Fatalf("ffmpeg ran %d times in all, want 1", n)
	}
}
