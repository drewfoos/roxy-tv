package music

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/roxy-media/roxy/server/internal/ffmpeg"
	"github.com/roxy-media/roxy/server/internal/store"
)

// Loudness: how loud each song is, measured from the audio itself (EBU R 128 / ITU-R
// BS.1770, the measure ReplayGain 2 and the streaming services use), so that a player
// can bring a quiet song and a loud one to the same level without anyone having tagged
// the files. It is measured in the background after a scan, a few songs at a time;
// nothing is written to the files.
//
// A song's loudness is not enough to give an album's. The standard measures in blocks of
// 400 ms and leaves out the ones more than 10 LU under the level of the whole (a fade, a
// spoken interlude): which blocks those are depends on what the whole is. So beside its
// loudness each song keeps how loud its blocks are (Blocks), and an album's loudness is
// measured from all its songs' blocks together, as if the album were one file.

// Loudness is one song's measurement.
type Loudness struct {
	LUFS   float64 // integrated loudness
	Peak   float64 // true peak, dBTP (0 = full scale)
	Blocks Blocks  // how loud its 400 ms blocks are
}

// silentLUFS: at or under this a "song" is silence, and there is nothing to level. It is
// also the standard's absolute gate: blocks under it count for nothing.
const silentLUFS = -70

// Blocks counts a song's measuring blocks (400 ms long, one starting every 100 ms) by how
// loud each is, in steps of a tenth of a LU from -70 LUFS up: Counts[i] blocks were at
// -70 + (First+i)/10 LUFS. Blocks under -70 LUFS are not kept (the standard never counts
// them). A tenth of a LU is what the measurement is reported in.
type Blocks struct {
	First  int
	Counts []uint32
}

// blocksTop is the last step kept: +10 LUFS, louder than digital sound can be.
const blocksTop = 800

// Add counts one block of m LUFS.
func (b *Blocks) Add(m float64) {
	if math.IsNaN(m) {
		return
	}
	// A block under -70 LUFS is a step under the first, which add does not keep.
	b.add(int(math.Round((m-silentLUFS)*10)), 1)
}

func (b *Blocks) add(step int, n uint32) {
	if n == 0 || step < 0 {
		return
	}
	if step > blocksTop {
		step = blocksTop
	}
	switch {
	case len(b.Counts) == 0:
		b.First, b.Counts = step, []uint32{0}
	case step < b.First:
		b.Counts = append(make([]uint32, b.First-step, b.First-step+len(b.Counts)), b.Counts...)
		b.First = step
	case step >= b.First+len(b.Counts):
		b.Counts = append(b.Counts, make([]uint32, step-b.First-len(b.Counts)+1)...)
	}
	b.Counts[step-b.First] += n
}

// Merge adds another song's blocks: the two played one after the other.
func (b *Blocks) Merge(o Blocks) {
	for i, n := range o.Counts {
		b.add(o.First+i, n)
	}
}

// Total is how many blocks there are.
func (b Blocks) Total() int {
	t := 0
	for _, n := range b.Counts {
		t += int(n)
	}
	return t
}

// Integrated is the loudness of the blocks together, as BS.1770 gates it: the mean
// energy of the blocks that are not more than 10 LU under the mean energy of all of
// them. ok is false when there are none.
func (b Blocks) Integrated() (lufs float64, ok bool) {
	mean := func(from int) (float64, bool) {
		var energy float64
		var n uint64
		for i, c := range b.Counts {
			if c == 0 || b.First+i < from {
				continue
			}
			energy += float64(c) * math.Pow(10, (silentLUFS+float64(b.First+i)/10)/10)
			n += uint64(c)
		}
		if n == 0 {
			return 0, false
		}
		return 10 * math.Log10(energy/float64(n)), true
	}
	all, ok := mean(0)
	if !ok {
		return 0, false
	}
	// The gate, as a step: blocks at or over it count.
	gate := int(math.Ceil((all-10-silentLUFS)*10 - 1e-9))
	l, ok := mean(gate)
	if !ok {
		return 0, false
	}
	return math.Round(l*10) / 10, true
}

// Encode packs the blocks for the database: a version, the first step, how many steps,
// then each count, all as varints. A song takes a few hundred bytes.
func (b Blocks) Encode() []byte {
	out := make([]byte, 0, 8+len(b.Counts)*2)
	out = binary.AppendUvarint(out, 1)
	out = binary.AppendUvarint(out, uint64(b.First))
	out = binary.AppendUvarint(out, uint64(len(b.Counts)))
	for _, n := range b.Counts {
		out = binary.AppendUvarint(out, uint64(n))
	}
	return out
}

// DecodeBlocks reads what Encode wrote. Nothing at all is a song with no blocks.
func DecodeBlocks(p []byte) (Blocks, error) {
	if len(p) == 0 {
		return Blocks{}, nil
	}
	bad := errors.New("loudness blocks: not readable")
	next := func() (uint64, bool) {
		v, n := binary.Uvarint(p)
		if n <= 0 {
			return 0, false
		}
		p = p[n:]
		return v, true
	}
	version, ok := next()
	if !ok || version != 1 {
		return Blocks{}, bad
	}
	first, ok1 := next()
	count, ok2 := next()
	if !ok1 || !ok2 || first > blocksTop || count > blocksTop+1 || first+count > blocksTop+1 {
		return Blocks{}, bad
	}
	b := Blocks{First: int(first), Counts: make([]uint32, count)}
	for i := range b.Counts {
		v, ok := next()
		if !ok || v > math.MaxUint32 {
			return Blocks{}, bad
		}
		b.Counts[i] = uint32(v)
	}
	if len(p) != 0 {
		return Blocks{}, bad
	}
	return b, nil
}

// loudnessReader reads what ffmpeg's ebur128 filter prints: a line for every 100 ms with
// the loudness of the 400 ms block that ends there ("M:"), then a summary.
type loudnessReader struct {
	l       Loudness
	summary bool // past the "Summary:" line
	sawI    bool
}

var (
	markM       = []byte(" M:")
	markSummary = []byte("Summary:")
	markI       = []byte("I:")
	markPeak    = []byte("Peak:")
)

func (r *loudnessReader) line(b []byte) {
	if !r.summary {
		if i := bytes.Index(b, markM); i >= 0 && bytes.Contains(b, []byte("TARGET:")) {
			if m, ok := leadingLevel(b[i+len(markM):]); ok {
				r.l.Blocks.Add(m)
			}
			return
		}
		if bytes.Contains(b, markSummary) {
			r.summary = true
		}
		return
	}
	t := bytes.TrimSpace(b)
	switch {
	case bytes.HasPrefix(t, markI):
		if v, ok := leadingLevel(t[len(markI):]); ok {
			r.l.LUFS, r.sawI = v, true
		}
	case bytes.HasPrefix(t, markPeak):
		if v, ok := leadingLevel(t[len(markPeak):]); ok {
			r.l.Peak = v
		}
	}
}

// leadingLevel reads the level a field starts with ("  -12.3 S:..."). "-inf" (digital
// silence) is as low as levels go here.
func leadingLevel(b []byte) (float64, bool) {
	b = bytes.TrimLeft(b, " \t")
	end := 0
	for end < len(b) && b[end] != ' ' && b[end] != '\t' {
		end++
	}
	s := string(b[:end])
	if s == "-inf" || s == "inf" || s == "nan" || s == "-nan" {
		return silentLUFS, s != "nan" && s != "-nan"
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) {
		return 0, false
	}
	return v, true
}

func (r *loudnessReader) result() (Loudness, error) {
	if !r.sawI {
		return Loudness{}, errors.New("no loudness in ffmpeg's output")
	}
	if r.l.LUFS < silentLUFS {
		r.l.LUFS = silentLUFS
	}
	return r.l, nil
}

// parseLoudness reads a whole run's output (tests; MeasureFile reads it as it comes).
func parseLoudness(out []byte) (Loudness, error) {
	var r loudnessReader
	for _, line := range bytes.Split(out, []byte("\n")) {
		r.line(line)
	}
	return r.result()
}

// MeasureFile measures one audio file.
func MeasureFile(ctx context.Context, ff *ffmpeg.FFmpeg, path string) (Loudness, error) {
	var r loudnessReader
	// framelog=info: a line for every block, read as it comes and not kept (an hour of
	// sound prints five megabytes of them).
	err := ff.RunLines(ctx, r.line, "-hide_banner", "-nostats", "-v", "info", "-i", path, "-map", "0:a:0",
		"-af", "ebur128=peak=true:framelog=info", "-f", "null", "-")
	if err != nil {
		return Loudness{}, fmt.Errorf("measuring %s: %w", filepath.Base(path), err)
	}
	return r.result()
}

// loudnessWorkers is how many songs are measured at a time: a quarter of the processors,
// four at most. It is background work on a box that is also playing and converting.
func loudnessWorkers() int {
	n := runtime.NumCPU() / 4
	if n < 1 {
		return 1
	}
	if n > 4 {
		return 4
	}
	return n
}

// unreachableLimit: this many files in a row that cannot be opened is a drive that is
// gone, not that many broken files, and the pass stops there.
const unreachableLimit = 20

// albumRefreshEvery: during a long pass albums get their loudness as their songs are
// done, this often.
const albumRefreshEvery = 30 * time.Second

// MeasureLoudness measures every song not yet measured as its file is now, a few at a
// time, until there are none left or ctx ends, and gives the albums they belong to their
// loudness as they are completed. It returns how many songs it measured. A call while
// another runs returns at once.
//
// A file that cannot be opened is not a file that cannot be measured: it is skipped
// without a mark against it (an unmounted drive would otherwise use up every song's
// tries), and a run of them ends the pass.
func (s *Service) MeasureLoudness(ctx context.Context) (int, error) {
	s.mu.Lock()
	if s.measuring {
		s.mu.Unlock()
		return 0, nil
	}
	s.measuring = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.measuring = false
		s.mu.Unlock()
	}()

	list, err := s.store.MusicTracksToMeasure(ctx)
	if err != nil || len(list) == 0 {
		return 0, err
	}
	started := time.Now()
	ctx, stop := context.WithCancel(ctx)
	defer stop()

	var (
		mu          sync.Mutex
		done        int
		failed      int
		skipped     int
		firstErr    error
		touched     = map[string]bool{} // albums with a song measured since they were last worked out
		lastRefresh = time.Now()
		unreachable atomic.Int32 // in a row
		gone        atomic.Bool
	)
	fail := func(err error) {
		mu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		mu.Unlock()
		stop()
	}
	// flush works out the albums touched so far. Called with mu held.
	flush := func(force bool) {
		if len(touched) == 0 || (!force && time.Since(lastRefresh) < albumRefreshEvery) {
			return
		}
		ids := make([]string, 0, len(touched))
		for id := range touched {
			ids = append(ids, id)
		}
		touched = map[string]bool{}
		lastRefresh = time.Now()
		if err := s.RefreshAlbumLoudness(context.WithoutCancel(ctx), ids); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	jobs := make(chan store.MusicUnmeasured)
	var wg sync.WaitGroup
	for w := 0; w < loudnessWorkers(); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range jobs {
				if ctx.Err() != nil {
					continue
				}
				if f, err := os.Open(t.Path); err != nil {
					mu.Lock()
					skipped++
					mu.Unlock()
					if unreachable.Add(1) >= unreachableLimit {
						gone.Store(true)
						stop()
					}
					continue
				} else {
					f.Close()
				}
				unreachable.Store(0)
				l, err := MeasureFile(ctx, s.ff, t.Path)
				if err != nil {
					if ctx.Err() != nil {
						continue // stopped, not failed: the file is measured next time
					}
					slog.Debug("music: loudness not measured", "file", t.Path, "err", err)
					if err := s.store.MusicLoudnessFailed(ctx, t.ID); err != nil {
						fail(err)
						continue
					}
					mu.Lock()
					failed++
					// A song given up on no longer holds its album back.
					if t.AlbumID != "" {
						touched[t.AlbumID] = true
					}
					mu.Unlock()
					continue
				}
				if err := s.store.SetMusicLoudness(ctx, t.ID, t.Mtime, l.LUFS, l.Peak, l.Blocks.Encode()); err != nil {
					fail(err)
					continue
				}
				mu.Lock()
				done++
				if t.AlbumID != "" {
					touched[t.AlbumID] = true
				}
				flush(false)
				mu.Unlock()
			}
		}()
	}
feed:
	for _, t := range list {
		select {
		case jobs <- t:
		case <-ctx.Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()

	mu.Lock()
	flush(true)
	err = firstErr
	mu.Unlock()
	switch {
	case gone.Load():
		slog.Warn("music: loudness left for later: the music files cannot be opened (a drive that is not there?)", "measured", done, "notOpened", skipped)
	case done > 0 || failed > 0:
		slog.Info("music: loudness measured", "songs", done, "failed", failed, "notOpened", skipped, "took", time.Since(started).Round(time.Millisecond))
	}
	if err != nil {
		return done, err
	}
	if gone.Load() {
		return done, nil
	}
	return done, ctx.Err()
}

// Measuring reports whether a loudness pass is running.
func (s *Service) Measuring() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.measuring
}

// albumMeasure gathers one album's songs.
type albumMeasure struct {
	blocks  Blocks
	peak    float64
	songs   int  // measured, and counted in
	pending bool // a song is still to be measured: the album's figure would be a guess
}

// loudness is the album's figure, when it has one.
func (a *albumMeasure) loudness() (lufs, peak float64, ok bool) {
	if a.pending || a.songs == 0 {
		return 0, 0, false
	}
	l, ok := a.blocks.Integrated()
	if !ok || l <= silentLUFS {
		return 0, 0, false
	}
	return l, a.peak, true
}

// RefreshAlbumLoudness works out the loudness and the true peak of the albums named (all
// of them when none are): from the blocks of all an album's songs together, once every
// song of it that is there is measured as its file is now. A song that cannot be measured
// (given up on) is left out and does not hold the album back.
func (s *Service) RefreshAlbumLoudness(ctx context.Context, albumIDs []string) error {
	out := map[string]store.MusicAlbumLevel{}
	var cur *albumMeasure
	curID := ""
	finish := func() {
		if cur == nil {
			return
		}
		level := store.MusicAlbumLevel{}
		if l, p, ok := cur.loudness(); ok {
			level.Loudness, level.Peak = &l, &p
		}
		out[curID] = level
	}
	err := s.store.MusicAlbumLoudnessInputs(ctx, albumIDs, func(r store.MusicAlbumTrackLoudness) error {
		if r.AlbumID != curID {
			finish()
			cur, curID = &albumMeasure{peak: math.Inf(-1)}, r.AlbumID
		}
		switch {
		case r.Measured:
			b, err := DecodeBlocks(r.Blocks)
			if err != nil {
				cur.pending = true // measured again by the next pass
				return nil
			}
			cur.blocks.Merge(b)
			cur.peak = math.Max(cur.peak, r.Peak)
			cur.songs++
		case !r.GivenUp:
			cur.pending = true
		}
		return nil
	})
	if err != nil {
		return err
	}
	finish()
	return s.store.SetMusicAlbumLoudness(ctx, out, len(albumIDs) == 0)
}
