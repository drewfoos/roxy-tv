package discover

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"sort"
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
	// One TMDB lookup per relation a day, checked and noted under one lock.
	key := rel.Provider + ":" + rel.ProviderID
	s.mu.Lock()
	if last, tried := s.storyFilmTried[key]; tried && time.Since(last) < storyFilmRetry {
		s.mu.Unlock()
		return
	}
	if s.storyFilmTried == nil {
		s.storyFilmTried = map[string]time.Time{}
	}
	s.storyFilmTried[key] = time.Now()
	s.mu.Unlock()

	tmdbID, why, err := s.findFilm(ctx, rel.Title, rel.Year)
	if err != nil || tmdbID == 0 {
		slog.Info("discover: story film not requested", "series", series.Title, "film", rel.Title, "year", rel.Year, "why", why, "err", err)
		return
	}
	// The film is claimed before anything else happens: one statement that only one caller
	// can win (store.ClaimSetting), so two runs at once cannot both pass a "not requested
	// yet" check and both send it. The claim stays (a film request someone deletes is not
	// made again); it is given back only when this attempt stops before saving anything.
	marker := fmt.Sprintf("storyfilm:%d", tmdbID)
	won, err := s.store.ClaimSetting(ctx, marker, series.ID)
	if err != nil || !won {
		return
	}
	release := func() { _ = s.store.DeleteSetting(ctx, marker) }
	if existing, err := s.store.GetRequest(ctx, "movie", tmdbID); err != nil {
		release()
		return
	} else if existing != nil {
		return
	}
	det, err := s.Detail(ctx, "movie", tmdbID)
	if err != nil {
		release()
		return
	}
	if det.LibraryItemID != "" {
		return
	}
	// The request is saved before Radarr hears of it, as "pending". If the send works and
	// the save after it fails, Roxy still knows the request: the pending row is sent again
	// by RetryPending, and adding a film Radarr already has returns that film (arr.AddMovie)
	// rather than a second one. A send nobody recorded cannot happen.
	r := &store.Request{ID: newID(), MediaType: "movie", TMDBID: tmdbID, Title: det.Title, Year: det.Year, PosterPath: det.Poster,
		UserID: series.UserID, UserName: series.UserName, Status: "pending"}
	if err := s.store.UpsertRequest(ctx, r); err != nil {
		slog.Warn("discover: saving story film request", "film", det.Title, "err", err)
		release()
		return
	}
	s.send(ctx, r, det)
	if err := s.store.UpsertRequest(ctx, r); err != nil {
		slog.Warn("discover: saving story film request after sending (retried as pending)", "film", det.Title, "err", err)
	}
	slog.Info("discover: story film requested with its series", "series", series.Title, "film", det.Title, "tmdb", tmdbID, "status", r.Status)
}

// findFilm is TMDB's id for a film AniList names (see pickFilm); why says why there is
// none.
func (s *Service) findFilm(ctx context.Context, title string, year int) (int, string, error) {
	var resp tmdbList
	if err := s.tmdb.Get(ctx, "/search/movie", url.Values{"query": {title}, "include_adult": {"false"}}, &resp); err != nil {
		return 0, "", err
	}
	id, why := pickFilm(title, year, resp.Results)
	return id, why, nil
}

// tmdbAnimation is TMDB's Animation genre.
const tmdbAnimation = 16

// storyFilmMargin: how far the best title must score above the next one for the choice
// to be clear.
const storyFilmMargin = 0.05

// pickFilm chooses TMDB's film for an AniList story film, or none when the search does not
// say clearly which: a wrong film would be downloaded with nobody asking. A candidate
// must be animated (a live-action remake of the same name is not it), released within a
// year of AniList's (its exact year preferred over a neighbour's), and titled at least
// storyFilmMinSimilarity alike; the best must beat the next by storyFilmMargin. Two
// candidates as good as each other (two films of one name and year) give none.
func pickFilm(title string, year int, results []tmdbListEntry) (int, string) {
	type cand struct {
		id    int
		score float64
		exact bool
	}
	var cs []cand
	seen := map[int]bool{}
	for _, e := range results {
		if seen[e.ID] {
			continue
		}
		seen[e.ID] = true
		if len(e.GenreIDs) > 0 && !containsInt(e.GenreIDs, tmdbAnimation) {
			continue
		}
		y, _ := strconv.Atoi(firstN(e.ReleaseDate, 4))
		if year > 0 && (y == 0 || y < year-1 || y > year+1) {
			continue
		}
		if sc := metadata.TitleSimilarity(title, e.Title); sc >= storyFilmMinSimilarity {
			cs = append(cs, cand{e.ID, sc, year > 0 && y == year})
		}
	}
	if len(cs) == 0 {
		return 0, "no animated film of that name and year"
	}
	var exact []cand
	for _, c := range cs {
		if c.exact {
			exact = append(exact, c)
		}
	}
	if len(exact) > 0 {
		cs = exact
	}
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].score > cs[j].score })
	if len(cs) > 1 && cs[0].score-cs[1].score < storyFilmMargin {
		return 0, "ambiguous: several films match about as well"
	}
	return cs[0].id, ""
}

func containsInt(xs []int, v int) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

func firstN(s string, n int) string {
	if len(s) < n {
		return s
	}
	return s[:n]
}
