//go:build linux

package ffmpeg

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// Work nobody waits for (measuring a library's loudness) runs at a low priority, so a
// film being converted for somebody watching gets the processors first; an ordinary run
// keeps the server's own.
func TestRunLinesLowGivesWay(t *testing.T) {
	// A stand-in ffmpeg that says how "nice" it is, a moment after it started.
	fake := filepath.Join(t.TempDir(), "ffmpeg")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nsleep 0.2\ncut -d' ' -f19 /proc/$$/stat\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := &FFmpeg{FFmpeg: fake}
	nice := func(run func(context.Context, func([]byte), ...string) error) int {
		t.Helper()
		var got string
		if err := run(context.Background(), func(b []byte) {
			if s := strings.TrimSpace(string(b)); s != "" {
				got = s
			}
		}); err != nil {
			t.Fatal(err)
		}
		n, err := strconv.Atoi(got)
		if err != nil {
			t.Fatalf("the stand-in printed %q", got)
		}
		return n
	}
	own, err := syscall.Getpriority(syscall.PRIO_PROCESS, 0)
	if err != nil {
		t.Fatal(err)
	}
	own = 20 - own // the system call counts from the other end
	if got := nice(f.RunLines); got != own {
		t.Errorf("an ordinary run is at nice %d, want the server's own %d", got, own)
	}
	if got := nice(f.RunLinesLow); got != 10 {
		t.Errorf("a run nobody waits for is at nice %d, want 10", got)
	}
}
