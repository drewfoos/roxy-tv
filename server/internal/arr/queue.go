package arr

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// What Sonarr / Radarr know about a title after it was added: its download queue,
// which episodes have files, when they were last searched, and what failed. Field
// names follow the v3 API resources (Sonarr v4, Radarr v5); see docs/api/native.md.

// QueueItem is one record of GET /api/v3/queue/details. Sonarr returns one record per
// episode, so a season pack is several records sharing a DownloadID.
type QueueItem struct {
	ID                      int             `json:"id"`
	Title                   string          `json:"title"`
	Status                  string          `json:"status"`                // queued | paused | downloading | completed | failed | warning | delay | downloadClientUnavailable | fallback
	TrackedDownloadStatus   string          `json:"trackedDownloadStatus"` // ok | warning | error
	TrackedDownloadState    string          `json:"trackedDownloadState"`  // downloading | importBlocked | importPending | importing | imported | failedPending | failed | ignored
	StatusMessages          []StatusMessage `json:"statusMessages"`
	ErrorMessage            string          `json:"errorMessage"`
	Size                    float64         `json:"size"`
	SizeLeft                float64         `json:"sizeleft"`
	TimeLeft                string          `json:"timeleft"` // "00:12:34" or "1.02:03:04"
	EstimatedCompletionTime *time.Time      `json:"estimatedCompletionTime"`
	DownloadID              string          `json:"downloadId"`
	Protocol                string          `json:"protocol"`
	DownloadClient          string          `json:"downloadClient"`
	Indexer                 string          `json:"indexer"`
	SeasonNumber            int             `json:"seasonNumber"`
	EpisodeID               int             `json:"episodeId"`
	Episode                 *struct {
		SeasonNumber  int `json:"seasonNumber"`
		EpisodeNumber int `json:"episodeNumber"`
	} `json:"episode"`
}

type StatusMessage struct {
	Title    string   `json:"title"`
	Messages []string `json:"messages"`
}

// Messages flattens the arr's explanation of a warning or block into one line.
func (q QueueItem) Messages() string {
	var parts []string
	if q.ErrorMessage != "" {
		parts = append(parts, q.ErrorMessage)
	}
	for _, m := range q.StatusMessages {
		parts = append(parts, m.Messages...)
		if len(m.Messages) == 0 && m.Title != "" {
			parts = append(parts, m.Title)
		}
	}
	if s := unexpectedForFolder(parts); s != "" {
		return s
	}
	return strings.Join(parts, " ")
}

// "Episode 2x01 was unexpected considering the <release> folder name": Sonarr read the
// episodes from the file names but could not match the batch's folder name (Judas's
// "(Season 02)", say), so it stops for a manual import. One line per episode made a wall
// of text; this says it once.
var unexpectedRe = regexp.MustCompile(`Episode (\d+)x(\d+) was unexpected considering the .+? folder name`)

func unexpectedForFolder(parts []string) string {
	seasons := map[int][]int{}
	for _, p := range parts {
		for _, m := range unexpectedRe.FindAllStringSubmatch(p, -1) {
			s, _ := strconv.Atoi(m[1])
			e, _ := strconv.Atoi(m[2])
			seasons[s] = append(seasons[s], e)
		}
	}
	if len(seasons) == 0 {
		return ""
	}
	var keys []int
	for s := range seasons {
		keys = append(keys, s)
	}
	sort.Ints(keys)
	var eps []string
	for _, s := range keys {
		eps = append(eps, episodeRange(s, seasons[s]))
	}
	return "Sonarr couldn't match the release's folder name to " + strings.Join(eps, ", ") +
		" (the files themselves are named right), so it is waiting for a manual import: Sonarr → Activity → Queue → Manual Import."
}

// episodeRange: "S2E1–E12" for a run, "S2E1, E3, E5" otherwise.
func episodeRange(season int, eps []int) string {
	sort.Ints(eps)
	run := true
	for i := 1; i < len(eps); i++ {
		if eps[i] != eps[i-1]+1 {
			run = false
		}
	}
	if len(eps) > 1 && run {
		return fmt.Sprintf("S%dE%d–E%d", season, eps[0], eps[len(eps)-1])
	}
	out := make([]string, len(eps))
	for i, e := range eps {
		if i == 0 {
			out[i] = fmt.Sprintf("S%dE%d", season, e)
		} else {
			out[i] = fmt.Sprintf("E%d", e)
		}
	}
	return strings.Join(out, ", ")
}

// TimeLeftSeconds parses the TimeSpan string ("1.02:03:04", "00:12:34"); -1 when unknown.
func (q QueueItem) TimeLeftSeconds() int {
	s := strings.TrimSpace(q.TimeLeft)
	if s == "" {
		return -1
	}
	days := 0
	if i := strings.Index(s, "."); i > 0 && i < strings.Index(s, ":") {
		days, _ = strconv.Atoi(s[:i])
		s = s[i+1:]
	}
	p := strings.Split(s, ":")
	if len(p) != 3 {
		return -1
	}
	h, _ := strconv.Atoi(p[0])
	m, _ := strconv.Atoi(p[1])
	sec, _ := strconv.ParseFloat(p[2], 64)
	return days*86400 + h*3600 + m*60 + int(sec)
}

// Queue returns the download queue entries for one series (Sonarr) or movie (Radarr).
// /queue/details is unpaged, unlike /queue (which defaults to 10 records).
func (c *Client) Queue(ctx context.Context, arrID int) ([]QueueItem, error) {
	q := url.Values{}
	if c.Name == "radarr" {
		q.Set("movieId", strconv.Itoa(arrID))
	} else {
		q.Set("seriesId", strconv.Itoa(arrID))
		q.Set("includeEpisode", "true")
	}
	var out []QueueItem
	err := c.do(ctx, http.MethodGet, "/queue/details", q, nil, &out)
	return out, err
}

// Episode is a Sonarr episode with what tracking needs.
type Episode struct {
	ID             int        `json:"id"`
	SeasonNumber   int        `json:"seasonNumber"`
	EpisodeNumber  int        `json:"episodeNumber"`
	Title          string     `json:"title"`
	HasFile        bool       `json:"hasFile"`
	Monitored      bool       `json:"monitored"`
	AirDateUTC     *time.Time `json:"airDateUtc"`
	AirDate        string     `json:"airDate"` // YYYY-MM-DD, the day it aired where it aired
	LastSearchTime *time.Time `json:"lastSearchTime"`
}

// Aired: has an air date in the past (no date at all counts as not aired: TBA).
func (e Episode) Aired(now time.Time) bool { return e.AirDateUTC != nil && !e.AirDateUTC.After(now) }

func (c *Client) Episodes(ctx context.Context, seriesID int) ([]Episode, error) {
	var out []Episode
	err := c.do(ctx, http.MethodGet, "/episode", url.Values{"seriesId": {strconv.Itoa(seriesID)}}, nil, &out)
	return out, err
}

// Movie is a Radarr movie with what tracking needs.
type Movie struct {
	ID              int        `json:"id"`
	Title           string     `json:"title"`
	HasFile         bool       `json:"hasFile"`
	Monitored       bool       `json:"monitored"`
	IsAvailable     bool       `json:"isAvailable"` // released enough for the minimum availability
	Status          string     `json:"status"`      // tba | announced | inCinemas | released | deleted
	InCinemas       *time.Time `json:"inCinemas"`
	DigitalRelease  *time.Time `json:"digitalRelease"`
	PhysicalRelease *time.Time `json:"physicalRelease"`
	LastSearchTime  *time.Time `json:"lastSearchTime"`
	MovieFile       *struct {
		Path string `json:"path"`
	} `json:"movieFile"`
}

// EpisodeFilePaths: where Sonarr keeps a series' episode files (its own paths).
func (c *Client) EpisodeFilePaths(ctx context.Context, seriesID int) ([]string, error) {
	var files []struct {
		Path string `json:"path"`
	}
	if err := c.do(ctx, http.MethodGet, "/episodefile", url.Values{"seriesId": {strconv.Itoa(seriesID)}}, nil, &files); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(files))
	for _, f := range files {
		if f.Path != "" {
			out = append(out, f.Path)
		}
	}
	return out, nil
}

func (c *Client) Movie(ctx context.Context, movieID int) (*Movie, error) {
	var m Movie
	if err := c.do(ctx, http.MethodGet, "/movie/"+strconv.Itoa(movieID), nil, nil, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// HistoryRecord is one entry of a title's history (grabbed, downloadFailed, imported...).
type HistoryRecord struct {
	ID          int               `json:"id"`
	EpisodeID   int               `json:"episodeId"`
	SourceTitle string            `json:"sourceTitle"`
	Date        time.Time         `json:"date"`
	EventType   string            `json:"eventType"` // grabbed | downloadFailed | downloadFolderImported | downloadIgnored | ...
	DownloadID  string            `json:"downloadId"`
	Data        map[string]string `json:"data"`
	// The release's custom format score when it was grabbed (what the file's name may not show).
	CustomFormatScore int `json:"customFormatScore"`
}

// History lists a series' or movie's history, newest first.
func (c *Client) History(ctx context.Context, arrID int) ([]HistoryRecord, error) {
	var out []HistoryRecord
	var err error
	if c.Name == "radarr" {
		err = c.do(ctx, http.MethodGet, "/history/movie", url.Values{"movieId": {strconv.Itoa(arrID)}}, nil, &out)
	} else {
		err = c.do(ctx, http.MethodGet, "/history/series", url.Values{"seriesId": {strconv.Itoa(arrID)}}, nil, &out)
	}
	// The apps return these oldest first; callers want the newest first.
	sort.SliceStable(out, func(i, j int) bool { return out[i].Date.After(out[j].Date) })
	return out, err
}

// SearchEpisodes / SearchMovie ask the arr app to look for releases again.
func (c *Client) SearchEpisodes(ctx context.Context, episodeIDs []int) error {
	if len(episodeIDs) == 0 {
		return nil
	}
	return c.do(ctx, http.MethodPost, "/command", nil, map[string]any{"name": "EpisodeSearch", "episodeIds": episodeIDs}, nil)
}

// SearchSeason asks Sonarr to search one whole season (matches season packs).
func (c *Client) SearchSeason(ctx context.Context, seriesID, season int) error {
	return c.do(ctx, http.MethodPost, "/command", nil, map[string]any{"name": "SeasonSearch", "seriesId": seriesID, "seasonNumber": season}, nil)
}

func (c *Client) SearchMovie(ctx context.Context, movieID int) error {
	return c.do(ctx, http.MethodPost, "/command", nil, map[string]any{"name": "MoviesSearch", "movieIds": []int{movieID}}, nil)
}

// RemoveAndBlocklist drops a queue entry from the download client, blocklists that
// release, and lets the arr app search for another (skipRedownload=false).
func (c *Client) RemoveAndBlocklist(ctx context.Context, queueIDs []int) error {
	if len(queueIDs) == 0 {
		return nil
	}
	q := url.Values{"removeFromClient": {"true"}, "blocklist": {"true"}, "skipRedownload": {"false"}}
	return c.do(ctx, http.MethodDelete, "/queue/bulk", q, map[string]any{"ids": queueIDs}, nil)
}

// Command is one of the arr app's queued or running commands (searches matter here).
type Command struct {
	Name   string `json:"name"`   // EpisodeSearch | SeasonSearch | SeriesSearch | MissingEpisodeSearch | MoviesSearch | ...
	Status string `json:"status"` // queued | started | completed | failed | ...
	Body   struct {
		EpisodeIDs   []int `json:"episodeIds"`
		SeriesID     int   `json:"seriesId"`
		SeasonNumber *int  `json:"seasonNumber"`
		MovieIDs     []int `json:"movieIds"`
	} `json:"body"`
}

// ActiveSearches lists the searches the arr app has queued or running right now.
func (c *Client) ActiveSearches(ctx context.Context) ([]Command, error) {
	var all []Command
	if err := c.do(ctx, http.MethodGet, "/command", nil, nil, &all); err != nil {
		return nil, err
	}
	var out []Command
	for _, cmd := range all {
		if (cmd.Status == "queued" || cmd.Status == "started") && strings.HasSuffix(cmd.Name, "Search") {
			out = append(out, cmd)
		}
	}
	return out, nil
}

// Covers says whether an active search includes this episode (Sonarr) or movie (Radarr).
func (cmd Command) Covers(seriesID int, e Episode, movieID int) bool {
	switch cmd.Name {
	case "EpisodeSearch":
		for _, id := range cmd.Body.EpisodeIDs {
			if id == e.ID {
				return true
			}
		}
	case "SeasonSearch":
		return cmd.Body.SeriesID == seriesID && cmd.Body.SeasonNumber != nil && *cmd.Body.SeasonNumber == e.SeasonNumber
	case "SeriesSearch":
		return cmd.Body.SeriesID == seriesID
	case "MissingEpisodeSearch":
		return cmd.Body.SeriesID == 0 || cmd.Body.SeriesID == seriesID
	case "MoviesSearch":
		for _, id := range cmd.Body.MovieIDs {
			if id == movieID {
				return true
			}
		}
	case "MissingMoviesSearch":
		return movieID != 0
	}
	return false
}

// RefreshDownloads asks the arr app to poll its download clients now
// (RefreshMonitoredDownloads), so a status read right after is fresh.
func (c *Client) RefreshDownloads(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/command", nil, map[string]any{"name": "RefreshMonitoredDownloads"}, nil)
}

// SeriesIDByTVDB is the id Sonarr has for the series with that TheTVDB id (0: none).
func (c *Client) SeriesIDByTVDB(ctx context.Context, tvdbID int) (int, error) {
	var out []struct {
		ID int `json:"id"`
	}
	if err := c.do(ctx, http.MethodGet, "/series", url.Values{"tvdbId": {strconv.Itoa(tvdbID)}}, nil, &out); err != nil {
		return 0, err
	}
	if len(out) == 0 {
		return 0, nil
	}
	return out[0].ID, nil
}
