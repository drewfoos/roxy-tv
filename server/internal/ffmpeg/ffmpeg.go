// Package ffmpeg locates and runs the jellyfin-ffmpeg binaries. Roxy never links libav;
// everything goes through subprocesses (ADR-0002).
package ffmpeg

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type FFmpeg struct {
	FFmpeg  string
	FFprobe string
	Version string
}

// New resolves the binaries: explicit paths, then PATH, then well-known install dirs.
func New(ffmpegPath, ffprobePath string) (*FFmpeg, error) {
	ff := &FFmpeg{}
	var err error
	if ff.FFmpeg, err = resolve(ffmpegPath, "ffmpeg"); err != nil {
		return nil, err
	}
	if ff.FFprobe, err = resolve(ffprobePath, "ffprobe"); err != nil {
		return nil, err
	}
	out, err := exec.Command(ff.FFmpeg, "-version").Output()
	if err != nil {
		return nil, fmt.Errorf("running %s -version: %w", ff.FFmpeg, err)
	}
	first := strings.SplitN(string(out), "\n", 2)[0]
	ff.Version = strings.TrimSpace(strings.TrimPrefix(first, "ffmpeg version "))
	return ff, nil
}

func resolve(explicit, name string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(explicit); err != nil {
			return "", fmt.Errorf("%s not found at %s", name, explicit)
		}
		return explicit, nil
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	exe := name
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	home, _ := os.UserHomeDir()
	candidates := []string{
		"/usr/lib/jellyfin-ffmpeg/" + name,
		filepath.Join(home, "tools", "jellyfin-ffmpeg", exe),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", fmt.Errorf("%s not found in PATH or known locations; set ROXY_%s_PATH", name, strings.ToUpper(name))
}

// Run executes ffmpeg with args and returns stdout; stderr is included in the error.
func (f *FFmpeg) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return runBin(ctx, f.FFmpeg, dir, args...)
}

// Probe executes ffprobe with args and returns stdout.
func (f *FFmpeg) Probe(ctx context.Context, args ...string) ([]byte, error) {
	return runBin(ctx, f.FFprobe, "", args...)
}

func runBin(ctx context.Context, bin, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return stdout.Bytes(), fmt.Errorf("%s exited %d: %s", filepath.Base(bin), exitErr.ExitCode(), tail(stderr.String(), 2000))
		}
		return stdout.Bytes(), fmt.Errorf("%s: %w: %s", filepath.Base(bin), err, tail(stderr.String(), 2000))
	}
	return stdout.Bytes(), nil
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return "..." + s[len(s)-n:]
	}
	return s
}
