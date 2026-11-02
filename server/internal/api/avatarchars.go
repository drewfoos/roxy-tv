package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/roxy-media/roxy/server/internal/store"
)

// The profile editor's "From your library" characters are saved per series on the server
// and filled in slowly by this worker: a few series per pass, a pause between lookups, so a
// library that grows by a thousand shows at once is worked through over hours instead of
// flooding AniList or TMDB. Anime are looked up on AniList (characters with their art);
// other series get TMDB's cast (the character's name with the actor's photo), so a library
// without anime still has suggestions.
const (
	charactersPerSeries = 8
	charactersBatch     = 10
	charactersPass      = 2 * time.Minute
	charactersIdle      = 15 * time.Minute
	charactersGap       = 3 * time.Second
	charactersRetry     = 24 * time.Hour
)

func (s *Server) startCharacterWorker(ctx context.Context) {
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second): // let the server finish starting
		}
		for {
			n := s.fetchCharacterBatch(ctx)
			wait := charactersPass
			if n == 0 {
				wait = charactersIdle
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
		}
	}()
}

// fetchCharacterBatch looks up one batch and reports how many series it tried.
func (s *Server) fetchCharacterBatch(ctx context.Context) int {
	ids, err := s.store.SeriesNeedingCharacters(ctx, charactersBatch, time.Now().Add(-charactersRetry))
	if err != nil {
		slog.Warn("characters: list series", "err", err)
		return 0
	}
	for i, id := range ids {
		if i > 0 {
			select {
			case <-ctx.Done():
				return i
			case <-time.After(charactersGap):
			}
		}
		chars, err := s.lookupCharacters(ctx, id)
		if err != nil {
			slog.Warn("characters: lookup", "series", id, "err", err)
		}
		if err := s.store.SaveSeriesCharacters(ctx, id, chars, err == nil); err != nil {
			slog.Warn("characters: save", "series", id, "err", err)
		}
	}
	if len(ids) > 0 {
		slog.Info("characters: looked up", "series", len(ids))
	}
	return len(ids)
}

func (s *Server) lookupCharacters(ctx context.Context, seriesID string) ([]store.SeriesCharacter, error) {
	if s.meta == nil {
		return nil, fmt.Errorf("metadata service not running")
	}
	it, err := s.store.GetItem(ctx, seriesID)
	if err != nil || it == nil {
		return nil, err
	}
	var out []store.SeriesCharacter
	if anilist, _ := s.store.ProviderID(ctx, seriesID, "anilist"); anilist != "" && s.meta.AniList() != nil {
		cast, err := s.meta.AniList().CharactersForMedia(ctx, anilist)
		if err != nil {
			return nil, err
		}
		for _, c := range cast {
			if len(out) == charactersPerSeries {
				break
			}
			out = append(out, store.SeriesCharacter{Source: "anilist", ID: c.ID, Name: c.Name, Image: c.Image, Series: c.Series})
		}
		return out, nil
	}
	tmdb, _ := s.store.ProviderID(ctx, seriesID, "tmdb")
	t := s.meta.TMDB()
	if tmdb == "" || !t.HasKey() {
		return nil, nil // nothing to look it up by: done, with no characters
	}
	var credits struct {
		Cast []struct {
			ID          int    `json:"id"`
			Name        string `json:"name"`
			Character   string `json:"character"`
			ProfilePath string `json:"profile_path"`
		} `json:"cast"`
	}
	if err := t.Get(ctx, "/tv/"+tmdb+"/credits", url.Values{}, &credits); err != nil {
		return nil, err
	}
	for _, c := range credits.Cast {
		if len(out) == charactersPerSeries {
			break
		}
		if c.ProfilePath == "" {
			continue
		}
		name := c.Character
		if name == "" {
			name = c.Name
		}
		// Minus the person id: AniList and TMDB ids never collide in the client's grid.
		out = append(out, store.SeriesCharacter{Source: "tmdb", ID: -c.ID, Name: name, Image: t.ImageURL("w185", c.ProfilePath), Series: it.Title})
	}
	return out, nil
}
