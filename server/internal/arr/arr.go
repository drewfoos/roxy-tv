// Package arr talks to Sonarr and Radarr (API v3) to add titles the household requests
// from Discover. The arr apps do the searching and downloading; Roxy only adds the
// series or movie, monitored, with a search kicked off.
//
// Untested against live instances as of 2026-10-19 (no server machine yet): the calls
// follow the v3 API as request app uses it; verify on deploy day.
package arr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client is one arr instance.
type Client struct {
	Name           string // "sonarr" | "radarr"
	BaseURL        string
	APIKey         string
	RootFolder     string // path; "" = first reported
	QualityProfile string // id or name; "" = first reported
	http           *http.Client
}

func New(name, baseURL, apiKey, rootFolder, qualityProfile string) *Client {
	if baseURL == "" || apiKey == "" {
		return nil
	}
	return &Client{Name: name, BaseURL: strings.TrimRight(baseURL, "/"), APIKey: apiKey, RootFolder: rootFolder, QualityProfile: qualityProfile, http: &http.Client{Timeout: 30 * time.Second}}
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
	u := c.BaseURL + "/api/v3" + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("X-Api-Key", c.APIKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", c.Name, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
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

// Ping checks the instance answers with our key.
func (c *Client) Ping(ctx context.Context) error {
	var st struct {
		Version string `json:"version"`
	}
	return c.do(ctx, http.MethodGet, "/system/status", nil, nil, &st)
}

type namedID struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

func (c *Client) qualityProfileID(ctx context.Context) (int, error) {
	var profiles []namedID
	if err := c.do(ctx, http.MethodGet, "/qualityprofile", nil, nil, &profiles); err != nil {
		return 0, err
	}
	if len(profiles) == 0 {
		return 0, fmt.Errorf("%s has no quality profiles", c.Name)
	}
	if c.QualityProfile == "" {
		return profiles[0].ID, nil
	}
	if n, err := strconv.Atoi(c.QualityProfile); err == nil {
		return n, nil
	}
	for _, p := range profiles {
		if strings.EqualFold(p.Name, c.QualityProfile) {
			return p.ID, nil
		}
	}
	return 0, fmt.Errorf("%s: quality profile %q not found", c.Name, c.QualityProfile)
}

func (c *Client) rootFolder(ctx context.Context) (string, error) {
	if c.RootFolder != "" {
		return c.RootFolder, nil
	}
	var folders []namedID
	if err := c.do(ctx, http.MethodGet, "/rootfolder", nil, nil, &folders); err != nil {
		return "", err
	}
	if len(folders) == 0 {
		return "", fmt.Errorf("%s has no root folders", c.Name)
	}
	return folders[0].Path, nil
}

// AddSeries adds a series to Sonarr by TVDB id (monitored, season folders, search on
// add; series type "anime" when asked, so absolute numbering is handled). Returns the
// Sonarr series id. An already-added series is returned, not an error.
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
	series := lookup[0]
	profile, err := c.qualityProfileID(ctx)
	if err != nil {
		return 0, err
	}
	root, err := c.rootFolder(ctx)
	if err != nil {
		return 0, err
	}
	series["qualityProfileId"] = profile
	series["rootFolderPath"] = root
	series["monitored"] = true
	series["seasonFolder"] = true
	if anime {
		series["seriesType"] = "anime"
	}
	series["addOptions"] = map[string]any{"searchForMissingEpisodes": true, "monitor": "all"}
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
	profile, err := c.qualityProfileID(ctx)
	if err != nil {
		return 0, err
	}
	root, err := c.rootFolder(ctx)
	if err != nil {
		return 0, err
	}
	movie["qualityProfileId"] = profile
	movie["rootFolderPath"] = root
	movie["monitored"] = true
	movie["minimumAvailability"] = "released"
	movie["addOptions"] = map[string]any{"searchForMovie": true}
	var added struct {
		ID int `json:"id"`
	}
	if err := c.do(ctx, http.MethodPost, "/movie", nil, movie, &added); err != nil {
		return 0, err
	}
	return added.ID, nil
}
