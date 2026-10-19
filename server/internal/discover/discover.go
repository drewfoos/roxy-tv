// Package discover is Roxy's own request app: TMDB browsing and search with "in your library"
// and request state, and requests that go to Sonarr / Radarr when they are configured.
package discover

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/roxy-media/roxy/server/internal/arr"
	"github.com/roxy-media/roxy/server/internal/config"
	"github.com/roxy-media/roxy/server/internal/metadata"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

// Item is one TMDB title as Discover shows it.
type Item struct {
	TMDBID   int      `json:"tmdbId"`
	Type     string   `json:"type"` // tv | movie
	Title    string   `json:"title"`
	Year     int      `json:"year,omitempty"`
	Overview string   `json:"overview,omitempty"`
	Poster   string   `json:"poster,omitempty"`   // absolute TMDB URL (w342)
	Backdrop string   `json:"backdrop,omitempty"` // absolute TMDB URL (w780)
	Rating   float64  `json:"rating,omitempty"`
	Genres   []string `json:"genres,omitempty"`
	IsAnime  bool     `json:"isAnime"`
	// Set when the library already has this title.
	LibraryItemID string `json:"libraryItemId,omitempty"`
	// Set when someone asked for it: pending | sent | failed.
	RequestStatus string `json:"requestStatus,omitempty"`
}

// Detail is the title page's data.
type Detail struct {
	Item
	Tagline  string         `json:"tagline,omitempty"`
	Status   string         `json:"status,omitempty"`
	Seasons  int            `json:"seasons,omitempty"`
	Episodes int            `json:"episodes,omitempty"`
	RuntimeM int            `json:"runtimeMinutes,omitempty"`
	Networks []string       `json:"networks,omitempty"`
	TVDBID   int            `json:"tvdbId,omitempty"`
	IMDBID   string         `json:"imdbId,omitempty"`
	Request  *store.Request `json:"request,omitempty"`
	Requests bool           `json:"requestsEnabled"` // Sonarr / Radarr configured for this type
}

type Row struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	Items []Item `json:"items"`
}

type Service struct {
	store  *store.Store
	cfg    *config.Config
	tmdb   *metadata.TMDB
	sonarr *arr.Client
	radarr *arr.Client

	mu     sync.Mutex
	genres map[string]map[int]string // "tv"/"movie" -> id -> name
	rows   []Row
	rowsAt time.Time
	// TMDB is a shared public service: besides the client's own token bucket (shared
	// with metadata matching), details and searches are cached so typing in the search
	// box or reopening a page does not cost another call.
	details *ttlCache[*Detail]
	search  *ttlCache[[]Item]
}

// ttlCache is a small bounded map with per-entry expiry.
type ttlCache[T any] struct {
	mu   sync.Mutex
	ttl  time.Duration
	max  int
	data map[string]cacheEntry[T]
}

type cacheEntry[T any] struct {
	v  T
	at time.Time
}

func newTTLCache[T any](ttl time.Duration, max int) *ttlCache[T] {
	return &ttlCache[T]{ttl: ttl, max: max, data: map[string]cacheEntry[T]{}}
}

func (c *ttlCache[T]) get(k string) (T, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.data[k]
	if !ok || time.Since(e.at) > c.ttl {
		var zero T
		return zero, false
	}
	return e.v, true
}

func (c *ttlCache[T]) put(k string, v T) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.data) >= c.max {
		// Make room: expired entries first, then whatever comes up. A courtesy cache, not state.
		for key, e := range c.data {
			if time.Since(e.at) > c.ttl || len(c.data) >= c.max {
				delete(c.data, key)
			}
			if len(c.data) < c.max/2 {
				break
			}
		}
	}
	c.data[k] = cacheEntry[T]{v: v, at: time.Now()}
}

func New(st *store.Store, cfg *config.Config, tmdb *metadata.TMDB) *Service {
	s := &Service{store: st, cfg: cfg, tmdb: tmdb, genres: map[string]map[int]string{}, details: newTTLCache[*Detail](10*time.Minute, 500), search: newTTLCache[[]Item](5*time.Minute, 300)}
	s.sonarr = arr.New("sonarr", cfg.SonarrURL, cfg.SonarrAPIKey, cfg.SonarrRootFolder, cfg.SonarrQualityProfile)
	s.radarr = arr.New("radarr", cfg.RadarrURL, cfg.RadarrAPIKey, cfg.RadarrRootFolder, cfg.RadarrQualityProfile)
	if s.sonarr != nil {
		slog.Info("discover: Sonarr configured", "url", cfg.SonarrURL)
	}
	if s.radarr != nil {
		slog.Info("discover: Radarr configured", "url", cfg.RadarrURL)
	}
	return s
}

// Enabled is false without a TMDB key.
func (s *Service) Enabled() bool { return s != nil && s.tmdb != nil }

func (s *Service) RequestsEnabled(mediaType string) bool {
	if mediaType == "movie" {
		return s.radarr != nil
	}
	return s.sonarr != nil
}

// ---- TMDB wire shapes (lists) ----

type tmdbListEntry struct {
	ID            int      `json:"id"`
	MediaType     string   `json:"media_type"`
	Title         string   `json:"title"`
	Name          string   `json:"name"`
	Overview      string   `json:"overview"`
	PosterPath    string   `json:"poster_path"`
	BackdropPath  string   `json:"backdrop_path"`
	VoteAverage   float64  `json:"vote_average"`
	ReleaseDate   string   `json:"release_date"`
	FirstAirDate  string   `json:"first_air_date"`
	GenreIDs      []int    `json:"genre_ids"`
	OriginCountry []string `json:"origin_country"`
	OriginalLang  string   `json:"original_language"`
}

type tmdbList struct {
	Results []tmdbListEntry `json:"results"`
}

func (s *Service) genreNames(ctx context.Context, kind string) map[int]string {
	s.mu.Lock()
	g, ok := s.genres[kind]
	s.mu.Unlock()
	if ok {
		return g
	}
	var resp struct {
		Genres []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"genres"`
	}
	g = map[int]string{}
	if err := s.tmdb.Get(ctx, "/genre/"+kind+"/list", nil, &resp); err == nil {
		for _, x := range resp.Genres {
			g[x.ID] = x.Name
		}
	}
	s.mu.Lock()
	s.genres[kind] = g
	s.mu.Unlock()
	return g
}

func (s *Service) toItem(ctx context.Context, e tmdbListEntry, kind string) Item {
	if e.MediaType != "" {
		kind = e.MediaType
	}
	title, date := e.Title, e.ReleaseDate
	if kind == "tv" {
		title, date = e.Name, e.FirstAirDate
	}
	year := 0
	if len(date) >= 4 {
		year, _ = strconv.Atoi(date[:4])
	}
	names := s.genreNames(ctx, kind)
	var genres []string
	anime := false
	for _, id := range e.GenreIDs {
		if n := names[id]; n != "" {
			genres = append(genres, n)
		}
		if id == 16 {
			anime = true
		}
	}
	jp := e.OriginalLang == "ja"
	for _, c := range e.OriginCountry {
		if c == "JP" {
			jp = true
		}
	}
	return Item{
		TMDBID: e.ID, Type: kind, Title: title, Year: year, Overview: e.Overview,
		Poster: s.tmdb.ImageURL("w342", e.PosterPath), Backdrop: s.tmdb.ImageURL("w780", e.BackdropPath),
		Rating: round1(e.VoteAverage), Genres: genres, IsAnime: anime && jp,
	}
}

func round1(f float64) float64 { return float64(int(f*10+0.5)) / 10 }

// annotate fills LibraryItemID and RequestStatus for a batch.
func (s *Service) annotate(ctx context.Context, items []Item) {
	ids := make([]string, 0, len(items))
	for _, it := range items {
		ids = append(ids, strconv.Itoa(it.TMDBID))
	}
	lib, err := s.store.ItemsByProviderIDs(ctx, "tmdb", ids)
	if err != nil {
		slog.Warn("discover: library lookup", "err", err)
	}
	reqs, err := s.store.RequestsByTMDB(ctx)
	if err != nil {
		slog.Warn("discover: requests lookup", "err", err)
	}
	for i := range items {
		it := &items[i]
		if li, ok := lib["tmdb:"+strconv.Itoa(it.TMDBID)]; ok && sameKind(li, it.Type) {
			it.LibraryItemID = li.ID
		}
		if r, ok := reqs[it.Type+":"+strconv.Itoa(it.TMDBID)]; ok {
			it.RequestStatus = r.Status
		}
	}
}

func sameKind(li model.Item, kind string) bool {
	if kind == "movie" {
		return li.Type == model.TypeMovie
	}
	return li.Type == model.TypeSeries
}

func (s *Service) list(ctx context.Context, path string, q url.Values, kind string, limit int) ([]Item, error) {
	var resp tmdbList
	if err := s.tmdb.Get(ctx, path, q, &resp); err != nil {
		return nil, err
	}
	var out []Item
	for _, e := range resp.Results {
		if e.MediaType != "" && e.MediaType != "tv" && e.MediaType != "movie" {
			continue
		}
		if e.PosterPath == "" {
			continue
		}
		out = append(out, s.toItem(ctx, e, kind))
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// Rows is the Discover front page, cached for 30 minutes (library / request state is
// refreshed on every call since it is local and cheap).
func (s *Service) Rows(ctx context.Context) ([]Row, error) {
	s.mu.Lock()
	cached, at := s.rows, s.rowsAt
	s.mu.Unlock()
	if cached == nil || time.Since(at) > 30*time.Minute {
		rows, err := s.buildRows(ctx)
		if err != nil {
			if cached == nil {
				return nil, err
			}
			slog.Warn("discover: refresh failed; serving cached rows", "err", err)
		} else {
			s.mu.Lock()
			s.rows, s.rowsAt = rows, time.Now()
			s.mu.Unlock()
			cached = rows
		}
	}
	// Copy so annotation does not race with other readers.
	out := make([]Row, len(cached))
	for i, r := range cached {
		items := append([]Item(nil), r.Items...)
		s.annotate(ctx, items)
		out[i] = Row{Key: r.Key, Title: r.Title, Items: items}
	}
	return out, nil
}

func (s *Service) buildRows(ctx context.Context) ([]Row, error) {
	today := time.Now().UTC()
	anime := url.Values{"with_genres": {"16"}, "with_origin_country": {"JP"}, "include_adult": {"false"}}
	type spec struct {
		key, title, path, kind string
		q                      url.Values
	}
	specs := []spec{
		{"trending", "Trending this week", "/trending/all/week", "", nil},
		{"anime-new", "New anime this season", "/discover/tv", "tv", with(anime, url.Values{
			"sort_by": {"popularity.desc"}, "first_air_date.gte": {today.AddDate(0, 0, -120).Format("2006-01-02")}, "first_air_date.lte": {today.Format("2006-01-02")},
		})},
		{"anime-popular", "Popular anime", "/discover/tv", "tv", with(anime, url.Values{"sort_by": {"popularity.desc"}})},
		{"anime-top", "Top rated anime", "/discover/tv", "tv", with(anime, url.Values{"sort_by": {"vote_average.desc"}, "vote_count.gte": {"300"}})},
		{"anime-movies", "Anime movies", "/discover/movie", "movie", with(anime, url.Values{"sort_by": {"popularity.desc"}, "vote_count.gte": {"50"}})},
		{"movies-popular", "Popular movies", "/movie/popular", "movie", nil},
		{"movies-upcoming", "Upcoming movies", "/movie/upcoming", "movie", url.Values{"region": {"US"}}},
		{"tv-popular", "Popular series", "/tv/popular", "tv", nil},
	}
	var rows []Row
	var firstErr error
	for _, sp := range specs {
		items, err := s.list(ctx, sp.path, sp.q, sp.kind, 20)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			slog.Warn("discover row failed", "row", sp.key, "err", err)
			continue
		}
		if len(items) > 0 {
			rows = append(rows, Row{Key: sp.key, Title: sp.title, Items: items})
		}
	}
	if len(rows) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return rows, nil
}

func with(base url.Values, extra url.Values) url.Values {
	q := url.Values{}
	for k, v := range base {
		q[k] = v
	}
	for k, v := range extra {
		q[k] = v
	}
	return q
}

// Search runs TMDB multi search (TV and movies only).
func (s *Service) Search(ctx context.Context, query string) ([]Item, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return []Item{}, nil
	}
	if len(query) > 120 {
		query = query[:120]
	}
	key := strings.ToLower(query)
	items, ok := s.search.get(key)
	if !ok {
		var err error
		items, err = s.list(ctx, "/search/multi", url.Values{"query": {query}, "include_adult": {"false"}}, "", 30)
		if err != nil {
			return nil, err
		}
		s.search.put(key, items)
	}
	out := append([]Item(nil), items...)
	s.annotate(ctx, out)
	return out, nil
}

// ---- detail ----

type tmdbDetail struct {
	tmdbListEntry
	Tagline string `json:"tagline"`
	Status  string `json:"status"`
	Runtime int    `json:"runtime"`
	Genres  []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"genres"`
	Networks []struct {
		Name string `json:"name"`
	} `json:"networks"`
	NumSeasons     int   `json:"number_of_seasons"`
	NumEpisodes    int   `json:"number_of_episodes"`
	EpisodeRunTime []int `json:"episode_run_time"`
	ExternalIDs    *struct {
		TvdbID int    `json:"tvdb_id"`
		ImdbID string `json:"imdb_id"`
	} `json:"external_ids"`
}

func (s *Service) Detail(ctx context.Context, kind string, tmdbID int) (*Detail, error) {
	if kind != "tv" && kind != "movie" {
		return nil, errors.New("type must be tv or movie")
	}
	key := fmt.Sprintf("%s:%d", kind, tmdbID)
	base, ok := s.details.get(key)
	if !ok {
		var d tmdbDetail
		if err := s.tmdb.Get(ctx, fmt.Sprintf("/%s/%d", kind, tmdbID), url.Values{"append_to_response": {"external_ids"}}, &d); err != nil {
			return nil, err
		}
		for _, g := range d.Genres {
			d.GenreIDs = append(d.GenreIDs, g.ID)
		}
		item := s.toItem(ctx, d.tmdbListEntry, kind)
		item.Backdrop = s.tmdb.ImageURL("w1280", d.BackdropPath)
		item.Poster = s.tmdb.ImageURL("w500", d.PosterPath)
		base = &Detail{Item: item, Tagline: d.Tagline, Status: d.Status, Seasons: d.NumSeasons, Episodes: d.NumEpisodes, RuntimeM: d.Runtime}
		if base.RuntimeM == 0 && len(d.EpisodeRunTime) > 0 {
			base.RuntimeM = d.EpisodeRunTime[0]
		}
		for _, n := range d.Networks {
			base.Networks = append(base.Networks, n.Name)
		}
		if d.ExternalIDs != nil {
			base.TVDBID, base.IMDBID = d.ExternalIDs.TvdbID, d.ExternalIDs.ImdbID
		}
		s.details.put(key, base)
	}
	// Local state (library, requests) is fresh on every call; the TMDB part is the cached copy.
	det := *base
	items := []Item{det.Item}
	s.annotate(ctx, items)
	det.Item = items[0]
	det.Requests = s.RequestsEnabled(kind)
	det.Request = nil
	if r, err := s.store.GetRequest(ctx, kind, tmdbID); err == nil && r != nil {
		det.Request = r
	}
	return &det, nil
}

// ---- requests ----

// Request records a request and forwards it to Sonarr / Radarr when configured.
// Without them it stays "pending" (shown to the household; sent when configured and
// retried through RetryPending).
func (s *Service) Request(ctx context.Context, kind string, tmdbID int, user *model.Principal) (*store.Request, error) {
	det, err := s.Detail(ctx, kind, tmdbID)
	if err != nil {
		return nil, err
	}
	if det.LibraryItemID != "" {
		return nil, errors.New("already in the library")
	}
	r := det.Request
	if r == nil {
		r = &store.Request{ID: newID(), MediaType: kind, TMDBID: tmdbID, Title: det.Title, Year: det.Year, PosterPath: det.Poster, UserID: user.UserID, Status: "pending"}
		if user.User != nil {
			r.UserName = user.User.Name
		}
	} else if r.Status == "sent" {
		return r, nil
	}
	s.send(ctx, r, det)
	if err := s.store.UpsertRequest(ctx, r); err != nil {
		return nil, err
	}
	return r, nil
}

func (s *Service) send(ctx context.Context, r *store.Request, det *Detail) {
	switch r.MediaType {
	case "tv":
		if s.sonarr == nil {
			return
		}
		if det.TVDBID == 0 {
			r.Status, r.Error = "failed", "TMDB has no TVDB id for this series"
			return
		}
		id, err := s.sonarr.AddSeries(ctx, det.TVDBID, det.IsAnime)
		if err != nil {
			r.Status, r.Error = "failed", err.Error()
			slog.Warn("discover: sonarr add failed", "title", r.Title, "err", err)
			return
		}
		r.Status, r.ArrID, r.Error = "sent", id, ""
	case "movie":
		if s.radarr == nil {
			return
		}
		id, err := s.radarr.AddMovie(ctx, r.TMDBID)
		if err != nil {
			r.Status, r.Error = "failed", err.Error()
			slog.Warn("discover: radarr add failed", "title", r.Title, "err", err)
			return
		}
		r.Status, r.ArrID, r.Error = "sent", id, ""
	}
}

// Requests lists every request with its library availability.
func (s *Service) Requests(ctx context.Context) ([]Item, error) {
	all, err := s.store.ListRequests(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]Item, 0, len(all))
	for _, r := range all {
		items = append(items, Item{TMDBID: r.TMDBID, Type: r.MediaType, Title: r.Title, Year: r.Year, Poster: r.PosterPath, RequestStatus: r.Status})
	}
	s.annotate(ctx, items)
	return items, nil
}

// Delete withdraws a request; only the requester or an admin may.
func (s *Service) Delete(ctx context.Context, id string, user *model.Principal) error {
	all, err := s.store.ListRequests(ctx)
	if err != nil {
		return err
	}
	for _, r := range all {
		if r.ID == id {
			if r.UserID != user.UserID && !user.IsAdmin {
				return errors.New("only the requester or an admin can withdraw a request")
			}
			return s.store.DeleteRequest(ctx, id)
		}
	}
	return errors.New("request not found")
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
