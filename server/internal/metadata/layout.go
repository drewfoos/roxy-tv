package metadata

import (
	"context"
	"sort"

	"github.com/roxy-media/roxy/server/internal/model"
)

// Season layouts: how a library's seasons line up with a provider's.
//
// Usually they are the same (Sonarr's TVDB-style folders, TMDB's seasons): library
// season N is provider season N. Some shows are split differently: TMDB lists JUJUTSU
// KAISEN as one season of 59 episodes while the folders hold S1 (24), S2 (23) and S3
// (12); InuYasha is 167 + 26 on TMDB and AniList, seven seasons on disk. Then the
// library's own seasons are the truth, and provider episodes are found by absolute
// position: library S2E1 is provider episode 25 (S1's 24 real episodes come first).
//
// A layout is "split" only when the library has a real season the provider does not list
// at all, its regular seasons run 1..n without a gap (a missing season has no length to
// count past), and their episodes add up to no more than the provider knows (plus one per
// season, for an OVA filed as a season's last episode). A season holding one episode more
// than the provider counts (KonoSuba's OVA as S01E11 next to TMDB's 10) is never a
// reason to split.

// seasonLayout maps library (season, episode) onto one provider's numbering.
type seasonLayout struct {
	split bool
	start map[int]int // library season -> absolute provider position of its episode 1
	last  int         // the last real regular library season
	// tail is how many provider episodes follow the last real season's last episode
	// inside the same provider season (missing or unaired episodes it may show).
	tail int
	// beyond maps library season numbers after last onto the later provider seasons
	// (placeholders for seasons the library does not have yet).
	beyond map[int]int
	// shift: in a 1:1 layout, library season -> how far the provider's episode numbers
	// run ahead of the files' (High School DxD Hero: TMDB counts "Episode 0" as E1, so
	// the files' E1 is TMDB's E2). Found by air dates (dateShifts); usually empty.
	shift map[int]int
}

// realSeasonLengths is, per regular library season with at least one real file, the
// highest real episode number in it (absolute-only files count by their number, a
// multi-episode file by its last episode).
func (s *Service) realSeasonLengths(ctx context.Context, seasons []model.Item) map[int]int {
	out := map[int]int{}
	for _, se := range seasons {
		if se.IndexAired == nil || *se.IndexAired < 1 {
			continue
		}
		eps, err := s.store.Children(ctx, se.ID, model.TypeEpisode)
		if err != nil {
			continue
		}
		top := 0
		for _, e := range eps {
			if e.IsVirtual {
				continue
			}
			n := 0
			switch {
			case e.IndexAiredEnd != nil:
				n = *e.IndexAiredEnd // a multi-episode file runs to its last episode
			case e.IndexAired != nil:
				n = *e.IndexAired
			case e.IndexAbsolute != nil:
				n = *e.IndexAbsolute
			}
			if n > top {
				top = n
			}
		}
		if top > 0 {
			out[*se.IndexAired] = top
		}
	}
	return out
}

// layoutFor works out how the library's seasons (lengths, from realSeasonLengths) map
// onto r's. It never returns nil for a non-nil r; a non-split layout maps 1:1.
func layoutFor(r *Result, lengths map[int]int) *seasonLayout {
	lay := &seasonLayout{}
	if r == nil {
		return lay
	}
	listed := map[int]bool{}
	total := 0
	for _, si := range r.Seasons {
		if si.Number > 0 {
			listed[si.Number] = true
			total += si.EpisodeCount
		}
	}
	if len(listed) == 0 {
		return lay
	}
	var nums []int
	for n := range lengths {
		if n > 0 {
			nums = append(nums, n)
		}
	}
	sort.Ints(nums)
	extra, sum := false, 0
	for i, n := range nums {
		if n != i+1 {
			return lay // a gap: the missing season's length is unknown
		}
		if !listed[n] {
			extra = true
		}
		sum += lengths[n]
	}
	if !extra || sum > total+len(nums) {
		return lay
	}
	lay.split = true
	lay.start = map[int]int{}
	lay.beyond = map[int]int{}
	abs := 1
	for _, n := range nums {
		lay.start[n] = abs
		abs += lengths[n]
		lay.last = n
	}
	// What the provider has after the library's last real episode.
	ps, pe, ok := r.AbsoluteToAired(abs - 1)
	if !ok {
		return lay
	}
	if si := seasonInfo(r, ps); si != nil {
		lay.tail = si.EpisodeCount - pe
	}
	next := lay.last
	for _, si := range r.Seasons {
		if si.Number > ps {
			next++
			lay.beyond[next] = si.Number
		}
	}
	return lay
}

// toProvider maps library (season, episode) onto r's numbering. Specials and 1:1
// layouts map to themselves; ok is false when r has no such episode.
func (l *seasonLayout) toProvider(r *Result, season, episode int) (int, int, bool) {
	if l == nil || season == 0 {
		return season, episode, true
	}
	if !l.split {
		if d := l.shift[season]; d != 0 {
			if episode+d < 1 {
				return 0, 0, false
			}
			return season, episode + d, true
		}
		return season, episode, true
	}
	if st, ok := l.start[season]; ok {
		return r.AbsoluteToAired(st + episode - 1)
	}
	if ps, ok := l.beyond[season]; ok {
		return ps, episode, true
	}
	return 0, 0, false
}

// wholeSeason reports the provider season a library season corresponds to one-to-one in
// a split layout: its first episode is the provider season's first, and its length
// covers the provider season (one episode either way allowed). ok is false otherwise.
func (l *seasonLayout) wholeSeason(r *Result, season, length int) (int, bool) {
	ps, pe, ok := l.toProvider(r, season, 1)
	if !ok || pe != 1 {
		return 0, false
	}
	si := seasonInfo(r, ps)
	if si == nil || si.EpisodeCount == 0 {
		return 0, false
	}
	d := length - si.EpisodeCount
	if d < -1 || d > 1 {
		return 0, false
	}
	if length <= si.EpisodeCount {
		if ps2, _, ok := l.toProvider(r, season, length); !ok || ps2 != ps {
			return 0, false
		}
	}
	return ps, true
}

// posterSeason is the provider season whose poster fits a library season: the same
// number in a 1:1 layout; in a split one only a season the library season covers whole
// (or one after the last real season), never the poster of a season it is a part of.
func (l *seasonLayout) posterSeason(r *Result, season, length int) (int, bool) {
	if l == nil || !l.split || season == 0 {
		return season, true
	}
	if ps, ok := l.beyond[season]; ok {
		return ps, true
	}
	if length == 0 {
		return 0, false
	}
	return l.wholeSeason(r, season, length)
}

// dateShifts finds, per regular library season, how far r's episode numbers run ahead of
// the files' when they do not line up, by air date: files hold numbers in TheTVDB's
// order (Sonarr's), and Sonarr's date for a file's number is compared with r's dates. A
// season shifts by d only when r's episode n+d aired on Sonarr's date for n for most of
// the season's dated files (at least two) and more often than unshifted, so a season the
// two agree on, or whose dates differ by a time zone, stays as it is. ens: per library
// season, the real episode numbers.
func dateShifts(r *Result, fetch func(*Result, int) *Season, ens map[int][]int, sonarr map[[2]int]string) map[int]int {
	out := map[int]int{}
	for season, nums := range ens {
		if season < 1 || len(nums) == 0 {
			continue
		}
		se := fetch(r, season)
		if se == nil {
			continue
		}
		aired := map[int]string{}
		for _, e := range se.Episodes {
			if len(e.AirDate) >= 10 {
				aired[e.Episode] = e.AirDate[:10]
			}
		}
		dated, score := 0, map[int]int{}
		for _, n := range nums {
			want := sonarr[[2]int{season, n}]
			if len(want) < 10 {
				continue
			}
			dated++
			for d := -2; d <= 2; d++ {
				if aired[n+d] == want[:10] {
					score[d]++
				}
			}
		}
		best := 0
		for d := -2; d <= 2; d++ {
			if d != 0 && score[d] > score[best] {
				best = d
			}
		}
		if best != 0 && score[best] >= 2 && 2*score[best] > dated && score[best] > score[0] {
			out[season] = best
		}
	}
	return out
}
