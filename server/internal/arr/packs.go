package arr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Season packs Sonarr cannot parse. Fansub groups often name a batch "(Season 1)" or
// "[Season 01]", which Sonarr reads as part of the title and drops, so a compact pack
// with the English dub never reaches its decisions and it falls back to the best single
// release per episode (a dub / sub mix). Prowlarr finds those packs; Roxy rewrites the
// season to "S01", has Sonarr parse and score the name, and pushes the best one only when
// it beats what Sonarr can see by itself. Sonarr still applies every rule on the push.

// Prowlarr is the indexer manager the arr apps search through.
type Prowlarr struct {
	url, key string
	http     *http.Client
}

// NewProwlarr returns a client, or nil without an address and key.
func NewProwlarr(u, key string) *Prowlarr {
	u, key = strings.TrimRight(strings.TrimSpace(u), "/"), strings.TrimSpace(key)
	if u == "" || key == "" {
		return nil
	}
	return &Prowlarr{url: u, key: key, http: &http.Client{Timeout: 90 * time.Second}}
}

// IndexerRelease is one Prowlarr search result.
type IndexerRelease struct {
	Title       string `json:"title"`
	Size        int64  `json:"size"`
	Seeders     int    `json:"seeders"`
	GUID        string `json:"guid"`
	DownloadURL string `json:"downloadUrl"`
	MagnetURL   string `json:"magnetUrl"`
	Indexer     string `json:"indexer"`
	PublishDate string `json:"publishDate"`
	Protocol    string `json:"protocol"`
}

// Search runs a free-text search on every indexer.
func (p *Prowlarr) Search(ctx context.Context, query string) ([]IndexerRelease, error) {
	q := url.Values{"query": {query}, "type": {"search"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.url+"/api/v1/search?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Api-Key", p.key)
	resp, err := p.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("prowlarr: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("prowlarr: HTTP %d", resp.StatusCode)
	}
	var out []IndexerRelease
	err = jsonDecode(resp.Body, &out)
	return out, err
}

// reSeasonWord: "(Season 1)", "[Season 01]", "Season 2" — a whole season named in words.
var reSeasonWord = regexp.MustCompile(`(?i)[\(\[]?\bSeason[ ._-]?0?(\d{1,2})\b[\)\]]?`)

// reSxx: names Sonarr already reads (S01, S01E02, 1x02).
var reSxx = regexp.MustCompile(`(?i)\bS\d{1,2}(E\d{1,4})?\b|\b\d{1,2}x\d{2,3}\b`)

// SeasonWordRewrite turns "(Season 1)" into "S01" when that is the only season marker;
// ok is false for names Sonarr can read already or that name no season.
func SeasonWordRewrite(title string) (string, int, bool) {
	if reSxx.MatchString(title) {
		return "", 0, false
	}
	m := reSeasonWord.FindStringSubmatchIndex(title)
	if m == nil {
		return "", 0, false
	}
	n, _ := strconv.Atoi(title[m[2]:m[3]])
	if n <= 0 {
		return "", 0, false
	}
	return title[:m[0]] + fmt.Sprintf("S%02d", n) + title[m[1]:], n, true
}

// ParsedRelease is what Sonarr makes of a release name.
type ParsedRelease struct {
	SeriesID   int
	Season     int
	FullSeason bool
	Score      int
	Formats    []string
}

// Parse asks Sonarr how it reads (and scores) a release name.
func (c *Client) Parse(ctx context.Context, title string) (*ParsedRelease, error) {
	var out struct {
		ParsedEpisodeInfo *struct {
			SeasonNumber int  `json:"seasonNumber"`
			FullSeason   bool `json:"fullSeason"`
		} `json:"parsedEpisodeInfo"`
		Series *struct {
			ID int `json:"id"`
		} `json:"series"`
		CustomFormats []struct {
			Name string `json:"name"`
		} `json:"customFormats"`
		CustomFormatScore int `json:"customFormatScore"`
	}
	if err := c.do(ctx, http.MethodGet, "/parse", url.Values{"title": {title}}, nil, &out); err != nil {
		return nil, err
	}
	p := &ParsedRelease{Score: out.CustomFormatScore}
	if out.ParsedEpisodeInfo != nil {
		p.Season, p.FullSeason = out.ParsedEpisodeInfo.SeasonNumber, out.ParsedEpisodeInfo.FullSeason
	}
	if out.Series != nil {
		p.SeriesID = out.Series.ID
	}
	for _, f := range out.CustomFormats {
		p.Formats = append(p.Formats, f.Name)
	}
	return p, nil
}

// SeasonRelease is one release from Sonarr's own (interactive) search of a season: a
// full-season pack, or one covering the listed episodes.
type SeasonRelease struct {
	Title      string
	FullSeason bool
	Episodes   []int
	Score      int
	Seeders    int
	Approved   bool
}

// SeasonReleases runs Sonarr's interactive search for a season (nothing is grabbed) and
// returns what it found, with its verdict on each.
func (c *Client) SeasonReleases(ctx context.Context, seriesID, season int) ([]SeasonRelease, error) {
	var rels []struct {
		Title             string `json:"title"`
		FullSeason        bool   `json:"fullSeason"`
		EpisodeNumbers    []int  `json:"episodeNumbers"`
		Approved          bool   `json:"approved"`
		CustomFormatScore int    `json:"customFormatScore"`
		Seeders           int    `json:"seeders"`
	}
	q := url.Values{"seriesId": {strconv.Itoa(seriesID)}, "seasonNumber": {strconv.Itoa(season)}}
	if err := c.do(ctx, http.MethodGet, "/release", q, nil, &rels); err != nil {
		return nil, err
	}
	out := make([]SeasonRelease, 0, len(rels))
	for _, r := range rels {
		out = append(out, SeasonRelease{Title: r.Title, FullSeason: r.FullSeason, Episodes: r.EpisodeNumbers, Score: r.CustomFormatScore, Seeders: r.Seeders, Approved: r.Approved})
	}
	return out, nil
}

// PushRelease hands Sonarr a release under the given name; it grabs it when every rule
// passes (approved) and otherwise says why not.
func (c *Client) PushRelease(ctx context.Context, title string, r IndexerRelease) (bool, []string, error) {
	link := r.MagnetURL
	if link == "" && strings.HasPrefix(r.GUID, "magnet:") {
		link = r.GUID
	}
	if link == "" {
		link = r.DownloadURL
	}
	proto := r.Protocol
	if proto == "" {
		proto = "torrent"
	}
	body := map[string]any{"title": title, "downloadUrl": link, "protocol": proto, "publishDate": r.PublishDate, "size": r.Size, "indexer": r.Indexer}
	var out []struct {
		Approved   bool     `json:"approved"`
		Rejections []string `json:"rejections"`
	}
	if err := c.do(ctx, http.MethodPost, "/release/push", nil, body, &out); err != nil {
		return false, nil, err
	}
	if len(out) == 0 {
		return false, []string{"no answer"}, nil
	}
	return out[0].Approved, out[0].Rejections, nil
}

// SeriesNames is the series' title and alternate titles as Sonarr knows them.
func (c *Client) SeriesNames(ctx context.Context, seriesID int) (string, []string, error) {
	var s struct {
		Title           string `json:"title"`
		AlternateTitles []struct {
			Title string `json:"title"`
		} `json:"alternateTitles"`
	}
	if err := c.do(ctx, http.MethodGet, "/series/"+strconv.Itoa(seriesID), nil, nil, &s); err != nil {
		return "", nil, err
	}
	var alts []string
	for _, a := range s.AlternateTitles {
		alts = append(alts, a.Title)
	}
	return s.Title, alts, nil
}

// CutoffFormatScore is the series' quality profile's custom format cutoff: Sonarr stops
// upgrading a file once it scores this much.
func (c *Client) CutoffFormatScore(ctx context.Context, seriesID int) (int, error) {
	var s struct {
		QualityProfileID int `json:"qualityProfileId"`
	}
	if err := c.do(ctx, http.MethodGet, "/series/"+strconv.Itoa(seriesID), nil, nil, &s); err != nil {
		return 0, err
	}
	var p struct {
		CutoffFormatScore int `json:"cutoffFormatScore"`
	}
	err := c.do(ctx, http.MethodGet, "/qualityprofile/"+strconv.Itoa(s.QualityProfileID), nil, nil, &p)
	return p.CutoffFormatScore, err
}

func jsonDecode(r interface{ Read([]byte) (int, error) }, v any) error {
	return json.NewDecoder(r).Decode(v)
}
