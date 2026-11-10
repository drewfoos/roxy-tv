package transcode

import (
	"strings"
	"testing"
)

// Each subtitle track gets its own choice: copied where Matroska carries it, turned into
// SRT where it is text Matroska cannot carry, left out where it is neither. One choice
// for the whole file asked ffmpeg to make SRT from an MP4's picture subtitles, and the
// stream never started.
func TestSubtitleChoicePerTrack(t *testing.T) {
	d := NewDetector("ffmpeg", string(Software), "")
	args := func(r Request) string {
		r.Path, r.MaxHeight, r.BitrateK = "/m/a", 720, 4000
		return strings.Join(d.Args(r), " ")
	}

	mp4 := args(Request{Container: "mp4", Subtitles: []string{"mov_text", "dvd_subtitle", "mov_text"}})
	if !strings.Contains(mp4, "-c:s:0 srt -c:s:1 copy -c:s:2 srt") {
		t.Errorf("an MP4's text tracks become SRT, its picture track is copied: %s", mp4)
	}
	if strings.Contains(mp4, "-c:s srt") || strings.Contains(mp4, "-map -0:s") {
		t.Errorf("no file-wide choice and nothing left out: %s", mp4)
	}

	// Teletext can be neither carried nor converted: it is left out, and the tracks
	// after it are numbered as the output numbers them.
	ts := args(Request{Container: "mpegts", Subtitles: []string{"dvb_subtitle", "dvb_teletext", "subrip"}})
	if !strings.Contains(ts, "-map -0:s:1 -c:s:0 copy -c:s:1 copy") {
		t.Errorf("the teletext track alone is left out: %s", ts)
	}
	if i, j := strings.Index(ts, "-map 0:s?"), strings.Index(ts, "-map -0:s:1"); i < 0 || j < i {
		t.Errorf("a track is taken out after the tracks are mapped: %s", ts)
	}

	// Whatever a Matroska file holds, Matroska can hold.
	mkv := args(Request{Container: "mkv", Subtitles: []string{"ass", "hdmv_pgs_subtitle", "arib_caption"}})
	if !strings.Contains(mkv, "-c:s:0 copy -c:s:1 copy -c:s:2 copy") || strings.Contains(mkv, "-map -0:s") {
		t.Errorf("a Matroska source keeps every track as it is: %s", mkv)
	}

	// Without the track list the container decides, as before.
	if old := args(Request{Container: "mp4"}); !strings.Contains(old, "-c:s srt") {
		t.Errorf("mp4 without a track list: %s", old)
	}
	if old := args(Request{Container: "mkv"}); !strings.Contains(old, "-c:s copy") {
		t.Errorf("mkv without a track list: %s", old)
	}
}
