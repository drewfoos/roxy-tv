package transcode

import (
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/roxy-media/roxy/server/internal/ffmpeg"
)

func TestRemuxArgs(t *testing.T) {
	got := strings.Join(RemuxArgs(RemuxRequest{Path: "/m/a.mkv", StartSec: 90.5, AudioIndex: 2, Codec: "eac3", Channels: 6}), " ")
	for _, want := range []string{"-ss 90.500 -i /m/a.mkv", "-map 0:v:0 -map 0:2", "-c:v copy", "-c:a aac -ac 6 -b:a 384k", "-sn -dn", "-f matroska pipe:1"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
	if strings.Contains(got, "-copyts") {
		t.Error("a remux for the browser must restart timestamps at zero")
	}

	copyArgs := strings.Join(RemuxArgs(RemuxRequest{Path: "/m/a.mkv", AudioIndex: 1, Codec: "aac", Channels: 2}), " ")
	if !strings.Contains(copyArgs, "-c:a copy") || strings.Contains(copyArgs, "-ss") {
		t.Errorf("AAC should be copied with no seek: %s", copyArgs)
	}

	def := strings.Join(RemuxArgs(RemuxRequest{Path: "/m/a.mkv", AudioIndex: -1, Codec: "truehd", Channels: 8}), " ")
	if !strings.Contains(def, "-map 0:a:0?") || !strings.Contains(def, "-c:a aac -ac 6") {
		t.Errorf("default track, TrueHD to AAC 5.1: %s", def)
	}
}

func TestParseRemuxStart(t *testing.T) {
	// What ffmpeg 7 prints for one copied H.264 packet (RemuxStartArgs).
	out := []byte("#extradata 0:       37, 0xd4690a6d\n#software: Lavf61.7.100\n#tb 0: 1/1000\n#media_type 0: video\n#codec_id 0: h264\n#dimensions 0: 320x240\n#sar 0: 1/1\n0,     590000,     590083,       41,    11264, 0x98f6fedb\n")
	if got, ok := ParseRemuxStart(out); !ok || got != 590 {
		t.Errorf("got %v %v, want 590 (the dts, not the pts)", got, ok)
	}
	if got, ok := ParseRemuxStart([]byte("#tb 0: 1/90000\n0, 900000, 900000, 3750, 10, 0x0\n")); !ok || got != 10 {
		t.Errorf("90 kHz: got %v %v", got, ok)
	}
	for _, bad := range []string{"", "#tb 0: 1/1000\n", "0, 5000, 5000, 41, 1, 0x0\n", "#tb 0: 1/1000\n0, x, 1, 1, 1, 0\n"} {
		if got, ok := ParseRemuxStart([]byte(bad)); ok {
			t.Errorf("%q: got %v, want no answer", bad, got)
		}
	}
	args := strings.Join(RemuxStartArgs("/m/a.mkv", 605), " ")
	if !strings.Contains(args, "-ss 605.000 -i /m/a.mkv") || !strings.Contains(args, "-copyts") || !strings.Contains(args, "-f framecrc") {
		t.Errorf("start args: %s", args)
	}
}

// TestRemuxStartsAtTheKeyframe runs the real remux of a long-GOP file with AC3 audio (the
// case the browser cannot play directly) and checks where its zero is: the keyframe
// RemuxStartArgs reports, with the audio (cut at the start point) that far in. The player
// used to take zero for the start point, so subtitles ran up to a GOP early.
func TestRemuxStartsAtTheKeyframe(t *testing.T) {
	ff, err := ffmpeg.New("", "")
	if err == nil {
		_, err = exec.LookPath(ff.FFprobe)
	}
	if err != nil {
		if os.Getenv("ROXY_REQUIRE_INTEGRATION") != "" {
			t.Fatalf("ROXY_REQUIRE_INTEGRATION is set but ffmpeg is not usable: %v", err)
		}
		t.Skip("needs ffmpeg and ffprobe")
	}
	ctx := context.Background()
	dir := t.TempDir()
	in := filepath.Join(dir, "in.mkv")
	// Keyframes every 10 s (240 frames at 24 fps), B-frames on, AC3 audio.
	if out, err := exec.Command(ff.FFmpeg, "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc2=size=160x120:rate=24",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000", "-t", "80",
		"-c:v", "libx264", "-preset", "veryfast", "-g", "240", "-keyint_min", "240", "-sc_threshold", "0",
		"-c:a", "ac3", "-b:a", "96k", in).CombinedOutput(); err != nil {
		t.Fatalf("making the video: %v %s", err, out)
	}
	for _, c := range []struct{ start, lo, hi float64 }{
		{65, 59.9, 60.1}, // mid-GOP: the keyframe at 60
		{60, 49.9, 60.1}, // at a keyframe's time ffmpeg may back off to the one before
		{0, 0, 0},
	} {
		out, err := ff.Run(ctx, "", RemuxStartArgs(in, c.start)...)
		k, ok := ParseRemuxStart(out)
		if c.start == 0 {
			k, ok = 0, true // the handler does not ask: zero is the file's start
		}
		if err != nil || !ok || k < c.lo || k > c.hi {
			t.Fatalf("start %v: keyframe %v %v %v %s", c.start, k, ok, err, out)
		}
		remux := filepath.Join(dir, "remux.mkv")
		args := RemuxArgs(RemuxRequest{Path: in, StartSec: c.start, AudioIndex: 1, Codec: "ac3", Channels: 1})
		args[len(args)-1] = remux
		args = append(args[:len(args)-1], "-t", "15", "-y", remux)
		if _, err := ff.Run(ctx, "", args...); err != nil {
			t.Fatal(err)
		}
		// The audio is cut at the start point: its first packet sits (start - zero) in.
		probe, err := ff.Probe(ctx, "-v", "error", "-select_streams", "a:0", "-show_entries", "packet=pts_time", "-read_intervals", "%+#1", "-of", "csv=p=0", remux)
		if err != nil {
			t.Fatal(err)
		}
		first, err := strconv.ParseFloat(strings.TrimSpace(strings.SplitN(string(probe), "\n", 2)[0]), 64)
		if err != nil {
			t.Fatalf("audio pts %q: %v", probe, err)
		}
		if want := c.start - k; math.Abs(first-want) > 0.1 {
			t.Errorf("start %v: the remux's audio begins at %.3f, want %.3f (start - keyframe %.3f)", c.start, first, want, k)
		}
	}
}
