package ffmpeg

import (
	"context"
	"strings"
	"testing"
	"time"
)

// A filter that reports ten times a second through a long file prints more than
// RunCombined will hold. RunLines hands the lines on as they come and keeps none.
func TestRunLinesHandsOnEveryLineOfALongRun(t *testing.T) {
	old := MaxCombined
	MaxCombined = 4096
	t.Cleanup(func() { MaxCombined = old })
	// 20 000 lines, half on each of stdout and stderr: far past what RunCombined holds.
	bin := fakeBin(t, `i=0; while [ $i -lt 10000 ]; do echo "out $i"; echo "err $i" >&2; i=$((i+1)); done; printf 'no newline at the end'`)
	f := &FFmpeg{FFmpeg: bin}
	if _, err := f.RunCombined(context.Background(), "-i", "x"); err == nil {
		t.Fatal("RunCombined held more than its limit: this test proves nothing")
	}
	out, errs, last := 0, 0, ""
	err := f.RunLines(context.Background(), func(b []byte) {
		s := string(b)
		switch {
		case strings.HasPrefix(s, "out "):
			out++
		case strings.HasPrefix(s, "err "):
			errs++
		}
		last = s
	}, "-i", "x")
	if err != nil {
		t.Fatal(err)
	}
	if out != 10000 || errs != 10000 {
		t.Errorf("lines handed on: %d of stdout, %d of stderr", out, errs)
	}
	if last != "no newline at the end" {
		t.Errorf("the last line, which had no end, came as %q", last)
	}
}

// A run that fails says so with the last of what it printed; one that hangs is stopped.
func TestRunLinesReportsFailureAndIsStopped(t *testing.T) {
	f := &FFmpeg{FFmpeg: fakeBin(t, `echo "reading"; echo "song.mp3: Invalid data found when processing input" >&2; exit 1`)}
	seen := 0
	err := f.RunLines(context.Background(), func([]byte) { seen++ }, "-i", "x")
	if err == nil || !strings.Contains(err.Error(), "Invalid data found") {
		t.Errorf("a failed run: %v", err)
	}
	if seen != 2 {
		t.Errorf("lines before the failure: %d", seen)
	}

	oldR := RunLimit
	RunLimit = 300 * time.Millisecond
	t.Cleanup(func() { RunLimit = oldR })
	hang := &FFmpeg{FFmpeg: fakeBin(t, "echo started; exec sleep 60")}
	start := time.Now()
	if err := hang.RunLines(context.Background(), func([]byte) {}, "-i", "x"); err == nil || time.Since(start) > 10*time.Second {
		t.Errorf("a hanging run: err %v after %v", err, time.Since(start))
	}
	// The caller leaving stops it too.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	RunLimit = time.Minute
	start = time.Now()
	if err := hang.RunLines(ctx, func([]byte) {}, "-i", "x"); err == nil || time.Since(start) > 10*time.Second {
		t.Errorf("a run whose caller left: err %v after %v", err, time.Since(start))
	}
}
