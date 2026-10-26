package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

// GET /v1/items/{id}/details: everything the "More details" page shows for a series or
// movie: the item, what the library holds (files, codecs, languages), TMDB extras (cast,
// keywords, original title, dates, networks, companies, countries) and links to the
// provider pages. The TMDB part is cached for 10 minutes per item.

type castMember struct {
	TMDBID    int    `json:"tmdbId,omitempty"`
	Name      string `json:"name"`
	Character string `json:"character,omitempty"`
	Photo     string `json:"photo,omitempty"`
}

type tmdbExtras struct {
	OriginalTitle string       `json:"originalTitle,omitempty"`
	FirstAired    string       `json:"firstAired,omitempty"`
	LastAired     string       `json:"lastAired,omitempty"`
	Status        string       `json:"status,omitempty"`
	Episodes      int          `json:"episodes,omitempty"`
	Seasons       int          `json:"seasons,omitempty"`
	RuntimeM      int          `json:"runtimeMinutes,omitempty"`
	Networks      []string     `json:"networks,omitempty"`
	Companies     []string     `json:"companies,omitempty"`
	Countries     []string     `json:"countries,omitempty"`
	Languages     []string     `json:"languages,omitempty"` // spoken languages, English names
	Keywords      []string     `json:"keywords,omitempty"`
	Cast          []castMember `json:"cast,omitempty"`
	Homepage      string       `json:"homepage,omitempty"`
	Tagline       string       `json:"tagline,omitempty"`
}

type detailsResponse struct {
	Item model.Item `json:"item"`
	// Backdrop: an absolute image URL for titles not in the library (Discover).
	Backdrop string            `json:"backdrop,omitempty"`
	Facts    *store.MediaFacts `json:"facts,omitempty"`
	TMDB     *tmdbExtras       `json:"tmdb,omitempty"`
	Links    map[string]string `json:"links,omitempty"` // tmdb, imdb, anilist, mal, tvdb -> URL
}

var (
	detailsMu    sync.Mutex
	detailsCache = map[string]struct {
		at time.Time
		v  *tmdbExtras
	}{}
)

func (s *Server) itemDetails(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	it, err := s.store.GetItem(ctx, chi.URLParam(r, "id"))
	if err != nil || it == nil {
		writeError(w, 404, "item not found")
		return
	}
	// Episodes and seasons resolve to their series.
	for it.Type != model.TypeSeries && it.Type != model.TypeMovie && it.ParentID != nil {
		parent, err := s.store.GetItem(ctx, *it.ParentID)
		if err != nil || parent == nil {
			break
		}
		it = parent
	}
	one := []model.Item{*it}
	s.attachUserData(ctx, one)
	resp := detailsResponse{Item: one[0], Links: map[string]string{}}
	if f, err := s.store.ItemMediaFacts(ctx, it.ID); err == nil {
		resp.Facts = f
	}
	for prov, id := range it.ProviderIDs {
		switch prov {
		case "tmdb":
			kind := "tv"
			if it.Type == model.TypeMovie {
				kind = "movie"
			}
			resp.Links["tmdb"] = "https://www.themoviedb.org/" + kind + "/" + id
		case "imdb":
			resp.Links["imdb"] = "https://www.imdb.com/title/" + id + "/"
		case "anilist":
			resp.Links["anilist"] = "https://anilist.co/anime/" + id
		case "mal":
			resp.Links["mal"] = "https://myanimelist.net/anime/" + id
		case "tvdb":
			resp.Links["tvdb"] = "https://thetvdb.com/dereferrer/series/" + id
		}
	}
	if tmdbID, err := strconv.Atoi(it.ProviderIDs["tmdb"]); err == nil && s.meta != nil && s.meta.TMDB() != nil {
		resp.TMDB = s.tmdbExtras(ctx, it.Type == model.TypeMovie, tmdbID)
	}
	writeJSON(w, 200, resp)
}

func (s *Server) tmdbExtras(ctx context.Context, movie bool, tmdbID int) *tmdbExtras {
	kind := "tv"
	if movie {
		kind = "movie"
	}
	key := fmt.Sprintf("%s:%d", kind, tmdbID)
	detailsMu.Lock()
	if c, ok := detailsCache[key]; ok && time.Since(c.at) < 10*time.Minute {
		detailsMu.Unlock()
		return c.v
	}
	detailsMu.Unlock()

	var d struct {
		Title         string `json:"title"`
		Name          string `json:"name"`
		OriginalTitle string `json:"original_title"`
		OriginalName  string `json:"original_name"`
		Tagline       string `json:"tagline"`
		Status        string `json:"status"`
		Homepage      string `json:"homepage"`
		FirstAirDate  string `json:"first_air_date"`
		LastAirDate   string `json:"last_air_date"`
		ReleaseDate   string `json:"release_date"`
		Runtime       int    `json:"runtime"`
		EpisodeRun    []int  `json:"episode_run_time"`
		NumEpisodes   int    `json:"number_of_episodes"`
		NumSeasons    int    `json:"number_of_seasons"`
		Networks      []struct {
			Name string `json:"name"`
		} `json:"networks"`
		Companies []struct {
			Name string `json:"name"`
		} `json:"production_companies"`
		Countries []struct {
			Name string `json:"name"`
		} `json:"production_countries"`
		OriginCountry []string `json:"origin_country"`
		Spoken        []struct {
			EnglishName string `json:"english_name"`
		} `json:"spoken_languages"`
		Credits struct {
			Cast []struct {
				ID          int    `json:"id"`
				Name        string `json:"name"`
				Character   string `json:"character"`
				ProfilePath string `json:"profile_path"`
			} `json:"cast"`
		} `json:"credits"`
		Keywords struct {
			Keywords []struct {
				Name string `json:"name"`
			} `json:"keywords"`
			Results []struct {
				Name string `json:"name"`
			} `json:"results"`
		} `json:"keywords"`
	}
	t := s.meta.TMDB()
	// TMDB's CDN resets a fair share of connections from some networks; a page that
	// shows no cast because of one reset is worse than a short wait.
	var err error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(time.Duration(attempt) * 300 * time.Millisecond):
			}
		}
		if err = t.Get(ctx, fmt.Sprintf("/%s/%d", kind, tmdbID), url.Values{"append_to_response": {"credits,keywords"}}, &d); err == nil {
			break
		}
	}
	if err != nil {
		slog.Warn("details: tmdb", "id", tmdbID, "err", err)
		return nil
	}
	x := &tmdbExtras{Tagline: d.Tagline, Status: d.Status, Homepage: d.Homepage, Episodes: d.NumEpisodes, Seasons: d.NumSeasons, RuntimeM: d.Runtime}
	x.OriginalTitle = d.OriginalName
	title := d.Name
	if movie {
		x.OriginalTitle = d.OriginalTitle
		title = d.Title
		x.FirstAired = d.ReleaseDate
	} else {
		x.FirstAired, x.LastAired = d.FirstAirDate, d.LastAirDate
		if x.RuntimeM == 0 && len(d.EpisodeRun) > 0 {
			x.RuntimeM = d.EpisodeRun[0]
		}
	}
	if x.OriginalTitle == title {
		x.OriginalTitle = "" // nothing to add
	}
	for _, n := range d.Networks {
		x.Networks = append(x.Networks, n.Name)
	}
	for _, c := range d.Companies {
		x.Companies = append(x.Companies, c.Name)
	}
	for _, c := range d.Countries {
		x.Countries = append(x.Countries, c.Name)
	}
	if len(x.Countries) == 0 {
		x.Countries = d.OriginCountry
	}
	for _, l := range d.Spoken {
		x.Languages = append(x.Languages, l.EnglishName)
	}
	for _, k := range append(d.Keywords.Keywords, d.Keywords.Results...) {
		x.Keywords = append(x.Keywords, k.Name)
	}
	if len(x.Keywords) > 20 {
		x.Keywords = x.Keywords[:20]
	}
	for i, c := range d.Credits.Cast {
		if i >= 15 {
			break
		}
		x.Cast = append(x.Cast, castMember{TMDBID: c.ID, Name: c.Name, Character: c.Character, Photo: t.ImageURL("w185", c.ProfilePath)})
	}
	detailsMu.Lock()
	detailsCache[key] = struct {
		at time.Time
		v  *tmdbExtras
	}{time.Now(), x}
	if len(detailsCache) > 300 {
		for k, c := range detailsCache {
			if time.Since(c.at) > 10*time.Minute {
				delete(detailsCache, k)
			}
		}
	}
	detailsMu.Unlock()
	return x
}
