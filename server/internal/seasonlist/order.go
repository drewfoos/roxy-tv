package seasonlist

import (
	"sort"
	"time"
)

// The order a series is watched in: its season list read from the top. Season 1's
// episodes, the OVAs that came out before season 2, season 2, and so on; the "Specials"
// at the foot of the list (shorts, recaps, undated ones) come after everything and are
// extras, which nothing offers as "next" unless one of them is being watched.
//
// It is the same rule as the list's (Build): a special's place is found by where(), a
// gap by gapOf(), a film-length special by IsFilm(). Before this, what played next went
// by the seasons' numbers alone, specials last: after a season's last episode came the
// next season's first, past the OVAs the season list shows between the two.
//
// A special whose file says where it belongs (its .nfo: before an episode, before or
// after a season) is put exactly there, whatever its date.

// Unnumbered is the number of a season that has none: it comes after the numbered ones.
const Unnumbered = 1 << 30

// Episode is what the order needs to know of one episode.
type Episode struct {
	ID       string
	SeasonID string
	Season   int // the season's number; 0 for the specials; Unnumbered for a season without one
	Number   int // the episode's number in its season
	Title    string
	Date     string // when it came out (YYYY-MM-DD), "" unknown
	// RuntimeMs: its length, 0 unknown.
	RuntimeMs int64
	// Virtual: a placeholder for an episode the library has no file of.
	Virtual bool
	// A special's place as its file gives it: before episode BeforeEpisode of season
	// BeforeSeason (no episode: before the season), or after season AfterSeason.
	BeforeSeason, BeforeEpisode, AfterSeason *int
}

// Key is an episode's place in the order its series is watched in.
type Key struct {
	// Order sorts the series' episodes into that order. It says where the episode
	// belongs (which season or which gap between two, and where in it), not how many
	// come before it: an episode added to the library leaves the others' numbers as they
	// were, so a number kept from an earlier look (a download's) still sorts rightly
	// among newer ones.
	Order int64
	// Extra: one of the specials at the foot of the season list. In the order it comes
	// after everything else; it is not what follows the last episode.
	Extra bool
}

// Where in a season or a gap (the low half of Order). A season's episode n is at
// n*perEpisode, which leaves room before it for specials placed before that episode.
const (
	perEpisode  = 1 << 10
	maxEpisode  = 1 << 20
	afterSeason = 0x7fff0000 // specials placed after a season: past its last episode
	perDay      = 1 << 12    // in a gap: by the day it came out, then by its number
	undatedAt   = 0x60000000 // at the foot: the undated ones after the dated
	notHeldAt   = 0x70000000 // and the ones the library does not hold after those
)

var day0 = time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)

// dayOf counts a date's days, for sorting by it; 0 for one that cannot be read.
func dayOf(date string) int64 {
	if len(date) < 4 {
		return 0
	}
	full := date
	switch {
	case len(date) >= 10:
		full = date[:10]
	case len(date) >= 7:
		full = date[:7] + "-01"
	default:
		full = date[:4] + "-01-01"
	}
	t, err := time.Parse("2006-01-02", full)
	if err != nil || t.Before(day0) {
		return 0
	}
	return int64(t.Sub(day0) / (24 * time.Hour))
}

func clamp(n, most int) int64 {
	if n < 0 {
		return 0
	}
	if n > most {
		return int64(most)
	}
	return int64(n)
}

// Keys gives every episode of a series (all of them, placeholders included: a season
// starts when its first episode came out, held or not) its place in the order of
// watching.
func Keys(eps []Episode) map[string]Key {
	// The seasons, in their numbers' order, each from its first episode's day.
	at := map[string]int{}
	var seasons []Season
	var lengths []int64
	for _, e := range eps {
		if e.Season == 0 {
			continue
		}
		i, ok := at[e.SeasonID]
		if !ok {
			i = len(seasons)
			at[e.SeasonID] = i
			seasons = append(seasons, Season{ID: e.SeasonID, Index: e.Season})
		}
		if len(e.Date) >= 10 && (seasons[i].Start == "" || e.Date[:10] < seasons[i].Start) {
			seasons[i].Start = e.Date[:10]
		}
		if !e.Virtual && e.RuntimeMs > 0 {
			lengths = append(lengths, e.RuntimeMs)
		}
	}
	sort.SliceStable(seasons, func(i, j int) bool {
		if seasons[i].Index != seasons[j].Index {
			return seasons[i].Index < seasons[j].Index
		}
		return seasons[i].ID < seasons[j].ID
	})
	place := map[string]int{} // season id -> its place among the seasons
	number := map[int]int{}   // season number -> the same (the first of that number)
	for i, s := range seasons {
		place[s.ID] = i
		if _, ok := number[s.Index]; !ok {
			number[s.Index] = i
		}
	}
	typical := median(lengths)

	// High half of Order: the gap before the first season is 1, season i is 2i+2, the
	// gap after it 2i+3; then the foot, then film-length specials without a date.
	season := func(i int) int64 { return int64(2*i + 2) }
	gap := func(i int) int64 { return int64(2*i + 3) }
	foot := gap(len(seasons)-1) + 1
	key := func(high, low int64, extra bool) Key { return Key{Order: high<<32 | low, Extra: extra} }

	out := make(map[string]Key, len(eps))
	for _, e := range eps {
		if e.Season != 0 {
			out[e.ID] = key(season(place[e.SeasonID]), clamp(e.Number, maxEpisode)*perEpisode, false)
			continue
		}
		n := e.Number
		// Where its file says, when it says so and that season is there.
		if !e.Virtual {
			if e.BeforeSeason != nil && *e.BeforeSeason > 0 {
				if i, ok := number[*e.BeforeSeason]; ok {
					ep := 1
					if e.BeforeEpisode != nil && *e.BeforeEpisode > 0 {
						ep = *e.BeforeEpisode
					}
					out[e.ID] = key(season(i), (clamp(ep, maxEpisode)-1)*perEpisode+1+clamp(n, perEpisode-3), false)
					continue
				}
			} else if e.AfterSeason != nil && *e.AfterSeason > 0 {
				if i, ok := number[*e.AfterSeason]; ok {
					out[e.ID] = key(season(i), afterSeason+clamp(n, 0xffff), false)
					continue
				}
			}
		}
		// A special as long as a film is an entry of its own, at its date (Build is
		// handed these as films).
		if !e.Virtual && IsFilm(e.RuntimeMs, typical) {
			if len(e.Date) < 4 {
				out[e.ID] = key(foot+1, clamp(n, 0xffffff), true)
			} else {
				out[e.ID] = key(gap(gapOf(seasons, e.Date)), dayOf(e.Date)*perDay+clamp(n, perDay-1), false)
			}
			continue
		}
		sp := Special{ID: e.ID, Title: e.Title, Date: e.Date, Virtual: e.Virtual, RuntimeMs: e.RuntimeMs}
		switch sp.where(typical) {
		case notHeld:
			out[e.ID] = key(foot, notHeldAt+clamp(n, 0xffffff), true)
		case atFoot:
			if len(e.Date) >= 10 {
				out[e.ID] = key(foot, dayOf(e.Date)*perDay+clamp(n, perDay-1), true)
			} else {
				out[e.ID] = key(foot, undatedAt+clamp(n, 0xffffff), true)
			}
		default:
			out[e.ID] = key(gap(gapOf(seasons, e.Date)), dayOf(e.Date)*perDay+clamp(n, perDay-1), false)
		}
	}
	return out
}
