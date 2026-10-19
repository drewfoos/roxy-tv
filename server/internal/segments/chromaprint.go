// Package segments finds skippable ranges (intro, outro, preview) in episodes without
// chapter markers by matching audio across a season, the way Jellyfin's Intro Skipper
// does: Chromaprint fingerprints of the first and last minutes of every episode are
// compared pairwise, and audio that repeats at the same offset is the OP or ED.
package segments

import (
	"context"
	"encoding/binary"
	"fmt"
	"math/bits"
	"regexp"
	"strconv"

	"github.com/roxy-media/roxy/server/internal/ffmpeg"
)

// SampleSec is the time one fingerprint point covers. Chromaprint works on 11025 Hz
// audio in frames of 4096 samples with a hop of 1365, so 1365 / 11025 s per point.
const SampleSec = 1365.0 / 11025.0

// fingerprint runs ffmpeg's chromaprint muxer over [startSec, startSec+lengthSec) of
// the first audio stream and returns the raw 32-bit points.
func fingerprint(ctx context.Context, ff *ffmpeg.FFmpeg, path string, startSec, lengthSec float64) ([]uint32, error) {
	args := []string{
		"-hide_banner", "-loglevel", "error", "-nostdin",
		"-ss", fmt.Sprintf("%.3f", startSec), "-t", fmt.Sprintf("%.3f", lengthSec),
		"-i", path, "-map", "0:a:0", "-vn", "-sn", "-dn", "-ac", "1",
		"-f", "chromaprint", "-fp_format", "raw", "-",
	}
	out, err := ff.Run(ctx, "", args...)
	if err != nil {
		return nil, err
	}
	n := len(out) / 4
	if n < 16 {
		return nil, fmt.Errorf("fingerprint too short (%d points); no audio?", n)
	}
	p := make([]uint32, n)
	for i := range p {
		p[i] = binary.LittleEndian.Uint32(out[4*i:])
	}
	return p, nil
}

// Silence is a quiet stretch of audio, in seconds from the start of the file.
type Silence struct{ Start, End float64 }

var silenceStartRe = regexp.MustCompile(`silence_start: ([0-9.]+)`)
var silenceEndRe = regexp.MustCompile(`silence_end: ([0-9.]+)`)

// silences runs ffmpeg's silencedetect over [startSec, startSec+lengthSec) and returns
// the quiet stretches (absolute times). Used to snap fingerprint boundaries, which are
// only accurate to about a point, onto the cut the editor made.
func silences(ctx context.Context, ff *ffmpeg.FFmpeg, path string, startSec, lengthSec float64) ([]Silence, error) {
	args := []string{
		"-hide_banner", "-nostdin",
		"-ss", fmt.Sprintf("%.3f", startSec), "-t", fmt.Sprintf("%.3f", lengthSec),
		"-i", path, "-map", "0:a:0", "-vn", "-sn", "-dn",
		"-af", "silencedetect=noise=-45dB:duration=0.25", "-f", "null", "-",
	}
	// silencedetect reports on stderr, which Run folds into the error only on failure;
	// ask for the combined output instead.
	out, err := ff.RunCombined(ctx, args...)
	if err != nil {
		return nil, err
	}
	text := string(out)
	starts := silenceStartRe.FindAllStringSubmatch(text, -1)
	ends := silenceEndRe.FindAllStringSubmatch(text, -1)
	var res []Silence
	for i, m := range starts {
		st, _ := strconv.ParseFloat(m[1], 64)
		en := st + 0.25
		if i < len(ends) {
			en, _ = strconv.ParseFloat(ends[i][1], 64)
		}
		res = append(res, Silence{Start: st + startSec, End: en + startSec})
	}
	return res, nil
}

// match is a shared stretch of audio between two fingerprints, in seconds relative to
// each fingerprint's start.
type match struct {
	aStart, aEnd float64
	bStart, bEnd float64
}

func (m match) length() float64 { return m.aEnd - m.aStart }

// longestCommon finds the longest run of audio that a and b share at a constant offset.
// Each 32-bit point is compared by Hamming distance (<= maxDiff counts as the same
// sound); runs may skip up to maxGapSec of mismatching points (a sound effect over the
// music). Candidate offsets come from an inverted index of b's exact point values.
func longestCommon(a, b []uint32, maxDiff int, maxGapSec, minSec float64) (match, bool) {
	if len(a) == 0 || len(b) == 0 {
		return match{}, false
	}
	index := make(map[uint32][]int32, len(b))
	for j, v := range b {
		index[v] = append(index[v], int32(j))
	}
	votes := make(map[int32]int32)
	for i, v := range a {
		for _, j := range index[v] {
			votes[j-int32(i)]++
		}
	}
	// Try the offsets with the most exact hits first; the OP produces hundreds, noise a few.
	cands := make([]cand, 0, len(votes))
	for s, n := range votes {
		if n >= 3 {
			cands = append(cands, cand{s, n})
		}
	}
	sortCands(cands)
	if len(cands) > 64 {
		cands = cands[:64]
	}
	maxGap := int(maxGapSec/SampleSec + 0.5)
	var best match
	found := false
	for _, c := range cands {
		m, ok := contiguous(a, b, int(c.shift), maxDiff, maxGap)
		if ok && m.length() >= minSec && (!found || m.length() > best.length()) {
			best, found = m, true
		}
	}
	return best, found
}

type cand struct{ shift, n int32 }

func sortCands(c []cand) {
	// insertion sort by votes descending; the slice is small
	for i := 1; i < len(c); i++ {
		for j := i; j > 0 && c[j].n > c[j-1].n; j-- {
			c[j], c[j-1] = c[j-1], c[j]
		}
	}
}

// contiguous walks a against b shifted by `shift` and returns the longest similar run.
func contiguous(a, b []uint32, shift, maxDiff, maxGap int) (match, bool) {
	start, last := -1, -1
	bestStart, bestEnd := -1, -1
	close := func() {
		if start >= 0 && (bestStart < 0 || last-start > bestEnd-bestStart) {
			bestStart, bestEnd = start, last
		}
		start, last = -1, -1
	}
	for i := range a {
		j := i + shift
		if j < 0 {
			continue
		}
		if j >= len(b) {
			break
		}
		if bits.OnesCount32(a[i]^b[j]) <= maxDiff {
			if start < 0 {
				start = i
			} else if i-last > maxGap {
				close()
				start = i
			}
			last = i
		}
	}
	close()
	if bestStart < 0 {
		return match{}, false
	}
	m := match{
		aStart: float64(bestStart) * SampleSec, aEnd: float64(bestEnd+1) * SampleSec,
		bStart: float64(bestStart+shift) * SampleSec, bEnd: float64(bestEnd+1+shift) * SampleSec,
	}
	return m, true
}

// snapForward prefers a silence edge up to `after` seconds later than t (the cut the
// match ran into), falling back to the nearest edge within tol.
func snapForward(t float64, sil []Silence, tol, after float64) float64 {
	best, bestD := -1.0, after
	for _, s := range sil {
		for _, edge := range []float64{s.Start, s.End} {
			d := edge - t
			if d >= -0.5 && d < bestD {
				best, bestD = edge, d
			}
		}
	}
	if best >= 0 {
		return best
	}
	return snap(t, sil, tol)
}

// snap moves t onto the nearest silence edge within tol seconds, if any.
func snap(t float64, sil []Silence, tol float64) float64 {
	best, bestD := t, tol
	for _, s := range sil {
		for _, edge := range []float64{s.Start, s.End} {
			d := edge - t
			if d < 0 {
				d = -d
			}
			if d < bestD {
				best, bestD = edge, d
			}
		}
	}
	return best
}
