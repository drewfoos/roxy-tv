package transcode

import (
	"strings"
	"testing"
)

// A web transcode carries one audio track in a browser codec, no subtitles, and a clock
// that restarts at the start point; the normal path keeps every track and the file's clock.
func TestWebArgs(t *testing.T) {
	d := NewDetector("ffmpeg", string(Software), "")
	web := strings.Join(d.Args(Request{Path: "/m/a.mkv", StartSec: 30, MaxHeight: 720, BitrateK: 4000, SrcHeight: 1080, Web: true, WebAudio: &WebAudioTrack{Index: 2, Codec: "eac3", Channels: 6}}), " ")
	for _, want := range []string{"-ss 30.000 -i /m/a.mkv -map 0:v:0 -map 0:2", "-c:a aac -ac 6 -b:a 384k", "-sn -dn", "-avoid_negative_ts make_zero", "-f matroska pipe:1"} {
		if !strings.Contains(web, want) {
			t.Errorf("missing %q in %s", want, web)
		}
	}
	for _, bad := range []string{"-copyts", "-map 0:s?", "-map 0:t?", "-c:s"} {
		if strings.Contains(web, bad) {
			t.Errorf("web stream must not carry %q: %s", bad, web)
		}
	}
	opus := strings.Join(d.Args(Request{Path: "/m/a.mkv", MaxHeight: 1080, BitrateK: 8000, Web: true, WebAudio: &WebAudioTrack{Index: 1, Codec: "opus", Channels: 2}}), " ")
	if !strings.Contains(opus, "-c:a copy") {
		t.Errorf("a browser codec is copied at normal quality: %s", opus)
	}
	low := strings.Join(d.Args(Request{Path: "/m/a.mkv", MaxHeight: 480, BitrateK: 1500, Web: true, WebAudio: &WebAudioTrack{Index: 1, Codec: "opus", Channels: 2}}), " ")
	if !strings.Contains(low, "-c:a aac -ac 2 -b:a 160k") {
		t.Errorf("480p-class streams get stereo AAC: %s", low)
	}
	normal := strings.Join(d.Args(Request{Path: "/m/a.mkv", MaxHeight: 720, BitrateK: 4000}), " ")
	if !strings.Contains(normal, "-copyts") || !strings.Contains(normal, "-map 0:s?") {
		t.Errorf("the mpv path is unchanged: %s", normal)
	}
}
