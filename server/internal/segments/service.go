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
	MaxDiff        = 6     // Hamming distance for "same sound"
	MaxGapSec      = 3.5   // a run may skip this much mismatching audio
	SnapTolSec     = 2.5   // move a boundary onto a silence edge this close
	OutroStartTol  = 6.0   // an ED often starts under the last scene: prefer the cut up to this much later
	MinEpisodeSec  = 300.0 // shorter files have no OP/ED structure (and identical test clips match whole)
	StartSnapSec   = 5.0   // an intro starting this early starts at 0
	MinPreviewSec  = 5.0   // after the ED: a next-episode preview this long or longer…
	MaxPreviewSec  = 60.0  // …but not a whole post-credits scene
	Origin         = "fingerprint"
	seasonDebounce = 20 * time.Second
)

// maxFailedPasses is how many detection passes in a row may fail to fingerprint an
// episode's audio before it is marked as worked on anyway (see markDetected).
const maxFailedPasses = 3

// aniskipMaxFailures is how many lookups in a row may fail before a pass stops asking
// (AniSkip is down, not one episode it cannot answer for).
const aniskipMaxFailures = 3

// DetectorVersion is stored per source after a detection pass (store.MarkDetected).
// Raise it when detection changes enough that every season should be redone once.
const DetectorVersion = 1

// Service runs detection for seasons as their episodes are ingested (debounced, so a
// season scan triggers one pass) and on demand.
type Service struct {
	store   *store.Store
	ff      *ffmpeg.FFmpeg
	cfg     *config.Config
	aniskip *AniSkip // nil = do not ask AniSkip

	mu      sync.Mutex
	pending map[string]pendingSeason // season id -> when it was last touched, and how
	wake    chan struct{}
}

type pendingSeason struct {
	at    time.Time
	force bool // redo every episode (admin refresh), not only the ones never detected
}

func New(st *store.Store, ff *ffmpeg.FFmpeg, cfg *config.Config) *Service {
	return &Service{store: st, ff: ff, cfg: cfg, aniskip: NewAniSkip(), pending: map[string]pendingSeason{}, wake: make(chan struct{}, 1)}
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
			for id, p := range s.due() {
				if err := s.runSeason(ctx, id, p.force); err != nil {
					slog.Warn("segment detection failed", "season", id, "err", err)
				}
			}
		}
	}()
}

// runSeason runs the fingerprint pass when an episode of the season was never detected
// (at this DetectorVersion), or when forced; otherwise only AniSkip, for episodes it was
// not yet asked about with the season's current MAL id.
func (s *Service) runSeason(ctx context.Context, seasonID string, force bool) error {
	if !force {
		eps, err := s.store.SeasonEpisodes(ctx, seasonID)
		if err != nil {
			return err
		}
		if !needsFingerprints(eps) {
			s.fillFromAniSkip(ctx, seasonID, false, &Report{SeasonID: seasonID})
			return nil
		}
	}
	_, err := s.DetectSeason(ctx, seasonID, false)
	return err
}

func (s *Service) due() map[string]pendingSeason {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := map[string]pendingSeason{}
	now := time.Now()
	for id, p := range s.pending {
		if now.Sub(p.at) >= seasonDebounce {
			ids[id] = p
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

// EnqueueSeason schedules a season: episodes never detected get the fingerprint pass,
// and AniSkip is asked about what it was not asked yet (a new MAL id).
func (s *Service) EnqueueSeason(seasonID string) { s.enqueue(seasonID, false) }

// RedetectSeason schedules a full pass over every episode of a season (admin refresh).
func (s *Service) RedetectSeason(seasonID string) { s.enqueue(seasonID, true) }

func (s *Service) enqueue(seasonID string, force bool) {
	s.mu.Lock()
	p := s.pending[seasonID]
	s.pending[seasonID] = pendingSeason{at: time.Now(), force: force || p.force}
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// SweepAll schedules every season with work left: an episode the detector never worked
// on (at this DetectorVersion), fingerprint results on a file now considered too short,
// or an episode missing an intro or outro that AniSkip was not yet asked about with the
// season's MAL id. An episode that simply has no intro or outro (a finale, a special, a
// show without an OP) does not bring its season back once both were tried.
func (s *Service) SweepAll(ctx context.Context) {
	ids, err := s.store.SeasonIDs(ctx)
	if err != nil {
		return
	}
	for _, id := range ids {
		eps, err := s.store.SeasonEpisodes(ctx, id)
		if err != nil || len(eps) == 0 {
			continue
		}
		if needsFingerprints(eps) {
			s.EnqueueSeason(id)
			continue
		}
		if s.aniskip == nil {
			continue
		}
		mal, err := s.store.ProviderID(ctx, id, "mal")
		if err != nil || mal == "" {
			continue
		}
		for _, e := range eps {
			if needsAniSkip(e, mal) {
				s.EnqueueSeason(id)
				break
			}
		}
	}
}

// needsFingerprints reports a season with an episode the detector has not worked on at
// this version, or one carrying fingerprint results from a file now considered too short
// (an earlier version matched identical test clips whole).
func needsFingerprints(eps []store.SeasonEpisode) bool {
	for _, e := range eps {
		if e.DetectVersion < DetectorVersion {
			return true
		}
		if float64(e.DurationMs)/1000 < MinEpisodeSec && hasOrigin(e.Segments, Origin) {
			return true
		}
	}
	return false
}

// needsAniSkip reports an episode AniSkip could still help: long enough, numbered,
// missing an intro or an outro, and not yet asked about with this MAL id.
func needsAniSkip(e store.SeasonEpisode, mal string) bool {
	if e.Index < 1 || float64(e.DurationMs)/1000 < MinEpisodeSec {
		return false
	}
	if hasType(e.Segments, "intro") && hasType(e.Segments, "outro") {
		return false
	}
	return !e.AniSkipAsked || e.AniSkipMal != mal
}

// Report is what one detection pass found, for logs and the admin endpoint.
type Report struct {
	SeasonID string          `json:"seasonId"`
	Episodes []EpisodeReport `json:"episodes"`
	Elapsed  string          `json:"elapsed"`
}

type EpisodeReport struct {
	ItemID      string               `json:"itemId"`
	Index       int                  `json:"index"`
	Found       []model.MediaSegment `json:"found"`
	Chapters    []model.MediaSegment `json:"chapters,omitempty"` // what the markers said, for comparison
	AniSkip     []model.MediaSegment `json:"aniskip,omitempty"`  // gaps filled from AniSkip
	MatchedWith int                  `json:"matchedWith,omitempty"`
	Note        string               `json:"note,omitempty"`
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
		// Nothing to compare a lone episode with; AniSkip may still know it.
		if !dry {
			s.markDetected(ctx, seasonID, eps, nil, nil)
		}
		s.fillFromAniSkip(ctx, seasonID, dry, rep)
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
			// A measured segment outranks a crowd-sourced one of the same type.
			if err := s.store.DeleteSegmentTypes(ctx, e.SourceID, AniSkipOrigin, segmentTypes(found)); err != nil {
				return rep, err
			}
			if err := s.store.ReplaceSegmentsByOrigin(ctx, e.SourceID, Origin, found); err != nil {
				return rep, err
			}
		}
	}
	if !dry {
		s.markDetected(ctx, seasonID, eps, heads, tails)
	}
	s.fillFromAniSkip(ctx, seasonID, dry, rep)
	rep.Elapsed = time.Since(started).String()
	slog.Info("segments detected", "season", seasonID, "episodes", len(eps), "took", rep.Elapsed, "dry", dry)
	return rep, nil
}

// bestNeighbour compares episode i's fingerprint with its direct neighbours (±1) and
// returns the longest valid shared run; the episodes two away (±2) are tried only when
// neither direct neighbour matched (or there is none: the first and last episode).
func bestNeighbour(fps [][]uint32, i int, minSec, maxSec float64) (match, int, bool) {
	var best match
	bestWith := -1
	try := func(d int) {
		j := i + d
		if j < 0 || j >= len(fps) || len(fps[i]) == 0 || len(fps[j]) == 0 {
			return
		}
		m, ok := longestCommon(fps[i], fps[j], MaxDiff, MaxGapSec, minSec)
		if !ok || m.length() > maxSec {
			return
		}
		if bestWith < 0 || m.length() > best.length() {
			best, bestWith = m, j
		}
	}
	try(1)
	try(-1)
	if bestWith < 0 {
		try(2)
		try(-2)
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

// fillFromAniSkip asks AniSkip for the episodes of a season that still miss an intro or
// an outro, and adds only the types they miss. It needs the season's own MAL id, which
// the metadata service stores when AniList and TMDB agree on the season.
func (s *Service) fillFromAniSkip(ctx context.Context, seasonID string, dry bool, rep *Report) {
	if s.aniskip == nil {
		return
	}
	mal, err := s.store.ProviderID(ctx, seasonID, "mal")
	if err != nil || mal == "" {
		return
	}
	eps, err := s.store.SeasonEpisodes(ctx, seasonID) // re-read: the fingerprint pass just wrote
	if err != nil {
		return
	}
	added, failures := 0, 0
	for _, e := range eps {
		dur := float64(e.DurationMs) / 1000
		// A dry run shows what AniSkip has now; a real one asks once per episode and MAL id.
		if !needsAniSkip(e, mal) && (!dry || e.Index < 1 || dur < MinEpisodeSec || (hasType(e.Segments, "intro") && hasType(e.Segments, "outro"))) {
			continue
		}
		if ctx.Err() != nil {
			return
		}
		segs, err := s.aniskip.Lookup(ctx, mal, e.Index, dur)
		if err != nil {
			// One episode it cannot answer for must not keep the rest of the season from
			// being asked about (it stays unasked, for the next pass). Several in a row:
			// AniSkip is down, so the pass stops asking.
			slog.Warn("aniskip lookup failed; trying again on the next pass", "season", seasonID, "mal", mal, "episode", e.Index, "err", err)
			if failures++; failures >= aniskipMaxFailures || ctx.Err() != nil {
				break
			}
			continue
		}
		failures = 0
		if !dry {
			if err := s.store.MarkAniSkipAsked(ctx, e.SourceID, mal); err != nil {
				slog.Warn("aniskip: recording the lookup", "item", e.ItemID, "err", err)
			}
		}
		var add []model.MediaSegment
		for _, sg := range segs {
			if !hasType(e.Segments, sg.Type) {
				add = append(add, sg)
			}
		}
		if len(add) == 0 {
			continue
		}
		added += len(add)
		rep.addAniSkip(e, add)
		if dry {
			continue
		}
		// Keep the AniSkip rows the episode already has and add the new ones.
		keep := add
		for _, sg := range e.Segments {
			if sg.Origin == AniSkipOrigin {
				keep = append(keep, sg)
			}
		}
		if err := s.store.ReplaceSegmentsByOrigin(ctx, e.SourceID, AniSkipOrigin, keep); err != nil {
			slog.Warn("aniskip: storing segments", "item", e.ItemID, "err", err)
		}
	}
	if added > 0 {
		slog.Info("segments from aniskip", "season", seasonID, "mal", mal, "added", added, "dry", dry)
	}
}

func (r *Report) addAniSkip(e store.SeasonEpisode, segs []model.MediaSegment) {
	for i := range r.Episodes {
		if r.Episodes[i].ItemID == e.ItemID {
			r.Episodes[i].AniSkip = append(r.Episodes[i].AniSkip, segs...)
			return
		}
	}
	r.Episodes = append(r.Episodes, EpisodeReport{ItemID: e.ItemID, Index: e.Index, AniSkip: segs})
}

func segmentTypes(segs []model.MediaSegment) []string {
	var out []string
	for _, s := range segs {
		out = append(out, s.Type)
	}
	return out
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

// markDetected records the pass on every episode it could fingerprint (heads/tails nil:
// a lone episode, nothing to fingerprint against) and on files too short to have an
// OP/ED. An episode whose fingerprint failed stays unmarked, so the next sweep retries
// it: but only maxFailedPasses times. A file that can never be fingerprinted (no sound,
// cut short) is then marked as well, or it would bring its whole season back on every
// sweep for good. A new file at that path starts over (the store drops the mark).
func (s *Service) markDetected(ctx context.Context, seasonID string, eps []store.SeasonEpisode, heads, tails [][]uint32) {
	for i, e := range eps {
		short := float64(e.DurationMs)/1000 < MinEpisodeSec
		if heads != nil && !short && (len(heads[i]) == 0 || len(tails[i]) == 0) {
			if ctx.Err() != nil {
				continue // interrupted (shutting down): says nothing about the file
			}
			if e.FailedPasses+1 < maxFailedPasses {
				if err := s.store.NoteFingerprintFailure(ctx, e.SourceID); err != nil {
					slog.Warn("segments: recording the failed pass", "item", e.ItemID, "err", err)
				}
				continue
			}
			slog.Warn("segments: no audio fingerprint after several passes; left alone until the file changes", "season", seasonID, "item", e.ItemID, "passes", e.FailedPasses+1)
		}
		if err := s.store.MarkDetected(ctx, e.SourceID, DetectorVersion); err != nil {
			slog.Warn("segments: recording the pass", "item", e.ItemID, "err", err)
		}
	}
}
