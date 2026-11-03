package ffmpeg

import (
	"reflect"
	"testing"
)

// Every input is opened through local protocols only, whatever else the command says.
func TestLocalInputs(t *testing.T) {
	got := LocalInputs([]string{"-ss", "5", "-i", "/m/a.mkv", "-i", "/m/a.srt", "-c", "copy", "out.mkv"})
	want := []string{"-ss", "5", "-protocol_whitelist", "file,pipe", "-i", "/m/a.mkv", "-protocol_whitelist", "file,pipe", "-i", "/m/a.srt", "-c", "copy", "out.mkv"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}
