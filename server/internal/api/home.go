package api

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/roxy-media/roxy/server/internal/auth"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

type homeRow struct {
	Key   string       `json:"key"`
	Title string       `json:"title"`
	Items []model.Item `json:"items"`
}

// GET /v1/home: the rows the home screen shows, in display order, each item carrying
// images and the caller's userData. One request for the whole screen.
func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows := make([]homeRow, 0, 5)
	add := func(key, title string, items []model.Item, err error) bool {
		if err != nil {
			writeError(w, 500, err.Error())
			return false
		}
		if items == nil {
			items = []model.Item{}
		}
		s.fillSeriesRefs(ctx, items)
		fillAvailability(items)
		s.attachUserData(ctx, items)
		s.fillAudioLanguages(ctx, items)
		rows = append(rows, homeRow{Key: key, Title: title, Items: items})
		return true
	}

	var resume []model.Item
	var err error
	if p := auth.PrincipalFrom(ctx); p != nil && p.User != nil {
		resume, err = s.store.ContinueItems(ctx, p.ScopeID, 20)
		// Streams ride along so the client can play straight away and show Dub | Sub.
		if err == nil {
			err = s.attachSources(ctx, resume)
		}
	}
	if !add("continue", "Continue Watching", resume, err) {
		return
	}

	// Latest episodes, one per series (the newest), up to 20.
	recent, err := s.store.RecentEpisodes(ctx, 200)
	var perSeries []model.Item
	if err == nil {
		seen := map[string]bool{}
		for _, ep := range recent {
			if seen[ep.SeriesID] {
				continue
			}
			seen[ep.SeriesID] = true
			perSeries = append(perSeries, ep)
			if len(perSeries) == 20 {
				break
			}
		}
	}
	// Streams ride along on the "recently added" rows too (Dub | Sub on the cards).
	if err == nil {
		err = s.attachSources(ctx, perSeries)
	}
	if !add("recentEpisodes", "Recently Added Episodes", perSeries, err) {
		return
	}

	withMedia := true
	movies, _, err := s.store.ListItems(ctx, store.ItemQuery{Types: []model.ItemType{model.TypeMovie}, SortBy: "date_created", Desc: true, Limit: 20, HasMedia: &withMedia})
	if err == nil {
		err = s.attachSources(ctx, movies)
	}
	if !add("recentMovies", "Recently Added Movies", movies, err) {
		return
	}
	series, _, err := s.store.ListItems(ctx, store.ItemQuery{Types: []model.ItemType{model.TypeSeries}, SortBy: "date_created", Desc: true, Limit: 50})
	if !add("series", "Series", series, err) {
		return
	}
	allMovies, _, err := s.store.ListItems(ctx, store.ItemQuery{Types: []model.ItemType{model.TypeMovie}, SortBy: "date_created", Desc: true, Limit: 50, HasMedia: &withMedia})
	if !add("movies", "Movies", allMovies, err) {
		return
	}
	writeJSON(w, 200, map[string]any{"rows": rows})
}

// fillAudioLanguages sets AudioLanguages on the series and movies in a batch, and IsAnime
// on series, movies and episodes (an episode inherits its series'). Two queries; runs
// after fillSeriesRefs so episodes know their series.
func (s *Server) fillAudioLanguages(ctx context.Context, items []model.Item) {
	var ids []string
	seen := map[string]bool{}
	for i := range items {
		var id string
		switch items[i].Type {
		case model.TypeSeries, model.TypeMovie:
			id = items[i].ID
		case model.TypeEpisode:
			id = items[i].SeriesID
		}
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return
	}
	langs, err := s.store.AudioLanguagesFor(ctx, ids)
	if err != nil {
		return
	}
	anime, _ := s.store.AniListTagged(ctx, ids)
	for i := range items {
		switch items[i].Type {
		case model.TypeSeries, model.TypeMovie:
			items[i].AudioLanguages = langs[items[i].ID]
			items[i].IsAnime = anime[items[i].ID]
		case model.TypeEpisode:
			items[i].IsAnime = anime[items[i].SeriesID]
		}
	}
}

// attachUserData sets UserData on every item for the calling user with one query.
// Without a user principal (API key, anonymous) it is a no-op.
func (s *Server) attachUserData(ctx context.Context, items []model.Item) {
	p := auth.PrincipalFrom(ctx)
	if p == nil || p.User == nil || len(items) == 0 {
		return
	}
	guids := make([]string, 0, len(items))
	for i := range items {
		guids = append(guids, items[i].GUID)
	}
	data, err := s.store.UserDataForItems(ctx, p.ScopeID, guids)
	if err != nil {
		// The list still goes out, without watch state; say so rather than nothing.
		slog.Warn("reading watch state for a list", "items", len(items), "err", err)
		return
	}
	for i := range items {
		items[i].UserData = data[items[i].GUID].Native()
	}
}
