package music

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// What ffmpeg prints when ebur128 is done (the end of a real run).
const loudSummary = `[Parsed_ebur128_0 @ 0x70c7680036c0] Summary:

  Integrated loudness:
    I:         -15.1 LUFS
    Threshold: -25.3 LUFS

  Loudness range:
    LRA:         7.7 LU
    Threshold: -35.2 LUFS
    LRA low:   -20.8 LUFS
    LRA high:  -13.1 LUFS

  True peak:
    Peak:        0.3 dBFS
[out#0/null @ 0x5c3b12c75e40] video:0KiB audio:26703KiB subtitle:0KiB other streams:0KiB
`

func TestParseLoudness(t *testing.T) {
	l, err := parseLoudness([]byte(loudSummary))
	if err != nil || l.LUFS != -15.1 || l.Peak != 0.3 {
		t.Errorf("%+v %v", l, err)
	}
	// Digital silence: as quiet as it gets, not an error and not a number to divide by.
	l, err = parseLoudness([]byte("    I:         -inf LUFS\n    Peak:       -inf dBFS\n"))
	if err != nil || l.LUFS != silentLUFS || l.Peak != silentLUFS {
		t.Errorf("silence: %+v %v", l, err)
	}
	// The summary is the last of it: a line of the same shape earlier on is not it.
	l, _ = parseLoudness([]byte("    I:         -30.0 LUFS\n" + loudSummary))
	if l.LUFS != -15.1 {
		t.Errorf("an earlier line was taken for the summary: %v", l.LUFS)
	}
	if _, err := parseLoudness([]byte("Error opening input: Invalid data found")); err == nil {
		t.Error("output with no measurement was read as one")
	}
}

func TestAlbumLoudness(t *testing.T) {
	// Two songs as loud as each other: the album is as loud as they are.
	if l, ok := AlbumLoudness([]float64{-10, -10}, []int64{200000, 100000}); !ok || l != -10 {
		t.Errorf("%v %v", l, ok)
	}
	// A loud song and a quiet one of the same length: nearer the loud one (energy, not decibels, is averaged).
	if l, ok := AlbumLoudness([]float64{-8, -20}, []int64{180000, 180000}); !ok || l < -11 || l > -10.5 {
		t.Errorf("%v %v", l, ok)
	}
	// A long quiet song weighs more than a short loud one.
	short, _ := AlbumLoudness([]float64{-8, -20}, []int64{10000, 600000})
	if short > -17 {
		t.Errorf("a ten-second song set an album's loudness: %v", short)
	}
	if _, ok := AlbumLoudness(nil, nil); ok {
		t.Error("no songs gave a loudness")
	}
	if _, ok := AlbumLoudness([]float64{silentLUFS}, []int64{1000}); ok {
		t.Error("silence gave a loudness")
	}
}

func loud(lufs, peak string) []byte {
	return []byte("  Integrated loudness:\n    I:         " + lufs + " LUFS\n  True peak:\n    Peak:        " + peak + " dBFS\n")
}

// Every song is measured once; a changed file is measured again; an album has a
// loudness when all its songs do; a file that cannot be measured is given up on.
func TestLoudnessIsMeasured(t *testing.T) {
	l := newTestLib(t)
	one := l.put("A/Album/one.mp3", song{tags: tags("title", "One", "artist", "A", "album", "Album")})
	two := l.put("A/Album/two.mp3", song{tags: tags("title", "Two", "artist", "A", "album", "Album")})
	l.put("A/bad.mp3", song{tags: tags("title", "Bad", "artist", "A")}) // no .loud: ffmpeg fails on it
	os.WriteFile(one+".loud", loud("-9.0", "0.5"), 0o644)
	l.scan(false)

	n, err := l.svc.MeasureLoudness(l.ctx)
	if err != nil || n != 1 {
		t.Fatalf("first pass measured %d: %v", n, err)
	}
	if got := l.track("One"); got.Loudness == nil || *got.Loudness != -9 || got.Peak == nil || *got.Peak != 0.5 {
		t.Errorf("One: %+v %+v", got.Loudness, got.Peak)
	}
	if got := l.track("Two"); got.Loudness != nil || got.AlbumLoudness != nil {
		t.Errorf("a song that could not be measured has a loudness, or its album has: %v %v", got.Loudness, got.AlbumLoudness)
	}
	// The second song can be measured now: the album gets its loudness.
	os.WriteFile(two+".loud", loud("-9.0", "-1.0"), 0o644)
	if n, _ = l.svc.MeasureLoudness(l.ctx); n != 1 {
		t.Fatalf("second pass measured %d", n)
	}
	if got := l.track("Two"); got.Loudness == nil || got.AlbumLoudness == nil || *got.AlbumLoudness != -9 {
		t.Errorf("Two and its album: %v %v", got.Loudness, got.AlbumLoudness)
	}
	// Nothing left that can be measured: the file that fails was tried three times in all, then left.
	l.svc.MeasureLoudness(l.ctx)
	calls := func() int {
		b, _ := os.ReadFile(filepath.Join(filepath.Dir(l.root), "ffmpeg.calls"))
		return strings.Count(string(b), "\n")
	}
	before := calls()
	if n, _ = l.svc.MeasureLoudness(l.ctx); n != 0 || calls() != before {
		t.Errorf("a pass with nothing to do measured %d and ran ffmpeg %d times", n, calls()-before)
	}
	measured, total, _ := l.st.MusicLoudnessCounts(l.ctx)
	if measured != 2 || total != 3 {
		t.Errorf("measured %d of %d", measured, total)
	}

	// A file that changed is measured again (the old figure stays until then), and one
	// that failed gets its tries back with its new contents.
	l.put("A/Album/one.mp3", song{tags: tags("title", "One", "artist", "A", "album", "Album")})
	os.WriteFile(one+".loud", loud("-14.5", "-0.2"), 0o644)
	later := time.Now().Add(time.Hour)
	os.Chtimes(one, later, later)
	l.scan(false)
	if got := l.track("One"); got.Loudness == nil || *got.Loudness != -9 {
		t.Errorf("before it is measured again the old figure stands: %v", got.Loudness)
	}
	if n, _ = l.svc.MeasureLoudness(l.ctx); n != 1 {
		t.Fatalf("after a change measured %d", n)
	}
	if got := l.track("One"); *got.Loudness != -14.5 || got.AlbumLoudness == nil || *got.AlbumLoudness >= -9 {
		t.Errorf("after the change: %v, album %v", *got.Loudness, got.AlbumLoudness)
	}
	// The file that was given up on is replaced by one that can be measured: it is tried again.
	bad := l.put("A/bad.mp3", song{tags: tags("title", "Bad", "artist", "A")})
	os.WriteFile(bad+".loud", loud("-11.0", "-1.0"), 0o644)
	os.Chtimes(bad, later, later)
	l.scan(false)
	if n, _ = l.svc.MeasureLoudness(l.ctx); n != 1 {
		t.Fatalf("a replaced file that had been given up on: measured %d", n)
	}
	if got := l.track("Bad"); got.Loudness == nil || *got.Loudness != -11 {
		t.Errorf("the replaced file: %v", got.Loudness)
	}
	if measured, total, _ = l.st.MusicLoudnessCounts(l.ctx); measured != 3 || total != 3 {
		t.Errorf("in the end measured %d of %d", measured, total)
	}
}

func TestParseAudioFormat(t *testing.T) {
	for _, c := range []struct {
		format, bitrate string
		want            AudioFormat
		ok              bool
	}{
		{"opus", "", AudioFormat{"opus", 128}, true},
		{"OPUS", "130", AudioFormat{"opus", 128}, true}, // snapped to a step
		{"opus", "999", AudioFormat{"opus", 256}, true},
		{"mp3", "", AudioFormat{"mp3", 192}, true},
		{"mp3", "320", AudioFormat{"mp3", 320}, true},
		{"aac", "1", AudioFormat{"aac", 64}, true},
		{"flac", "320", AudioFormat{"flac", 0}, true},
		{"wav", "", AudioFormat{}, false},
		{"", "", AudioFormat{}, false},
		{"opus", "fast", AudioFormat{}, false},
		{"mp3", "-5", AudioFormat{}, false},
	} {
		got, err := ParseAudioFormat(c.format, c.bitrate)
		if (err == nil) != c.ok || (c.ok && got != c.want) {
			t.Errorf("ParseAudioFormat(%q, %q) = %+v, %v; want %+v", c.format, c.bitrate, got, err, c.want)
		}
	}
}

func TestConversionCommand(t *testing.T) {
	has := func(args []string, want ...string) bool {
		return strings.Contains(" "+strings.Join(args, " ")+" ", " "+strings.Join(want, " ")+" ")
	}
	opus := AudioFormat{"opus", 128}.args("/in.flac", "/out.ogg", 96000, 2)
	if !has(opus, "-i", "/in.flac") || !has(opus, "-map", "0:a:0") || !has(opus, "-c:a", "libopus", "-b:a", "128k") || opus[len(opus)-1] != "/out.ogg" || has(opus, "-ac", "2") {
		t.Errorf("opus: %v", opus)
	}
	// MP3 and AAC cannot carry 96 kHz; six channels become two.
	mp3 := AudioFormat{"mp3", 320}.args("/in.flac", "/o.mp3", 96000, 6)
	if !has(mp3, "-ar", "48000") || !has(mp3, "-ac", "2") || !has(mp3, "-b:a", "320k") {
		t.Errorf("mp3: %v", mp3)
	}
	if a := (AudioFormat{"mp3", 192}).args("/in", "/o", 44100, 2); has(a, "-ar", "48000") || has(a, "-ar", "44100") {
		t.Errorf("a rate MP3 can carry was changed: %v", a)
	}
	// FLAC comes down to CD-like: 16 bits, and a rate from the file's own family.
	for rate, want := range map[int]string{96000: "48000", 192000: "48000", 88200: "44100", 176400: "44100"} {
		if a := (AudioFormat{Codec: "flac"}).args("/in", "/o", rate, 2); !has(a, "-ar", want) || !has(a, "-sample_fmt", "s16") {
			t.Errorf("flac from %d Hz: %v", rate, a)
		}
	}
	if a := (AudioFormat{Codec: "flac"}).args("/in", "/o", 44100, 2); has(a, "-ar", "44100") || has(a, "-ar", "48000") {
		t.Errorf("a CD-rate file was resampled: %v", a)
	}
}

// Several requests for the same song in the same format are one conversion; the result
// is kept; the cache stays within its size, the files longest unused going first.
func TestTranscoderKeepsAndTrims(t *testing.T) {
	l := newTestLib(t)
	dir := filepath.Join(filepath.Dir(l.root), "data", "cache", "music", "converted")
	tc := NewTranscoder(l.svc.ff, dir, 1<<20)
	src := func(name string) Source {
		p := l.put(name, song{tags: tags("title", name, "artist", "A")})
		return Source{TrackID: name, Path: p, Size: 10, Mtime: 1, SampleRate: 44100, Channels: 2}
	}
	a := src("a.mp3")
	var wg sync.WaitGroup
	paths := make([]string, 8)
	for i := range paths {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p, err := tc.File(context.Background(), a, AudioFormat{"opus", 128})
			if err != nil {
				t.Error(err)
			}
			paths[i] = p
		}(i)
	}
	wg.Wait()
	calls := func() int {
		b, _ := os.ReadFile(filepath.Join(filepath.Dir(l.root), "ffmpeg.calls"))
		return strings.Count(string(b), "converted")
	}
	if calls() != 1 {
		t.Errorf("eight requests made %d conversions", calls())
	}
	for _, p := range paths {
		if p != paths[0] {
			t.Errorf("the requests got different files: %v", paths)
		}
	}
	if b, _ := os.ReadFile(paths[0]); !strings.HasPrefix(string(b), "converted ") {
		t.Errorf("the kept file: %q", b)
	}
	// A changed source file (another time) is another version.
	changed := a
	changed.Mtime = 2
	p2, _ := tc.File(context.Background(), changed, AudioFormat{"opus", 128})
	if p2 == paths[0] || calls() != 2 {
		t.Errorf("a changed file was served from the old conversion: %s, %d conversions", p2, calls())
	}
	// A conversion that fails keeps nothing, and the next request tries again.
	bad := src("bad.mp3")
	os.WriteFile(bad.Path+".noconvert", nil, 0o644)
	if _, err := tc.File(context.Background(), bad, AudioFormat{"mp3", 192}); err == nil {
		t.Error("a failed conversion gave a file")
	}
	left, _ := filepath.Glob(filepath.Join(dir, "*", "*.part"))
	if len(left) != 0 {
		t.Errorf("half-made files left: %v", left)
	}
	os.Remove(bad.Path + ".noconvert")
	if _, err := tc.File(context.Background(), bad, AudioFormat{"mp3", 192}); err != nil {
		t.Errorf("the retry: %v", err)
	}

	// Over its size: the files longest unused go, the newest stays.
	small := NewTranscoder(l.svc.ff, dir, 60)
	old := time.Now().Add(-48 * time.Hour)
	os.Chtimes(paths[0], old, old)
	newest, err := small.File(context.Background(), src("c.mp3"), AudioFormat{"opus", 128})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths[0]); !os.IsNotExist(err) {
		t.Error("the file longest unused is still there with the cache over its size")
	}
	if _, err := os.Stat(newest); err != nil {
		t.Errorf("the file just made was removed: %v", err)
	}
}
