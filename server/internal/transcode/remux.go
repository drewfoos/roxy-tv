package transcode

import (
	"bufio"
	"bytes"
	"math"
	"strconv"
	"strings"
)

// RemuxRequest asks for the file with the video copied untouched and one chosen audio
// track, for players that decode the video themselves but cannot pick an audio track
// or cannot decode this one (a browser: no AC3, E-AC3, DTS or TrueHD, and no track
// switching in MKV). Nothing is re-encoded except an audio track the browser cannot
// play, so the cost is a copy.
type RemuxRequest struct {
	Path     string
	StartSec float64
	// AudioIndex is the ffprobe stream index of the track to carry (-map 0:<index>);
	// -1 keeps ffmpeg's default audio choice.
	AudioIndex int
	Codec      string
	Channels   int
}

// browserAudio lists the codecs every browser decodes; anything else becomes AAC.
var browserAudio = map[string]bool{"aac": true, "opus": true, "flac": true, "vorbis": true, "mp3": true}

// RemuxArgs builds the ffmpeg command line: timestamps restart at zero, the video and the
// chosen audio are mapped, subtitles and fonts are left out (the player fetches them from
// the API), Matroska on stdout. Zero is NOT StartSec: the video is copied, so the stream
// begins at the keyframe ffmpeg's seek lands on, up to a GOP before StartSec (10 s and
// more in some encodes), with the audio starting at StartSec. RemuxStartArgs finds that
// keyframe; the player adds it back, not StartSec, or subtitles run early by the gap.
func RemuxArgs(r RemuxRequest) []string {
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
	if r.StartSec > 0 {
		args = append(args, "-ss", strconv.FormatFloat(r.StartSec, 'f', 3, 64))
	}
	args = append(args, "-i", r.Path, "-map", "0:v:0")
	if r.AudioIndex >= 0 {
		args = append(args, "-map", "0:"+strconv.Itoa(r.AudioIndex))
	} else {
		args = append(args, "-map", "0:a:0?")
	}
	args = append(args, "-c:v", "copy")
	switch {
	case browserAudio[strings.ToLower(r.Codec)]:
		args = append(args, "-c:a", "copy")
	case r.Channels > 2:
		args = append(args, "-c:a", "aac", "-ac", "6", "-b:a", "384k")
	default:
		args = append(args, "-c:a", "aac", "-ac", "2", "-b:a", "160k")
	}
	args = append(args, "-sn", "-dn", "-map_chapters", "-1",
		"-avoid_negative_ts", "make_zero", "-max_muxing_queue_size", "4096",
		"-f", "matroska", "pipe:1")
	return args
}

// RemuxStartArgs asks ffmpeg where a remux of path started at startSec really begins:
// the same input seek as RemuxArgs (ffmpeg's own, which can land a whole GOP early: at
// exactly a keyframe's time it backs off a little and takes the one before), one video
// packet copied with the file's own timestamps, printed by the framecrc muxer.
// ParseRemuxStart reads the answer.
func RemuxStartArgs(path string, startSec float64) []string {
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
	if startSec > 0 {
		args = append(args, "-ss", strconv.FormatFloat(startSec, 'f', 3, 64))
	}
	return append(args, "-i", path, "-map", "0:v:0", "-c", "copy", "-copyts", "-frames:v", "1", "-f", "framecrc", "pipe:1")
}

// ParseRemuxStart reads RemuxStartArgs' output: the first packet's decode time in seconds
// (the remux's -avoid_negative_ts make_zero puts the first packet's dts at zero). False
// when there is no packet line or no time base.
func ParseRemuxStart(out []byte) (float64, bool) {
	num, den := 0.0, 0.0
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if rest, ok := strings.CutPrefix(line, "#tb 0:"); ok {
			n, d, ok := strings.Cut(strings.TrimSpace(rest), "/")
			if !ok {
				return 0, false
			}
			num, _ = strconv.ParseFloat(n, 64)
			den, _ = strconv.ParseFloat(d, 64)
			continue
		}
		if line == "" || line[0] == '#' {
			continue
		}
		f := strings.Split(line, ",")
		if len(f) < 3 || strings.TrimSpace(f[0]) != "0" || num <= 0 || den <= 0 {
			continue
		}
		dts, err := strconv.ParseInt(strings.TrimSpace(f[1]), 10, 64)
		if err != nil {
			return 0, false
		}
		t := float64(dts) * num / den
		if math.IsNaN(t) || math.IsInf(t, 0) {
			return 0, false
		}
		return t, true
	}
	return 0, false
}
