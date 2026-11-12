package discover

import (
	"context"
	"net/url"
	"sort"
	"strconv"

	"github.com/roxy-media/roxy/server/internal/metadata"
)

// TMDB's film for a film AniList names. Which films are listed and which may be
// requested with their series is in films.go.

// storyFilmMinSimilarity: how close TMDB's title must be to AniList's.
const storyFilmMinSimilarity = 0.6

// findFilm is TMDB's entry for a film AniList names (see pickFilm); why says why there
// is none.
func (s *Service) findFilm(ctx context.Context, title string, year int) (*tmdbListEntry, string, error) {
	var resp tmdbList
	if err := s.tmdb.Get(ctx, "/search/movie", url.Values{"query": {title}, "include_adult": {"false"}}, &resp); err != nil {
		return nil, "", err
	}
	id, why := pickFilm(title, year, resp.Results)
	for i := range resp.Results {
		if id != 0 && resp.Results[i].ID == id {
			return &resp.Results[i], "", nil
		}
	}
	return nil, why, nil
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
