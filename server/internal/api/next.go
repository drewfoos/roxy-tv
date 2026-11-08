package api

import (
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"

	"github.com/roxy-media/roxy/server/internal/model"
)

// GET /v1/items/{id}/next -> {"item": Episode | null}: the episode a show's Play means for
// the caller (its profile), worked out here: the clients loaded every episode of the show
// (thousands for the longest) to pick one. A film answers itself. Same rule as the
// clients' nextToPlay / upNextOf (see nextToPlay).
func (s *Server) itemNext(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	it, err := s.store.GetItemOrMoved(ctx, chi.URLParam(r, "id"))
	if err != nil || it == nil {
		writeError(w, 404, "item not found")
		return
	}
	switch it.Type {
	case model.TypeMovie:
		one := []model.Item{*it}
		fillAvailability(one)
		s.attachUserData(ctx, one)
		writeJSON(w, 200, map[string]any{"item": one[0]})
		return
	case model.TypeSeries:
	default:
		writeError(w, 400, "a series or a film")
		return
	}
	eps, err := s.store.EpisodesOf(ctx, it.ID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	fillAvailability(eps)
	s.attachUserData(ctx, eps)
	writeJSON(w, 200, map[string]any{"item": nextToPlay(eps)})
}

// nextToPlay is the episode a show's Play means: one in progress (the latest in order,
// rewatches included), else the first unwatched one after the last watched, else the
// first unwatched, else the first (watch again). Specials only when the show has nothing
// else; what cannot play (missing, unaired) never. The web's nextToPlay and the apps'
// upNextOf follow the same rule (and the same cases in their tests).
func nextToPlay(eps []model.Item) *model.Item {
	playable := []model.Item{}
	for _, e := range eps {
		if e.HasMedia && e.Availability != "missing" && e.Availability != "unaired" {
			playable = append(playable, e)
		}
	}
	season := func(e model.Item) int {
		if e.SeasonAired != nil {
			return *e.SeasonAired
		}
		return 1
	}
	number := func(e model.Item) int {
		switch {
		case e.IndexAired != nil:
			return *e.IndexAired
		case e.IndexAbsolute != nil:
			return *e.IndexAbsolute
		}
		return 0
	}
	ordered := []model.Item{}
	for _, e := range playable {
		if season(e) != 0 {
			ordered = append(ordered, e)
		}
	}
	if len(ordered) == 0 {
		ordered = playable
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if season(ordered[i]) != season(ordered[j]) {
			return season(ordered[i]) < season(ordered[j])
		}
		return number(ordered[i]) < number(ordered[j])
	})
	played := func(e model.Item) bool { return e.UserData != nil && e.UserData.Played }
	for i := len(ordered) - 1; i >= 0; i-- {
		if ud := ordered[i].UserData; ud != nil && ud.PositionMs > 0 {
			return &ordered[i]
		}
	}
	last := -1
	for i, e := range ordered {
		if played(e) {
			last = i
		}
	}
	for i := last + 1; i < len(ordered); i++ {
		if !played(ordered[i]) {
			return &ordered[i]
		}
	}
	for i := range ordered {
		if !played(ordered[i]) {
			return &ordered[i]
		}
	}
	if len(ordered) > 0 {
		return &ordered[0]
	}
	return nil
}
