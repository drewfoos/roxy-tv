package music

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseLyricsSynced(t *testing.T) {
	got := ParseLyrics("[ar:Joost]\r\n[ti:Song]\r\n[00:12.50]First line\r\n[00:05.1]Before it\r\n[01:02.345][00:20.00]Sung twice\r\n[00:30.00]\r\n", "lrc")
	want := []LyricLine{{5100, "Before it"}, {12500, "First line"}, {20000, "Sung twice"}, {30000, ""}, {62345, "Sung twice"}}
	if !got.Synced || got.Source != "lrc" {
		t.Errorf("synced %v source %q", got.Synced, got.Source)
	}
	if !reflect.DeepEqual(got.Lines, want) {
		t.Errorf("lines:\n got %v\nwant %v", got.Lines, want)
	}
}

// "[offset:+500]" moves every line half a second earlier; a line cannot start before 0.
func TestParseLyricsOffset(t *testing.T) {
	got := ParseLyrics("[offset:+500]\n[00:00.20]a\n[00:02.00]b", "lrc")
	if want := []LyricLine{{0, "a"}, {1500, "b"}}; !reflect.DeepEqual(got.Lines, want) {
		t.Errorf("got %v, want %v", got.Lines, want)
	}
}

// Word-by-word times inside a line are dropped: the line is shown whole.
func TestParseLyricsWordTimes(t *testing.T) {
	got := ParseLyrics("[00:10.00]<00:10.00>Hello <00:10.50>there", "lrc")
	if len(got.Lines) != 1 || got.Lines[0].Text != "Hello there" {
		t.Errorf("got %v", got.Lines)
	}
}

func TestParseLyricsPlain(t *testing.T) {
	got := ParseLyrics("\n\nLine one\nLine two\n\nSecond verse\n\n", "txt")
	if got.Synced {
		t.Error("plain words came back synced")
	}
	want := []LyricLine{{0, "Line one"}, {0, "Line two"}, {0, ""}, {0, "Second verse"}}
	if !reflect.DeepEqual(got.Lines, want) {
		t.Errorf("got %v, want %v", got.Lines, want)
	}
	if empty := ParseLyrics("  \n\n", "txt"); len(empty.Lines) != 0 {
		t.Errorf("blank text gave lines: %v", empty.Lines)
	}
}

func TestSidecarKind(t *testing.T) {
	present := map[string]bool{"song.lrc": true, "song.txt": true, "other.txt": true, "song.mp3": true}
	if k := SidecarKind("Song.mp3", present); k != "lrc" {
		t.Errorf("a synced file beside the song is preferred: %q", k)
	}
	if k := SidecarKind("Other.flac", present); k != "txt" {
		t.Errorf("%q", k)
	}
	if k := SidecarKind("None.mp3", present); k != "" {
		t.Errorf("%q", k)
	}
}

func TestReadLyricsFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, b []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if s, ok := ReadLyricsFile(write("utf8.lrc", []byte("\xEF\xBB\xBF[00:01.00]Fryslân"))); !ok || s != "[00:01.00]Fryslân" {
		t.Errorf("UTF-8 with a mark: %q %v", s, ok)
	}
	if s, ok := ReadLyricsFile(write("utf16.lrc", []byte{0xFF, 0xFE, 'h', 0, 'i', 0})); !ok || s != "hi" {
		t.Errorf("UTF-16: %q %v", s, ok)
	}
	if s, ok := ReadLyricsFile(write("latin1.lrc", []byte{'c', 'a', 'f', 0xE9})); !ok || s != "café" {
		t.Errorf("Latin-1: %q %v", s, ok)
	}
	if _, ok := ReadLyricsFile(write("binary.txt", []byte{0, 1, 2, 0xFF, 0})); ok {
		t.Error("a binary file was read as lyrics")
	}
	if _, ok := ReadLyricsFile(write("huge.txt", make([]byte, maxLyricsFile+1))); ok {
		t.Error("a file far too large was read as lyrics")
	}
	if p, ok := SidecarPath(write("Song.mp3", nil), "lrc"); ok {
		t.Errorf("found a lyrics file that is not there: %s", p)
	}
	write("SONG.LRC", []byte("x"))
	if p, ok := SidecarPath(filepath.Join(dir, "Song.mp3"), "lrc"); !ok || filepath.Base(p) != "SONG.LRC" {
		t.Errorf("a lyrics file named in another case was not found: %q %v", p, ok)
	}
}
