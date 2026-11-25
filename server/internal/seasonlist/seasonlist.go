// Package seasonlist is the order of a series: its season list in release order (a
// season, the OVAs that came out before the next one, the franchise's films, and the
// other specials at the foot), and from the same rule the order its episodes are watched
// in (order.go). One rule, so that what plays next is what the season list shows next.
package seasonlist

import (
	"regexp"
	"sort"
	"strconv"

	"github.com/roxy-media/roxy/server/internal/model"
)

// Section is one entry of a series' season list, in release order (Crunchyroll's): a
// season, the specials (OVAs, specials) that came out between two seasons, or a film of
// the franchise the library holds, which plays as the entry's one row.
type Section struct {
	Kind  string `json:"kind"` // season | specials | movie
	Title string `json:"title"`
	// SeasonID: the season whose episodes this entry shows (for specials, the Specials
	// season, narrowed to EpisodeIDs).
	SeasonID   string      `json:"seasonId,omitempty"`
	EpisodeIDs []string    `json:"episodeIds,omitempty"`
	Item       *model.Item `json:"item,omitempty"` // movie
	Episodes   int         `json:"episodes"`
	Year       int         `json:"year,omitempty"`
}

type Season struct {
	ID, Title string
	Index     int
	Start     string // the earliest air date of its episodes ("" unknown)
	Count     int
}

type Special struct {
	ID, Title, Date string
	Virtual         bool  // a placeholder: not in the library
	RuntimeMs       int64 // 0 unknown
}

type Movie struct {
	Item model.Item
	Date string
}

// What a special is by its length, measured against the show's typical episode (the
// median of its regular episodes' lengths): TV shows run from 11 minutes (SpongeBob's
// segments) to 90 (Sherlock), and their specials over an hour (a Christmas special) are
// episodes, not films.
const (
	filmMin       = 60 * 60 * 1000 // a film is at least an hour...
	filmTimes     = 2.5            // ...and 2.5 times the show's episode (KonoSuba 90/24, JJK 0 105/24)
	episodeMax    = 15 * 60 * 1000 // episode-length: half the show's episode, at most 15 min...
	episodeMin    = 5 * 60 * 1000  // ...and at least 5 (DxD's 3-minute shorts are extras)
	episodeNoNorm = 15 * 60 * 1000 // with no regular episode to measure against
)

// IsFilm: a special that is a film of its own (an entry like the franchise's films). Never
// when the show's episode length is unknown: a long special of a show is far more often
// an episode than a film.
func IsFilm(runtime, typical int64) bool {
	return typical > 0 && runtime >= filmMin && float64(runtime) >= filmTimes*float64(typical)
}

// EpisodeLength is how long a special must be to count as an episode of the story.
func EpisodeLength(typical int64) int64 {
	if typical <= 0 {
		return episodeNoNorm
	}
	return min(max(typical/2, episodeMin), episodeMax)
}

// TypicalRuntime is the median length of a series' regular episodes with a file, 0 when
// none has one.
func TypicalRuntime(eps []model.Item, regular map[string]bool) int64 {
	var rs []int64
	for _, e := range eps {
		if !e.IsVirtual && e.ParentID != nil && regular[*e.ParentID] && e.RuntimeMs != nil && *e.RuntimeMs > 0 {
			rs = append(rs, *e.RuntimeMs)
		}
	}
	return median(rs)
}

// median is the middle of some lengths (the upper of two middles), 0 of none.
func median(rs []int64) int64 {
	if len(rs) == 0 {
		return 0
	}
	sort.Slice(rs, func(i, j int) bool { return rs[i] < rs[j] })
	return rs[len(rs)/2]
}

var reOVA = regexp.MustCompile(`(?i)\bOVA\b|\bOAD\b`)

// reNotStory: long specials that tell no new story (a recap is episode-length too).
var reNotStory = regexp.MustCompile(`(?i)\b(recaps?|digest|compilation|summary|picture drama|music video|live)\b`)

// IsEpisode: named an OVA, or episode-length and not a recap. An unknown length (0) is
// not episode-length: such a special goes to the bottom unless named an OVA.
func (sp Special) IsEpisode(minLength int64) bool {
	if reOVA.MatchString(sp.Title) {
		return true
	}
	return sp.RuntimeMs > 0 && sp.RuntimeMs >= minLength && !reNotStory.MatchString(sp.Title)
}

// gapOf is where something of a date sits among a series' seasons (in their numbers'
// order): -1 before the first season, i after seasons[i] and before the one after it. A
// season counts from the day its first episode came out.
func gapOf(seasons []Season, date string) int {
	at := -1
	for i, s := range seasons {
		if s.Start != "" && s.Start <= date {
			at = i
		}
	}
	return at
}

// Where a special goes in the season list, and so in the order of watching (order.go).
const (
	betweenSeasons = iota // an OVA, or a special of an episode's length: in the gap of its date
	atFoot                // short, undated or a recap: one of the "Specials" at the foot
	notHeld               // a placeholder (the library has no file): at the foot, after the others
)

func (sp Special) where(typical int64) int {
	switch {
	case sp.Virtual:
		return notHeld
	case len(sp.Date) < 10 || !sp.IsEpisode(EpisodeLength(typical)):
		return atFoot
	}
	return betweenSeasons
}

// Build orders a series' season list (Crunchyroll's): each season in its
// number's order, and after it the OVAs (episode-length specials) and films that came out
// before the next season started (before the first season, what came out before it).
// OVAs in the same gap form one entry, apart from an episode-length special not named an
// OVA ("Hero Episode 0"); each film is an entry of its own. Every other special (short,
// undated, or not in the library) is in one "Specials" entry at the bottom: the ones the
// library has first, by date, then the missing ones.
// typical: the show's episode length (TypicalRuntime), 0 unknown.
func Build(seasons []Season, specials []Special, movies []Movie, typical int64) []Section {
	sort.SliceStable(seasons, func(i, j int) bool { return seasons[i].Index < seasons[j].Index })
	slot := func(date string) int { return gapOf(seasons, date) }
	type placed struct {
		date    string
		special *Special
		movie   *Movie
	}
	gaps := map[int][]placed{}
	var bottom, missing []Special
	var undatedMovies []Movie
	for i := range specials {
		sp := specials[i]
		switch sp.where(typical) {
		case notHeld:
			missing = append(missing, sp)
		case atFoot:
			bottom = append(bottom, sp)
		default:
			gaps[slot(sp.Date)] = append(gaps[slot(sp.Date)], placed{date: sp.Date[:10], special: &specials[i]})
		}
	}
	for i := range movies {
		m := movies[i]
		if len(m.Date) < 4 {
			undatedMovies = append(undatedMovies, m)
			continue
		}
		gaps[slot(m.Date)] = append(gaps[slot(m.Date)], placed{date: m.Date, movie: &movies[i]})
	}
	var out []Section
	emit := func(g int) {
		ps := gaps[g]
		sort.SliceStable(ps, func(i, j int) bool { return ps[i].date < ps[j].date })
		var run []Special
		flush := func() {
			if len(run) > 0 {
				out = append(out, ovaSection(run))
				run = nil
			}
		}
		for _, p := range ps {
			if p.special != nil {
				if len(run) > 0 && reOVA.MatchString(run[0].Title) != reOVA.MatchString(p.special.Title) {
					flush()
				}
				run = append(run, *p.special)
				continue
			}
			flush()
			it := p.movie.Item
			out = append(out, Section{Kind: "movie", Title: it.Title, Item: &it, Episodes: 1, Year: yearOf(p.movie.Date)})
		}
		flush()
	}
	emit(-1)
	for i, s := range seasons {
		title := s.Title
		if title == "" {
			title = "Season " + strconv.Itoa(s.Index)
		}
		out = append(out, Section{Kind: "season", Title: title, SeasonID: s.ID, Episodes: s.Count, Year: yearOf(s.Start)})
		emit(i)
	}
	if len(bottom)+len(missing) > 0 {
		// Dated first, by date; undated after, in their own order.
		sort.SliceStable(bottom, func(i, j int) bool {
			di, dj := len(bottom[i].Date) >= 10, len(bottom[j].Date) >= 10
			if di != dj {
				return di
			}
			return di && bottom[i].Date < bottom[j].Date
		})
		all := append(bottom, missing...)
		ids := make([]string, len(all))
		for i, sp := range all {
			ids[i] = sp.ID
		}
		out = append(out, Section{Kind: "specials", Title: "Specials", EpisodeIDs: ids, Episodes: len(all)})
	}
	for _, m := range undatedMovies {
		it := m.Item
		out = append(out, Section{Kind: "movie", Title: it.Title, Item: &it, Episodes: 1})
	}
	return out
}

// ovaSection names the OVAs of one gap: one by its own title, several "OVAs".
func ovaSection(run []Special) Section {
	ids := make([]string, len(run))
	for i, sp := range run {
		ids[i] = sp.ID
	}
	title := "OVAs"
	if len(run) == 1 {
		title = run[0].Title
	}
	return Section{Kind: "specials", Title: title, EpisodeIDs: ids, Episodes: len(run), Year: yearOf(run[0].Date)}
}

func yearOf(date string) int {
	if len(date) < 4 {
		return 0
	}
	y, _ := strconv.Atoi(date[:4])
	return y
}
