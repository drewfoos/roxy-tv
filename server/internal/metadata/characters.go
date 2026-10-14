package metadata

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Character is an AniList character offered as a profile avatar.
type Character struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Image  string `json:"image"`  // large portrait on AniList's CDN
	Series string `json:"series"` // the anime they are best known from
}

const anilistCharacterSearch = `query ($search: String) {
  Page(perPage: 24) {
    characters(search: $search, sort: FAVOURITES_DESC) {
      id name { full } image { large }
      media(perPage: 1, sort: POPULARITY_DESC, type: ANIME) { nodes { title { romaji english } } }
    }
  }
}`

const anilistMediaCharacters = `query ($id: Int) {
  Media(id: $id, type: ANIME) {
    title { romaji english }
    characters(sort: [ROLE, FAVOURITES_DESC], perPage: 12) { nodes { id name { full } image { large } } }
  }
}`

type anilistCharacterResp struct {
	Data struct {
		Page *struct {
			Characters []anilistCharacter `json:"characters"`
		} `json:"Page"`
		Media *struct {
			Title      anilistTitle `json:"title"`
			Characters struct {
				Nodes []anilistCharacter `json:"nodes"`
			} `json:"characters"`
		} `json:"Media"`
	} `json:"data"`
}

type anilistTitle struct {
	Romaji  string `json:"romaji"`
	English string `json:"english"`
}

func (t anilistTitle) best() string {
	if t.English != "" {
		return t.English
	}
	return t.Romaji
}

type anilistCharacter struct {
	ID    int `json:"id"`
	Name  struct{ Full string }
	Image struct{ Large string }
	Media *struct {
		Nodes []struct {
			Title anilistTitle `json:"title"`
		} `json:"nodes"`
	} `json:"media"`
}

// SearchCharacters finds avatar candidates by name on AniList.
func (a *AniList) SearchCharacters(ctx context.Context, q string) ([]Character, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}
	var resp anilistCharacterResp
	if err := a.rawQuery(ctx, anilistCharacterSearch, map[string]any{"search": q}, &resp); err != nil {
		return nil, err
	}
	if resp.Data.Page == nil {
		return nil, nil
	}
	out := make([]Character, 0, len(resp.Data.Page.Characters))
	for _, c := range resp.Data.Page.Characters {
		if c.Image.Large == "" {
			continue
		}
		ch := Character{ID: c.ID, Name: c.Name.Full, Image: c.Image.Large}
		if c.Media != nil && len(c.Media.Nodes) > 0 {
			ch.Series = c.Media.Nodes[0].Title.best()
		}
		out = append(out, ch)
	}
	return out, nil
}

// CharactersForMedia lists the main cast of one anime (by AniList id), for the
// "from your library" avatar suggestions. Cached for an hour per id.
func (a *AniList) CharactersForMedia(ctx context.Context, anilistID string) ([]Character, error) {
	id, err := strconv.Atoi(anilistID)
	if err != nil {
		return nil, nil
	}
	if cached, ok := a.castCache.get(id); ok {
		return cached, nil
	}
	var resp anilistCharacterResp
	if err := a.rawQuery(ctx, anilistMediaCharacters, map[string]any{"id": id}, &resp); err != nil {
		return nil, err
	}
	if resp.Data.Media == nil {
		return nil, nil
	}
	series := resp.Data.Media.Title.best()
	var out []Character
	for _, c := range resp.Data.Media.Characters.Nodes {
		if c.Image.Large == "" {
			continue
		}
		out = append(out, Character{ID: c.ID, Name: c.Name.Full, Image: c.Image.Large, Series: series})
	}
	a.castCache.put(id, out)
	return out, nil
}

func (a *AniList) rawQuery(ctx context.Context, q string, vars map[string]any, out any) error {
	body, _ := json.Marshal(map[string]any{"query": q, "variables": vars})
	return getJSON(ctx, a.client, a.lim, http.MethodPost, a.endpoint, map[string]string{"Content-Type": "application/json"}, body, out)
}

// castCache remembers a show's cast for an hour so the suggestions grid does not hit
// AniList every time the picker opens.
type castCache struct {
	mu sync.Mutex
	m  map[int]castEntry
}

type castEntry struct {
	at   time.Time
	cast []Character
}

func (c *castCache) get(id int) ([]Character, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[id]
	if !ok || time.Since(e.at) > time.Hour {
		return nil, false
	}
	return e.cast, true
}

func (c *castCache) put(id int, cast []Character) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[int]castEntry{}
	}
	c.m[id] = castEntry{at: time.Now(), cast: cast}
}
