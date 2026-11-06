// Package discover is Roxy's request flow: TMDB browsing and search with "in your library"
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
	// Character is the role, on a person's credits.
	Character string `json:"character,omitempty"`
	// Set when the library already has this title.
	LibraryItemID string `json:"libraryItemId,omitempty"`
	// Set when someone asked for it: pending | sent | failed.
	RequestStatus string `json:"requestStatus,omitempty"`
	// Download is the request's download status once Sonarr / Radarr has it (lists carry it too).
	Download *Download `json:"download,omitempty"`
}

// Season is one TMDB season with what the library and the request list say about it.
type Season struct {
	Number    int    `json:"number"`
	Name      string `json:"name"`
	Episodes  int    `json:"episodes"`
	AirDate   string `json:"airDate,omitempty"`
	Poster    string `json:"poster,omitempty"`
	Have      int    `json:"have"`      // episodes with a file in the library
	Known     int    `json:"known"`     // episodes the library knows (files + placeholders)
	Requested bool   `json:"requested"` // covered by a pending / sent request
	// Specials (TMDB season 0): requested one episode at a time, never as a unit.
	Special bool `json:"special,omitempty"`
}

// Detail is the title page's data.
type Detail struct {
	Item
	Seasons []Season `json:"seasons,omitempty"`
	Tagline string   `json:"tagline,omitempty"`
	Status  string   `json:"status,omitempty"`
	// Logo is the title's logo art (English or language-less), drawn over the banner
	// like the library's title pages; empty when TMDB has none.
	Logo string `json:"logo,omitempty"`
	// OfficialRating is the US age rating (TV-14, PG-13), shown as the age badge.
	OfficialRating string         `json:"officialRating,omitempty"`
	SeasonCount    int            `json:"seasonCount,omitempty"`
	Episodes       int            `json:"episodes,omitempty"`
	RuntimeM       int            `json:"runtimeMinutes,omitempty"`
	Networks       []string       `json:"networks,omitempty"`
	TVDBID         int            `json:"tvdbId,omitempty"`
	IMDBID         string         `json:"imdbId,omitempty"`
	Request        *store.Request `json:"request,omitempty"`
	Requests       bool           `json:"requestsEnabled"` // Sonarr / Radarr configured for this type
	// SeasonLayout says where Seasons came from: "tmdb", or "episodeGroup" when TMDB's
	// "Seasons" grouping (TVDB's numbering, what Sonarr uses) replaced its merged season.
	SeasonLayout string `json:"seasonLayout,omitempty"`
	// groupEpisodes holds each season's episodes when the layout is an episode group.
	groupEpisodes map[int][]Episode
}

type Row struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	Items []Item `json:"items"`
}

// ArrSettings is the admin-editable arr configuration (settings key "arr"); the
// ROXY_SONARR_* / ROXY_RADARR_* variables seed it when nothing is saved.
type ArrSettings struct {
	Sonarr arr.Config `json:"sonarr"`
	Radarr arr.Config `json:"radarr"`
	// Webhook paths: how the arr apps' paths map onto Roxy's (Docker mounts differ).
	// Read by internal/arrhook; empty means "match by suffix under the library roots".
	PathMappings []PathMapping `json:"pathMappings,omitempty"`
}

type PathMapping struct {
	From string `json:"from"`
	To   string `json:"to"`
}

const settingsKey = "arr"

type Service struct {
	store  *store.Store
	cfg    *config.Config
	tmdb   *metadata.TMDB
	sonarr *arr.Client
	radarr *arr.Client
	arrCfg ArrSettings
	// prowlarr finds season packs Sonarr cannot parse (nil when not configured).
	prowlarr *arr.Prowlarr
	packs    packState
	// importHook adds files an arr app imported to the library (see SetImportHook).
	importHook func(ctx context.Context, arrPaths []string) int
	browse     *ttlCache[*BrowseResult]
	people     *ttlCache[*Person]
	similar    *ttlCache[[]Item]

	mu     sync.Mutex
	genres map[string]map[int]string // "tv"/"movie" -> id -> name
	rows   []Row
	rowsAt time.Time
	// TMDB is a shared public service: besides the client's own token bucket (shared
	// with metadata matching), details and searches are cached so typing in the search
	// box or reopening a page does not cost another call.
	details  *ttlCache[*Detail]
	search   *ttlCache[[]Item]
	episodes *ttlCache[[]Episode]
	dl       tracker
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
	s := &Service{store: st, cfg: cfg, tmdb: tmdb, genres: map[string]map[int]string{}, details: newTTLCache[*Detail](10*time.Minute, 500), search: newTTLCache[[]Item](5*time.Minute, 300), browse: newTTLCache[*BrowseResult](5*time.Minute, 300), people: newTTLCache[*Person](time.Hour, 300), similar: newTTLCache[[]Item](time.Hour, 500), episodes: newTTLCache[[]Episode](10*time.Minute, 500)}
	s.dl = tracker{status: map[string]*Download{}, progress: map[string]progressMark{}, kick: make(chan struct{}, 1), refresh: map[string]time.Time{}, stalled: map[string]time.Time{}}
	s.loadArr(context.Background())
	return s
}

// loadArr builds the arr clients from the saved settings, falling back to the environment.
func (s *Service) loadArr(ctx context.Context) {
	cfg := ArrSettings{
		Sonarr: arr.Config{URL: s.cfg.SonarrURL, APIKey: s.cfg.SonarrAPIKey, RootFolder: s.cfg.SonarrRootFolder, QualityProfileID: atoi(s.cfg.SonarrQualityProfile)},
		Radarr: arr.Config{URL: s.cfg.RadarrURL, APIKey: s.cfg.RadarrAPIKey, RootFolder: s.cfg.RadarrRootFolder, QualityProfileID: atoi(s.cfg.RadarrQualityProfile)},
	}
	if ok, err := s.store.GetSetting(ctx, settingsKey, &cfg); err != nil {
		slog.Warn("discover: arr settings unreadable; using environment", "err", err)
	} else if !ok {
		slog.Debug("discover: no saved arr settings; using environment")
	}
	s.mu.Lock()
	s.arrCfg = cfg
	s.sonarr = arr.New("sonarr", cfg.Sonarr)
	s.radarr = arr.New("radarr", cfg.Radarr)
	s.prowlarr = arr.NewProwlarr(s.cfg.ProwlarrURL, s.cfg.ProwlarrAPIKey)
	if s.prowlarr != nil && s.sonarr != nil {
		s.sonarr.SetPackFinder(s.packFinder(s.sonarr))
	}
	s.mu.Unlock()
	if s.sonarr != nil {
		slog.Info("discover: Sonarr configured", "url", cfg.Sonarr.URL)
	}
	if s.radarr != nil {
		slog.Info("discover: Radarr configured", "url", cfg.Radarr.URL)
	}
	if s.prowlarr != nil {
		slog.Info("discover: Prowlarr configured (season packs Sonarr can't parse)", "url", s.cfg.ProwlarrURL)
	}
}

func atoi(v string) int { n, _ := strconv.Atoi(strings.TrimSpace(v)); return n }

// ArrSettings returns the saved configuration (the caller masks secrets for the wire).
func (s *Service) ArrSettings() ArrSettings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.arrCfg
}

// SaveArrSettings stores the configuration, rebuilds the clients and sends pending requests.
func (s *Service) SaveArrSettings(ctx context.Context, cfg ArrSettings) error {
	if err := s.store.SetSetting(ctx, settingsKey, cfg); err != nil {
		return err
	}
	s.loadArr(ctx)
	// A server that was removed or replaced: its failed requests go back to pending so
	// they are retried against the new one instead of showing a stale error.
	if all, err := s.store.ListRequests(ctx); err == nil {
		for i := range all {
			r := &all[i]
			if r.Status == "failed" {
				r.Status, r.Error = "pending", ""
				_ = s.store.UpsertRequest(ctx, r)
			}
		}
	}
	go s.RetryPending(context.Background())
	return nil
}

// TestArr probes an arr server with the given URL and key (not necessarily saved yet).
func (s *Service) TestArr(ctx context.Context, kind string, cfg arr.Config) (*arr.Probe, error) {
	if kind != "sonarr" && kind != "radarr" {
		return nil, errors.New("kind must be sonarr or radarr")
	}
	c := arr.New(kind, cfg)
	if c == nil {
		return nil, errors.New("URL and API key are required")
	}
	return c.Test(ctx)
}

// RetryPending sends every pending (or failed) request to its arr app; a no-op for
// kinds without a configured server. Called after settings change and periodically.
func (s *Service) RetryPending(ctx context.Context) {
	all, err := s.store.ListRequests(ctx)
	if err != nil {
		return
	}
	for i := range all {
		r := &all[i]
		// Sent ones are done; ones waiting for (or refused) approval wait for an admin.
		if r.Status == "sent" || r.Status == "approval" || r.Status == "declined" || !s.RequestsEnabled(r.MediaType) {
			continue
		}
		det, err := s.Detail(ctx, r.MediaType, r.TMDBID)
		if err != nil {
			slog.Warn("discover: retry detail", "title", r.Title, "err", err)
			continue
		}
		if det.LibraryItemID != "" && r.MediaType == "movie" {
			continue // already there; the request shows as available (a series may still lack seasons)
		}
		s.send(ctx, r, det)
		if err := s.store.UpsertRequest(ctx, r); err != nil {
			slog.Warn("discover: retry save", "title", r.Title, "err", err)
		}
	}
}

// Start runs the pending-request retry every 10 minutes and the download tracker.
func (s *Service) Start(ctx context.Context) {
	s.startTracker(ctx)
	s.startPackSweep(ctx)
	go func() {
		t := time.NewTicker(10 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.RetryPending(ctx)
			}
		}
	}()
}

// Enabled is false without a TMDB key.
func (s *Service) Enabled() bool { return s != nil && s.tmdb.HasKey() }

func (s *Service) RequestsEnabled(mediaType string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if mediaType == "movie" {
		return s.radarr != nil && s.arrCfg.Radarr.ReadyForRequests()
	}
	return s.sonarr != nil && s.arrCfg.Sonarr.ReadyForRequests()
}

func (s *Service) clients() (*arr.Client, *arr.Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sonarr, s.radarr
}

// ---- browse (filters) ----

// BrowseQuery is the filter bar: everything optional.
type BrowseQuery struct {
	Type      string // tv | movie
	Sort      string // popular | top | newest | oldest | votes
	Genres    []int  // all must match
	Language  string // original language, ISO 639-1 ("ja", "ko", "en")
	Anime     bool   // Animation + Japanese
	YearFrom  int
	YearTo    int
	MinRating float64
	Status    string // tv: airing | ended
	Page      int
}

type BrowseResult struct {
	Items      []Item `json:"items"`
	Page       int    `json:"page"`
	TotalPages int    `json:"totalPages"`
}

func (q BrowseQuery) key() string {
	g := make([]string, len(q.Genres))
	for i, x := range q.Genres {
		g[i] = strconv.Itoa(x)
	}
	return fmt.Sprintf("%s|%s|%s|%s|%t|%d|%d|%.1f|%s|%d", q.Type, q.Sort, strings.Join(g, ","), q.Language, q.Anime, q.YearFrom, q.YearTo, q.MinRating, q.Status, q.Page)
}

// Browse maps the filter bar onto TMDB's discover endpoint.
func (s *Service) Browse(ctx context.Context, q BrowseQuery) (*BrowseResult, error) {
	if q.Type != "movie" {
		q.Type = "tv"
	}
	if q.Page < 1 {
		q.Page = 1
	}
	if q.Page > 50 {
		q.Page = 50
	}
	if res, ok := s.browse.get(q.key()); ok {
		out := &BrowseResult{Items: append([]Item(nil), res.Items...), Page: res.Page, TotalPages: res.TotalPages}
		s.annotate(ctx, out.Items)
		return out, nil
	}
	v := url.Values{"include_adult": {"false"}, "page": {strconv.Itoa(q.Page)}}
	dateKey := "primary_release_date"
	if q.Type == "tv" {
		dateKey = "first_air_date"
	}
	switch q.Sort {
	case "top":
		v.Set("sort_by", "vote_average.desc")
		v.Set("vote_count.gte", "200")
	case "newest":
		v.Set("sort_by", dateKey+".desc")
		v.Set("vote_count.gte", "20")
	case "oldest":
		v.Set("sort_by", dateKey+".asc")
		v.Set("vote_count.gte", "20")
	case "votes":
		v.Set("sort_by", "vote_count.desc")
	default:
		v.Set("sort_by", "popularity.desc")
	}
	genres := append([]int(nil), q.Genres...)
	if q.Anime {
		genres = append(genres, 16)
		v.Set("with_original_language", "ja")
	} else if q.Language != "" {
		v.Set("with_original_language", q.Language)
	}
	if len(genres) > 0 {
		g := make([]string, len(genres))
		for i, x := range genres {
			g[i] = strconv.Itoa(x)
		}
		v.Set("with_genres", strings.Join(g, ","))
	}
	if q.YearFrom > 0 {
		v.Set(dateKey+".gte", fmt.Sprintf("%04d-01-01", q.YearFrom))
	}
	if q.YearTo > 0 {
		v.Set(dateKey+".lte", fmt.Sprintf("%04d-12-31", q.YearTo))
	}
	if q.MinRating > 0 {
		v.Set("vote_average.gte", fmt.Sprintf("%.1f", q.MinRating))
		if v.Get("vote_count.gte") == "" {
			v.Set("vote_count.gte", "50")
		}
	}
	if q.Type == "tv" {
		switch q.Status {
		case "airing":
			v.Set("with_status", "0")
		case "ended":
			v.Set("with_status", "3")
		}
	}
	var resp struct {
		Page       int             `json:"page"`
		TotalPages int             `json:"total_pages"`
		Results    []tmdbListEntry `json:"results"`
	}
	if err := s.tmdb.Get(ctx, "/discover/"+q.Type, v, &resp); err != nil {
		return nil, err
	}
	res := &BrowseResult{Page: resp.Page, TotalPages: resp.TotalPages}
	for _, e := range resp.Results {
		if e.PosterPath == "" {
			continue
		}
		res.Items = append(res.Items, s.toItem(ctx, e, q.Type))
	}
	if res.Items == nil {
		res.Items = []Item{}
	}
	s.browse.put(q.key(), res)
	out := &BrowseResult{Items: append([]Item(nil), res.Items...), Page: res.Page, TotalPages: res.TotalPages}
	s.annotate(ctx, out.Items)
	return out, nil
}

// Genre is one TMDB genre for the filter sheet.
type Genre struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// Genres lists TMDB's genres for tv or movie, sorted by name.
func (s *Service) Genres(ctx context.Context, kind string) []Genre {
	if kind != "movie" {
		kind = "tv"
	}
	names := s.genreNames(ctx, kind)
	out := make([]Genre, 0, len(names))
	for id, n := range names {
		out = append(out, Genre{id, n})
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Name < out[j-1].Name; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
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
			// Every row shows the same corner box as "Your requests".
			if it.Download == nil {
				it.Download = s.Download(r.ID)
			}
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
	NumSeasons  int `json:"number_of_seasons"`
	NumEpisodes int `json:"number_of_episodes"`
	SeasonList  []struct {
		SeasonNumber int    `json:"season_number"`
		Name         string `json:"name"`
		EpisodeCount int    `json:"episode_count"`
		AirDate      string `json:"air_date"`
		PosterPath   string `json:"poster_path"`
	} `json:"seasons"`
	EpisodeRunTime []int `json:"episode_run_time"`
	ExternalIDs    *struct {
		TvdbID int    `json:"tvdb_id"`
		ImdbID string `json:"imdb_id"`
	} `json:"external_ids"`
	Images *struct {
		Backdrops []tmdbArt `json:"backdrops"`
		Logos     []tmdbArt `json:"logos"`
	} `json:"images"`
	ContentRatings *struct { // tv
		Results []struct {
			Country string `json:"iso_3166_1"`
			Rating  string `json:"rating"`
		} `json:"results"`
	} `json:"content_ratings"`
	ReleaseDates *struct { // movie
		Results []struct {
			Country string `json:"iso_3166_1"`
			Dates   []struct {
				Certification string `json:"certification"`
			} `json:"release_dates"`
		} `json:"results"`
	} `json:"release_dates"`
}

type tmdbArt struct {
	FilePath string `json:"file_path"`
	Lang     string `json:"iso_639_1"`
}

// pickArt returns the first (most voted) image in lang, skipping SVGs the app can't draw.
func pickArt(list []tmdbArt, lang string) string {
	for _, a := range list {
		if a.Lang == lang && !strings.HasSuffix(strings.ToLower(a.FilePath), ".svg") {
			return a.FilePath
		}
	}
	return ""
}

// usRating is the title's US age rating from TMDB's content ratings / release dates.
func (d *tmdbDetail) usRating() string {
	if d.ContentRatings != nil {
		for _, r := range d.ContentRatings.Results {
			if r.Country == "US" && r.Rating != "" {
				return r.Rating
			}
		}
	}
	if d.ReleaseDates != nil {
		for _, r := range d.ReleaseDates.Results {
			if r.Country == "US" {
				for _, x := range r.Dates {
					if x.Certification != "" {
						return x.Certification
					}
				}
			}
		}
	}
	return ""
}

func (s *Service) Detail(ctx context.Context, kind string, tmdbID int) (*Detail, error) {
	if kind != "tv" && kind != "movie" {
		return nil, errors.New("type must be tv or movie")
	}
	key := fmt.Sprintf("%s:%d", kind, tmdbID)
	base, ok := s.details.get(key)
	if !ok {
		var d tmdbDetail
		extra := "external_ids,images,content_ratings"
		if kind == "movie" {
			extra = "external_ids,images,release_dates"
		}
		q := url.Values{"append_to_response": {extra}, "include_image_language": {"en,null"}}
		if err := s.tmdb.Get(ctx, fmt.Sprintf("/%s/%d", kind, tmdbID), q, &d); err != nil {
			return nil, err
		}
		for _, g := range d.Genres {
			d.GenreIDs = append(d.GenreIDs, g.ID)
		}
		item := s.toItem(ctx, d.tmdbListEntry, kind)
		item.Backdrop = s.tmdb.ImageURL("w1280", d.BackdropPath)
		item.Poster = s.tmdb.ImageURL("w500", d.PosterPath)
		base = &Detail{Item: item, Tagline: d.Tagline, Status: d.Status, SeasonCount: d.NumSeasons, Episodes: d.NumEpisodes, RuntimeM: d.Runtime, OfficialRating: d.usRating()}
		if d.Images != nil {
			// Text-free art under the logo, like the library's banners.
			if b := pickArt(d.Images.Backdrops, ""); b != "" {
				base.Backdrop = s.tmdb.ImageURL("w1280", b)
			}
			if l := pickArt(d.Images.Logos, "en"); l != "" {
				base.Logo = s.tmdb.ImageURL("w500", l)
			} else if l := pickArt(d.Images.Logos, ""); l != "" {
				base.Logo = s.tmdb.ImageURL("w500", l)
			}
		}
		if base.RuntimeM == 0 && len(d.EpisodeRunTime) > 0 {
			base.RuntimeM = d.EpisodeRunTime[0]
		}
		for _, n := range d.Networks {
			base.Networks = append(base.Networks, n.Name)
		}
		var specials *Season
		for _, sn := range d.SeasonList {
			season := Season{Number: sn.SeasonNumber, Name: sn.Name, Episodes: sn.EpisodeCount, AirDate: sn.AirDate, Poster: s.tmdb.ImageURL("w185", sn.PosterPath), Special: sn.SeasonNumber == 0}
			if season.Special {
				if sn.EpisodeCount > 0 {
					specials = &season
				}
				continue
			}
			base.Seasons = append(base.Seasons, season)
		}
		if specials != nil {
			base.Seasons = append(base.Seasons, *specials) // last, like Jellyfin
		}
		if kind == "tv" {
			base.SeasonLayout = "tmdb"
			regular, regularEps := 0, 0
			for _, sn := range base.Seasons {
				if !sn.Special {
					regular++
					regularEps += sn.Episodes
				}
			}
			if seasons, eps, ok := s.groupedSeasons(ctx, tmdbID, regular, regularEps); ok {
				base.Seasons, base.groupEpisodes, base.SeasonLayout = seasons, eps, "episodeGroup"
				base.SeasonCount = 0
				for _, sn := range seasons {
					if !sn.Special {
						base.SeasonCount++
					}
				}
			}
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
	det.Download = nil
	if r, err := s.store.GetRequest(ctx, kind, tmdbID); err == nil && r != nil {
		det.Request = r
		det.Download = s.Download(r.ID)
	}
	// Seasons: library counts and request coverage are live; copy the slice first.
	det.Seasons = append([]Season(nil), base.Seasons...)
	if det.LibraryItemID != "" {
		if counts, err := s.store.SeasonAvailability(ctx, det.LibraryItemID); err == nil {
			for i := range det.Seasons {
				c := counts[det.Seasons[i].Number]
				det.Seasons[i].Have, det.Seasons[i].Known = c.Have, c.Total
			}
		}
	}
	if det.Request != nil && det.Request.Status != "failed" && det.Request.Status != "declined" {
		for i := range det.Seasons {
			if det.Seasons[i].Special {
				// All specials only when asked for as a season; single ones show per episode.
				det.Seasons[i].Requested = contains(det.Request.Seasons, 0)
				continue
			}
			det.Seasons[i].Requested = len(det.Request.Seasons) == 0 || contains(det.Request.Seasons, det.Seasons[i].Number)
		}
	}
	return &det, nil
}

// ---- requests ----

// Request records a request and forwards it to Sonarr / Radarr when configured.
// Without them it stays "pending" (shown to the household; sent when configured and
// retried through RetryPending). For series, seasons narrows it (empty = everything
// the library lacks); a later request for more seasons widens the existing one.
func (s *Service) Request(ctx context.Context, kind string, tmdbID int, user *model.Principal, seasons []int, episodes []string) (*store.Request, error) {
	det, err := s.Detail(ctx, kind, tmdbID)
	if err != nil {
		return nil, err
	}
	if kind == "tv" && len(episodes) > 0 {
		return s.requestEpisodes(ctx, det, user, episodes)
	}
	if kind == "movie" {
		seasons = nil
		if det.LibraryItemID != "" {
			return nil, errors.New("already in the library")
		}
	} else {
		// Everything requestable that is not complete in the library.
		var missing []int
		for _, sn := range det.Seasons {
			if sn.Special {
				continue
			}
			if sn.Known == 0 || sn.Have < sn.Episodes {
				missing = append(missing, sn.Number)
			}
		}
		if len(seasons) == 0 {
			if len(det.Seasons) > 0 && len(missing) == 0 {
				return nil, errors.New("every season is already in the library")
			}
			if len(missing) < len(det.Seasons) {
				seasons = missing // only ask for what is not there
			}
		} else {
			hasSpecials := false
			for _, sn := range det.Seasons {
				hasSpecials = hasSpecials || sn.Special
			}
			var keep []int
			for _, n := range seasons {
				// Specials (season 0) can be asked for as a whole, besides one by one.
				if contains(missing, n) || len(det.Seasons) == 0 || (n == 0 && hasSpecials) {
					keep = append(keep, n)
				}
			}
			if len(keep) == 0 {
				return nil, errors.New("those seasons are already in the library")
			}
			seasons = keep
		}
	}
	r := det.Request
	if r == nil {
		r = &store.Request{ID: newID(), MediaType: kind, TMDBID: tmdbID, Title: det.Title, Year: det.Year, PosterPath: det.Poster, UserID: user.UserID, Status: "pending", Seasons: seasons}
		if user.User != nil {
			r.UserName = user.User.Name
		}
		// Several people share one login as profiles: name the profile that asked.
		if user.ScopeID != "" {
			if pr, err := s.store.GetProfile(ctx, user.ScopeID); err == nil && pr != nil && pr.Name != "" {
				r.UserName = pr.Name
			}
		}
	} else {
		// Widen: union of seasons; whole-series stays whole. New seasons mean a new send.
		if len(r.Seasons) > 0 && len(seasons) > 0 {
			added := false
			for _, n := range seasons {
				if !contains(r.Seasons, n) {
					r.Seasons = append(r.Seasons, n)
					added = true
				}
			}
			// Real seasons replace the "episodes only" sentinel.
			if added && contains(r.Seasons, -1) {
				var kept []int
				for _, n := range r.Seasons {
					if n != -1 {
						kept = append(kept, n)
					}
				}
				r.Seasons = kept
			}
			if !added && r.Status == "sent" {
				if err := s.searchSeasonsAgain(ctx, r, seasons); err != nil {
					return nil, err
				}
				return r, nil
			}
		} else if len(r.Seasons) > 0 && len(seasons) == 0 {
			r.Seasons = nil
		} else if r.Status == "sent" {
			if err := s.searchSeasonsAgain(ctx, r, seasons); err != nil {
				return nil, err
			}
			return r, nil
		}
		if r.Status != "approval" {
			r.Status, r.Error = "pending", ""
		}
	}
	s.sendOrHold(ctx, r, det, user)
	if err := s.store.UpsertRequest(ctx, r); err != nil {
		return nil, err
	}
	return r, nil
}

// sendOrHold sends a request to Sonarr / Radarr, unless it comes from a basic user and
// the title hasn't been approved yet: then it waits as "approval" for an admin.
func (s *Service) sendOrHold(ctx context.Context, r *store.Request, det *Detail, user *model.Principal) {
	basic := user != nil && user.User != nil && user.User.Role == model.RoleBasic
	if basic && r.ArrID == 0 && (r.Status == "pending" || r.Status == "approval" || r.Status == "declined" || r.Status == "failed") {
		r.Status, r.Error = "approval", ""
		return
	}
	s.send(ctx, r, det)
}

// Approve sends a waiting request on (admin).
func (s *Service) Approve(ctx context.Context, id string) (*store.Request, error) {
	r, err := s.store.GetRequestByID(ctx, id)
	if err != nil || r == nil {
		return nil, errors.New("request not found")
	}
	if r.Status != "approval" && r.Status != "declined" {
		return r, nil
	}
	det, err := s.Detail(ctx, r.MediaType, r.TMDBID)
	if err != nil {
		return nil, err
	}
	r.Status, r.Error = "pending", ""
	s.send(ctx, r, det)
	return r, s.store.UpsertRequest(ctx, r)
}

// Decline turns a waiting request down (admin); reason is shown to the household.
func (s *Service) Decline(ctx context.Context, id, reason string) (*store.Request, error) {
	r, err := s.store.GetRequestByID(ctx, id)
	if err != nil || r == nil {
		return nil, errors.New("request not found")
	}
	if r.Status != "approval" {
		return nil, errors.New("only requests waiting for approval can be declined")
	}
	r.Status, r.Error = "declined", strings.TrimSpace(reason)
	return r, s.store.UpsertRequest(ctx, r)
}

// requestEpisodes adds individual episodes (specials) to the series' request. The
// series itself is added to Sonarr with nothing monitored beyond what was asked.
func (s *Service) requestEpisodes(ctx context.Context, det *Detail, user *model.Principal, episodes []string) (*store.Request, error) {
	r := det.Request
	if r == nil {
		r = &store.Request{ID: newID(), MediaType: "tv", TMDBID: det.TMDBID, Title: det.Title, Year: det.Year, PosterPath: det.Poster, UserID: user.UserID, Status: "pending", Seasons: []int{-1}}
		if user.User != nil {
			r.UserName = user.User.Name
		}
		if user.ScopeID != "" {
			if pr, err := s.store.GetProfile(ctx, user.ScopeID); err == nil && pr != nil && pr.Name != "" {
				r.UserName = pr.Name
			}
		}
	}
	added := false
	for _, code := range episodes {
		code = strings.ToUpper(strings.TrimSpace(code))
		if code == "" || containsStr(r.Episodes, code) {
			continue
		}
		r.Episodes = append(r.Episodes, code)
		added = true
	}
	if !added && r.Status == "sent" {
		if sonarr, _ := s.clients(); sonarr != nil && r.ArrID != 0 {
			want := codeSet(episodes)
			if err := s.searchMissing(ctx, sonarr, r, func(ep arr.Episode) bool { return want[epCode(ep.SeasonNumber, ep.EpisodeNumber)] }); err != nil {
				return nil, err
			}
			s.markSearching(r.ID, nil, episodes...)
			s.KickDownloads()
		}
		return r, nil
	}
	if r.Status != "approval" {
		r.Status, r.Error = "pending", ""
	}
	s.sendOrHold(ctx, r, det, user)
	if err := s.store.UpsertRequest(ctx, r); err != nil {
		return nil, err
	}
	return r, nil
}

func containsStr(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

// seasonsForSonarr turns the stored seasons into what AddSeries monitors. The sentinel
// -1 ("episodes only") is passed on alone, so AddSeries monitors just the episodes; empty
// is the whole series.
func seasonsForSonarr(r *store.Request) []int {
	var out []int
	for _, n := range r.Seasons {
		if n >= 0 {
			out = append(out, n)
		}
	}
	if len(out) == 0 && len(r.Seasons) > 0 {
		return []int{-1}
	}
	return out
}

func (s *Service) send(ctx context.Context, r *store.Request, det *Detail) {
	sonarr, radarr := s.clients()
	switch r.MediaType {
	case "tv":
		if sonarr == nil {
			return
		}
		if det.TVDBID == 0 {
			r.Status, r.Error = "failed", "TMDB has no TVDB id for this series"
			return
		}
		// Episodes (specials) are monitored inside AddSeries: for a new series only once
		// Sonarr has listed them, which takes a few seconds after the add.
		id, err := sonarr.AddSeries(ctx, det.TVDBID, det.IsAnime, seasonsForSonarr(r), r.Episodes)
		if err != nil {
			r.Status, r.Error = "failed", err.Error()
			slog.Warn("discover: sonarr add failed", "title", r.Title, "err", err)
			return
		}
		r.Status, r.ArrID, r.Error = "sent", id, ""
		s.KickDownloads()
	case "movie":
		if radarr == nil {
			return
		}
		id, err := radarr.AddMovie(ctx, r.TMDBID, det != nil && det.IsAnime)
		if err != nil {
			r.Status, r.Error = "failed", err.Error()
			slog.Warn("discover: radarr add failed", "title", r.Title, "err", err)
			return
		}
		r.Status, r.ArrID, r.Error = "sent", id, ""
		s.KickDownloads()
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
		it := Item{TMDBID: r.TMDBID, Type: r.MediaType, Title: r.Title, Year: r.Year, Poster: r.PosterPath, RequestStatus: r.Status, Download: s.Download(r.ID)}
		// "Anime" under the title, as on every other row (the detail is cached).
		if det, err := s.Detail(ctx, r.MediaType, r.TMDBID); err == nil {
			it.IsAnime = det.IsAnime
		}
		items = append(items, it)
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

func contains(xs []int, n int) bool {
	for _, x := range xs {
		if x == n {
			return true
		}
	}
	return false
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Episode is one episode of a season on Discover, with whether the library has it.
type Episode struct {
	Number   int    `json:"number"`
	Title    string `json:"title"`
	Overview string `json:"overview,omitempty"`
	AirDate  string `json:"airDate,omitempty"`
	Still    string `json:"still,omitempty"`
	Have     bool   `json:"have"`
	// Specials only: covered by a request (season requests never cover season 0).
	Requested bool `json:"requested,omitempty"`
	// Download: this episode's state while it is requested and has no file yet.
	Download *EpisodeDL `json:"download,omitempty"`
}

// Episodes lists a season's episodes from TMDB (cached 10 min) marked with what the
// library holds (so a half-complete season shows which half) and, for specials, whether
// each one is requested.
func (s *Service) Episodes(ctx context.Context, tmdbID, season int) ([]Episode, error) {
	key := fmt.Sprintf("eps:%d:%d", tmdbID, season)
	base, ok := s.episodes.get(key)
	// A show laid out by an episode group lists that group's episodes, numbered within
	// each season the way Sonarr counts them.
	if !ok {
		if det, err := s.Detail(ctx, "tv", tmdbID); err == nil && det.groupEpisodes != nil {
			if eps, found := det.groupEpisodes[season]; found {
				base, ok = eps, true
			}
		}
	}
	if !ok {
		var resp struct {
			Episodes []struct {
				EpisodeNumber int    `json:"episode_number"`
				Name          string `json:"name"`
				Overview      string `json:"overview"`
				AirDate       string `json:"air_date"`
				StillPath     string `json:"still_path"`
			} `json:"episodes"`
		}
		if err := s.tmdb.Get(ctx, fmt.Sprintf("/tv/%d/season/%d", tmdbID, season), nil, &resp); err != nil {
			return nil, err
		}
		base = make([]Episode, 0, len(resp.Episodes))
		for _, e := range resp.Episodes {
			base = append(base, Episode{Number: e.EpisodeNumber, Title: e.Name, Overview: e.Overview, AirDate: e.AirDate, Still: s.tmdb.ImageURL("w300", e.StillPath)})
		}
		s.episodes.put(key, base)
	}
	out := append([]Episode(nil), base...)
	if r, err := s.store.GetRequest(ctx, "tv", tmdbID); err == nil && r != nil && r.Status != "failed" && r.Status != "declined" {
		if season == 0 {
			for i := range out {
				out[i].Requested = containsStr(r.Episodes, fmt.Sprintf("S0E%d", out[i].Number)) || contains(r.Seasons, 0)
			}
		}
		if d := s.Download(r.ID); d != nil {
			for i := range out {
				for j := range d.Episodes {
					if e := d.Episodes[j]; e.Season == season && e.Episode == out[i].Number {
						out[i].Download = &e
						break
					}
				}
			}
		}
	}
	lib, err := s.store.ItemsByProviderIDs(ctx, "tmdb", []string{strconv.Itoa(tmdbID)})
	if err == nil {
		if li, ok := lib["tmdb:"+strconv.Itoa(tmdbID)]; ok && li.Type == model.TypeSeries {
			if files, err := s.store.EpisodeFiles(ctx, li.ID); err == nil {
				for i := range out {
					out[i].Have = files[season][out[i].Number]
				}
			}
		}
	}
	return out, nil
}
