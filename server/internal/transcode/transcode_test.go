package transcode

import (
	"strings"
	"testing"
)

func TestAudioArgs(t *testing.T) {
	tracks := []AudioTrack{
		{Codec: "aac", Channels: 2},                      // compact: copy
		{Codec: "flac", Channels: 6, Bitrate: 2_000_000}, // heavy surround: AAC 5.1
		{Codec: "truehd", Channels: 8},                   // heavy 7.1: AAC 5.1
		{Codec: "ac3", Channels: 6, Bitrate: 640_000},    // compact surround: copy (passthrough)
		{Codec: "dts", Channels: 6, Bitrate: 1_536_000},  // heavy: AAC 5.1
		{Codec: "flac", Channels: 2},                     // heavy stereo: AAC stereo
	}
	got := strings.Join(audioArgs(Request{MaxHeight: 720, BitrateK: 4000, Audio: tracks}), " ")
	want := "-c:a:0 copy " +
		"-c:a:1 aac -ac:a:1 6 -b:a:1 384k " +
		"-c:a:2 aac -ac:a:2 6 -b:a:2 384k " +
		"-c:a:3 copy " +
		"-c:a:4 aac -ac:a:4 6 -b:a:4 384k " +
		"-c:a:5 aac -ac:a:5 2 -b:a:5 160k"
	if got != want {
		t.Errorf("720p:\n got  %s\n want %s", got, want)
	}

	low := strings.Join(audioArgs(Request{MaxHeight: 480, BitrateK: 1500, Audio: tracks[:4]}), " ")
	wantLow := "-c:a:0 copy " +
		"-c:a:1 aac -ac:a:1 2 -b:a:1 128k " +
		"-c:a:2 aac -ac:a:2 2 -b:a:2 128k " +
		"-c:a:3 aac -ac:a:3 2 -b:a:3 128k"
	if low != wantLow {
		t.Errorf("480p:\n got  %s\n want %s", low, wantLow)
	}

	if got := strings.Join(audioArgs(Request{}), " "); got != "-c:a aac -ac 2 -b:a 160k" {
		t.Errorf("no track info: %s", got)
	}
}
