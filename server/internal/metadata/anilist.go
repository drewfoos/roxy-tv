package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/roxy-media/roxy/server/internal/model"
)

// AniList is the anilist.co GraphQL provider (no API key). It is the primary source
// for anime-mode libraries and the fallback/enrichment source elsewhere.
type AniList struct {
	endpoint  string
	client    *http.Client
	lim       *limiter
	castCache castCache
}

const anilistEndpoint = "https://graphql.anilist.co"

func NewAniList() *AniList {
	return &AniList{
		endpoint: anilistEndpoint,
		client:   &http.Client{Timeout: 20 * time.Second},
		// AniList allows 90/min; stay well under it.
		lim: newLimiter(30, time.Minute),
	}
}

func (a *AniList) Name() string { return "anilist" }

const anilistMediaFields = `
	id idMal
	title { romaji english native }
	synonyms
	description(asHtml: false)
	coverImage { extraLarge large }
	bannerImage
	genres
	averageScore
	startDate { year month day }
	status
	format
	episodes
	duration
	studios(isMain: true) { nodes { name } }
	streamingEpisodes { title thumbnail }
	relations { edges { relationType node { id idMal type format episodes title { romaji english } startDate { year } coverImage { extraLarge } } } }`

const anilistSearchQuery = `query ($search: String, $format: [MediaFormat]) {
  Page(perPage: 8) {
    media(search: $search, type: ANIME, format_in: $format, sort: SEARCH_MATCH) {` + anilistMediaFields + `
    }
  }
}`

const anilistByIDQuery = `query ($id: Int) {
  Media(id: $id, type: ANIME) {` + anilistMediaFields + `
  }
}`

// ---- wire types ----

type anilistDate struct {
	Year  int `json:"year"`
	Month int `json:"month"`
	Day   int `json:"day"`
}

func (d anilistDate) String() string {
	if d.Year == 0 {
		return ""
	}
	if d.Month == 0 {
		return fmt.Sprintf("%04d", d.Year)
	}
	if d.Day == 0 {
		return fmt.Sprintf("%04d-%02d", d.Year, d.Month)
	}
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, d.Month, d.Day)
}

type anilistMedia struct {
	ID    int  `json:"id"`
	IDMal *int `json:"idMal"`
	Title struct {
		Romaji  string `json:"romaji"`
		English string `json:"english"`
		Native  string `json:"native"`
	} `json:"title"`
	Synonyms    []string `json:"synonyms"`
	Description string   `json:"description"`
	CoverImage  struct {
		ExtraLarge string `json:"extraLarge"`
		Large      string `json:"large"`
	} `json:"coverImage"`
	BannerImage  string      `json:"bannerImage"`
	Genres       []string    `json:"genres"`
	AverageScore *int        `json:"averageScore"`
	StartDate    anilistDate `json:"startDate"`
	Status       string      `json:"status"`
	Format       string      `json:"format"`
	Episodes     *int        `json:"episodes"`
	Duration     *int        `json:"duration"`
	Studios      struct {
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	} `json:"studios"`
	StreamingEpisodes []struct {
		Title     string `json:"title"`
		Thumbnail string `json:"thumbnail"`
	} `json:"streamingEpisodes"`
	Relations struct {
		Edges []anilistRelationEdge `json:"edges"`
	} `json:"relations"`
}

type anilistRelationEdge struct {
	RelationType string `json:"relationType"` // SEQUEL, PREQUEL, SIDE_STORY, SPIN_OFF, ALTERNATIVE, SUMMARY, PARENT, CHARACTER, OTHER, ADAPTATION, SOURCE
	Node         struct {
		ID       int    `json:"id"`
		IDMal    *int   `json:"idMal"`
		Type     string `json:"type"` // ANIME | MANGA
		Format   string `json:"format"`
		Episodes *int   `json:"episodes"`
		Title    struct {
			Romaji  string `json:"romaji"`
			English string `json:"english"`
		} `json:"title"`
		StartDate  anilistDate `json:"startDate"`
		CoverImage struct {
			ExtraLarge string `json:"extraLarge"`
		} `json:"coverImage"`
	} `json:"node"`
}

func (e *anilistRelationEdge) title() string {
	return firstNonEmpty(e.Node.Title.English, e.Node.Title.Romaji)
}

// isSeasonFormat says whether a related entry can be a "season" of a TV series.
func isSeasonFormat(f string) bool { return f == "TV" || f == "TV_SHORT" || f == "ONA" }

// relationKind maps AniList's relation type + node format onto Roxy's Relation.Type.
func relationKind(relationType, format string) string {
	switch format {
	case "MOVIE":
		return "movie"
	case "OVA":
		return "ova"
	case "SPECIAL":
		return "special"
	}
	switch relationType {
	case "SEQUEL":
		return "sequel"
	case "PREQUEL":
		return "prequel"
	case "SIDE_STORY":
		return "side_story"
	case "SPIN_OFF":
		return "spin_off"
	case "ALTERNATIVE":
		return "alternative"
	case "SUMMARY":
		return "summary"
	case "PARENT":
		return "parent"
	}
	return "other"
}

type anilistResponse struct {
	Data struct {
		Page *struct {
			Media []anilistMedia `json:"media"`
		} `json:"Page"`
		Media *anilistMedia `json:"Media"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
		Status  int    `json:"status"`
	} `json:"errors"`
}

// ---- conversion (pure; unit tested from fixtures) ----

func (a *AniList) mediaResult(m *anilistMedia) *Result {
	title := m.Title.English
	if title == "" {
		title = m.Title.Romaji
	}
	r := &Result{
		Provider:      "anilist",
		ID:            strconv.Itoa(m.ID),
		Title:         title,
		OriginalTitle: m.Title.Romaji,
		Year:          m.StartDate.Year,
		Overview:      StripHTML(m.Description),
		Genres:        m.Genres,
		Premiered:     m.StartDate.String(),
		Status:        anilistStatus(m.Status),
		ProviderIDs:   map[string]string{"anilist": strconv.Itoa(m.ID)},
		Images:        map[string]string{},
	}
	if m.AverageScore != nil && *m.AverageScore > 0 {
		r.CommunityRating = float64(*m.AverageScore) / 10
	}
	if m.IDMal != nil && *m.IDMal > 0 {
		r.ProviderIDs["mal"] = strconv.Itoa(*m.IDMal)
	}
	if len(m.Studios.Nodes) > 0 {
		r.Studio = m.Studios.Nodes[0].Name
	}
	if m.Episodes != nil {
		r.EpisodeCount = *m.Episodes
		r.Seasons = []SeasonInfo{{Number: 1, EpisodeCount: *m.Episodes, Poster: m.CoverImage.ExtraLarge, ProviderID: strconv.Itoa(m.ID)}}
	}
	// Anime relations (movies, OVAs, side stories). Sequels/prequels of TV format are
	// seasons and are folded into r.Seasons by followChain; everything else is "related".
	for _, e := range m.Relations.Edges {
		if e.Node.Type != "ANIME" {
			continue
		}
		if (e.RelationType == "SEQUEL" || e.RelationType == "PREQUEL") && isSeasonFormat(e.Node.Format) {
			continue
		}
		switch e.RelationType {
		case "CHARACTER", "OTHER", "ADAPTATION", "SOURCE", "CONTAINS", "COMPILATION":
			continue // crossovers and source material are not part of this story
		}
		rel := Relation{Type: relationKind(e.RelationType, e.Node.Format), Provider: "anilist", ProviderID: strconv.Itoa(e.Node.ID), Title: e.title(), Format: e.Node.Format, Year: e.Node.StartDate.Year, Poster: e.Node.CoverImage.ExtraLarge}
		if e.Node.Episodes != nil {
			rel.Episodes = *e.Node.Episodes
		}
		r.Related = append(r.Related, rel)
	}
	if p := firstNonEmpty(m.CoverImage.ExtraLarge, m.CoverImage.Large); p != "" {
		r.Images["poster"] = p
	}
	if m.BannerImage != "" {
		r.Images["backdrop"] = m.BannerImage
	}
	return r
}

var reStreamingTitle = regexp.MustCompile(`(?i)^(?:episode|ep\.?)\s*(\d+)\s*[-–:]?\s*(.*)$`)

// seasonResult builds the episode list from AniList's streamingEpisodes
// ("Episode 3 - Title" + thumbnail). Entries without a parsable number are dropped.
func (a *AniList) seasonResult(m *anilistMedia, season int) *Season {
	out := &Season{Number: season, Poster: m.CoverImage.ExtraLarge}
	seen := map[int]bool{}
	for _, se := range m.StreamingEpisodes {
		mm := reStreamingTitle.FindStringSubmatch(strings.TrimSpace(se.Title))
		if mm == nil {
			continue
		}
		n, _ := strconv.Atoi(mm[1])
		if n <= 0 || seen[n] {
			continue
		}
		seen[n] = true
		out.Episodes = append(out.Episodes, EpisodeResult{Season: season, Episode: n, Absolute: n, Title: strings.TrimSpace(mm[2]), Thumb: se.Thumbnail})
	}
	return out
}

func anilistStatus(s string) string {
	switch s {
	case "FINISHED":
		return "Ended"
	case "RELEASING":
		return "Continuing"
	case "NOT_YET_RELEASED":
		return "Upcoming"
	case "CANCELLED":
		return "Ended"
	}
	return ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// ---- Provider ----

func (a *AniList) query(ctx context.Context, q string, vars map[string]any) (*anilistResponse, error) {
	body, _ := json.Marshal(map[string]any{"query": q, "variables": vars})
	var resp anilistResponse
	err := getJSON(ctx, a.client, a.lim, http.MethodPost, a.endpoint, map[string]string{"Content-Type": "application/json"}, body, &resp)
	if err != nil {
		if isNotFound(err) {
			return &resp, nil // AniList answers 404 for an unknown Media id
		}
		return nil, err
	}
	if len(resp.Errors) > 0 && resp.Errors[0].Status != 404 {
		return nil, fmt.Errorf("anilist: %s", resp.Errors[0].Message)
	}
	return &resp, nil
}

func (a *AniList) Match(ctx context.Context, item *model.Item, hints Hints) (*Result, error) {
	var media *anilistMedia
	if v, ok := item.ProviderIDs["anilist"]; ok && v != "" {
		id, _ := strconv.Atoi(v)
		resp, err := a.query(ctx, anilistByIDQuery, map[string]any{"id": id})
		if err != nil {
			return nil, err
		}
		media = resp.Data.Media
	}
	if media == nil {
		var err error
		media, err = a.search(ctx, item)
		if err != nil || media == nil {
			return nil, err
		}
	}
	if item.Type == model.TypeSeries {
		return a.followChain(ctx, media)
	}
	return a.mediaResult(media), nil
}

// followChain turns AniList's one-Media-per-season model into a seasons list: walk
// PREQUEL edges back to the first TV entry, then SEQUEL edges forward, numbering the
// entries 1..n. Each hop is one by-id query (relations are only one level deep).
// Related entries (movies, OVAs, side stories) of every hop are merged, de-duplicated.
func (a *AniList) followChain(ctx context.Context, start *anilistMedia) (*Result, error) {
	const maxHops = 12
	fetch := func(id int) (*anilistMedia, error) {
		resp, err := a.query(ctx, anilistByIDQuery, map[string]any{"id": id})
		if err != nil {
			return nil, err
		}
		return resp.Data.Media, nil
	}
	next := func(m *anilistMedia, relType string) int {
		for _, e := range m.Relations.Edges {
			if e.RelationType == relType && e.Node.Type == "ANIME" && isSeasonFormat(e.Node.Format) {
				return e.Node.ID
			}
		}
		return 0
	}
	// Back to the root.
	root := start
	seen := map[int]bool{start.ID: true}
	for hops := 0; hops < maxHops; hops++ {
		id := next(root, "PREQUEL")
		if id == 0 || seen[id] {
			break
		}
		m, err := fetch(id)
		if err != nil || m == nil {
			break
		}
		seen[id] = true
		root = m
	}
	// Forward from the root.
	base := a.mediaResult(root)
	chain := []*anilistMedia{root}
	cur := root
	seen = map[int]bool{root.ID: true}
	for hops := 0; hops < maxHops; hops++ {
		id := next(cur, "SEQUEL")
		if id == 0 || seen[id] {
			break
		}
		m, err := fetch(id)
		if err != nil || m == nil {
			break
		}
		seen[id] = true
		chain = append(chain, m)
		cur = m
	}
	base.Seasons = base.Seasons[:0]
	base.EpisodeCount = 0
	related := map[string]Relation{}
	fingerprints := map[string]int{} // streaming-episode title list -> first season that had it
	for i, m := range chain {
		n := 0
		if m.Episodes != nil {
			n = *m.Episodes
		}
		parsed := a.seasonResult(m, i+1)
		if len(parsed.Episodes) > n {
			n = len(parsed.Episodes) // Crunchyroll lists OVAs as the last episode
		}
		si := SeasonInfo{Number: i + 1, EpisodeCount: n, Poster: m.CoverImage.ExtraLarge, ProviderID: strconv.Itoa(m.ID)}
		// AniList sometimes attaches season 1's Crunchyroll episode list to a sequel
		// entry verbatim; a repeated list is wrong by definition, so refuse it.
		if len(parsed.Episodes) > 0 {
			var sb strings.Builder
			for _, e := range parsed.Episodes {
				sb.WriteString(e.Title)
				sb.WriteByte('\n')
			}
			fp := sb.String()
			if first, dup := fingerprints[fp]; dup {
				si.NoEpisodeData = true
				if m.Episodes != nil {
					si.EpisodeCount = *m.Episodes
				}
				_ = first
			} else {
				fingerprints[fp] = i + 1
			}
		}
		base.Seasons = append(base.Seasons, si)
		base.EpisodeCount += si.EpisodeCount
		for _, rel := range a.mediaResult(m).Related {
			if _, ok := related[rel.ProviderID]; !ok && !seen[atoi(rel.ProviderID)] {
				related[rel.ProviderID] = rel
			}
		}
	}
	base.Related = base.Related[:0]
	for _, rel := range related {
		base.Related = append(base.Related, rel)
	}
	sortRelations(base.Related)
	if base.Status == "Ended" && chain[len(chain)-1].Status != "FINISHED" {
		base.Status = anilistStatus(chain[len(chain)-1].Status)
	}
	return base, nil
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

func sortRelations(rs []Relation) {
	sort.SliceStable(rs, func(i, j int) bool {
		if rs[i].Year != rs[j].Year {
			return rs[i].Year < rs[j].Year
		}
		return rs[i].Title < rs[j].Title
	})
}

func (a *AniList) search(ctx context.Context, item *model.Item) (*anilistMedia, error) {
	vars := map[string]any{"search": cleanSearchTitle(item.Title)}
	if item.Type == model.TypeMovie {
		vars["format"] = []string{"MOVIE", "SPECIAL", "OVA", "ONA"}
	} else if item.Type == model.TypeSeries {
		vars["format"] = []string{"TV", "TV_SHORT", "ONA", "OVA", "SPECIAL"}
	}
	resp, err := a.query(ctx, anilistSearchQuery, vars)
	if err != nil {
		return nil, err
	}
	if resp.Data.Page == nil {
		return nil, nil
	}
	cands := make([]candidate, len(resp.Data.Page.Media))
	for i, m := range resp.Data.Page.Media {
		names := append([]string{m.Title.Romaji, m.Title.English, m.Title.Native}, m.Synonyms...)
		cands[i] = candidate{names: names, year: m.StartDate.Year}
	}
	i := pickBest(item.Title, item.Year, cands)
	if i < 0 {
		return nil, nil
	}
	return &resp.Data.Page.Media[i], nil
}

var reTrailingYear = regexp.MustCompile(`\s*\(?\b(19|20)\d{2}\)?\s*$`)

// cleanSearchTitle drops a trailing year, which AniList's search treats as a token.
func cleanSearchTitle(t string) string {
	return strings.TrimSpace(reTrailingYear.ReplaceAllString(t, ""))
}

// Episodes returns one season's streaming episodes. Seasons map to the AniList Media
// ids collected by followChain (season n = Seasons[n-1].ProviderID).
func (a *AniList) Episodes(ctx context.Context, series *Result, season int) (*Season, error) {
	if series == nil || series.Provider != "anilist" || season < 1 {
		return nil, nil
	}
	id, _ := strconv.Atoi(series.ID)
	if season-1 < len(series.Seasons) && series.Seasons[season-1].ProviderID != "" {
		if series.Seasons[season-1].NoEpisodeData {
			return nil, nil
		}
		id = atoi(series.Seasons[season-1].ProviderID)
	} else if season != 1 {
		return nil, nil
	}
	resp, err := a.query(ctx, anilistByIDQuery, map[string]any{"id": id})
	if err != nil {
		return nil, err
	}
	if resp.Data.Media == nil {
		return nil, nil
	}
	return a.seasonResult(resp.Data.Media, season), nil
}
