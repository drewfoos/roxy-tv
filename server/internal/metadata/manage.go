package metadata

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/roxy-media/roxy/server/internal/model"
)

// MatchCandidate is one search hit offered on the Identify screen.
type MatchCandidate struct {
	Provider string `json:"provider"` // tmdb | anilist
	ID       string `json:"id"`
	Title    string `json:"title"`
	Year     int    `json:"year,omitempty"`
	Overview string `json:"overview,omitempty"`
	Poster   string `json:"poster,omitempty"`
}

// Search lists what a provider finds for a title, for the admin to pick from.
func (s *Service) Search(ctx context.Context, kind model.ItemType, provider, query string, year *int) ([]MatchCandidate, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("type a title to search for")
	}
	switch provider {
	case "anilist":
		return s.searchAniList(ctx, kind, query)
	default:
		t := s.TMDB()
		if t == nil {
			return nil, fmt.Errorf("TMDB is not configured on this server")
		}
		return t.candidates(ctx, kind == model.TypeMovie, query, year)
	}
}

func (t *TMDB) candidates(ctx context.Context, isMovie bool, query string, year *int) ([]MatchCandidate, error) {
	q := url.Values{"query": {query}, "include_adult": {"false"}}
	var out []MatchCandidate
	if isMovie {
		if year != nil {
			q.Set("year", strconv.Itoa(*year))
		}
		var res tmdbSearchMovie
		if err := t.get(ctx, "/search/movie", q, &res); err != nil {
			return nil, err
		}
		for _, r := range res.Results {
			out = append(out, MatchCandidate{Provider: "tmdb", ID: strconv.Itoa(r.ID), Title: r.Title, Year: dateYear(r.ReleaseDate), Overview: r.Overview, Poster: t.image("w185", r.PosterPath)})
		}
		return out, nil
	}
	if year != nil {
		q.Set("first_air_date_year", strconv.Itoa(*year))
	}
	var res tmdbSearchTV
	if err := t.get(ctx, "/search/tv", q, &res); err != nil {
		return nil, err
	}
	for _, r := range res.Results {
		out = append(out, MatchCandidate{Provider: "tmdb", ID: strconv.Itoa(r.ID), Title: r.Name, Year: dateYear(r.FirstAirDate), Overview: r.Overview, Poster: t.image("w185", r.PosterPath)})
	}
	return out, nil
}

func (s *Service) searchAniList(ctx context.Context, kind model.ItemType, query string) ([]MatchCandidate, error) {
	a := s.AniList()
	if a == nil {
		return nil, fmt.Errorf("AniList is not available")
	}
	vars := map[string]any{"search": cleanSearchTitle(query)}
	if kind == model.TypeMovie {
		vars["format"] = []string{"MOVIE", "SPECIAL", "OVA", "ONA"}
	} else {
		vars["format"] = []string{"TV", "TV_SHORT", "ONA", "OVA", "SPECIAL"}
	}
	resp, err := a.query(ctx, anilistSearchQuery, vars)
	if err != nil {
		return nil, err
	}
	var out []MatchCandidate
	if resp.Data.Page == nil {
		return out, nil
	}
	for _, m := range resp.Data.Page.Media {
		title := firstNonEmpty(m.Title.English, m.Title.Romaji, m.Title.Native)
		ov := StripHTML(m.Description)
		if len(ov) > 400 {
			ov = ov[:400] + "…"
		}
		out = append(out, MatchCandidate{Provider: "anilist", ID: strconv.Itoa(m.ID), Title: title, Year: m.StartDate.Year, Overview: ov, Poster: firstNonEmpty(m.CoverImage.Large, m.CoverImage.ExtraLarge)})
	}
	return out, nil
}

// Identify points an item at the provider entry the admin picked: its old ids go (a wrong
// match must not steer the lookup), the lock comes off, and a forced lookup runs.
func (s *Service) Identify(ctx context.Context, itemID, provider, id string) error {
	if provider != "tmdb" && provider != "anilist" {
		return fmt.Errorf("unknown provider %q", provider)
	}
	if err := s.store.ReplaceProviderIDs(ctx, itemID, map[string]string{provider: id}); err != nil {
		return err
	}
	if err := s.store.SetMetadataLocked(ctx, itemID, false); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.failed, itemID)
	s.mu.Unlock()
	s.Refresh(itemID)
	return nil
}

// RefreshAll re-runs lookups for the whole library. missingOnly limits it to titles with
// no match yet (and ignores their back-off); otherwise every title is looked up again,
// replacing what it had (locked titles and locked images are kept). It blocks while the
// queue is full, so run it in a goroutine.
func (s *Service) RefreshAll(ctx context.Context, missingOnly bool) error {
	titles, err := s.store.LibraryTitles(ctx)
	if err != nil {
		return err
	}
	for _, t := range titles {
		if t.Locked || (missingOnly && t.Matched) {
			continue
		}
		s.mu.Lock()
		delete(s.failed, t.ID)
		s.pending[t.ID] = true
		s.mu.Unlock()
		select {
		case s.queue <- job{itemID: t.ID, force: true}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// QueueLength is how many lookups are waiting (Library settings shows it as progress).
func (s *Service) QueueLength() int { return len(s.queue) }
