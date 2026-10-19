package segments

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/roxy-media/roxy/server/internal/config"
	"github.com/roxy-media/roxy/server/internal/ffmpeg"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

// Detection windows and limits (Intro Skipper's defaults, tuned for TV anime where OP
// and ED are 89..91 s almost without exception).
const (
	HeadSec        = 600.0 // fingerprint the first 10 minutes
	TailSec        = 360.0 // and the last 6
	MinIntroSec    = 15.0
	MaxIntroSec    = 150.0
	MinOutroSec    = 15.0
	MaxOutroSec    = 300.0
	MaxDiff        = 6   // Hamming distance for "same sound"
	MaxGapSec      = 3.5 // a run may skip this much mismatching audio
	SnapTolSec     = 2.5 // move a boundary onto a silence edge this close
	OutroStartTol  = 6.0 // an ED often starts under the last scene: prefer the cut up to this much later
	MinEpisodeSec  = 300.0 // shorter files have no OP/ED structure (and identical test clips match whole)
	StartSnapSec   = 5.0 // an intro starting this early starts at 0
	MinPreviewSec  = 5.0 // after the ED: a next-episode preview this long or longer…
	MaxPreviewSec  = 60.0 // …but not a whole post-credits scene
	Origin         = "fingerprint"
	seasonDebounce = 20 * time.Second
)

// Service runs detection for seasons as their episodes are ingested (debounced, so a
// season scan triggers one pass) and on demand.
type Service struct {
	store *store.Store
	ff    *ffmpeg.FFmpeg
	cfg   *config.Config

	mu      sync.Mutex
	pending map[string]time.Time // season id -> when it was last touched
	wake    chan struct{}
}

func New(st *store.Store, ff *ffmpeg.FFmpeg, cfg *config.Config) *Service {
	return &Service{store: st, ff: ff, cfg: cfg, pending: map[string]time.Time{}, wake: make(chan struct{}, 1)}
}

// Start runs the worker: seasons sit in the pending set until nothing new has arrived
// for a while, then get detected one at a time.
func (s *Service) Start(ctx context.Context) {
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.wake:
			case <-t.C:
			}
			for _, id := range s.due() {
				if _, err := s.DetectSeason(ctx, id, false); err != nil {
					slog.Warn("segment detection failed", "season", id, "err", err)
				}
			}
		}
	}()
}

func (s *Service) due() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []string
	now := time.Now()
	for id, at := range s.pending {
		if now.Sub(at) >= seasonDebounce {
			ids = append(ids, id)
			delete(s.pending, id)
		}
	}
	return ids
}

// EnqueueSource is the ingest hook: the source's season is (re)scheduled.
func (s *Service) EnqueueSource(sourceID string) {
	season, err := s.store.SeasonOfSource(context.Background(), sourceID)
	if err != nil || season == "" {
		return
	}
	s.EnqueueSeason(season)
}

func (s *Service) EnqueueSeason(seasonID string) {
	s.mu.Lock()
	s.pending[seasonID] = time.Now()
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// SweepAll schedules every season whose episodes still lack segments.
func (s *Service) SweepAll(ctx context.Context) {
	ids, err := s.store.SeasonIDs(ctx)
	if err != nil {
		return
	}
	for _, id := range ids {
		eps, err := s.store.SeasonEpisodes(ctx, id)
		if err != nil {
			continue
		}
		for _, e := range eps {
			// Missing a segment, or carrying fingerprint results from a file now considered
			// too short (an earlier version matched identical test clips whole): redo it.
			stale := float64(e.DurationMs)/1000 < MinEpisodeSec && hasOrigin(e.Segments, Origin)
			if stale || !hasType(e.Segments, "intro") || !hasType(e.Segments, "outro") {
				s.EnqueueSeason(id)
				break
			}
		}
	}
}

// Report is what one detection pass found, for logs and the admin endpoint.
type Report struct {
	SeasonID string          `json:"seasonId"`
	Episodes []EpisodeReport `json:"episodes"`
	Elapsed  string          `json:"elapsed"`
}

type EpisodeReport struct {
	ItemID   string               `json:"itemId"`
	Index    int                  `json:"index"`
	Found    []model.MediaSegment `json:"found"`
	Chapters []model.MediaSegment `json:"chapters,omitempty"` // what the markers said, for comparison
	MatchedWith int               `json:"matchedWith,omitempty"`
	Note     string               `json:"note,omitempty"`
}

// DetectSeason fingerprints the season's episodes (cached) and compares neighbours.
// Results are written with origin "fingerprint" unless dry is set; chapter segments are
// never overwritten (the store keeps the existing row on a type conflict).
func (s *Service) DetectSeason(ctx context.Context, seasonID string, dry bool) (*Report, error) {
	started := time.Now()
	eps, err := s.store.SeasonEpisodes(ctx, seasonID)
	if err != nil {
		return nil, err
	}
	rep := &Report{SeasonID: seasonID}
	if len(eps) < 2 {
		rep.Elapsed = time.Since(started).String()
		return rep, nil
	}
	heads := make([][]uint32, len(eps))
	tails := make([][]uint32, len(eps))
	tailStart := make([]float64, len(eps))
	for i, e := range eps {
		if ctx.Err() != nil {
			return rep, ctx.Err()
		}
		h, err := s.cachedFingerprint(ctx, e, "head", 0, HeadSec)
		if err != nil {
			slog.Warn("fingerprint head", "item", e.ItemID, "err", err)
		}
		heads[i] = h
		dur := float64(e.DurationMs) / 1000
		ts := dur - TailSec
		if ts < 0 {
			ts = 0
		}
		t, err := s.cachedFingerprint(ctx, e, "tail", ts, TailSec)
		if err != nil {
			slog.Warn("fingerprint tail", "item", e.ItemID, "err", err)
		}
		tails[i] = t
		tailStart[i] = ts
	}

	for i, e := range eps {
		er := EpisodeReport{ItemID: e.ItemID, Index: e.Index}
		for _, sg := range e.Segments {
			if sg.Origin == "chapters" {
				er.Chapters = append(er.Chapters, sg)
			}
		}
		dur := float64(e.DurationMs) / 1000
		var found []model.MediaSegment
		if dur < MinEpisodeSec {
			er.Note = "too short for OP/ED detection"
			rep.Episodes = append(rep.Episodes, er)
			if !dry {
				if err := s.store.ReplaceSegmentsByOrigin(ctx, e.SourceID, Origin, nil); err != nil {
					return rep, err
				}
			}
			continue
		}

		// Intro: the longest audio shared with a neighbour at the front.
		if intro, with, ok := bestNeighbour(heads, i, MinIntroSec, MaxIntroSec); ok {
			st, en := intro.aStart, intro.aEnd
			if sil, err := silences(ctx, s.ff, e.Path, 0, HeadSec); err == nil {
				st, en = snap(st, sil, SnapTolSec), snap(en, sil, SnapTolSec)
			}
			if st <= StartSnapSec {
				st = 0
			}
			found = append(found, model.MediaSegment{Type: "intro", StartMs: ms(st), EndMs: ms(en), Origin: Origin})
			er.MatchedWith = eps[with].Index
		}
		// Outro: the same at the back; what follows it is the preview.
		if outro, _, ok := bestNeighbour(tails, i, MinOutroSec, MaxOutroSec); ok {
			st, en := tailStart[i]+outro.aStart, tailStart[i]+outro.aEnd
			if sil, err := silences(ctx, s.ff, e.Path, tailStart[i], TailSec); err == nil {
				// The ED's music usually fades in under the final shot, so the audio match
				// starts a few seconds before the cut; the cut is the first silence after it.
				st = snapForward(st, sil, SnapTolSec, OutroStartTol)
				en = snap(en, sil, SnapTolSec)
			}
			if dur-en <= StartSnapSec {
				en = dur
			}
			introEnd := 0.0
			for _, f := range found {
				if f.Type == "intro" {
					introEnd = float64(f.EndMs) / 1000
				}
			}
			if st < introEnd {
				er.Note = "outro overlapped the intro; dropped"
				st = -1
			}
			if st >= 0 {
				found = append(found, model.MediaSegment{Type: "outro", StartMs: ms(st), EndMs: ms(en), Origin: Origin})
			}
			if rest := dur - en; st >= 0 && rest >= MinPreviewSec && rest <= MaxPreviewSec {
				found = append(found, model.MediaSegment{Type: "preview", StartMs: ms(en), EndMs: ms(dur), Origin: Origin})
			}
		}
		er.Found = found
		if len(found) == 0 {
			er.Note = "no shared audio with a neighbour"
		}
		rep.Episodes = append(rep.Episodes, er)
		if !dry {
			if err := s.store.ReplaceSegmentsByOrigin(ctx, e.SourceID, Origin, found); err != nil {
				return rep, err
			}
		}
	}
	rep.Elapsed = time.Since(started).String()
	slog.Info("segments detected", "season", seasonID, "episodes", len(eps), "took", rep.Elapsed, "dry", dry)
	return rep, nil
}

// bestNeighbour compares episode i's fingerprint with its neighbours (±1, then ±2) and
// returns the longest valid shared run.
func bestNeighbour(fps [][]uint32, i int, minSec, maxSec float64) (match, int, bool) {
	var best match
	bestWith := -1
	for _, d := range []int{1, -1, 2, -2} {
		j := i + d
		if j < 0 || j >= len(fps) || len(fps[i]) == 0 || len(fps[j]) == 0 {
			continue
		}
		m, ok := longestCommon(fps[i], fps[j], MaxDiff, MaxGapSec, minSec)
		if !ok || m.length() > maxSec {
			continue
		}
		if bestWith < 0 || m.length() > best.length() {
			best, bestWith = m, j
		}
		if d == -1 && bestWith >= 0 {
			break // both direct neighbours tried; ±2 only when neither matched
		}
	}
	return best, bestWith, bestWith >= 0
}

func (s *Service) cachedFingerprint(ctx context.Context, e store.SeasonEpisode, kind string, startSec, lengthSec float64) ([]uint32, error) {
	if fp, err := s.store.GetFingerprint(ctx, e.SourceID, kind); err == nil && fp != nil && fp.StartMs == ms(startSec) {
		return fp.Points, nil
	}
	p, err := fingerprint(ctx, s.ff, e.Path, startSec, lengthSec)
	if err != nil {
		return nil, err
	}
	_ = s.store.PutFingerprint(ctx, &store.Fingerprint{SourceID: e.SourceID, Kind: kind, StartMs: ms(startSec), LengthMs: ms(lengthSec), Points: p})
	return p, nil
}

func hasOrigin(segs []model.MediaSegment, o string) bool {
	for _, s := range segs {
		if s.Origin == o {
			return true
		}
	}
	return false
}

func hasType(segs []model.MediaSegment, t string) bool {
	for _, s := range segs {
		if s.Type == t {
			return true
		}
	}
	return false
}

func ms(sec float64) int64 { return int64(sec*1000 + 0.5) }
