package metadata

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/roxy-media/roxy/server/internal/model"
)

// TMDB is the themoviedb.org v3 provider. Movies and series; TVDB ids from Sonarr are
// resolved through /find so TheTVDB itself is never called.
type TMDB struct {
	key    string
	base   string
	images string
	client *http.Client
	lim    *limiter
}

const (
	tmdbBase      = "https://api.themoviedb.org/3"
	tmdbImageBase = "https://image.tmdb.org/t/p/"
)

// NewTMDB returns a provider for the given API key (a v3 key, or a v4 read token
// starting with "eyJ").
func NewTMDB(key string) *TMDB {
	return &TMDB{
		key:    strings.TrimSpace(key),
		base:   tmdbBase,
		images: tmdbImageBase,
		client: &http.Client{Timeout: 20 * time.Second},
		lim:    newLimiter(35, 10*time.Second),
	}
}

func (t *TMDB) Name() string { return "tmdb" }

func (t *TMDB) get(ctx context.Context, path string, q url.Values, out any) error {
	if q == nil {
		q = url.Values{}
	}
	headers := map[string]string{}
	if strings.HasPrefix(t.key, "eyJ") {
		headers["Authorization"] = "Bearer " + t.key
	} else {
		q.Set("api_key", t.key)
	}
	return getJSON(ctx, t.client, t.lim, http.MethodGet, t.base+path+"?"+q.Encode(), headers, nil, out)
}

// Get performs an authenticated TMDB v3 request (for callers outside matching, such as Discover).
func (t *TMDB) Get(ctx context.Context, path string, q url.Values, out any) error { return t.get(ctx, path, q, out) }

// ImageURL builds a TMDB image URL ("w342", "w780", "original") or "" for no path.
func (t *TMDB) ImageURL(size, path string) string { return t.image(size, path) }

func (t *TMDB) image(size, path string) string {
	if path == "" {
		return ""
	}
	return t.images + size + path
}

// ---- wire types ----

type tmdbGenre struct {
	Name string `json:"name"`
}

type tmdbCompany struct {
	Name string `json:"name"`
}

type tmdbExternalIDs struct {
	IMDB string `json:"imdb_id"`
	TVDB int    `json:"tvdb_id"`
}

type tmdbImage struct {
	FilePath string  `json:"file_path"`
	Lang     string  `json:"iso_639_1"`
	Votes    float64 `json:"vote_average"`
}

type tmdbImages struct {
	Logos []tmdbImage `json:"logos"`
}

type tmdbMovie struct {
	ID            int              `json:"id"`
	Title         string           `json:"title"`
	OriginalTitle string           `json:"original_title"`
	Overview      string           `json:"overview"`
	Tagline       string           `json:"tagline"`
	ReleaseDate   string           `json:"release_date"`
	Status        string           `json:"status"`
	VoteAverage   float64          `json:"vote_average"`
	PosterPath    string           `json:"poster_path"`
	BackdropPath  string           `json:"backdrop_path"`
	Genres        []tmdbGenre      `json:"genres"`
	Companies     []tmdbCompany    `json:"production_companies"`
	ExternalIDs   *tmdbExternalIDs `json:"external_ids"`
	Images        *tmdbImages      `json:"images"`
	ReleaseDates  *struct {
		Results []struct {
			Country  string `json:"iso_3166_1"`
			Releases []struct {
				Certification string `json:"certification"`
			} `json:"release_dates"`
		} `json:"results"`
	} `json:"release_dates"`
}

type tmdbTV struct {
	ID           int           `json:"id"`
	Name         string        `json:"name"`
	OriginalName string        `json:"original_name"`
	Overview     string        `json:"overview"`
	Tagline      string        `json:"tagline"`
	FirstAirDate string        `json:"first_air_date"`
	Status       string        `json:"status"`
	VoteAverage  float64       `json:"vote_average"`
	PosterPath   string        `json:"poster_path"`
	BackdropPath string        `json:"backdrop_path"`
	Genres       []tmdbGenre   `json:"genres"`
	Networks     []tmdbCompany `json:"networks"`
	Companies    []tmdbCompany `json:"production_companies"`
	NumEpisodes  int           `json:"number_of_episodes"`
	Seasons      []struct {
		SeasonNumber int    `json:"season_number"`
		EpisodeCount int    `json:"episode_count"`
		PosterPath   string `json:"poster_path"`
	} `json:"seasons"`
	ExternalIDs    *tmdbExternalIDs `json:"external_ids"`
	Images         *tmdbImages      `json:"images"`
	ContentRatings *struct {
		Results []struct {
			Country string `json:"iso_3166_1"`
			Rating  string `json:"rating"`
		} `json:"results"`
	} `json:"content_ratings"`
}

type tmdbSeason struct {
	SeasonNumber int    `json:"season_number"`
	PosterPath   string `json:"poster_path"`
	Episodes     []struct {
		EpisodeNumber int    `json:"episode_number"`
		SeasonNumber  int    `json:"season_number"`
		Name          string `json:"name"`
		Overview      string `json:"overview"`
		AirDate       string `json:"air_date"`
		StillPath     string `json:"still_path"`
	} `json:"episodes"`
}

type tmdbSearchMovie struct {
	Results []struct {
		ID            int    `json:"id"`
		Title         string `json:"title"`
		OriginalTitle string `json:"original_title"`
		ReleaseDate   string `json:"release_date"`
	} `json:"results"`
}

type tmdbSearchTV struct {
	Results []struct {
		ID           int    `json:"id"`
		Name         string `json:"name"`
		OriginalName string `json:"original_name"`
		FirstAirDate string `json:"first_air_date"`
	} `json:"results"`
}

type tmdbFind struct {
	MovieResults []struct {
		ID int `json:"id"`
	} `json:"movie_results"`
	TVResults []struct {
		ID int `json:"id"`
	} `json:"tv_results"`
}

// ---- conversion (pure; unit tested from fixtures) ----

func (t *TMDB) movieResult(m *tmdbMovie) *Result {
	r := &Result{
		Provider:        "tmdb",
		ID:              strconv.Itoa(m.ID),
		Title:           m.Title,
		OriginalTitle:   m.OriginalTitle,
		Year:            dateYear(m.ReleaseDate),
		Overview:        strings.TrimSpace(m.Overview),
		Tagline:         strings.TrimSpace(m.Tagline),
		CommunityRating: round1(m.VoteAverage),
		Premiered:       m.ReleaseDate,
		Status:          m.Status,
		ProviderIDs:     map[string]string{"tmdb": strconv.Itoa(m.ID)},
		Images:          map[string]string{},
	}
	for _, g := range m.Genres {
		r.Genres = append(r.Genres, g.Name)
	}
	if len(m.Companies) > 0 {
		r.Studio = m.Companies[0].Name
	}
	if m.ExternalIDs != nil && m.ExternalIDs.IMDB != "" {
		r.ProviderIDs["imdb"] = m.ExternalIDs.IMDB
	}
	if m.ReleaseDates != nil {
		for _, rd := range m.ReleaseDates.Results {
			if rd.Country != "US" {
				continue
			}
			for _, rel := range rd.Releases {
				if rel.Certification != "" {
					r.OfficialRating = rel.Certification
					break
				}
			}
		}
	}
	t.setImages(r, m.PosterPath, m.BackdropPath, m.Images)
	return r
}

func (t *TMDB) tvResult(s *tmdbTV) *Result {
	r := &Result{
		Provider:        "tmdb",
		ID:              strconv.Itoa(s.ID),
		Title:           s.Name,
		OriginalTitle:   s.OriginalName,
		Year:            dateYear(s.FirstAirDate),
		Overview:        strings.TrimSpace(s.Overview),
		Tagline:         strings.TrimSpace(s.Tagline),
		CommunityRating: round1(s.VoteAverage),
		Premiered:       s.FirstAirDate,
		Status:          tmdbStatus(s.Status),
		ProviderIDs:     map[string]string{"tmdb": strconv.Itoa(s.ID)},
		Images:          map[string]string{},
		EpisodeCount:    s.NumEpisodes,
	}
	for _, g := range s.Genres {
		r.Genres = append(r.Genres, g.Name)
	}
	if len(s.Networks) > 0 {
		r.Studio = s.Networks[0].Name
	} else if len(s.Companies) > 0 {
		r.Studio = s.Companies[0].Name
	}
	if s.ExternalIDs != nil {
		if s.ExternalIDs.IMDB != "" {
			r.ProviderIDs["imdb"] = s.ExternalIDs.IMDB
		}
		if s.ExternalIDs.TVDB != 0 {
			r.ProviderIDs["tvdb"] = strconv.Itoa(s.ExternalIDs.TVDB)
		}
	}
	if s.ContentRatings != nil {
		for _, cr := range s.ContentRatings.Results {
			if cr.Country == "US" && cr.Rating != "" {
				r.OfficialRating = cr.Rating
				break
			}
		}
		if r.OfficialRating == "" && len(s.ContentRatings.Results) > 0 {
			r.OfficialRating = s.ContentRatings.Results[0].Rating
		}
	}
	for _, se := range s.Seasons {
		r.Seasons = append(r.Seasons, SeasonInfo{Number: se.SeasonNumber, EpisodeCount: se.EpisodeCount, Poster: t.image("w780", se.PosterPath)})
	}
	t.setImages(r, s.PosterPath, s.BackdropPath, s.Images)
	return r
}

func (t *TMDB) seasonResult(s *tmdbSeason) *Season {
	out := &Season{Number: s.SeasonNumber, Poster: t.image("w780", s.PosterPath)}
	for _, e := range s.Episodes {
		out.Episodes = append(out.Episodes, EpisodeResult{
			Season:   e.SeasonNumber,
			Episode:  e.EpisodeNumber,
			Title:    strings.TrimSpace(e.Name),
			Overview: strings.TrimSpace(e.Overview),
			AirDate:  e.AirDate,
			Thumb:    t.image("w780", e.StillPath),
		})
	}
	return out
}

func (t *TMDB) setImages(r *Result, poster, backdrop string, imgs *tmdbImages) {
	if p := t.image("w780", poster); p != "" {
		r.Images["poster"] = p
	}
	if b := t.image("w1280", backdrop); b != "" {
		r.Images["backdrop"] = b
	}
	if imgs != nil {
		// Prefer an English (or language-less) logo; TMDB sorts by votes already.
		for _, pass := range []string{"en", "", "*"} {
			for _, l := range imgs.Logos {
				if pass == "*" || l.Lang == pass {
					if strings.HasSuffix(strings.ToLower(l.FilePath), ".svg") {
						continue
					}
					r.Images["logo"] = t.image("w500", l.FilePath)
					return
				}
			}
		}
	}
}

func tmdbStatus(s string) string {
	switch s {
	case "Returning Series", "In Production", "Pilot":
		return "Continuing"
	case "Ended", "Canceled":
		return "Ended"
	case "Planned":
		return "Upcoming"
	}
	return s
}

func round1(f float64) float64 {
	return float64(int(f*10+0.5)) / 10
}

// ---- Provider ----

func (t *TMDB) Match(ctx context.Context, item *model.Item, hints Hints) (*Result, error) {
	if t.key == "" {
		return nil, nil
	}
	isMovie := item.Type == model.TypeMovie
	id := 0
	if v, ok := item.ProviderIDs["tmdb"]; ok {
		id, _ = strconv.Atoi(v)
	}
	if id == 0 {
		for _, src := range []struct{ provider, source string }{{"tvdb", "tvdb_id"}, {"imdb", "imdb_id"}} {
			v, ok := item.ProviderIDs[src.provider]
			if !ok || v == "" {
				continue
			}
			var f tmdbFind
			if err := t.get(ctx, "/find/"+url.PathEscape(v), url.Values{"external_source": {src.source}}, &f); err != nil {
				if isNotFound(err) {
					continue
				}
				return nil, err
			}
			if isMovie && len(f.MovieResults) > 0 {
				id = f.MovieResults[0].ID
			} else if !isMovie && len(f.TVResults) > 0 {
				id = f.TVResults[0].ID
			}
			if id != 0 {
				break
			}
		}
	}
	if id == 0 {
		var err error
		id, err = t.search(ctx, item, isMovie)
		if err != nil || id == 0 {
			return nil, err
		}
	}
	if isMovie {
		var m tmdbMovie
		if err := t.get(ctx, "/movie/"+strconv.Itoa(id), url.Values{"append_to_response": {"external_ids,images,release_dates"}, "include_image_language": {"en,null"}}, &m); err != nil {
			if isNotFound(err) {
				return nil, nil
			}
			return nil, err
		}
		return t.movieResult(&m), nil
	}
	var s tmdbTV
	if err := t.get(ctx, "/tv/"+strconv.Itoa(id), url.Values{"append_to_response": {"external_ids,images,content_ratings"}, "include_image_language": {"en,null"}}, &s); err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return t.tvResult(&s), nil
}

func (t *TMDB) search(ctx context.Context, item *model.Item, isMovie bool) (int, error) {
	q := url.Values{"query": {item.Title}, "include_adult": {"false"}}
	if isMovie {
		if item.Year != nil {
			q.Set("year", strconv.Itoa(*item.Year))
		}
		var res tmdbSearchMovie
		if err := t.get(ctx, "/search/movie", q, &res); err != nil {
			return 0, err
		}
		if len(res.Results) == 0 && item.Year != nil {
			q.Del("year")
			if err := t.get(ctx, "/search/movie", q, &res); err != nil {
				return 0, err
			}
		}
		cands := make([]candidate, len(res.Results))
		for i, r := range res.Results {
			cands[i] = candidate{names: []string{r.Title, r.OriginalTitle}, year: dateYear(r.ReleaseDate)}
		}
		if i := pickBest(item.Title, item.Year, cands); i >= 0 {
			return res.Results[i].ID, nil
		}
		return 0, nil
	}
	if item.Year != nil {
		q.Set("first_air_date_year", strconv.Itoa(*item.Year))
	}
	var res tmdbSearchTV
	if err := t.get(ctx, "/search/tv", q, &res); err != nil {
		return 0, err
	}
	if len(res.Results) == 0 && item.Year != nil {
		q.Del("first_air_date_year")
		if err := t.get(ctx, "/search/tv", q, &res); err != nil {
			return 0, err
		}
	}
	cands := make([]candidate, len(res.Results))
	for i, r := range res.Results {
		cands[i] = candidate{names: []string{r.Name, r.OriginalName}, year: dateYear(r.FirstAirDate)}
	}
	if i := pickBest(item.Title, item.Year, cands); i >= 0 {
		return res.Results[i].ID, nil
	}
	return 0, nil
}

func (t *TMDB) Episodes(ctx context.Context, series *Result, season int) (*Season, error) {
	if t.key == "" || series == nil || series.Provider != "tmdb" {
		return nil, nil
	}
	var s tmdbSeason
	if err := t.get(ctx, fmt.Sprintf("/tv/%s/season/%d", series.ID, season), nil, &s); err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return t.seasonResult(&s), nil
}
