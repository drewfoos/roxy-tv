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
	return runBin(ctx, f.FFmpeg, dir, LocalInputs(args)...)
}

// localProtocols: what an input may be opened through. A media file is opened from disk
// (pipe for stdin). A crafted file that is really a playlist or concat script must not
// reach http/tcp/subfile and friends: the bundled jellyfin-ffmpeg already refuses the
// known tricks, but a different ffmpeg (ROXY_FFMPEG_PATH) might not.
const localProtocols = "file,pipe"

// LocalInputs puts -protocol_whitelist file,pipe before every -i in an ffmpeg command.
func LocalInputs(args []string) []string {
	out := make([]string, 0, len(args)+4)
	for _, a := range args {
		if a == "-i" {
			out = append(out, "-protocol_whitelist", localProtocols)
		}
		out = append(out, a)
	}
	return out
}

// RunCombined executes ffmpeg and returns stdout and stderr together (for filters such
// as silencedetect that report on stderr).
func (f *FFmpeg) RunCombined(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, f.FFmpeg, LocalInputs(args)...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return buf.Bytes(), fmt.Errorf("ffmpeg: %w: %s", err, tail(buf.String(), 2000))
	}
	return buf.Bytes(), nil
}

// Probe executes ffprobe with args and returns stdout.
func (f *FFmpeg) Probe(ctx context.Context, args ...string) ([]byte, error) {
	// ffprobe takes one input; the option applies to it wherever it stands.
	return runBin(ctx, f.FFprobe, "", append([]string{"-protocol_whitelist", localProtocols}, args...)...)
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
