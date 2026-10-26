package transcode

import (
	"strings"
	"testing"
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
