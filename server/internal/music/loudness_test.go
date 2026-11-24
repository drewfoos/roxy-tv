package music

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/roxy-media/roxy/server/internal/model"
)

// What ffmpeg prints while and when ebur128 is done (a real run, cut short): a line for
// every 100 ms with the loudness of the 400 ms block ending there (the first three have no
// whole block yet), then the summary.
const loudRun = `[Parsed_ebur128_0 @ 0x7c5e04003640] t: 0.0999792  TARGET:-23 LUFS    M:-120.7 S:-120.7     I: -70.0 LUFS       LRA:   0.0 LU  FTPK:  -inf  -inf dBFS  TPK:  -inf  -inf dBFS
[Parsed_ebur128_0 @ 0x7c5e04003640] t: 0.199979   TARGET:-23 LUFS    M:-120.7 S:-120.7     I: -70.0 LUFS       LRA:   0.0 LU  FTPK: -83.1 -82.9 dBFS  TPK: -83.1 -82.9 dBFS
[Parsed_ebur128_0 @ 0x7c5e04003640] t: 0.299979   TARGET:-23 LUFS    M:-120.7 S:-120.7     I: -70.0 LUFS       LRA:   0.0 LU  FTPK:  -3.8  -7.1 dBFS  TPK:  -3.8  -7.1 dBFS
[Parsed_ebur128_0 @ 0x7c5e04003640] t: 0.399979   TARGET:-23 LUFS    M: -16.3 S:-120.7     I: -16.3 LUFS       LRA:   0.0 LU  FTPK:  -5.3  -5.7 dBFS  TPK:  -3.8  -5.7 dBFS
[Parsed_ebur128_0 @ 0x7c5e04003640] t: 0.499979   TARGET:-23 LUFS    M: -14.0 S:-120.7     I: -15.0 LUFS       LRA:   0.0 LU  FTPK:  -4.9  -6.0 dBFS  TPK:  -3.8  -5.7 dBFS
[Parsed_ebur128_0 @ 0x7c5e04003640] t: 0.599979   TARGET:-23 LUFS    M: -12.8 S:-120.7     I: -14.1 LUFS       LRA:   0.0 LU  FTPK:  -6.8  -5.5 dBFS  TPK:  -3.8  -5.5 dBFS
[Parsed_ebur128_0 @ 0x7c5e04003640] t: 0.699979   TARGET:-23 LUFS    M: -12.8 S:-120.7     I: -13.6 LUFS       LRA:   0.0 LU  FTPK:  -4.4  -6.0 dBFS  TPK:  -3.8  -5.5 dBFS
[Parsed_ebur128_0 @ 0x7c5e04003640] Summary:

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
	l, err := parseLoudness([]byte(loudRun))
	if err != nil || l.LUFS != -15.1 || l.Peak != 0.3 {
		t.Errorf("%+v %v", l, err)
	}
	// Four blocks: the three lines before a whole block had been heard are not blocks, and
	// neither is the running "I:" on every line the song's loudness.
	if l.Blocks.Total() != 4 || l.Blocks.First != 537 || len(l.Blocks.Counts) != 36 || l.Blocks.Counts[35] != 2 {
		t.Errorf("blocks: %+v", l.Blocks)
	}
	// Digital silence: as quiet as it gets, not an error and not a number to divide by.
	l, err = parseLoudness([]byte("x] Summary:\n    I:         -inf LUFS\n    Peak:       -inf dBFS\n"))
	if err != nil || l.LUFS != silentLUFS || l.Peak != silentLUFS || l.Blocks.Total() != 0 {
		t.Errorf("silence: %+v %v", l, err)
	}
	// Only the summary gives the song's figure: a line of the same shape before it does not.
	if _, err := parseLoudness([]byte("    I:         -30.0 LUFS\n")); err == nil {
		t.Error("a line before the summary was taken for it")
	}
	if _, err := parseLoudness([]byte("Error opening input: Invalid data found")); err == nil {
		t.Error("output with no measurement was read as one")
	}
}

func blocksOf(levels map[float64]int) Blocks {
	var b Blocks
	for m, n := range levels {
		for i := 0; i < n; i++ {
			b.Add(m)
		}
	}
	return b
}

// The loudness of blocks together, gated as the standard says.
func TestBlocksIntegrated(t *testing.T) {
	// Every block as loud as the next: that loudness.
	if l, ok := blocksOf(map[float64]int{-10: 500}).Integrated(); !ok || l != -10 {
		t.Errorf("even blocks: %v %v", l, ok)
	}
	// Loud and a little less loud, half and half: energy is averaged, not decibels.
	if l, _ := blocksOf(map[float64]int{-8: 100, -14: 100}).Integrated(); l != -10 {
		t.Errorf("-8 and -14: %v, want -10.0", l)
	}
	// A quiet half more than 10 LU under the whole is left out: the level is the loud
	// half's, not pulled down by three decibels as a mean over all of it would be.
	if l, _ := blocksOf(map[float64]int{-10: 300, -30: 300}).Integrated(); l != -10 {
		t.Errorf("a loud half and a quiet half: %v, want -10.0", l)
	}
	// Within 10 LU of the whole it counts.
	if l, _ := blocksOf(map[float64]int{-10: 300, -18: 300}).Integrated(); l != -12.4 {
		t.Errorf("-10 and -18: %v, want -12.4", l)
	}
	// Nothing, and silence under the absolute gate, have no loudness.
	if _, ok := (Blocks{}).Integrated(); ok {
		t.Error("no blocks gave a loudness")
	}
	var quiet Blocks
	quiet.Add(-90)
	quiet.Add(-120.7)
	if quiet.Total() != 0 {
		t.Errorf("blocks under -70 LUFS were kept: %+v", quiet)
	}
	// Louder than can be is kept at the top step, not dropped or out of bounds.
	var loudest Blocks
	loudest.Add(25)
	if loudest.Total() != 1 || loudest.First != blocksTop {
		t.Errorf("%+v", loudest)
	}
}

func TestBlocksMergeAndPack(t *testing.T) {
	a := blocksOf(map[float64]int{-12.3: 40, -9: 200, -31.7: 5})
	b := blocksOf(map[float64]int{-45: 7, -9: 10, -3.1: 1})
	sum := a
	sum.Counts = append([]uint32(nil), a.Counts...)
	sum.Merge(b)
	if sum.Total() != a.Total()+b.Total() {
		t.Errorf("merged %d blocks from %d and %d", sum.Total(), a.Total(), b.Total())
	}
	want := blocksOf(map[float64]int{-12.3: 40, -9: 210, -31.7: 5, -45: 7, -3.1: 1})
	if sum.First != want.First || len(sum.Counts) != len(want.Counts) {
		t.Fatalf("merged %+v, want %+v", sum, want)
	}
	for i := range sum.Counts {
		if sum.Counts[i] != want.Counts[i] {
			t.Fatalf("step %d: %d, want %d", i, sum.Counts[i], want.Counts[i])
		}
	}
	// Packed and read back, it is the same; a song takes little room.
	packed := sum.Encode()
	back, err := DecodeBlocks(packed)
	if err != nil || back.First != sum.First || len(back.Counts) != len(sum.Counts) || back.Total() != sum.Total() {
		t.Fatalf("read back %+v (%v), wrote %+v", back, err, sum)
	}
	if l1, _ := back.Integrated(); l1 != mustIntegrated(sum) {
		t.Errorf("loudness changed by packing: %v", l1)
	}
	if len(packed) > 600 {
		t.Errorf("a song's blocks take %d bytes", len(packed))
	}
	if b, err := DecodeBlocks(nil); err != nil || b.Total() != 0 {
		t.Errorf("nothing: %+v %v", b, err)
	}
	if b, err := DecodeBlocks(Blocks{}.Encode()); err != nil || b.Total() != 0 {
		t.Errorf("no blocks, packed: %+v %v", b, err)
	}
	// What is not a packed set of blocks is refused, not read as one.
	for name, bad := range map[string][]byte{
		"another version": {2, 0, 0},
		"cut short":       packed[:len(packed)-3],
		"more after it":   append(append([]byte(nil), packed...), 1),
		"steps past the top": func() []byte {
			return Blocks{First: 790, Counts: make([]uint32, 5)}.Encode()[:3] // says 5 steps from 790, has none
		}(),
		"too many steps": {1, 0xa0, 0x06, 0xff, 0x7f},
	} {
		if _, err := DecodeBlocks(bad); err == nil {
			t.Errorf("%s: read without complaint", name)
		}
	}
}

func mustIntegrated(b Blocks) float64 {
	l, _ := b.Integrated()
	return l
}

// loud is what the stand-in ffmpeg prints for a song: a line for each block, then the
// summary. Without blocks given, the song is 20 seconds of blocks at its loudness.
func loud(lufs, peak string, blocks ...float64) []byte {
	if len(blocks) == 0 {
		v, _ := strconv.ParseFloat(lufs, 64)
		for i := 0; i < 200; i++ {
			blocks = append(blocks, v)
		}
	}
	var b strings.Builder
	for i, m := range blocks {
		fmt.Fprintf(&b, "[Parsed_ebur128_0 @ 0x1] t: %-10.6g TARGET:-23 LUFS    M:%6.1f S:-120.7     I: -70.0 LUFS       LRA:   0.0 LU  FTPK:  -5.3  -5.7 dBFS  TPK:  -3.8  -5.7 dBFS\n", 0.4+float64(i)/10, m)
	}
	b.WriteString("[Parsed_ebur128_0 @ 0x1] Summary:\n\n  Integrated loudness:\n    I:         " + lufs + " LUFS\n  True peak:\n    Peak:        " + peak + " dBFS\n")
	return []byte(b.String())
}

func repeat(m float64, n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = m
	}
	return out
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
	if got := l.track("Two"); got.Loudness != nil || got.AlbumLoudness != nil || got.AlbumPeak != nil {
		t.Errorf("a song that could not be measured has a loudness, or its album has: %v %v", got.Loudness, got.AlbumLoudness)
	}
	// The second song can be measured now: the album gets its loudness, and its peak is
	// the higher of the two songs'.
	os.WriteFile(two+".loud", loud("-9.0", "-1.0"), 0o644)
	if n, _ = l.svc.MeasureLoudness(l.ctx); n != 1 {
		t.Fatalf("second pass measured %d", n)
	}
	if got := l.track("Two"); got.Loudness == nil || got.AlbumLoudness == nil || *got.AlbumLoudness != -9 || got.AlbumPeak == nil || *got.AlbumPeak != 0.5 {
		t.Errorf("Two and its album: %v %v %v", got.Loudness, got.AlbumLoudness, got.AlbumPeak)
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
	if got := l.track("One"); *got.Loudness != -14.5 || got.AlbumLoudness == nil || *got.AlbumLoudness >= -9 || *got.AlbumPeak != -0.2 {
		t.Errorf("after the change: %v, album %v, album peak %v", *got.Loudness, got.AlbumLoudness, got.AlbumPeak)
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

// A song measured before its blocks were kept (the loudness of an earlier version of
// this server) is measured once more; one measured and found to have no blocks at all
// (shorter than one) is measured, not "never measured".
func TestSongsWithoutBlocksAreMeasuredOnceMore(t *testing.T) {
	l := newTestLib(t)
	a := l.put("A/a.mp3", song{tags: tags("title", "Old measure", "artist", "A")})
	b := l.put("A/b.mp3", song{tags: tags("title", "Blip", "artist", "A")})
	os.WriteFile(a+".loud", loud("-10.0", "-1.0"), 0o644)
	// A blip: ffmpeg gives a summary and no block.
	os.WriteFile(b+".loud", []byte("[Parsed_ebur128_0 @ 0x1] Summary:\n\n  Integrated loudness:\n    I:         -70.0 LUFS\n  True peak:\n    Peak:        -3.0 dBFS\n"), 0o644)
	l.scan(false)
	if n, _ := l.svc.MeasureLoudness(l.ctx); n != 2 {
		t.Fatalf("measured %d", n)
	}
	calls := func() int {
		c, _ := os.ReadFile(filepath.Join(filepath.Dir(l.root), "ffmpeg.calls"))
		return strings.Count(string(c), "\n")
	}
	before := calls()
	if n, _ := l.svc.MeasureLoudness(l.ctx); n != 0 || calls() != before {
		t.Errorf("the blip, which has no blocks, was measured again: %d songs, %d runs of ffmpeg", n, calls()-before)
	}
	if measured, total, _ := l.st.MusicLoudnessCounts(l.ctx); measured != 2 || total != 2 {
		t.Errorf("measured %d of %d", measured, total)
	}
	// As an earlier version left a song: a loudness, and no blocks.
	old := l.track("Old measure")
	if _, err := l.st.DB().ExecContext(l.ctx, `UPDATE music_tracks SET loudness_blocks = NULL WHERE id = ?`, old.ID); err != nil {
		t.Fatal(err)
	}
	if measured, _, _ := l.st.MusicLoudnessCounts(l.ctx); measured != 1 {
		t.Errorf("a song without blocks counts as measured: %d", measured)
	}
	if got := l.track("Old measure"); got.Loudness == nil || *got.Loudness != -10 {
		t.Errorf("until it is measured again it keeps the loudness it has: %v", got.Loudness)
	}
	if n, _ := l.svc.MeasureLoudness(l.ctx); n != 1 {
		t.Errorf("a song without blocks: measured %d, want it measured once more", n)
	}
	if n, _ := l.svc.MeasureLoudness(l.ctx); n != 0 {
		t.Errorf("and then again: %d", n)
	}
	// The store itself: a measurement handed over with no blocks at all is a measurement.
	if err := l.st.SetMusicLoudness(l.ctx, old.ID, old.Mtime, -10, -1, nil); err != nil {
		t.Fatal(err)
	}
	if measured, _, _ := l.st.MusicLoudnessCounts(l.ctx); measured != 2 {
		t.Errorf("a measurement with no blocks is not counted: %d", measured)
	}
}

// An album is measured as one: from the blocks of all its songs together. A quiet
// interlude between loud songs is more than 10 LU under the album and is left out, as
// it would be were the album one file; a mean of the songs' own loudness over their
// lengths puts such an album well under what it is.
func TestAlbumIsMeasuredAsOne(t *testing.T) {
	l := newTestLib(t)
	a := l.put("A/Album/1.mp3", song{tags: tags("title", "Loud one", "artist", "A", "album", "Album", "track", "1")})
	b := l.put("A/Album/2.mp3", song{tags: tags("title", "Interlude", "artist", "A", "album", "Album", "track", "2")})
	c := l.put("A/Album/3.mp3", song{tags: tags("title", "Loud two", "artist", "A", "album", "Album", "track", "3")})
	os.WriteFile(a+".loud", loud("-8.0", "-0.1", repeat(-8, 1800)...), 0o644)
	os.WriteFile(b+".loud", loud("-32.0", "-14.0", repeat(-32, 1800)...), 0o644) // three quiet minutes
	os.WriteFile(c+".loud", loud("-8.0", "0.6", repeat(-8, 1800)...), 0o644)
	l.scan(false)
	if n, err := l.svc.MeasureLoudness(l.ctx); n != 3 || err != nil {
		t.Fatalf("measured %d: %v", n, err)
	}
	for _, title := range []string{"Loud one", "Interlude", "Loud two"} {
		got := l.track(title)
		// The mean over the three songs' lengths would be -9.8.
		if got.AlbumLoudness == nil || *got.AlbumLoudness != -8 {
			t.Errorf("%s: album loudness %v, want -8.0", title, got.AlbumLoudness)
		}
		// One peak for the album: the highest of its songs', so that all of them can be
		// turned by the same amount.
		if got.AlbumPeak == nil || *got.AlbumPeak != 0.6 {
			t.Errorf("%s: album peak %v, want 0.6", title, got.AlbumPeak)
		}
	}
	if got := l.track("Interlude"); *got.Loudness != -32 || *got.Peak != -14 {
		t.Errorf("the interlude's own: %v %v", *got.Loudness, *got.Peak)
	}
	// A song with no album has no album figures.
	d := l.put("A/single.mp3", song{tags: tags("title", "Single", "artist", "A")})
	os.WriteFile(d+".loud", loud("-10.0", "-1.0"), 0o644)
	l.scan(false)
	l.svc.MeasureLoudness(l.ctx)
	if got := l.track("Single"); got.Loudness == nil || got.AlbumLoudness != nil || got.AlbumPeak != nil {
		t.Errorf("a single: %v %v %v", got.Loudness, got.AlbumLoudness, got.AlbumPeak)
	}
}

// A song that cannot be measured is given up on after three passes, and then no longer
// holds its album back: the album is measured from its other songs.
func TestAGivenUpSongDoesNotHoldItsAlbumBack(t *testing.T) {
	l := newTestLib(t)
	a := l.put("A/Album/1.mp3", song{tags: tags("title", "Good", "artist", "A", "album", "Album", "track", "1")})
	l.put("A/Album/2.mp3", song{tags: tags("title", "Broken", "artist", "A", "album", "Album", "track", "2")})
	os.WriteFile(a+".loud", loud("-12.0", "-2.0"), 0o644)
	l.scan(false)
	for pass := 1; pass <= 2; pass++ {
		l.svc.MeasureLoudness(l.ctx)
		if got := l.track("Good"); got.Loudness == nil || got.AlbumLoudness != nil {
			t.Fatalf("pass %d: the album has a loudness while a song of it may still be measured: %v", pass, got.AlbumLoudness)
		}
	}
	l.svc.MeasureLoudness(l.ctx) // the third failure: given up on
	if got := l.track("Good"); got.AlbumLoudness == nil || *got.AlbumLoudness != -12 || *got.AlbumPeak != -2 {
		t.Errorf("the album once its broken song is given up on: %v %v", got.AlbumLoudness, got.AlbumPeak)
	}
	if got := l.track("Broken"); got.Loudness != nil || got.AlbumLoudness == nil {
		t.Errorf("the broken song: own %v, album %v", got.Loudness, got.AlbumLoudness)
	}
}

// A drive that is not there is not a library of broken files. Songs whose files cannot be
// opened are passed over without a mark against them (three marks and a song is given up
// on), and a run of them ends the pass before it has asked ffmpeg about a whole library.
func TestAnUnmountedDriveCostsNoTries(t *testing.T) {
	l := newTestLib(t)
	var paths []string
	for i := 0; i < 60; i++ {
		p := l.put(fmt.Sprintf("A/song%02d.mp3", i), song{tags: tags("title", fmt.Sprintf("Song %02d", i), "artist", "A")})
		os.WriteFile(p+".loud", loud("-10.0", "-1.0"), 0o644)
		paths = append(paths, p)
	}
	l.scan(false)
	calls := func() int {
		b, _ := os.ReadFile(filepath.Join(filepath.Dir(l.root), "ffmpeg.calls"))
		return strings.Count(string(b), "\n")
	}
	// The folder is gone (the drive under it was unmounted).
	away := l.root + ".away"
	if err := os.Rename(l.root, away); err != nil {
		t.Fatal(err)
	}
	before := calls()
	for pass := 0; pass < 4; pass++ {
		if n, err := l.svc.MeasureLoudness(l.ctx); n != 0 || err != nil {
			t.Fatalf("with the drive gone: measured %d, %v", n, err)
		}
	}
	if calls() != before {
		t.Errorf("ffmpeg was asked about %d files that are not there", calls()-before)
	}
	for _, tr := range l.tracks() {
		if tries, _ := l.st.MusicLoudnessTries(l.ctx, tr.ID); tries != 0 {
			t.Fatalf("%s has %d failed tries for a drive that was away", tr.Title, tries)
		}
	}
	// It is back: every song is measured.
	if err := os.Rename(away, l.root); err != nil {
		t.Fatal(err)
	}
	if n, err := l.svc.MeasureLoudness(l.ctx); n != 60 || err != nil {
		t.Fatalf("with the drive back: measured %d of 60, %v", n, err)
	}
	// One file gone among others that are there is passed over, and the rest are measured.
	later := time.Now().Add(time.Hour)
	for _, p := range paths[:5] {
		os.Chtimes(p, later, later)
	}
	l.scan(false)
	os.Remove(paths[2])
	if n, _ := l.svc.MeasureLoudness(l.ctx); n != 4 {
		t.Errorf("four changed files there and one gone: measured %d", n)
	}
	if tries, _ := l.st.MusicLoudnessTries(l.ctx, l.track("Song 02").ID); tries != 0 {
		t.Errorf("the file that was gone has %d failed tries", tries)
	}
}

// Each music folder is judged by itself. With one drive away the songs on another were
// not measured either: the pass counted files that would not open across every folder
// and stopped altogether at twenty.
func TestADriveAwayDoesNotStopAnotherFolder(t *testing.T) {
	l := newTestLib(t)
	// A second music folder, on "another drive".
	other := filepath.Join(filepath.Dir(l.root), "Second") // after the first, however the songs are put in order
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	second := model.MusicFolder{Name: "More", Root: other}
	if err := l.st.CreateMusicFolder(l.ctx, &second); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 40; i++ {
		p := l.put(fmt.Sprintf("A/song%02d.mp3", i), song{tags: tags("title", fmt.Sprintf("Away %02d", i), "artist", "A")})
		os.WriteFile(p+".loud", loud("-10.0", "-1.0"), 0o644)
	}
	// The same stand-in files in the second folder (put writes under the first root).
	for i := 0; i < 12; i++ {
		p := l.put(fmt.Sprintf("B/here%02d.mp3", i), song{tags: tags("title", fmt.Sprintf("Here %02d", i), "artist", "B")})
		os.WriteFile(p+".loud", loud("-12.0", "-1.0"), 0o644)
	}
	if err := os.Rename(filepath.Join(l.root, "B"), filepath.Join(other, "B")); err != nil {
		t.Fatal(err)
	}
	if err := l.svc.Scan(l.ctx, false); err != nil {
		t.Fatal(err)
	}
	if n := len(l.tracks()); n != 52 {
		t.Fatalf("%d songs scanned, want 52", n)
	}
	// Anything measured by the scan's own pass is put back to "not measured".
	if _, err := l.st.DB().ExecContext(l.ctx, `UPDATE music_tracks SET loudness = NULL, loudness_mtime = 0, loudness_blocks = NULL`); err != nil {
		t.Fatal(err)
	}
	// The songs of the folder that will be away came in last, so the pass reaches them
	// first: forty that will not open, in a row, before any that will.
	if _, err := l.st.DB().ExecContext(l.ctx, `UPDATE music_tracks SET added_at = added_at + 1000 WHERE path LIKE ?`, l.root+string(filepath.Separator)+"%"); err != nil {
		t.Fatal(err)
	}
	calls := func() int {
		b, _ := os.ReadFile(filepath.Join(filepath.Dir(l.root), "ffmpeg.calls"))
		return strings.Count(string(b), "\n")
	}
	// The first folder's drive goes away.
	away := l.root + ".away"
	if err := os.Rename(l.root, away); err != nil {
		t.Fatal(err)
	}
	before := calls()
	n, err := l.svc.MeasureLoudness(l.ctx)
	if err != nil || n != 12 {
		t.Fatalf("one folder away: measured %d of the other folder's 12, %v", n, err)
	}
	if got := calls() - before; got != 12 {
		t.Errorf("ffmpeg was run %d times, want once for each of the 12 songs that are there", got)
	}
	for _, tr := range l.tracks() {
		if tries, _ := l.st.MusicLoudnessTries(l.ctx, tr.ID); tries != 0 {
			t.Fatalf("%s has %d failed tries", tr.Title, tries)
		}
	}
	// Back: the rest is measured.
	if err := os.Rename(away, l.root); err != nil {
		t.Fatal(err)
	}
	if n, err := l.svc.MeasureLoudness(l.ctx); n != 40 || err != nil {
		t.Fatalf("with the drive back: measured %d of 40, %v", n, err)
	}
}

// reachOf: the longest folder a file lies under is its folder, and a name that only
// begins like a folder's is not in it.
func TestSongsAreCountedInTheirOwnFolder(t *testing.T) {
	a, deep, b := &folderReach{root: "/m/Music"}, &folderReach{root: "/m/Music/Live"}, &folderReach{root: "/m/Music2"}
	loose := &folderReach{}
	all := []*folderReach{a, deep, b}
	for path, want := range map[string]*folderReach{
		"/m/Music/A/song.mp3":      a,
		"/m/Music/Live/B/song.mp3": deep,
		"/m/Music2/C/song.mp3":     b,
		"/m/Musical/D/song.mp3":    loose,
		"/elsewhere/song.mp3":      loose,
	} {
		if got := reachOf(all, loose, path); got != want {
			t.Errorf("%s is counted in %q, want %q", path, got.root, want.root)
		}
	}
}

// Nobody waits for a loudness measurement: while the server converts a film for somebody
// no new song is begun, and the pass goes on when that is over.
func TestMeasuringGivesWayToConversions(t *testing.T) {
	old := busyPoll
	busyPoll = 10 * time.Millisecond
	t.Cleanup(func() { busyPoll = old })
	l := newTestLib(t)
	for i := 0; i < 6; i++ {
		p := l.put(fmt.Sprintf("A/song%02d.mp3", i), song{tags: tags("title", fmt.Sprintf("Song %02d", i), "artist", "A")})
		os.WriteFile(p+".loud", loud("-10.0", "-1.0"), 0o644)
	}
	var busy atomic.Bool
	busy.Store(true)
	l.svc.Busy = busy.Load
	l.scan(false) // the scan's own pass waits behind the conversion too
	calls := func() int {
		b, _ := os.ReadFile(filepath.Join(filepath.Dir(l.root), "ffmpeg.calls"))
		return strings.Count(string(b), "\n")
	}
	before := calls()
	done := make(chan int, 1)
	go func() { n, _ := l.svc.MeasureLoudness(l.ctx); done <- n }()
	time.Sleep(300 * time.Millisecond)
	if got := calls() - before; got != 0 {
		t.Fatalf("%d songs were measured while a film was being converted", got)
	}
	select {
	case n := <-done:
		t.Fatalf("the pass ended (%d measured) instead of waiting", n)
	default:
	}
	busy.Store(false)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the pass did not go on once the conversion was over")
	}
	for _, tr := range l.tracks() {
		if tr.Loudness == nil {
			t.Fatalf("%s was not measured", tr.Title)
		}
	}
	// A pass that is waiting ends with the server.
	p := l.put("A/late.mp3", song{tags: tags("title", "Late", "artist", "A")})
	os.WriteFile(p+".loud", loud("-10.0", "-1.0"), 0o644)
	busy.Store(true)
	ctx, cancel := context.WithCancel(l.ctx)
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()
	if err := l.svc.Scan(ctx, false); err != nil && ctx.Err() == nil {
		t.Fatal(err)
	}
	ended := make(chan struct{})
	go func() { _, _ = l.svc.MeasureLoudness(ctx); close(ended) }()
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("a waiting pass did not end when the server stopped")
	}
}

// A long file prints more lines than a run's output may hold; it is measured all the same.
func TestALongFileIsMeasured(t *testing.T) {
	l := newTestLib(t)
	p := l.put("A/mix.mp3", song{tags: tags("title", "Mix", "artist", "A")})
	// Two hours: 72 000 blocks, eleven megabytes of lines.
	os.WriteFile(p+".loud", loud("-13.0", "-0.5", repeat(-13, 72000)...), 0o644)
	l.scan(false)
	if n, err := l.svc.MeasureLoudness(l.ctx); n != 1 || err != nil {
		t.Fatalf("measured %d: %v", n, err)
	}
	if got := l.track("Mix"); got.Loudness == nil || *got.Loudness != -13 {
		t.Errorf("%v", got.Loudness)
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
			p, err := tc.File(context.Background(), "", a, AudioFormat{"opus", 128})
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
	p2, _ := tc.File(context.Background(), "", changed, AudioFormat{"opus", 128})
	if p2 == paths[0] || calls() != 2 {
		t.Errorf("a changed file was served from the old conversion: %s, %d conversions", p2, calls())
	}
	// A conversion that fails keeps nothing, and the next request tries again.
	bad := src("bad.mp3")
	os.WriteFile(bad.Path+".noconvert", nil, 0o644)
	if _, err := tc.File(context.Background(), "", bad, AudioFormat{"mp3", 192}); err == nil {
		t.Error("a failed conversion gave a file")
	}
	left, _ := filepath.Glob(filepath.Join(dir, "*", "*.part"))
	if len(left) != 0 {
		t.Errorf("half-made files left: %v", left)
	}
	os.Remove(bad.Path + ".noconvert")
	if _, err := tc.File(context.Background(), "", bad, AudioFormat{"mp3", 192}); err != nil {
		t.Errorf("the retry: %v", err)
	}

	// Over its size: the files longest unused go, the newest stays.
	small := NewTranscoder(l.svc.ff, dir, 60)
	old := time.Now().Add(-48 * time.Hour)
	os.Chtimes(paths[0], old, old)
	newest, err := small.File(context.Background(), "", src("c.mp3"), AudioFormat{"opus", 128})
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

// The queue of conversions has an end. It had none: every song in every format could be
// asked for at once, each was converted though nobody waited for it any more, and the
// next person's song stood behind them all.
func TestTranscoderQueueIsBounded(t *testing.T) {
	l := newTestLib(t)
	dir := filepath.Join(filepath.Dir(l.root), "data", "cache", "music", "converted")
	tc := NewTranscoder(l.svc.ff, dir, 1<<20)
	tc.MaxPending, tc.PerClient = 5, 3
	held := map[string]string{}
	src := func(name string) Source {
		p := l.put(name, song{tags: tags("title", name, "artist", "A")})
		if err := os.WriteFile(p+".hold", nil, 0o644); err != nil {
			t.Fatal(err)
		}
		held[name] = p
		return Source{TrackID: name, Path: p, Size: 10, Mtime: 1, SampleRate: 44100, Channels: 2}
	}
	started := func(name string) int {
		b, _ := os.ReadFile(held[name] + ".started")
		return len(b) / 2
	}
	opus := AudioFormat{"opus", 128}
	type call struct {
		cancel context.CancelFunc
		done   chan error
	}
	ask := func(client string, s Source) call {
		ctx, cancel := context.WithCancel(context.Background())
		c := call{cancel, make(chan error, 1)}
		go func() { _, err := tc.File(ctx, client, s, opus); c.done <- err }()
		return c
	}
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		for i := 0; i < 400 && !ok(); i++ {
			time.Sleep(10 * time.Millisecond)
		}
		if !ok() {
			t.Fatalf("never: %s", what)
		}
	}
	// One client asks for three songs: two run (the converter's two places), one waits.
	s1, s2, s3, s4 := src("1.mp3"), src("2.mp3"), src("3.mp3"), src("4.mp3")
	c1, c2 := ask("phone", s1), ask("phone", s2)
	waitFor("the first two running", func() bool { return started("1.mp3") == 1 && started("2.mp3") == 1 })
	c3 := ask("phone", s3)
	waitFor("the third waiting", func() bool { return tc.Pending() == 3 })
	// One that should be refused is refused at once; were it taken instead it would wait
	// behind the held ones, so the question is given a moment and no more.
	refused := func(client string, s Source) error {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, err := tc.File(ctx, client, s, opus)
		return err
	}
	// A fourth of its own is refused; the same song again joins the conversion on its way.
	if err := refused("phone", s4); !errors.Is(err, ErrBusy) {
		t.Fatalf("a client past its share: err = %v, want ErrBusy", err)
	}
	again := ask("phone", s3)
	// Somebody else is not held up by that client's share...
	o1, o2 := src("o1.mp3"), src("o2.mp3")
	c5 := ask("tv", o1)
	waitFor("another client's song waiting", func() bool { return tc.Pending() == 4 })
	c6 := ask("tv", o2)
	waitFor("and a second", func() bool { return tc.Pending() == 5 })
	// ...until the whole queue is full.
	if err := refused("browser", src("b.mp3")); !errors.Is(err, ErrBusy) {
		t.Fatalf("a full queue: err = %v, want ErrBusy", err)
	}
	// The third song's two requests leave before its turn: it is not converted at all,
	// and its place in the queue is free at once.
	c3.cancel()
	again.cancel()
	<-c3.done
	<-again.done
	waitFor("the dropped one gone from the queue", func() bool { return tc.Pending() == 4 })
	// A running one whose request leaves is finished all the same (the next request finds it done).
	c1.cancel()
	<-c1.done
	for _, n := range []string{"1.mp3", "2.mp3", "o1.mp3", "o2.mp3"} {
		_ = os.Remove(held[n] + ".hold")
	}
	for _, c := range []call{c2, c5, c6} {
		if err := <-c.done; err != nil {
			t.Fatal(err)
		}
	}
	waitFor("the queue empty", func() bool { return tc.Pending() == 0 })
	if p, err := tc.File(context.Background(), "phone", s1, opus); err != nil || p == "" {
		t.Fatalf("the song whose request left was not kept: %v", err)
	}
	if n := started("1.mp3"); n != 1 {
		t.Errorf("the first song was converted %d times, want once", n)
	}
	if n := started("3.mp3"); n != 0 {
		t.Errorf("a song nobody waited for was converted (%d times)", n)
	}
	// Asked for again, the dropped song is converted like any other.
	_ = os.Remove(held["3.mp3"] + ".hold")
	if _, err := tc.File(context.Background(), "phone", s3, opus); err != nil {
		t.Fatal(err)
	}
	if n := started("3.mp3"); n != 1 {
		t.Errorf("the dropped song, asked for again, was converted %d times", n)
	}
}
