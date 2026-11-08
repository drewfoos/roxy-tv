package ffmpeg

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeBin writes an executable shell script and returns its path.
func fakeBin(t *testing.T, script string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "fake")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// A run whose caller set no deadline is stopped after RunLimit (ProbeLimit for ffprobe):
// a file that makes ffmpeg hang used to keep it, and whatever waited on it, running for
// good. A caller's own deadline is left as it is.
func TestRunsWithoutADeadlineAreStopped(t *testing.T) {
	oldR, oldP := RunLimit, ProbeLimit
	RunLimit, ProbeLimit = 300*time.Millisecond, 300*time.Millisecond
	t.Cleanup(func() { RunLimit, ProbeLimit = oldR, oldP })
	hang := fakeBin(t, "exec sleep 60")
	f := &FFmpeg{FFmpeg: hang, FFprobe: hang}
	for name, run := range map[string]func() error{
		"Run":         func() error { _, err := f.Run(context.Background(), "", "-i", "x"); return err },
		"RunCombined": func() error { _, err := f.RunCombined(context.Background(), "-i", "x"); return err },
		"Probe":       func() error { _, err := f.Probe(context.Background(), "-i", "x"); return err },
	} {
		start := time.Now()
		err := run()
		if took := time.Since(start); err == nil || took > 10*time.Second {
			t.Errorf("%s of a hanging process: err %v after %v", name, err, took)
		}
	}
	// A caller's shorter deadline still applies; so does a longer one (not cut to the limit).
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	if _, err := f.Run(ctx, "", "-i", "x"); err == nil || time.Since(start) < time.Second {
		t.Errorf("a caller's 2 s deadline: err %v after %v (the 300 ms default must not replace it)", err, time.Since(start))
	}
}

// Error output is kept to its end: a damaged file can make ffmpeg report an error per
// frame for hours, and all of it used to be held in memory.
func TestErrorOutputIsBounded(t *testing.T) {
	noisy := fakeBin(t, `i=0; while [ $i -lt 4000 ]; do echo "frame $i: error decoding, a fairly long line of noise" >&2; i=$((i+1)); done; echo "the last word" >&2; exit 1`)
	f := &FFmpeg{FFmpeg: noisy}
	_, err := f.Run(context.Background(), "", "-i", "x")
	if err == nil || !strings.Contains(err.Error(), "the last word") || len(err.Error()) > 2200 {
		t.Fatalf("error from a noisy run: %d bytes, last line kept %v", len(err.Error()), err != nil && strings.Contains(err.Error(), "the last word"))
	}
	tw := &tailWriter{max: 100}
	for i := 0; i < 1000; i++ {
		tw.Write([]byte("0123456789"))
	}
	if len(tw.buf) != 100 {
		t.Fatalf("tailWriter kept %d bytes, want 100", len(tw.buf))
	}
}

// RunCombined's callers parse the whole output (silencedetect), so past MaxCombined it
// stops ffmpeg and fails instead of keeping a part. It used to collect everything: a run
// printing without end grew without bound. A normal report comes back whole.
func TestRunCombinedOutputIsCapped(t *testing.T) {
	old := MaxCombined
	MaxCombined = 64 << 10
	t.Cleanup(func() { MaxCombined = old })
	endless := &FFmpeg{FFmpeg: fakeBin(t, "exec yes 'silence_start: 1.0'")}
	start := time.Now()
	out, err := endless.RunCombined(context.Background(), "-i", "x")
	if !errors.Is(err, ErrOutputTooLarge) || out != nil {
		t.Fatalf("endless output: %d bytes, err %v", len(out), err)
	}
	if took := time.Since(start); took > 10*time.Second {
		t.Fatalf("ffmpeg was not stopped at the cap (took %v)", took)
	}
	report := &FFmpeg{FFmpeg: fakeBin(t, `echo "[silencedetect] silence_start: 1.5" >&2; echo "[silencedetect] silence_end: 2.25" >&2`)}
	out, err = report.RunCombined(context.Background(), "-i", "x")
	if err != nil || !strings.Contains(string(out), "silence_start: 1.5") || !strings.Contains(string(out), "silence_end: 2.25") {
		t.Fatalf("a normal report: %q %v", out, err)
	}
}
