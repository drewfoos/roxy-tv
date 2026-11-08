package transcode

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// The encoder checks hold a bounded part of ffmpeg's output and stop: an ffmpeg that
// prints without end (a broken or odd binary) used to be read whole into memory, and the
// filter list had no time limit at all.
func TestEncoderChecksAreBounded(t *testing.T) {
	fake := filepath.Join(t.TempDir(), "ffmpeg")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexec cat /dev/zero\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := checkTimeout
	checkTimeout = time.Second
	t.Cleanup(func() { checkTimeout = old })
	d := NewDetector(fake, "software", "")
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()
	done := make(chan bool)
	go func() { done <- d.hasFilter("tonemapx") }()
	select {
	case found := <-done:
		if found {
			t.Error("a filter was found in endless zeros")
		}
	case <-time.After(60 * time.Second):
		t.Fatal("the filter check never stopped")
	}
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	if grew := int64(after.HeapAlloc) - int64(before.HeapAlloc); grew > 64<<20 {
		t.Errorf("the filter check held %d MB of output", grew>>20)
	}
	t.Logf("stopped after %v", time.Since(start).Round(time.Second))
	c := &capped{max: 10}
	for i := 0; i < 100; i++ {
		c.Write([]byte("abcdefgh"))
	}
	if len(c.buf) != 10 {
		t.Errorf("capped kept %d bytes, want 10", len(c.buf))
	}
}
