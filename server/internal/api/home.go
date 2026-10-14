package api

import (
	"context"
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
		rows = append(rows, homeRow{Key: key, Title: title, Items: items})
		return true
	}

	var resume []model.Item
	var err error
	if p := auth.PrincipalFrom(ctx); p != nil && p.User != nil {
		resume, err = s.store.ResumeItems(ctx, p.UserID, 20)
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
	if !add("recentEpisodes", "Recently Added Episodes", perSeries, err) {
		return
	}

	withMedia := true
	movies, _, err := s.store.ListItems(ctx, store.ItemQuery{Types: []model.ItemType{model.TypeMovie}, SortBy: "date_created", Desc: true, Limit: 20, HasMedia: &withMedia})
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
	data, err := s.store.UserDataForItems(ctx, p.UserID, guids)
	if err != nil {
		return
	}
	for i := range items {
		items[i].UserData = data[items[i].GUID].Native()
	}
}
