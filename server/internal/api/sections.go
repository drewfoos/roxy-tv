package api

import (
	"net/http"
	"regexp"
	"sort"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/roxy-media/roxy/server/internal/model"
)

// section is one entry of a series' season list, in release order (Crunchyroll's): a
// season, the specials (OVAs, specials) that came out between two seasons, or a film of
// the franchise the library holds, which plays as the entry's one row.
type section struct {
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

type seasonIn struct {
	ID, Title string
	Index     int
	Start     string // the earliest air date of its episodes ("" unknown)
	Count     int
}

type specialIn struct {
	ID, Title, Date string
	Virtual         bool  // a placeholder: not in the library
	RuntimeMs       int64 // 0 unknown
}

type movieIn struct {
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

// isFilm: a special that is a film of its own (an entry like the franchise's films). Never
// when the show's episode length is unknown: a long special of a show is far more often
// an episode than a film.
func isFilm(runtime, typical int64) bool {
	return typical > 0 && runtime >= filmMin && float64(runtime) >= filmTimes*float64(typical)
}

// episodeLength is how long a special must be to count as an episode of the story.
func episodeLength(typical int64) int64 {
	if typical <= 0 {
		return episodeNoNorm
	}
	return min(max(typical/2, episodeMin), episodeMax)
}

// typicalRuntime is the median length of a series' regular episodes with a file, 0 when
// none has one.
func typicalRuntime(eps []model.Item, regular map[string]bool) int64 {
	var rs []int64
	for _, e := range eps {
		if !e.IsVirtual && e.ParentID != nil && regular[*e.ParentID] && e.RuntimeMs != nil && *e.RuntimeMs > 0 {
			rs = append(rs, *e.RuntimeMs)
		}
	}
	if len(rs) == 0 {
		return 0
	}
	sort.Slice(rs, func(i, j int) bool { return rs[i] < rs[j] })
	return rs[len(rs)/2]
}

var reOVA = regexp.MustCompile(`(?i)\bOVA\b|\bOAD\b`)

// reNotStory: long specials that tell no new story (a recap is episode-length too).
var reNotStory = regexp.MustCompile(`(?i)\b(recaps?|digest|compilation|summary|picture drama|music video|live)\b`)

// isEpisode: named an OVA, or episode-length and not a recap. An unknown length (0) is
// not episode-length: such a special goes to the bottom unless named an OVA.
func (sp specialIn) isEpisode(minLength int64) bool {
	if reOVA.MatchString(sp.Title) {
		return true
	}
	return sp.RuntimeMs > 0 && sp.RuntimeMs >= minLength && !reNotStory.MatchString(sp.Title)
}

// buildSections orders a series' season list (Crunchyroll's): each season in its
// number's order, and after it the OVAs (episode-length specials) and films that came out
// before the next season started (before the first season, what came out before it).
// OVAs in the same gap form one entry, apart from an episode-length special not named an
// OVA ("Hero Episode 0"); each film is an entry of its own. Every other special (short,
// undated, or not in the library) is in one "Specials" entry at the bottom: the ones the
// library has first, by date, then the missing ones.
// typical: the show's episode length (typicalRuntime), 0 unknown.
func buildSections(seasons []seasonIn, specials []specialIn, movies []movieIn, typical int64) []section {
	sort.SliceStable(seasons, func(i, j int) bool { return seasons[i].Index < seasons[j].Index })
	// slot: -1 before the first season, i after seasons[i].
	slot := func(date string) int {
		at := -1
		for i, s := range seasons {
			if s.Start != "" && s.Start <= date {
				at = i
			}
		}
		return at
	}
	type placed struct {
		date    string
		special *specialIn
		movie   *movieIn
	}
	gaps := map[int][]placed{}
	var bottom, missing []specialIn
	var undatedMovies []movieIn
	for i := range specials {
		sp := specials[i]
		switch {
		case sp.Virtual:
			missing = append(missing, sp)
		case len(sp.Date) < 10 || !sp.isEpisode(episodeLength(typical)):
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
	var out []section
	emit := func(g int) {
		ps := gaps[g]
		sort.SliceStable(ps, func(i, j int) bool { return ps[i].date < ps[j].date })
		var run []specialIn
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
			out = append(out, section{Kind: "movie", Title: it.Title, Item: &it, Episodes: 1, Year: yearOf(p.movie.Date)})
		}
		flush()
	}
	emit(-1)
	for i, s := range seasons {
		title := s.Title
		if title == "" {
			title = "Season " + strconv.Itoa(s.Index)
		}
		out = append(out, section{Kind: "season", Title: title, SeasonID: s.ID, Episodes: s.Count, Year: yearOf(s.Start)})
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
		out = append(out, section{Kind: "specials", Title: "Specials", EpisodeIDs: ids, Episodes: len(all)})
	}
	for _, m := range undatedMovies {
		it := m.Item
		out = append(out, section{Kind: "movie", Title: it.Title, Item: &it, Episodes: 1})
	}
	return out
}

// ovaSection names the OVAs of one gap: one by its own title, several "OVAs".
func ovaSection(run []specialIn) section {
	ids := make([]string, len(run))
	for i, sp := range run {
		ids[i] = sp.ID
	}
	title := "OVAs"
	if len(run) == 1 {
		title = run[0].Title
	}
	return section{Kind: "specials", Title: title, EpisodeIDs: ids, Episodes: len(run), Year: yearOf(run[0].Date)}
}

func yearOf(date string) int {
	if len(date) < 4 {
		return 0
	}
	y, _ := strconv.Atoi(date[:4])
	return y
}

// GET /v1/items/{id}/sections -> {sections: [...]}: a series' season list in release
// order, with its specials between the seasons they came out between and the franchise's
// films the library holds (see buildSections).
func (s *Server) seriesSections(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	series, err := s.store.GetItemOrMoved(ctx, chi.URLParam(r, "id"))
	if err != nil || series == nil || series.Type != model.TypeSeries {
		writeError(w, 404, "series not found")
		return
	}
	seasons, err := s.store.Children(ctx, series.ID, model.TypeSeason)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	eps, err := s.store.EpisodesOf(ctx, series.ID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	byParent := map[string][]model.Item{}
	for _, e := range eps {
		if e.ParentID != nil {
			byParent[*e.ParentID] = append(byParent[*e.ParentID], e)
		}
	}
	regular := map[string]bool{}
	for _, se := range seasons {
		if se.IndexAired == nil || *se.IndexAired != 0 {
			regular[se.ID] = true
		}
	}
	typical := typicalRuntime(eps, regular)
	var ins []seasonIn
	var specials []specialIn
	var films []model.Item // specials that are films (KonoSuba's Legend of Crimson, filed as S00E05)
	specialsID := ""
	for _, se := range seasons {
		children := byParent[se.ID]
		if se.IndexAired != nil && *se.IndexAired == 0 {
			specialsID = se.ID
			for _, e := range children {
				if !e.IsVirtual && e.RuntimeMs != nil && isFilm(*e.RuntimeMs, typical) {
					films = append(films, e)
					continue
				}
				sp := specialIn{ID: e.ID, Title: e.Title, Date: e.Premiered, Virtual: e.IsVirtual}
				if e.RuntimeMs != nil {
					sp.RuntimeMs = *e.RuntimeMs
				}
				specials = append(specials, sp)
			}
			continue
		}
		in := seasonIn{ID: se.ID, Title: se.Title, Index: 1 << 30, Count: len(children)}
		if se.IndexAired != nil {
			in.Index = *se.IndexAired
		}
		for _, e := range children {
			if len(e.Premiered) >= 10 && (in.Start == "" || e.Premiered[:10] < in.Start) {
				in.Start = e.Premiered[:10]
			}
		}
		ins = append(ins, in)
	}
	movies, err := s.relatedFilms(r, series)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if len(films) > 0 {
		fillAvailability(films)
		s.attachUserData(ctx, films)
		for _, f := range films {
			movies = append(movies, movieIn{Item: f, Date: f.Premiered})
		}
		sort.SliceStable(movies, func(i, j int) bool { return movies[i].Date < movies[j].Date })
	}
	out := buildSections(ins, specials, movies, typical)
	for i := range out {
		if out[i].Kind == "specials" {
			out[i].SeasonID = specialsID
		}
	}
	if out == nil {
		out = []section{}
	}
	writeJSON(w, 200, map[string]any{"sections": out})
}

// relatedFilms are the franchise's films the library holds (the provider's relations
// resolved against it), with their release dates.
func (s *Server) relatedFilms(r *http.Request, series *model.Item) ([]movieIn, error) {
	ctx := r.Context()
	rels, err := s.store.RelationsFor(ctx, series.ID)
	if err != nil {
		return nil, err
	}
	byProvider := map[string][]string{}
	for _, rl := range rels {
		byProvider[rl.Provider] = append(byProvider[rl.Provider], rl.ProviderID)
	}
	seen := map[string]bool{}
	var films []model.Item
	for p, ids := range byProvider {
		m, err := s.store.ItemsByProviderIDs(ctx, p, ids)
		if err != nil {
			return nil, err
		}
		for _, it := range m {
			if it.Type == model.TypeMovie && !seen[it.ID] {
				seen[it.ID] = true
				films = append(films, it)
			}
		}
	}
	fillAvailability(films)
	s.attachUserData(ctx, films)
	out := make([]movieIn, len(films))
	for i, f := range films {
		date := f.Premiered
		if len(date) < 10 && f.Year != nil {
			date = strconv.Itoa(*f.Year) + "-07-01" // the year alone: its middle
		}
		out[i] = movieIn{Item: f, Date: date}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out, nil
}
