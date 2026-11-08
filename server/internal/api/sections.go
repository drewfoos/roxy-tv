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
	Virtual         bool // a placeholder: not in the library
}

type movieIn struct {
	Item model.Item
	Date string
}

var reOVA = regexp.MustCompile(`(?i)\bOVA\b|\bOAD\b`)

// buildSections orders a series' seasons, specials and films by release date: each
// season in its number's order, and after it whatever came out before the next season
// started (before the first season, what came out before it). Specials of one kind in
// the same gap form one entry ("OVAs", "Specials"); each film is an entry of its own. Specials with no
// date, and the ones not in the library, go in a last "Specials" entry, as before.
func buildSections(seasons []seasonIn, specials []specialIn, movies []movieIn) []section {
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
	var trailing []specialIn
	var undatedMovies []movieIn
	for i := range specials {
		sp := specials[i]
		if sp.Virtual || len(sp.Date) < 10 {
			trailing = append(trailing, sp)
			continue
		}
		gaps[slot(sp.Date)] = append(gaps[slot(sp.Date)], placed{date: sp.Date[:10], special: &specials[i]})
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
				out = append(out, specialsSection(run))
				run = nil
			}
		}
		for _, p := range ps {
			if p.special != nil {
				// A run is one kind: OVAs and other specials in the same gap are two entries,
				// each named for what it holds.
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
	if len(trailing) > 0 {
		sec := specialsSection(trailing)
		sec.Title, sec.Year = "Specials", 0
		for _, o := range out {
			if o.Kind == "specials" { // after other specials, two "Specials" in a row read as one twice
				sec.Title = "More specials"
				break
			}
		}
		out = append(out, sec)
	}
	for _, m := range undatedMovies {
		it := m.Item
		out = append(out, section{Kind: "movie", Title: it.Title, Item: &it, Episodes: 1})
	}
	return out
}

// specialsSection names a run of specials after what they are: OVAs when every one is
// an OVA, else specials.
func specialsSection(run []specialIn) section {
	ova := true
	ids := make([]string, len(run))
	for i, sp := range run {
		ids[i] = sp.ID
		if !reOVA.MatchString(sp.Title) {
			ova = false
		}
	}
	title := "Specials"
	switch {
	case ova && len(run) == 1:
		title = "OVA"
	case ova:
		title = "OVAs"
	case len(run) == 1:
		title = "Special"
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
	var ins []seasonIn
	var specials []specialIn
	specialsID := ""
	for _, se := range seasons {
		children := byParent[se.ID]
		if se.IndexAired != nil && *se.IndexAired == 0 {
			specialsID = se.ID
			for _, e := range children {
				specials = append(specials, specialIn{ID: e.ID, Title: e.Title, Date: e.Premiered, Virtual: e.IsVirtual})
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
	out := buildSections(ins, specials, movies)
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
