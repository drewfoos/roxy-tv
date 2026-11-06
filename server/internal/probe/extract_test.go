package probe

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/roxy-media/roxy/server/internal/ffmpeg"
	"github.com/roxy-media/roxy/server/internal/safefs"
)

// fakeFFmpeg writes a shell script standing in for ffmpeg: it writes body to its last
// argument (the output file), appends a line to calls, and exits with code.
func fakeFFmpeg(t *testing.T, body string, code int, calls string) *ffmpeg.FFmpeg {
	t.Helper()
	script := filepath.Join(t.TempDir(), "ffmpeg")
	src := "#!/bin/sh\nfor a; do out=\"$a\"; done\necho x >> '" + calls + "'\nsleep 0.05\nprintf '%s' '" + body + "' > \"$out\"\nexit " + string(rune('0'+code)) + "\n"
	if err := os.WriteFile(script, []byte(src), 0o755); err != nil {
		t.Fatal(err)
	}
	return &ffmpeg.FFmpeg{FFmpeg: script}
}

func callCount(t *testing.T, calls string) int {
	b, err := os.ReadFile(calls)
	if err != nil {
		return 0
	}
	return strings.Count(string(b), "x")
}

// An extraction that fails after writing part of the track must leave nothing at the
// cache path: callers serve whatever is there and never extract again.
func TestExtractTextSubtitleFailureLeavesNoFile(t *testing.T) {
	data := t.TempDir()
	safefs.Init(data)
	calls := filepath.Join(t.TempDir(), "calls")
	dst := filepath.Join(data, "cache", "subtitles", "src1", "2.ass")
	ff := fakeFFmpeg(t, "[Script Info]\\npartial", 1, calls)
	if err := ExtractTextSubtitle(context.Background(), ff, "/media/x.mkv", 2, "ass", dst); err == nil {
		t.Fatal("a failed extraction reported success")
	}
	if _, err := os.Stat(dst); err == nil {
		t.Fatal("a partial subtitle was left at the cache path")
	}
	left, _ := os.ReadDir(filepath.Dir(dst))
	if len(left) != 0 {
		t.Fatalf("temporary files left behind: %v", left)
	}

	ff = fakeFFmpeg(t, "[Script Info]", 0, calls)
	if err := ExtractTextSubtitle(context.Background(), ff, "/media/x.mkv", 2, "ass", dst); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(dst); err != nil || string(b) != "[Script Info]" {
		t.Fatalf("extracted file = %q, %v", b, err)
	}
	left, _ = os.ReadDir(filepath.Dir(dst))
	if len(left) != 1 {
		t.Fatalf("want only the subtitle in the cache folder, got %v", left)
	}
}

// Concurrent requests for one uncached track run ffmpeg once.
func TestEnsureTextSubtitleSerialisesPerFile(t *testing.T) {
	data := t.TempDir()
	safefs.Init(data)
	calls := filepath.Join(t.TempDir(), "calls")
	dst := filepath.Join(data, "cache", "subtitles", "src1", "3.srt")
	ff := fakeFFmpeg(t, "1", 0, calls)
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = EnsureTextSubtitle(context.Background(), ff, "/media/x.mkv", 3, "subrip", dst)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if n := callCount(t, calls); n != 1 {
		t.Fatalf("ffmpeg ran %d times, want 1", n)
	}
}
