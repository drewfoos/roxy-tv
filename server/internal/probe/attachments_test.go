package probe

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/roxy-media/roxy/server/internal/ffmpeg"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/safefs"
)

// fontFFmpeg stands in for `ffmpeg -dump_attachment`: it notes the call, writes the two
// fonts into its working directory and then runs tail (a sleep, an exit code).
func fontFFmpeg(t *testing.T, calls, tail string) *ffmpeg.FFmpeg {
	t.Helper()
	script := filepath.Join(t.TempDir(), "ffmpeg")
	src := "#!/bin/sh\necho x >> '" + calls + "'\nprintf font-a > A.ttf\nsleep 0.05\nprintf font-b > B.otf\n" + tail + "\n"
	if err := os.WriteFile(script, []byte(src), 0o755); err != nil {
		t.Fatal(err)
	}
	return &ffmpeg.FFmpeg{FFmpeg: script}
}

func twoFonts() []model.Attachment {
	return []model.Attachment{{Index: 3, Filename: "A.ttf"}, {Index: 4, Filename: "B.otf"}}
}

// A subtitle track's fonts are all asked for at once: one ffmpeg run serves them all, and
// the folder holds the fonts and nothing else afterwards.
func TestEnsureAttachmentsRunsOnce(t *testing.T) {
	data := t.TempDir()
	safefs.Init(data)
	calls := filepath.Join(t.TempDir(), "calls")
	dir := filepath.Join(data, "cache", "attachments", "src1")
	ff := fontFFmpeg(t, calls, "exit 0")

	var wg sync.WaitGroup
	got := make([][]model.Attachment, 8)
	errs := make([]error, len(got))
	for i := range got {
		got[i] = twoFonts()
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = EnsureAttachments(context.Background(), ff, "/media/x.mkv", dir, got[i])
		}(i)
	}
	wg.Wait()
	for i := range got {
		if errs[i] != nil {
			t.Fatalf("call %d: %v", i, errs[i])
		}
		for _, a := range got[i] {
			if a.ExtractedPath != filepath.Join(dir, a.Filename) || a.ExtractedAt == nil {
				t.Fatalf("call %d: %s not filled in: %+v", i, a.Filename, a)
			}
		}
	}
	if n := callCount(t, calls); n != 1 {
		t.Fatalf("ffmpeg ran %d times, want 1", n)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "B.otf")); string(b) != "font-b" {
		t.Fatalf("B.otf = %q", b)
	}
	if names := dirNames(t, dir); names != "A.ttf B.otf" {
		t.Fatalf("the folder holds %q, want only the two fonts", names)
	}

	// Ingest always extracts again (the file may have changed); it leaves no litter either.
	again := twoFonts()
	if err := ExtractAttachments(context.Background(), ff, "/media/x.mkv", dir, again); err != nil {
		t.Fatal(err)
	}
	if n := callCount(t, calls); n != 2 {
		t.Fatalf("ffmpeg ran %d times after a second ingest, want 2", n)
	}
	if names := dirNames(t, dir); names != "A.ttf B.otf" {
		t.Fatalf("the folder holds %q after a second ingest", names)
	}
}

// An extraction stopped half way (the request ended) leaves no font behind: a later
// request would have taken the half-written file for the font.
func TestStoppedAttachmentExtractionKeepsNothing(t *testing.T) {
	data := t.TempDir()
	safefs.Init(data)
	calls := filepath.Join(t.TempDir(), "calls")
	dir := filepath.Join(data, "cache", "attachments", "src1")

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	atts := twoFonts()
	if err := EnsureAttachments(ctx, fontFFmpeg(t, calls, "exec sleep 30"), "/media/x.mkv", dir, atts); err == nil {
		t.Fatal("a stopped extraction reported success")
	}
	if names := dirNames(t, dir); names != "" {
		t.Fatalf("a stopped extraction left %q", names)
	}
	for _, a := range atts {
		if a.ExtractedPath != "" {
			t.Fatalf("%s is recorded as extracted", a.Filename)
		}
	}

	atts = twoFonts()
	if err := EnsureAttachments(context.Background(), fontFFmpeg(t, calls, "exit 0"), "/media/x.mkv", dir, atts); err != nil {
		t.Fatal(err)
	}
	if names := dirNames(t, dir); names != "A.ttf B.otf" {
		t.Fatalf("the folder holds %q, want the two fonts", names)
	}
}

func dirNames(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return strings.Join(names, " ")
}
