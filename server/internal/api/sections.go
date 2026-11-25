package api

import (
	"net/http"
	"sort"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/seasonlist"
)

// GET /v1/items/{id}/sections -> {sections: [...]}: a series' season list in release
// order, with its specials between the seasons they came out between and the franchise's
// films the library holds (see seasonlist.Build).
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
	typical := seasonlist.TypicalRuntime(eps, regular)
	var ins []seasonlist.Season
	var specials []seasonlist.Special
	var films []model.Item // specials that are films (KonoSuba's Legend of Crimson, filed as S00E05)
	specialsID := ""
	for _, se := range seasons {
		children := byParent[se.ID]
		if se.IndexAired != nil && *se.IndexAired == 0 {
			specialsID = se.ID
			for _, e := range children {
				if !e.IsVirtual && e.RuntimeMs != nil && seasonlist.IsFilm(*e.RuntimeMs, typical) {
					films = append(films, e)
					continue
				}
				sp := seasonlist.Special{ID: e.ID, Title: e.Title, Date: e.Premiered, Virtual: e.IsVirtual}
				if e.RuntimeMs != nil {
					sp.RuntimeMs = *e.RuntimeMs
				}
				specials = append(specials, sp)
			}
			continue
		}
		in := seasonlist.Season{ID: se.ID, Title: se.Title, Index: 1 << 30, Count: len(children)}
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
			movies = append(movies, seasonlist.Movie{Item: f, Date: f.Premiered})
		}
		sort.SliceStable(movies, func(i, j int) bool { return movies[i].Date < movies[j].Date })
	}
	out := seasonlist.Build(ins, specials, movies, typical)
	for i := range out {
		if out[i].Kind == "specials" {
			out[i].SeasonID = specialsID
		}
	}
	if out == nil {
		out = []seasonlist.Section{}
	}
	writeJSON(w, 200, map[string]any{"sections": out})
}

// relatedFilms are the franchise's films the library holds (the provider's relations
// resolved against it), with their release dates.
func (s *Server) relatedFilms(r *http.Request, series *model.Item) ([]seasonlist.Movie, error) {
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
	out := make([]seasonlist.Movie, len(films))
	for i, f := range films {
		date := f.Premiered
		if len(date) < 10 && f.Year != nil {
			date = strconv.Itoa(*f.Year) + "-07-01" // the year alone: its middle
		}
		out[i] = seasonlist.Movie{Item: f, Date: date}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out, nil
}
