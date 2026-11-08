package discover

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"time"

	"github.com/roxy-media/roxy/server/internal/metadata"
	"github.com/roxy-media/roxy/server/internal/store"
)

// Story films: a requested series brings the films that are part of its story along
// (Bunny Girl Senpai's Dreaming Girl, Sister Venturing Out and Knapsack Kid sit between
// its two seasons). Which films those are is known once the series is in the library:
// its AniList relations mark them (store.ItemRelation.Story: a prequel or sequel film,
// or one between two seasons). Side stories, recaps and spin-offs are never requested.
//
// Each film is requested once: a marker setting ("storyfilm:<tmdb id>") remembers it,
// so a film request someone deleted, or a film removed from the library, is not asked
// for again. A film already in the library or already requested is left alone.

// storyFilmRetry: a film TMDB could not be matched to is looked up again after this.
const storyFilmRetry = 24 * time.Hour

// storyFilmMinSimilarity: how close TMDB's title must be to AniList's.
const storyFilmMinSimilarity = 0.6

// RequestStoryFilms requests the story films of every series request that has reached
// the library. Run after RetryPending, every 10 minutes.
func (s *Service) RequestStoryFilms(ctx context.Context) {
	if !s.RequestsEnabled("movie") {
		return
	}
	all, err := s.store.ListRequests(ctx)
	if err != nil {
		return
	}
	for i := range all {
		r := all[i]
		if r.MediaType != "tv" || r.Status != "sent" {
			continue
		}
		det, err := s.Detail(ctx, "tv", r.TMDBID)
		if err != nil || det.LibraryItemID == "" {
			continue // not in the library yet: its relations are not known
		}
		rels, err := s.store.RelationsFor(ctx, det.LibraryItemID)
		if err != nil {
			continue
		}
		for _, rel := range rels {
			if !rel.Story || rel.Format != "MOVIE" {
				continue
			}
			s.requestStoryFilm(ctx, &r, rel)
		}
	}
}

func (s *Service) requestStoryFilm(ctx context.Context, series *store.Request, rel store.ItemRelation) {
	// In the library under its AniList id already.
	if have, err := s.store.ItemsByProviderIDs(ctx, rel.Provider, []string{rel.ProviderID}); err == nil && len(have) > 0 {
		return
	}
	key := rel.Provider + ":" + rel.ProviderID
	s.mu.Lock()
	last, tried := s.storyFilmTried[key]
	s.mu.Unlock()
	if tried && time.Since(last) < storyFilmRetry {
		return
	}
	s.mu.Lock()
	if s.storyFilmTried == nil {
		s.storyFilmTried = map[string]time.Time{}
	}
	s.storyFilmTried[key] = time.Now()
	s.mu.Unlock()

	tmdbID, err := s.findFilm(ctx, rel.Title, rel.Year)
	if err != nil || tmdbID == 0 {
		slog.Info("discover: story film not found on TMDB", "series", series.Title, "film", rel.Title, "year", rel.Year, "err", err)
		return
	}
	marker := fmt.Sprintf("storyfilm:%d", tmdbID)
	var done string
	if ok, err := s.store.GetSetting(ctx, marker, &done); err != nil || ok {
		return // requested once already (and maybe deleted since): not again
	}
	if existing, err := s.store.GetRequest(ctx, "movie", tmdbID); err == nil && existing != nil {
		_ = s.store.SetSetting(ctx, marker, series.ID)
		return
	}
	det, err := s.Detail(ctx, "movie", tmdbID)
	if err != nil {
		return
	}
	if det.LibraryItemID != "" {
		_ = s.store.SetSetting(ctx, marker, series.ID)
		return
	}
	r := &store.Request{ID: newID(), MediaType: "movie", TMDBID: tmdbID, Title: det.Title, Year: det.Year, PosterPath: det.Poster,
		UserID: series.UserID, UserName: series.UserName, Status: "pending"}
	s.send(ctx, r, det)
	if err := s.store.UpsertRequest(ctx, r); err != nil {
		slog.Warn("discover: saving story film request", "film", det.Title, "err", err)
		return
	}
	_ = s.store.SetSetting(ctx, marker, series.ID)
	slog.Info("discover: story film requested with its series", "series", series.Title, "film", det.Title, "tmdb", tmdbID, "status", r.Status)
}

// findFilm is TMDB's id for a film AniList names: a search by title, the closest title
// released within a year of AniList's (0 when nothing is close enough).
func (s *Service) findFilm(ctx context.Context, title string, year int) (int, error) {
	var resp tmdbList
	if err := s.tmdb.Get(ctx, "/search/movie", url.Values{"query": {title}, "include_adult": {"false"}}, &resp); err != nil {
		return 0, err
	}
	return pickFilm(title, year, resp.Results), nil
}

// pickFilm chooses among TMDB search results: the title most like AniList's (at least
// storyFilmMinSimilarity), released within a year of it when the year is known.
func pickFilm(title string, year int, results []tmdbListEntry) int {
	best, bestScore := 0, 0.0
	for _, e := range results {
		if year > 0 {
			y, _ := strconv.Atoi(firstN(e.ReleaseDate, 4))
			if y == 0 || y < year-1 || y > year+1 {
				continue
			}
		}
		if sc := metadata.TitleSimilarity(title, e.Title); sc >= storyFilmMinSimilarity && sc > bestScore {
			best, bestScore = e.ID, sc
		}
	}
	return best
}

func firstN(s string, n int) string {
	if len(s) < n {
		return s
	}
	return s[:n]
}
