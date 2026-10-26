package arr

import (
	"context"
	"net/http"
	"net/url"
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
	return strings.Join(parts, " ")
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
// /queue/details is unpaged, unlike /queue (which defaults to 10 records: request app's bug).
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

// RefreshDownloads asks the arr app to poll its download clients now
// (RefreshMonitoredDownloads), so a status read right after is fresh.
func (c *Client) RefreshDownloads(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/command", nil, map[string]any{"name": "RefreshMonitoredDownloads"}, nil)
}
