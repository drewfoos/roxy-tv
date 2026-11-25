package api

import (
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"

	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/seasonlist"
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
	// The order of watching (the season list's). Not read: the seasons' numbers alone.
	keys, err := s.store.WatchKeys(ctx, it.ID)
	if err != nil {
		keys = nil
	}
	writeJSON(w, 200, map[string]any{"item": nextToPlay(eps, keys)})
}

// nextToPlay is the episode a show's Play means: one in progress (the latest in order,
// rewatches included), else the first unwatched one after the last watched, else the
// first unwatched, else the first (watch again). What cannot play (missing, unaired)
// never.
//
// "In order" is the order of watching, keys (the season list read from the top:
// seasonlist.Keys): after a season's last episode come the OVAs that came out before the
// next one, then that season. The extras at the list's foot count only when the show
// has nothing else. Without keys it is the seasons' numbers, specials only when there is
// nothing else: the rule the web's nextToPlay and the apps' upNextOf keep for when the
// server cannot be asked (the same cases in their tests).
func nextToPlay(eps []model.Item, keys map[string]seasonlist.Key) *model.Item {
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
	// An episode's place: the season list's when it is known for every one of them.
	placed := keys != nil
	for _, e := range playable {
		if _, ok := keys[e.ID]; !ok {
			placed = false
		}
	}
	extra := func(e model.Item) bool {
		if placed {
			return keys[e.ID].Extra
		}
		return season(e) == 0
	}
	ordered := []model.Item{}
	for _, e := range playable {
		if !extra(e) {
			ordered = append(ordered, e)
		}
	}
	if len(ordered) == 0 {
		ordered = playable
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if placed {
			return keys[ordered[i].ID].Order < keys[ordered[j].ID].Order
		}
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
