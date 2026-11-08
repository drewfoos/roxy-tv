package probe

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/roxy-media/roxy/server/internal/ffmpeg"
)

// Subtitle extractions run a few at a time across all files (each reads its whole
// video). With every slot taken, another waits instead of starting ffmpeg, and gives up
// when its request ends.
func TestSubtitleExtractionsAreBounded(t *testing.T) {
	ran := filepath.Join(t.TempDir(), "ran")
	fake := filepath.Join(t.TempDir(), "ffmpeg")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\ntouch '"+ran+"'\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < cap(extractSlots); i++ {
		extractSlots <- struct{}{}
	}
	defer func() {
		for len(extractSlots) > 0 {
			<-extractSlots
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err := EnsureTextSubtitle(ctx, &ffmpeg.FFmpeg{FFmpeg: fake}, "/nowhere/video.mkv", 2, "subrip", filepath.Join(t.TempDir(), "2.srt"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("with every slot taken: %v, want the request's deadline", err)
	}
	if _, err := os.Stat(ran); err == nil {
		t.Fatal("ffmpeg was started past the slot count")
	}
}
