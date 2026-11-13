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

// anilistLimiter is shared by every AniList client in the process (matching, Identify
// search, profile avatars): AniList allows 90 requests a minute, 30 when it runs
// degraded, and also limits bursts. One request every 2 s with a burst of 3 stays
// under the degraded limit; a 429 pauses it for the Retry-After.
var anilistLimiter = newLimiter(3, 6*time.Second)

func NewAniList() *AniList {
	return &AniList{
		endpoint: anilistEndpoint,
		client:   &http.Client{Timeout: 20 * time.Second},
		lim:      anilistLimiter,
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
	tags { name category rank isMediaSpoiler }
	averageScore
	startDate { year month day }
	status
	format
	episodes
	duration
	studios(isMain: true) { nodes { name } }
	streamingEpisodes { title thumbnail }
	relations { edges { relationType node { id idMal type format episodes title { romaji english } startDate { year month day } coverImage { extraLarge } } } }`

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
	BannerImage string   `json:"bannerImage"`
	Genres      []string `json:"genres"`
	Tags        []struct {
		Name     string `json:"name"`
		Category string `json:"category"`
		Rank     int    `json:"rank"`
		Spoiler  bool   `json:"isMediaSpoiler"`
	} `json:"tags"`
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
		Advisories:    anilistAdvisories(m),
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
		rel := Relation{Type: relationKind(e.RelationType, e.Node.Format), Provider: "anilist", ProviderID: strconv.Itoa(e.Node.ID), Title: e.title(), Format: e.Node.Format, Year: e.Node.StartDate.Year, Poster: e.Node.CoverImage.ExtraLarge,
			Story: e.Node.Format == "MOVIE" && (e.RelationType == "SEQUEL" || e.RelationType == "PREQUEL")}
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
	w, err := a.walkChain(ctx, start)
	if err != nil {
		return nil, err
	}
	root, chain, seen, storyFilms := w.chain[0], w.chain, w.seen, w.films
	base := a.mediaResult(root)
	base.Seasons = a.chainSeasons(chain)
	base.EpisodeCount = 0
	for _, si := range base.Seasons {
		base.EpisodeCount += si.EpisodeCount
	}
	related := map[string]Relation{}
	for _, m := range chain {
		for _, rel := range a.mediaResult(m).Related {
			if old, ok := related[rel.ProviderID]; ok {
				// Linked from two seasons (a side story of one, the sequel of the next):
				// part of the story if either says so.
				old.Story = old.Story || rel.Story
				related[rel.ProviderID] = old
			} else if !seen[atoi(rel.ProviderID)] {
				related[rel.ProviderID] = rel
			}
		}
	}
	// The films passed between seasons are story films, linked to a season or not.
	for id, f := range storyFilms {
		if seen[id] {
			continue
		}
		key := strconv.Itoa(id)
		rel, ok := related[key]
		if !ok {
			fr := a.mediaResult(f)
			rel = Relation{Type: "movie", Provider: "anilist", ProviderID: key, Title: fr.Title, Format: f.Format, Year: f.StartDate.Year, Poster: f.CoverImage.ExtraLarge}
		}
		rel.Story = true
		related[key] = rel
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

// chainWalk is what walkChain found: the seasons in order, the films passed between
// them, and which two seasons each run of films joined.
type chainWalk struct {
	chain []*anilistMedia
	seen  map[int]bool          // the chain's ids
	films map[int]*anilistMedia // films passed on the way, read in full
	paths []StoryPath
	// through: films a season leads to only through specials (StoryGraph fills it).
	through []throughFilm
}

// walkChain walks PREQUEL edges back to the first TV entry, then SEQUEL edges forward.
func (a *AniList) walkChain(ctx context.Context, start *anilistMedia) (*chainWalk, error) {
	const maxHops = 12
	var paths []StoryPath
	addPath := func(after, before int, films []int) {
		for _, p := range paths {
			if p.After == after && p.Before == before {
				return
			}
		}
		paths = append(paths, StoryPath{After: after, Before: before, Films: films})
	}
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
	// Films on the way: AniList often links two seasons only through films (Bunny Girl
	// Senpai: Season 1 -> Dreaming Girl -> Sister Venturing Out -> Knapsack Kid -> Season
	// 2). step goes to the next season by a direct edge, else through up to maxFilmHops
	// films; the films it passes are part of the story (storyFilms).
	const maxFilmHops = 4
	storyFilms := map[int]*anilistMedia{}
	filmEdge := func(m *anilistMedia, relType string) int {
		for _, e := range m.Relations.Edges {
			if e.RelationType == relType && e.Node.Type == "ANIME" && e.Node.Format == "MOVIE" {
				return e.Node.ID
			}
		}
		return 0
	}
	step := func(m *anilistMedia, relType string) (int, error) {
		if id := next(m, relType); id != 0 {
			return id, nil
		}
		cur := m
		passed := map[int]bool{} // this hop's films: a film linking back is not a loop forever
		var order []int
		for i := 0; i < maxFilmHops; i++ {
			fid := filmEdge(cur, relType)
			if fid == 0 || passed[fid] {
				return 0, nil
			}
			passed[fid] = true
			f := storyFilms[fid] // passed the other way already: no second query
			if f == nil {
				var err error
				if f, err = fetch(fid); err != nil {
					return 0, err
				}
				if f == nil {
					return 0, nil
				}
				storyFilms[fid] = f
			}
			order = append(order, fid)
			if id := next(f, relType); id != 0 {
				// A run of films that reached a season: it sits between the two.
				if relType == "SEQUEL" {
					addPath(m.ID, id, order)
				} else {
					rev := make([]int, len(order))
					for i, v := range order {
						rev[len(order)-1-i] = v
					}
					addPath(id, m.ID, rev)
				}
				return id, nil
			}
			cur = f
		}
		return 0, nil
	}
	// Back to the root.
	root := start
	seen := map[int]bool{start.ID: true}
	for hops := 0; hops < maxHops; hops++ {
		id, err := step(root, "PREQUEL")
		if err != nil {
			return nil, err
		}
		if id == 0 || seen[id] {
			break
		}
		m, err := fetch(id)
		if err != nil {
			// A failed hop (rate limit, outage) must not pass for a shorter chain: the
			// seasons and relations would be saved short.
			return nil, err
		}
		if m == nil {
			break
		}
		seen[id] = true
		root = m
	}
	// Forward from the root.
	chain := []*anilistMedia{root}
	cur := root
	seen = map[int]bool{root.ID: true}
	for hops := 0; hops < maxHops; hops++ {
		id, err := step(cur, "SEQUEL")
		if err != nil {
			return nil, err
		}
		if id == 0 || seen[id] {
			break
		}
		m, err := fetch(id)
		if err != nil {
			return nil, err
		}
		if m == nil {
			break
		}
		seen[id] = true
		chain = append(chain, m)
		cur = m
	}
	return &chainWalk{chain: chain, seen: seen, films: storyFilms, paths: paths}, nil
}

// chainSeasons numbers the chain's entries as seasons 1..n.
//
// An entry's Crunchyroll episode list (streamingEpisodes) is only trusted when it fits
// the entry. AniList sometimes attaches another season's list: season 1's repeated on a
// sequel, or a later season's on season 1 (KonoSuba S1 carries S3's 11 titles, Mushoku
// Tensei's first cour S2's 25). Such a list gives no titles or stills for the season
// (NoEpisodeData) and does not stretch its episode count:
//   - a list longer than the entry's episode count by more than one (one more is
//     Crunchyroll listing an OVA as the last episode), or numbering an episode past it;
//   - a list another entry of the chain carries too: it belongs to the entry whose
//     episode count it matches (the first one when none or several do).
func (a *AniList) chainSeasons(chain []*anilistMedia) []SeasonInfo {
	out := make([]SeasonInfo, 0, len(chain))
	lists := map[string][]int{} // streaming-episode title list -> indices of the entries carrying it
	listLen := map[string]int{}
	var order []string
	for i, m := range chain {
		n := 0
		if m.Episodes != nil {
			n = *m.Episodes
		}
		parsed := a.seasonResult(m, i+1)
		si := SeasonInfo{Number: i + 1, EpisodeCount: n, Poster: m.CoverImage.ExtraLarge, ProviderID: strconv.Itoa(m.ID)}
		if m.IDMal != nil && *m.IDMal > 0 {
			si.MalID = strconv.Itoa(*m.IDMal)
		}
		if len(parsed.Episodes) > 0 {
			if !streamingListFits(m, parsed) {
				si.NoEpisodeData = true
			} else {
				if len(parsed.Episodes) > si.EpisodeCount {
					si.EpisodeCount = len(parsed.Episodes) // Crunchyroll lists OVAs as the last episode
				}
				var sb strings.Builder
				for _, e := range parsed.Episodes {
					sb.WriteString(e.Title)
					sb.WriteByte('\n')
				}
				fp := sb.String()
				if _, ok := lists[fp]; !ok {
					order = append(order, fp)
				}
				lists[fp] = append(lists[fp], i)
				listLen[fp] = len(parsed.Episodes)
			}
		}
		out = append(out, si)
	}
	for _, fp := range order {
		idx := lists[fp]
		if len(idx) < 2 {
			continue
		}
		owner := idx[0]
		for _, i := range idx {
			if m := chain[i]; m.Episodes != nil && *m.Episodes == listLen[fp] {
				owner = i
				break
			}
		}
		for _, i := range idx {
			if i == owner {
				continue
			}
			out[i].NoEpisodeData = true
			if m := chain[i]; m.Episodes != nil {
				out[i].EpisodeCount = *m.Episodes
			}
		}
	}
	return out
}

// streamingListFits reports whether an entry's parsed streaming episodes can be its own:
// no more than one episode beyond its episode count, none numbered past it. An entry
// with no episode count yet (still airing) is taken at its word.
func streamingListFits(m *anilistMedia, parsed *Season) bool {
	if m.Episodes == nil || *m.Episodes <= 0 {
		return true
	}
	limit := *m.Episodes + 1
	if len(parsed.Episodes) > limit {
		return false
	}
	for _, e := range parsed.Episodes {
		if e.Episode > limit {
			return false
		}
	}
	return true
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
	gap := 0
	if item.Type == model.TypeMovie {
		gap = movieYearGap
	}
	i := pickBest(item.Title, item.Year, cands, gap)
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
	se := a.seasonResult(resp.Data.Media, season)
	if !streamingListFits(resp.Data.Media, se) {
		return nil, nil // another season's list (see chainSeasons)
	}
	return se, nil
}

// AdvisoryOrder is the display order of content descriptors (Crunchyroll's vocabulary).
var AdvisoryOrder = []string{"Sexual Content", "Nudity", "Profanity", "Violence", "Graphic Violence", "Drug/Alcohol Use", "Suicide/Self-Harm", "Horror"}

// violenceTags are AniList tags that mean on-screen fighting or killing.
var violenceTags = map[string]bool{
	"Swordplay": true, "Guns": true, "Martial Arts": true, "Military": true, "Battle Royale": true,
	"War": true, "Terrorism": true, "Assassins": true, "Archery": true, "Spearplay": true, "Death Game": true,
}

// anilistAdvisories turns the community's tags into content descriptors. A tag counts
// when at least a third of voters agree (rank >= 35) and it is not a spoiler tag.
func anilistAdvisories(m *anilistMedia) []string {
	seen := map[string]bool{}
	for _, g := range m.Genres {
		if g == "Ecchi" {
			seen["Sexual Content"] = true
		}
		if g == "Horror" {
			seen["Horror"] = true
		}
		// Crunchyroll marks nearly every action series "Violence"; AniList has no plain
		// violence tag, so the genre stands for it.
		if g == "Action" {
			seen["Violence"] = true
		}
	}
	for _, t := range m.Tags {
		if t.Rank < 35 || t.Spoiler {
			continue
		}
		switch {
		case t.Category == "Sexual Content":
			seen["Sexual Content"] = true
		case t.Name == "Nudity":
			seen["Nudity"] = true
		case t.Name == "Gore" || t.Name == "Body Horror" || t.Name == "Cannibalism" || t.Name == "Torture" || t.Name == "Dismemberment":
			seen["Graphic Violence"] = true
		case t.Name == "Suicide" || t.Name == "Self-Harm":
			seen["Suicide/Self-Harm"] = true
		case t.Name == "Drugs" || t.Name == "Alcohol" || t.Name == "Smoking":
			seen["Drug/Alcohol Use"] = true
		// By name, not category: AniList files Military and War outside "Theme-Action", so
		// the old category check never let them count.
		case violenceTags[t.Name]:
			seen["Violence"] = true
		}
	}
	if seen["Graphic Violence"] {
		delete(seen, "Violence")
	}
	var out []string
	for _, k := range AdvisoryOrder {
		if seen[k] {
			out = append(out, k)
		}
	}
	return out
}
