package transcode

import (
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

// RemuxArgs builds the ffmpeg command line: timestamps restart at zero at StartSec (the
// player adds the offset back), the video and the chosen audio are mapped, subtitles
// and fonts are left out (the player fetches them from the API), Matroska on stdout.
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
