// Package metadata matches library items against TMDB and AniList and caches the
// result (fields + images) in the store. See docs/api/native.md "Metadata and images".
//
// Providers are small HTTP clients behind one interface; Service owns the work queue
// that runs lookups after ingest.
package metadata

import (
	"context"
	"fmt"
	"html"
	"regexp"
	"strings"
	"unicode"

	"github.com/roxy-media/roxy/server/internal/model"
)

// Hints carries what the library knows about an item beyond the item row itself.
type Hints struct {
	Anime bool // the library is in anime mode
}

// Result is a provider's view of a movie or series.
type Result struct {
	Provider        string
	ID              string // the provider's own id for the match
	Title           string
	OriginalTitle   string
	Year            int
	Overview        string
	Tagline         string
	Genres          []string
	Advisories      []string // content descriptors in Crunchyroll's vocabulary ("Sexual Content", "Violence")
	CommunityRating float64  // 0..10, 0 = unknown
	OfficialRating  string
	Premiered       string // YYYY-MM-DD
	Status          string // series: Continuing | Ended | Upcoming
	Studio          string
	ProviderIDs     map[string]string // tmdb, tvdb, imdb, anilist, mal
	Images          map[string]string // poster, backdrop, logo -> absolute URL
	EpisodeCount    int               // series: total known episodes (0 = unknown)
	Seasons         []SeasonInfo      // series: aired seasons in order (absolute-number mapping)
	Related         []Relation        // movies / OVAs / side stories linked to this title
}

// SeasonInfo is what a provider knows about a season without fetching its episodes.
type SeasonInfo struct {
	Number       int
	EpisodeCount int
	Poster       string
	// ProviderID is the provider's id for this season when seasons are separate entries
	// (AniList models each TV season as its own Media; TMDB does not need it).
	ProviderID string
	// NoEpisodeData marks a season whose per-episode data from this provider is known
	// to be wrong (AniList sometimes repeats season 1's Crunchyroll episode list on a
	// sequel). Episodes() returns nil for it so another provider can fill the season.
	NoEpisodeData bool
}

// Relation is another title the provider links to this one that is NOT one of its
// seasons: movies, OVAs, side stories, alternative versions. Clients show them on the
// series page (Crunchyroll-style "seasons and movies together") and link back from the
// movie page when the related entry is in the library.
type Relation struct {
	Type       string // movie | ova | special | side_story | spin_off | alternative | summary | sequel | prequel | parent | other
	Provider   string
	ProviderID string
	Title      string
	Format     string // provider format: MOVIE, OVA, ONA, SPECIAL, TV, ...
	Year       int
	Poster     string
	Episodes   int
}

// Season is a season's episode list plus its own artwork.
type Season struct {
	Number   int
	Poster   string
	Episodes []EpisodeResult
}

// EpisodeResult is one episode's metadata.
type EpisodeResult struct {
	Season   int
	Episode  int
	Absolute int // 0 when unknown
	Title    string
	Overview string
	AirDate  string // YYYY-MM-DD
	Thumb    string // absolute URL of a still
}

// Provider is a metadata source.
type Provider interface {
	Name() string
	// Match finds the item, by provider id when item.ProviderIDs carries one, else by
	// title + year. A nil result with nil error means "no confident match".
	Match(ctx context.Context, item *model.Item, hints Hints) (*Result, error)
	// Episodes lists a season of a matched series. A nil Season means the provider
	// knows nothing about that season.
	Episodes(ctx context.Context, series *Result, season int) (*Season, error)
}

// AbsoluteToAired maps an absolute episode number onto (season, episode) using the
// provider's season list. Specials (season 0) are skipped. ok is false when the number
// is beyond the known episodes.
func (r *Result) AbsoluteToAired(abs int) (season, episode int, ok bool) {
	if abs <= 0 {
		return 0, 0, false
	}
	n := abs
	for _, s := range r.Seasons {
		if s.Number == 0 {
			continue
		}
		if n <= s.EpisodeCount {
			return s.Number, n, true
		}
		n -= s.EpisodeCount
	}
	return 0, 0, false
}

// ---- helpers shared by providers ----

var (
	reTags  = regexp.MustCompile(`(?is)<br\s*/?>\s*|</p>\s*|<[^>]+>`)
	reSpace = regexp.MustCompile(`[ \t]+`)
	reNL    = regexp.MustCompile(`\n{3,}`)
)

// StripHTML turns AniList's HTML descriptions into plain text with newlines.
func StripHTML(s string) string {
	s = reTags.ReplaceAllStringFunc(s, func(tag string) string {
		l := strings.ToLower(tag)
		if strings.HasPrefix(l, "<br") || l == "</p>" {
			return "\n"
		}
		return ""
	})
	s = html.UnescapeString(s)
	s = reSpace.ReplaceAllString(s, " ")
	s = reNL.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

// normalizeTitle lowercases, folds punctuation to spaces and collapses whitespace.
func normalizeTitle(s string) string {
	var b strings.Builder
	lastSpace := true
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastSpace = false
		default:
			if !lastSpace {
				b.WriteByte(' ')
				lastSpace = true
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// TitleSimilarity scores two titles in [0, 1]: 1 for a normalized exact match, else the
// share of tokens they have in common (relative to the longer one), with containment
// (one title is a prefix/substring of the other) scoring at least 0.8.
func TitleSimilarity(a, b string) float64 {
	na, nb := normalizeTitle(a), normalizeTitle(b)
	if na == "" || nb == "" {
		return 0
	}
	if na == nb {
		return 1
	}
	ta, tb := strings.Fields(na), strings.Fields(nb)
	set := map[string]bool{}
	for _, t := range ta {
		set[t] = true
	}
	common := 0
	for _, t := range tb {
		if set[t] {
			common++
			delete(set, t)
		}
	}
	longest := len(ta)
	if len(tb) > longest {
		longest = len(tb)
	}
	score := float64(common) / float64(longest)
	if strings.Contains(" "+na+" ", " "+nb+" ") || strings.Contains(" "+nb+" ", " "+na+" ") {
		if score < 0.8 {
			score = 0.8
		}
	}
	return score
}

// minMatchScore is the similarity below which a search hit is treated as noise.
// Search endpoints always return *something*; "Roxy Test Show" must not become a match.
const minMatchScore = 0.6

// pickBest scores candidates by title similarity (best of the given names) plus a year
// bonus, and returns the index of the winner or -1.
func pickBest(query string, year *int, candidates []candidate) int {
	best, bestScore := -1, 0.0
	for i, c := range candidates {
		score := 0.0
		for _, n := range c.names {
			if s := TitleSimilarity(query, n); s > score {
				score = s
			}
		}
		if score < minMatchScore {
			continue
		}
		if year != nil && c.year != 0 {
			d := *year - c.year
			if d < 0 {
				d = -d
			}
			switch {
			case d == 0:
				score += 0.3
			case d == 1:
				score += 0.1
			default:
				score -= 0.2
			}
		}
		if score > bestScore {
			best, bestScore = i, score
		}
	}
	return best
}

type candidate struct {
	names []string
	year  int
}

func dateYear(d string) int {
	if len(d) < 4 {
		return 0
	}
	var y int
	_, _ = fmt.Sscanf(d[:4], "%d", &y)
	return y
}
