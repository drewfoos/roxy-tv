package music

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/roxy-media/roxy/server/internal/ffmpeg"
)

// Loudness: how loud each song is, measured from the audio itself (EBU R 128, the
// measure ReplayGain 2 and the streaming services use), so that a player can bring a
// quiet song and a loud one to the same level without anyone having tagged the files.
// It is measured in the background, one song at a time, after a scan; nothing is
// written to the files.

// Loudness is one song's measurement.
type Loudness struct {
	LUFS float64 // integrated loudness
	Peak float64 // true peak, dBTP (0 = full scale)
}

// silentLUFS: at or under this a "song" is silence, and there is nothing to level.
const silentLUFS = -70

var (
	reIntegrated = regexp.MustCompile(`(?m)^\s*I:\s+(-?(?:\d+(?:\.\d+)?|inf))\s+LUFS`)
	reTruePeak   = regexp.MustCompile(`(?m)^\s*Peak:\s+(-?(?:\d+(?:\.\d+)?|inf))\s+dBFS`)
)

// parseLoudness reads the summary ffmpeg's ebur128 filter prints when it is done.
func parseLoudness(out []byte) (Loudness, error) {
	// The summary comes last; an earlier line of the same shape cannot be taken for it.
	i := lastMatch(reIntegrated, out)
	if i == "" {
		return Loudness{}, errors.New("no loudness in ffmpeg's output")
	}
	l := Loudness{LUFS: decibel(i), Peak: 0}
	if p := lastMatch(reTruePeak, out); p != "" {
		l.Peak = decibel(p)
	}
	if l.LUFS < silentLUFS {
		l.LUFS = silentLUFS
	}
	return l, nil
}

func lastMatch(re *regexp.Regexp, b []byte) string {
	all := re.FindAllSubmatch(b, -1)
	if len(all) == 0 {
		return ""
	}
	return string(all[len(all)-1][1])
}

// decibel reads a level; "-inf" (digital silence) is as low as levels go here.
func decibel(s string) float64 {
	if s == "-inf" || s == "inf" {
		return silentLUFS
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) {
		return silentLUFS
	}
	return v
}

// MeasureFile measures one audio file.
func MeasureFile(ctx context.Context, ff *ffmpeg.FFmpeg, path string) (Loudness, error) {
	// framelog=verbose keeps the filter's ten lines a second out of the output: only the
	// summary is printed.
	out, err := ff.RunCombined(ctx, "-hide_banner", "-nostats", "-v", "info", "-i", path, "-map", "0:a:0",
		"-af", "ebur128=peak=true:framelog=verbose", "-f", "null", "-")
	if err != nil {
		return Loudness{}, fmt.Errorf("measuring %s: %w", filepath.Base(path), err)
	}
	return parseLoudness(out)
}

// AlbumLoudness is the loudness of songs played one after another: the mean of their
// energy over their lengths. ok is false when there is nothing to go by.
func AlbumLoudness(lufs []float64, durationsMs []int64) (float64, bool) {
	var energy, total float64
	for i, l := range lufs {
		d := float64(durationsMs[i])
		if d <= 0 || l <= silentLUFS {
			continue
		}
		energy += d * math.Pow(10, l/10)
		total += d
	}
	if total == 0 || energy == 0 {
		return 0, false
	}
	return math.Round(10*math.Log10(energy/total)*10) / 10, true
}

// MeasureLoudness measures every song not yet measured as its file is now, one at a
// time, until there are none left or ctx ends, then works out the albums' loudness. It
// returns how many songs it measured. A call while another runs returns at once.
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

	started := time.Now()
	done, failed := 0, 0
	// A file is tried once in a pass: one that fails is tried again by a later pass (three
	// in all), not three times in a row within this one.
	tried := map[string]bool{}
	for ctx.Err() == nil {
		// Failed files stay on the list, so it is asked for with room for them.
		batch, _, err := s.store.MusicTracksToMeasure(ctx, 50+len(tried))
		if err != nil {
			return done, err
		}
		fresh := batch[:0]
		for _, t := range batch {
			if !tried[t.ID] {
				fresh = append(fresh, t)
			}
		}
		if len(fresh) == 0 {
			break
		}
		for _, t := range fresh {
			if ctx.Err() != nil {
				break
			}
			tried[t.ID] = true
			l, err := MeasureFile(ctx, s.ff, t.Path)
			if err != nil {
				if ctx.Err() != nil {
					break // stopped, not failed: the file is measured next time
				}
				failed++
				slog.Debug("music: loudness not measured", "file", t.Path, "err", err)
				if err := s.store.MusicLoudnessFailed(ctx, t.ID); err != nil {
					return done, err
				}
				continue
			}
			if err := s.store.SetMusicLoudness(ctx, t.ID, t.Mtime, l.LUFS, l.Peak); err != nil {
				return done, err
			}
			done++
		}
	}
	if done > 0 || failed > 0 {
		if err := s.refreshAlbumLoudness(ctx); err != nil && ctx.Err() == nil {
			return done, err
		}
		slog.Info("music: loudness measured", "songs", done, "failed", failed, "took", time.Since(started).Round(time.Millisecond))
	}
	return done, ctx.Err()
}

// Measuring reports whether a loudness pass is running.
func (s *Service) Measuring() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.measuring
}

// refreshAlbumLoudness gives every album whose songs are all measured its loudness.
func (s *Service) refreshAlbumLoudness(ctx context.Context) error {
	rows, err := s.store.MusicAlbumLoudnessInputs(ctx)
	if err != nil {
		return err
	}
	type album struct {
		lufs      []float64
		durations []int64
		whole     bool
	}
	albums := map[string]*album{}
	for _, r := range rows {
		a := albums[r.AlbumID]
		if a == nil {
			a = &album{whole: true}
			albums[r.AlbumID] = a
		}
		if r.Loudness == nil {
			a.whole = false // one song not measured: the album's figure would be a guess
			continue
		}
		a.lufs = append(a.lufs, *r.Loudness)
		a.durations = append(a.durations, r.DurationMs)
	}
	out := map[string]float64{}
	for id, a := range albums {
		if !a.whole {
			continue
		}
		if l, ok := AlbumLoudness(a.lufs, a.durations); ok {
			out[id] = l
		}
	}
	return s.store.SetMusicAlbumLoudness(ctx, out)
}
