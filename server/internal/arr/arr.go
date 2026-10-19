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
	SeasonFolders         *bool  `json:"seasonFolders,omitempty"` // Sonarr; default true
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
	// Free space on a root folder, bytes (0 when unknown).
	FreeSpace int64 `json:"freeSpace,omitempty"`
}

// Probe is what Test connection returns: proof the key works and the choices to offer.
type Probe struct {
	Version         string    `json:"version"`
	AppName         string    `json:"appName"`
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

// AddSeries adds a series to Sonarr by TVDB id (monitored, search on add; series type
// "anime" with the anime root / profile when asked). Returns the Sonarr series id. An
// already-added series is returned, not an error.
func (c *Client) AddSeries(ctx context.Context, tvdbID int, anime bool) (int, error) {
	var existing []struct {
		ID     int `json:"id"`
		TvdbID int `json:"tvdbId"`
	}
	if err := c.do(ctx, http.MethodGet, "/series", url.Values{"tvdbId": {strconv.Itoa(tvdbID)}}, nil, &existing); err == nil {
		for _, s := range existing {
			if s.TvdbID == tvdbID {
				return s.ID, nil
			}
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
		series["seriesType"] = "anime"
	}
	series["addOptions"] = map[string]any{"searchForMissingEpisodes": c.cfg.searchOnAdd(), "monitor": "all"}
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
