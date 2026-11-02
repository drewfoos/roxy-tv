// Package arr talks to Sonarr and Radarr (API v3) to add titles the household requests
// from Discover. The arr apps do the searching and downloading; Roxy only adds the
// series or movie, monitored, with a search kicked off. The settings model follows
// request app's: one server each, a root folder and quality profile for new requests, separate
// ones for anime series, optional tags, and Radarr's minimum availability.
//
// Untested against live instances as of 2026-10-19 (the owner's arr stack runs on a
// server that is not reachable from the dev laptop); the calls follow the v3 API as
// request app uses it. Verify with Test connection on deploy day.
package arr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Config is what the admin saves for one arr server (request app's Services page, minus 4K).
type Config struct {
	URL    string `json:"url"`    // http://sonarr:8989, with any URL base included
	APIKey string `json:"apiKey"` // Settings → General → Security in the arr app
	// Defaults for new requests; ids and paths as the arr app reports them.
	RootFolder       string `json:"rootFolder"`
	QualityProfileID int    `json:"qualityProfileId"`
	// Sonarr only: used instead of the above when the series is anime (TMDB: Animation + Japan).
	AnimeRootFolder       string `json:"animeRootFolder,omitempty"`
	AnimeQualityProfileID int    `json:"animeQualityProfileId,omitempty"`
	Tags                  []int  `json:"tags,omitempty"`
	AnimeTags             []int  `json:"animeTags,omitempty"`
	// Sonarr only: the series type an anime request is added with, "standard" (default:
	// SxxEyy release names, which most groups and hand-tuned profiles expect) or "anime"
	// (absolute numbering, "Show - 05").
	AnimeSeriesType string `json:"animeSeriesType,omitempty"`
	SeasonFolders   *bool  `json:"seasonFolders,omitempty"` // Sonarr; default true
	// Radarr only: announced | inCinemas | released (default released).
	MinimumAvailability string `json:"minimumAvailability,omitempty"`
	SearchOnAdd         *bool  `json:"searchOnAdd,omitempty"` // default true
	ExternalURL         string `json:"externalUrl,omitempty"` // a public address for links, optional
}

func (c Config) Configured() bool {
	return strings.TrimSpace(c.URL) != "" && strings.TrimSpace(c.APIKey) != ""
}

func (c Config) seasonFolders() bool { return c.SeasonFolders == nil || *c.SeasonFolders }
func (c Config) searchOnAdd() bool   { return c.SearchOnAdd == nil || *c.SearchOnAdd }

// animeSeriesType is Sonarr's seriesType for an anime request: "standard" unless the
// admin chose "anime".
func (c Config) animeSeriesType() string {
	if strings.EqualFold(strings.TrimSpace(c.AnimeSeriesType), "anime") {
		return "anime"
	}
	return "standard"
}

// Client is one arr instance.
type Client struct {
	Name string // "sonarr" | "radarr"
	cfg  Config
	http *http.Client
}

// New returns a client, or nil when the config is incomplete.
func New(name string, cfg Config) *Client {
	if !cfg.Configured() {
		return nil
	}
	cfg.URL = strings.TrimRight(strings.TrimSpace(cfg.URL), "/")
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	return &Client{Name: name, cfg: cfg, http: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) do(ctx context.Context, method, path string, q url.Values, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	u := c.cfg.URL + "/api/v3" + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("X-Api-Key", c.cfg.APIKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "deadline exceeded") {
			return fmt.Errorf("%s at %s did not answer (check the address, and that Roxy Server can reach it)", c.Name, c.cfg.URL)
		}
		return fmt.Errorf("%s: %w", c.Name, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == 401 {
		return fmt.Errorf("%s: the API key was rejected", c.Name)
	}
	if resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(data))
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return fmt.Errorf("%s: HTTP %d %s %s: %s", c.Name, resp.StatusCode, method, path, msg)
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

// NamedID is a root folder (Path), quality profile (ID, Name) or tag (ID, Label).
type NamedID struct {
	ID    int    `json:"id"`
	Name  string `json:"name,omitempty"`
	Label string `json:"label,omitempty"`
	Path  string `json:"path,omitempty"`
	// Roxy (root folders only): the Roxy library that reads this folder, and how sure:
	// "yes" (same path or inside it) or "maybe" (same folder name: likely the same disk
	// seen from another container). Empty: Roxy doesn't read it, so downloads saved here
	// would never show up.
	Roxy      string `json:"roxy,omitempty"`
	RoxyMatch string `json:"roxyMatch,omitempty"`
	// Free space on a root folder, bytes (0 when unknown).
	FreeSpace int64 `json:"freeSpace,omitempty"`
}

// Probe is what Test connection returns: proof the key works and the choices to offer.
type Probe struct {
	Version string `json:"version"`
	AppName string `json:"appName"`
	// Supported: a version Roxy is tested with; Note explains otherwise.
	Supported       bool      `json:"supported"`
	Note            string    `json:"note,omitempty"`
	RootFolders     []NamedID `json:"rootFolders"`
	QualityProfiles []NamedID `json:"qualityProfiles"`
	Tags            []NamedID `json:"tags"`
}

// Test checks the instance answers with our key and lists its root folders, quality
// profiles and tags so the admin can pick from dropdowns. A host that does not answer
// fails within a few seconds (the admin is waiting on a button).
func (c *Client) Test(ctx context.Context) (*Probe, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var st struct {
		Version string `json:"version"`
		AppName string `json:"appName"`
	}
	if err := c.do(ctx, http.MethodGet, "/system/status", nil, nil, &st); err != nil {
		return nil, err
	}
	p := &Probe{Version: st.Version, AppName: st.AppName}
	p.Supported, p.Note = CheckVersion(c.Name, st.Version)
	if err := c.do(ctx, http.MethodGet, "/rootfolder", nil, nil, &p.RootFolders); err != nil {
		return nil, err
	}
	if err := c.do(ctx, http.MethodGet, "/qualityprofile", nil, nil, &p.QualityProfiles); err != nil {
		return nil, err
	}
	_ = c.do(ctx, http.MethodGet, "/tag", nil, nil, &p.Tags) // tags are optional
	return p, nil
}

func (c *Client) rootAndProfile(ctx context.Context, anime bool) (string, int, []int, error) {
	root, profile, tags := c.cfg.RootFolder, c.cfg.QualityProfileID, c.cfg.Tags
	if anime {
		if c.cfg.AnimeRootFolder != "" {
			root = c.cfg.AnimeRootFolder
		}
		if c.cfg.AnimeQualityProfileID != 0 {
			profile = c.cfg.AnimeQualityProfileID
		}
		if len(c.cfg.AnimeTags) > 0 {
			tags = c.cfg.AnimeTags
		}
	}
	// Nothing saved yet: take the first the server reports (keeps env-only setups working).
	if root == "" || profile == 0 {
		p, err := c.Test(ctx)
		if err != nil {
			return "", 0, nil, err
		}
		if root == "" {
			if len(p.RootFolders) == 0 {
				return "", 0, nil, fmt.Errorf("%s has no root folders", c.Name)
			}
			root = p.RootFolders[0].Path
		}
		if profile == 0 {
			if len(p.QualityProfiles) == 0 {
				return "", 0, nil, fmt.Errorf("%s has no quality profiles", c.Name)
			}
			profile = p.QualityProfiles[0].ID
		}
	}
	if tags == nil {
		tags = []int{}
	}
	return root, profile, tags, nil
}

// MonitorEpisodes marks specific episodes of a Sonarr series monitored and searches for
// them (specials are asked for one at a time; Sonarr cannot search season 0 usefully).
// codes are "S0E5"-style.
func (c *Client) MonitorEpisodes(ctx context.Context, seriesID int, codes []string) error {
	if len(codes) == 0 {
		return nil
	}
	want := map[string]bool{}
	for _, code := range codes {
		want[strings.ToUpper(code)] = true
	}
	var eps []struct {
		ID            int  `json:"id"`
		SeasonNumber  int  `json:"seasonNumber"`
		EpisodeNumber int  `json:"episodeNumber"`
		Monitored     bool `json:"monitored"`
		HasFile       bool `json:"hasFile"`
	}
	if err := c.do(ctx, http.MethodGet, "/episode", url.Values{"seriesId": {strconv.Itoa(seriesID)}}, nil, &eps); err != nil {
		return err
	}
	var ids []int
	for _, e := range eps {
		if want[fmt.Sprintf("S%dE%d", e.SeasonNumber, e.EpisodeNumber)] && !e.HasFile {
			ids = append(ids, e.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	if err := c.do(ctx, http.MethodPut, "/episode/monitor", nil, map[string]any{"episodeIds": ids, "monitored": true}, nil); err != nil {
		return err
	}
	if c.cfg.searchOnAdd() {
		_ = c.do(ctx, http.MethodPost, "/command", nil, map[string]any{"name": "EpisodeSearch", "episodeIds": ids}, nil)
	}
	return nil
}

// AddSeries adds a series to Sonarr by TVDB id (monitored, search on add; series type
// "anime" with the anime root / profile when asked). With seasons, only those are
// monitored and searched (request app's per-season requests); empty means the whole series.
// Returns the Sonarr series id. A series Sonarr already has gets the requested seasons
// monitored and a SeasonSearch for each instead of an error.
func (c *Client) AddSeries(ctx context.Context, tvdbID int, anime bool, seasons []int) (int, error) {
	want := map[int]bool{}
	for _, n := range seasons {
		want[n] = true
	}
	var existing []map[string]any
	if err := c.do(ctx, http.MethodGet, "/series", url.Values{"tvdbId": {strconv.Itoa(tvdbID)}}, nil, &existing); err == nil {
		for _, s := range existing {
			id, _ := s["id"].(float64)
			if tv, _ := s["tvdbId"].(float64); int(tv) != tvdbID || id == 0 {
				continue
			}
			// Already there: monitor the asked-for seasons (all when none given) and search them.
			changed := false
			if ss, ok := s["seasons"].([]any); ok {
				for _, x := range ss {
					sm, ok := x.(map[string]any)
					if !ok {
						continue
					}
					n, _ := sm["seasonNumber"].(float64)
					if (len(want) == 0 && n != 0) || want[int(n)] {
						if m, _ := sm["monitored"].(bool); !m {
							sm["monitored"] = true
							changed = true
						}
					}
				}
			}
			s["monitored"] = true
			// Asked for the whole series: follow its new seasons too.
			if len(want) == 0 && s["monitorNewItems"] != "all" {
				s["monitorNewItems"] = "all"
				changed = true
			}
			if changed {
				if err := c.do(ctx, http.MethodPut, "/series/"+strconv.Itoa(int(id)), nil, s, nil); err != nil {
					return 0, err
				}
			}
			if c.cfg.searchOnAdd() {
				for n := range want {
					_ = c.do(ctx, http.MethodPost, "/command", nil, map[string]any{"name": "SeasonSearch", "seriesId": int(id), "seasonNumber": n}, nil)
				}
				if len(want) == 0 {
					_ = c.do(ctx, http.MethodPost, "/command", nil, map[string]any{"name": "SeriesSearch", "seriesId": int(id)}, nil)
				}
			}
			return int(id), nil
		}
	}
	var lookup []map[string]any
	if err := c.do(ctx, http.MethodGet, "/series/lookup", url.Values{"term": {"tvdb:" + strconv.Itoa(tvdbID)}}, nil, &lookup); err != nil {
		return 0, err
	}
	if len(lookup) == 0 {
		return 0, fmt.Errorf("sonarr: tvdb:%d not found", tvdbID)
	}
	root, profile, tags, err := c.rootAndProfile(ctx, anime)
	if err != nil {
		return 0, err
	}
	series := lookup[0]
	series["qualityProfileId"] = profile
	series["rootFolderPath"] = root
	series["monitored"] = true
	series["seasonFolder"] = c.cfg.seasonFolders()
	series["tags"] = tags
	if anime {
		series["seriesType"] = c.cfg.animeSeriesType()
	}
	if ss, ok := series["seasons"].([]any); ok {
		for _, x := range ss {
			if sm, ok := x.(map[string]any); ok {
				n, _ := sm["seasonNumber"].(float64)
				sm["monitored"] = (len(want) == 0 && n != 0) || want[int(n)]
			}
		}
	}
	// New seasons that start airing later are grabbed only for a whole-series request;
	// asking for Season 1 of an ongoing show must not pull in Season 4 next year. (The
	// lookup carries Sonarr's default, "all".) New episodes of a monitored season are
	// grabbed as they air either way.
	if len(want) == 0 {
		series["monitorNewItems"] = "all"
	} else {
		series["monitorNewItems"] = "none"
	}
	// No "monitor" option: Sonarr keeps the per-season flags set above.
	series["addOptions"] = map[string]any{"searchForMissingEpisodes": c.cfg.searchOnAdd(), "ignoreEpisodesWithFiles": true}
	var added struct {
		ID int `json:"id"`
	}
	if err := c.do(ctx, http.MethodPost, "/series", nil, series, &added); err != nil {
		return 0, err
	}
	return added.ID, nil
}

// AddMovie adds a movie to Radarr by TMDB id (monitored, search on add). Returns the Radarr movie id.
func (c *Client) AddMovie(ctx context.Context, tmdbID int) (int, error) {
	var existing []struct {
		ID     int `json:"id"`
		TmdbID int `json:"tmdbId"`
	}
	if err := c.do(ctx, http.MethodGet, "/movie", url.Values{"tmdbId": {strconv.Itoa(tmdbID)}}, nil, &existing); err == nil {
		for _, m := range existing {
			if m.TmdbID == tmdbID {
				return m.ID, nil
			}
		}
	}
	var movie map[string]any
	if err := c.do(ctx, http.MethodGet, "/movie/lookup/tmdb", url.Values{"tmdbId": {strconv.Itoa(tmdbID)}}, nil, &movie); err != nil {
		return 0, err
	}
	if movie == nil || movie["title"] == nil {
		return 0, fmt.Errorf("radarr: tmdb:%d not found", tmdbID)
	}
	root, profile, tags, err := c.rootAndProfile(ctx, false)
	if err != nil {
		return 0, err
	}
	avail := c.cfg.MinimumAvailability
	if avail == "" {
		avail = "released"
	}
	movie["qualityProfileId"] = profile
	movie["rootFolderPath"] = root
	movie["monitored"] = true
	movie["tags"] = tags
	movie["minimumAvailability"] = avail
	movie["addOptions"] = map[string]any{"searchForMovie": c.cfg.searchOnAdd()}
	var added struct {
		ID int `json:"id"`
	}
	if err := c.do(ctx, http.MethodPost, "/movie", nil, movie, &added); err != nil {
		return 0, err
	}
	return added.ID, nil
}

// Supported versions: what Roxy is built and tested against (API v3 in all of them).
const (
	SupportedSonarr = "Sonarr v4"
	SupportedRadarr = "Radarr v5 and v6"
)

// CheckVersion says whether an arr version is one Roxy is tested with, and why not.
func CheckVersion(name, version string) (bool, string) {
	major, _ := strconv.Atoi(strings.SplitN(version, ".", 2)[0])
	switch name {
	case "sonarr":
		if major >= 4 {
			return true, ""
		}
		return false, "Sonarr v" + version + " is older than Roxy is tested with (Sonarr v4). Requests may work, but download status needs v4."
	case "radarr":
		if major >= 5 {
			return true, ""
		}
		return false, "Radarr v" + version + " is older than Roxy is tested with (Radarr v5 or v6). Requests may work, but download status needs v5 or newer."
	}
	return true, ""
}
