package discover

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/roxy-media/roxy/server/internal/arr"
)

// Season packs Sonarr cannot parse (see arr/packs.go). Two moments:
//
//  1. Before Sonarr searches a finished season Roxy asked for: a pack named "(Season 1)"
//     that beats every pack Sonarr can see by itself is pushed, and that season's search
//     is skipped. Otherwise Sonarr searches as always.
//  2. Twice a day, for finished seasons Roxy asked for whose episodes all came in below
//     the profile's custom format cutoff (sub-only, say): the same look, against what was
//     grabbed. Anime dubs often appear weeks after the subs, and Sonarr never goes back
//     to look for them; this does, once a day per season at most.
//
// Seasons still airing are left alone: their episodes come one by one, and packs only
// exist once a season is finished. Sonarr decides every push (size, banned codecs, its
// upgrade rules); Roxy only finds what it could not read.

type packState struct {
	mu      sync.Mutex
	checked map[string]time.Time // "series:season" -> last sweep look
}

const (
	packSweepEvery = 12 * time.Hour
	packRecheck    = 24 * time.Hour
	packMaxPushes  = 3 // candidates tried per season (Sonarr may refuse the best)
)

// packSweepFirst: the first sweep comes this long after start, not a whole
// packSweepEvery later: a server restarted more often than that never swept at all.
// (The once-a-day limit per season is saved, so restarts do not repeat looks.)
var packSweepFirst = 2 * time.Minute

// packFinder is the hook arr.Client calls before each season search.
func (s *Service) packFinder(c *arr.Client) func(ctx context.Context, seriesID, season int) bool {
	return func(ctx context.Context, seriesID, season int) bool {
		pushed, err := s.findPack(ctx, c, seriesID, season, nil)
		if err != nil {
			slog.Info("packs: look failed; Sonarr searches as usual", "series", seriesID, "season", season, "err", err)
		}
		return pushed
	}
}

// findPack looks for a season pack Sonarr could not parse and pushes the best one that
// beats both Sonarr's own best approved pack and, when given, what was grabbed already
// (floor). True when Sonarr took one.
func (s *Service) findPack(ctx context.Context, c *arr.Client, seriesID, season int, floor *int) (bool, error) {
	p := s.prowlarr
	if p == nil || season <= 0 {
		return false, nil
	}
	eps, err := c.Episodes(ctx, seriesID)
	if err != nil {
		return false, err
	}
	if !seasonFinished(eps, season, time.Now()) {
		return false, nil
	}
	title, alts, err := c.SeriesNames(ctx, seriesID)
	if err != nil {
		return false, err
	}
	type cand struct {
		name  string
		rel   arr.IndexerRelease
		score int
	}
	var cands []cand
	seen := map[string]bool{}
	for _, q := range searchNames(title, alts) {
		rels, err := p.Search(ctx, q)
		if err != nil {
			return false, err
		}
		for _, r := range rels {
			name, n, ok := arr.SeasonWordRewrite(r.Title)
			if !ok || n != season || seen[name] {
				continue
			}
			seen[name] = true
			parsed, err := c.Parse(ctx, name)
			if err != nil || parsed.SeriesID != seriesID || !parsed.FullSeason || parsed.Season != season {
				continue
			}
			cands = append(cands, cand{name: name, rel: r, score: parsed.Score})
		}
	}
	if len(cands) == 0 {
		return false, nil
	}
	// What Sonarr would take by itself: its best approved pack, or episode by episode
	// (as good as its weakest episode), whichever is better. A pack must beat that, so a
	// sub-only pack never displaces dubbed single episodes.
	// Without Sonarr's own search there is no bar: nothing is pushed (a failed search
	// used to leave the bar at "nothing", so any pack with seeders won, sub-only included,
	// and the caller then skipped Sonarr's search for the season).
	bar, barFrom := -1<<31, "nothing"
	own, err := c.SeasonReleases(ctx, seriesID, season)
	if err != nil {
		return false, fmt.Errorf("sonarr's own search of the season: %w", err)
	}
	pack, packFrom, single := ownBest(own, episodeNumbers(eps, season))
	if pack > bar {
		bar, barFrom = pack, packFrom
	}
	if single > bar {
		bar, barFrom = single, "single episodes"
	}
	if floor != nil && *floor > bar {
		bar, barFrom = *floor, "what was grabbed"
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].score != cands[j].score {
			return cands[i].score > cands[j].score
		}
		return cands[i].rel.Seeders > cands[j].rel.Seeders
	})
	tried := 0
	for _, cd := range cands {
		if cd.score <= bar || tried >= packMaxPushes {
			break
		}
		if cd.rel.Seeders == 0 {
			continue
		}
		tried++
		ok, why, err := c.PushRelease(ctx, cd.name, cd.rel)
		if err != nil {
			return false, err
		}
		if ok {
			slog.Info("packs: pushed a season pack Sonarr could not read", "series", title, "season", season, "release", cd.rel.Title, "score", cd.score, "beat", barFrom, "barScore", bar)
			return true, nil
		}
		slog.Info("packs: Sonarr refused a candidate", "series", title, "season", season, "release", cd.rel.Title, "why", strings.Join(why, "; "))
	}
	return false, nil
}

// ownBest: from Sonarr's own search of a season, the best approved pack (score, title)
// and the episode-by-episode score: the weakest episode's best approved release, or the
// lowest possible score when some episode has none.
func ownBest(rels []arr.SeasonRelease, episodes []int) (int, string, int) {
	pack, packFrom := -1<<31, ""
	best := map[int]int{}
	for _, r := range rels {
		if !r.Approved {
			continue
		}
		if r.FullSeason {
			if r.Score > pack {
				pack, packFrom = r.Score, r.Title
			}
			continue
		}
		for _, n := range r.Episodes {
			if cur, ok := best[n]; !ok || r.Score > cur {
				best[n] = r.Score
			}
		}
	}
	single := -1 << 31
	for i, n := range episodes {
		sc, ok := best[n]
		if !ok {
			return pack, packFrom, -1 << 31
		}
		if i == 0 || sc < single {
			single = sc
		}
	}
	return pack, packFrom, single
}

// episodeNumbers: a season's episode numbers.
func episodeNumbers(eps []arr.Episode, season int) []int {
	var out []int
	for _, e := range eps {
		if e.SeasonNumber == season {
			out = append(out, e.EpisodeNumber)
		}
	}
	return out
}

// seasonFinished: the season has episodes and every one has aired.
func seasonFinished(eps []arr.Episode, season int, now time.Time) bool {
	n := 0
	for _, e := range eps {
		if e.SeasonNumber != season {
			continue
		}
		n++
		if !e.Aired(now) {
			return false
		}
	}
	return n > 0
}

// searchNames: the title, and the first alternate title that differs (often the romaji
// name fansub groups use).
func searchNames(title string, alts []string) []string {
	out := []string{title}
	for _, a := range alts {
		if a != "" && !strings.EqualFold(a, title) {
			return append(out, a)
		}
	}
	return out
}

// startPackSweep runs the later-dub look (moment 2) every packSweepEvery.
func (s *Service) startPackSweep(ctx context.Context) {
	if s.prowlarr == nil {
		return
	}
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(packSweepFirst):
			s.packSweep(ctx)
		}
		t := time.NewTicker(packSweepEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.packSweep(ctx)
			}
		}
	}()
}

// packLooked is when the sweep last looked at a season (zero: never). Saved as a
// setting besides the map, so it outlives a restart.
func (s *Service) packLooked(ctx context.Context, key string) time.Time {
	s.packs.mu.Lock()
	defer s.packs.mu.Unlock()
	if s.packs.checked == nil {
		s.packs.checked = map[string]time.Time{}
	}
	if at, ok := s.packs.checked[key]; ok {
		return at
	}
	var at time.Time
	if s.store != nil {
		_, _ = s.store.GetSetting(ctx, packLookedSetting+key, &at)
	}
	s.packs.checked[key] = at
	return at
}

func (s *Service) setPackLooked(ctx context.Context, key string, at time.Time) {
	s.packs.mu.Lock()
	if s.packs.checked == nil {
		s.packs.checked = map[string]time.Time{}
	}
	s.packs.checked[key] = at
	s.packs.mu.Unlock()
	if s.store != nil {
		if err := s.store.SetSetting(ctx, packLookedSetting+key, at); err != nil {
			slog.Warn("packs: saving the time of a look", "season", key, "err", err)
		}
	}
}

const packLookedSetting = "packsweep:"

func (s *Service) packSweep(ctx context.Context) {
	sonarr, _ := s.clients()
	if sonarr == nil {
		return
	}
	all, err := s.store.ListRequests(ctx)
	if err != nil {
		return
	}
	now := time.Now()
	for i := range all {
		r := &all[i]
		if r.MediaType != "tv" || r.Status != "sent" || r.ArrID == 0 {
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		s.sweepRequest(cctx, sonarr, r.ArrID, func(e arr.Episode) bool { return inScope(r, e) }, now)
		cancel()
	}
}

// sweepRequest looks once per season (per packRecheck) for a better pack for finished
// seasons whose every episode has a file and scored below the profile's cutoff.
func (s *Service) sweepRequest(ctx context.Context, c *arr.Client, seriesID int, scope func(arr.Episode) bool, now time.Time) {
	eps, err := c.Episodes(ctx, seriesID)
	if err != nil {
		return
	}
	history, _ := c.History(ctx, seriesID)
	grabbed := latestGrabScores(history)
	cutoff, err := c.CutoffFormatScore(ctx, seriesID)
	if err != nil {
		return
	}
	bySeason := map[int][]arr.Episode{}
	for _, e := range eps {
		if e.SeasonNumber > 0 && scope(e) {
			bySeason[e.SeasonNumber] = append(bySeason[e.SeasonNumber], e)
		}
	}
	for season, list := range bySeason {
		floor, ok := seasonFloor(list, grabbed, now)
		if !ok || floor >= cutoff {
			continue // airing, missing files (the tracker handles those), or good enough
		}
		key := keyOf(seriesID, season)
		if now.Sub(s.packLooked(ctx, key)) < packRecheck {
			continue
		}
		pushed, err := s.findPack(ctx, c, seriesID, season, &floor)
		if err != nil {
			// Not counted as a look: the next sweep tries again (it used to cost the
			// season a day).
			slog.Info("packs: sweep look failed", "series", seriesID, "season", season, "err", err)
			continue
		}
		s.setPackLooked(ctx, key, now)
		if pushed {
			s.KickDownloads()
		}
	}
}

// latestGrabScores: per episode, the custom format score of the newest grab (a file's
// own name may not carry the tags its release had, so the file's score can read 0).
func latestGrabScores(history []arr.HistoryRecord) map[int]int {
	out := map[int]int{}
	when := map[int]time.Time{}
	for _, h := range history {
		if h.EventType != "grabbed" {
			continue
		}
		if t, ok := when[h.EpisodeID]; !ok || h.Date.After(t) {
			when[h.EpisodeID], out[h.EpisodeID] = h.Date, h.CustomFormatScore
		}
	}
	return out
}

// seasonFloor: the lowest grabbed score in a finished season whose episodes all have a
// file; ok is false otherwise.
func seasonFloor(list []arr.Episode, grabbed map[int]int, now time.Time) (int, bool) {
	floor, any := 0, false
	for _, e := range list {
		if !e.Aired(now) || !e.HasFile {
			return 0, false
		}
		sc, ok := grabbed[e.ID]
		if !ok {
			return 0, false // imported by hand or before history: leave it alone
		}
		if !any || sc < floor {
			floor, any = sc, true
		}
	}
	return floor, any
}

func keyOf(seriesID, season int) string {
	return strings.Join([]string{itoa(seriesID), itoa(season)}, ":")
}

func itoa(n int) string { return strconv.Itoa(n) }
