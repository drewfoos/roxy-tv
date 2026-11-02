package library

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Fallback filename parsing for libraries not fed by Sonarr/Radarr webhooks (ADR-0005).
// Handles TRaSH-style names and the common anime shapes. Webhook ingest supersedes this.

var (
	reSxxExx  = regexp.MustCompile(`(?i)\bS(\d{1,3})E(\d{1,4})(?:-?E?(\d{1,4}))?\b`)
	reAbsDash = regexp.MustCompile(`\s-\s(\d{1,4})(?:v\d)?\s-\s`) // "Title - S01E01 - 001 - Ep"  or "Title - 001 - Ep"
	// "[Group] Title S2 - 01 (1080p)", "Title 2nd Season - 03", "Title Season 2 - 03": a
	// season token before the dash makes the number an episode of that season, not an
	// absolute number.
	reSeasonDash = regexp.MustCompile(`(?i)(?:^|[\s._\])])(?:S(\d{1,2})|(\d{1,2})(?:st|nd|rd|th)[\s._]+Season|Season[\s._]+(\d{1,2}))\s+-\s+(\d{1,4})(?:v\d)?(?:$|[\s._\[(])`)
	reAbsBracket = regexp.MustCompile(`\s-\s(\d{1,4})(?:v\d)?\s*(?:\[|\(|$)`)
	// "[DB]Tokyo Magnitude 8.0_-_01_(Dual Audio_10bit_BD1080p_x265)": underscores for spaces.
	reAbsUnderscore = regexp.MustCompile(`_-_(\d{1,4})(?:v\d)?(?:_|\(|\[|$)`)
	// "Episode 1 - The Golden Age" (Berserk Memorial Edition): an episode of the folder's season.
	reEpisodeWord = regexp.MustCompile(`(?i)(?:^|[\s._\-\]])episode[\s._]*(\d{1,4})(?:v\d)?(?:$|[\s._\-\[(])`)
	reIDTag       = regexp.MustCompile(`(?i)[\[{](tvdbid|tmdbid|imdbid|tvdb|tmdb|imdb)[-=](tt\d+|\d+)[\]}]`)
	reYear        = regexp.MustCompile(`\((19|20)\d{2}\)`)
	reSeasonDir   = regexp.MustCompile(`(?i)^(?:season|s)\s*(\d{1,3})$`)
	reSpecials    = regexp.MustCompile(`(?i)^specials?$`)
	reRlsGroup    = regexp.MustCompile(`-([A-Za-z0-9]+)$`)
	// Scene-style "Title.Of.Film.2019.1080p.BluRay.x265-GRP": the first bare year token
	// separated by dots/underscores/spaces ends the title.
	reSceneYear = regexp.MustCompile(`(?:^|[. _(\[])((?:19|20)\d{2})(?:[. _)\]]|$)`)
)

// ParseMovieName extracts a title and year from a movie file stem or folder name.
// Handles "Title (2019) [tags]" and scene-style "Title.2019.1080p.x265-GRP".
func ParseMovieName(name string) (title string, year *int, ids map[string]string) {
	ids = idTags(name)
	s := reIDTag.ReplaceAllString(name, "")
	// Dotted scene names: the part outside brackets has no spaces and dots as separators.
	if head := stripBrackets(s); !strings.Contains(head, " ") && strings.Count(head, ".") >= 2 {
		s = strings.ReplaceAll(s, ".", " ")
	}
	s = strings.ReplaceAll(s, "_", " ")
	if m := reSceneYear.FindStringSubmatchIndex(s); m != nil {
		y, _ := strconv.Atoi(s[m[2]:m[3]])
		year = &y
		s = s[:m[2]] // everything before the year is the title
	}
	s = stripBrackets(s)
	s = strings.TrimRight(strings.TrimSpace(s), " -([")
	return strings.TrimSpace(s), year, ids
}

// LooksLikeMovie reports whether a file in a series library is better treated as a
// movie: no season/episode/absolute numbering, and a year in the file name, or in the
// folder name when the file sits directly in that folder (depth 2 = <dir>/<file>).
// Files deeper than that are inside a season folder and stay episodes.
func LooksLikeMovie(p ParsedName, fileStem, dir string, depth int) bool {
	if p.Season != nil || p.Episode != nil || p.Absolute != nil {
		return false
	}
	if reSceneYear.MatchString(fileStem) {
		return true
	}
	return depth == 2 && reSceneYear.MatchString(dir)
}

type ParsedName struct {
	Season     *int
	Episode    *int
	EpisodeEnd *int
	// SeasonDash: the numbers came from a "S2 - 01" style name (read as absolute
	// numbers before such names were understood).
	SeasonDash   bool
	Absolute     *int
	ProviderIDs  map[string]string
	ReleaseGroup string
	CleanTitle   string
}

func ParseEpisodeFile(name string) ParsedName {
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	p := ParsedName{ProviderIDs: idTags(stem)}
	if loc := reSxxExx.FindStringSubmatchIndex(stem); loc != nil {
		s, _ := strconv.Atoi(stem[loc[2]:loc[3]])
		e, _ := strconv.Atoi(stem[loc[4]:loc[5]])
		p.Season, p.Episode = &s, &e
		if loc[6] >= 0 {
			ee, _ := strconv.Atoi(stem[loc[6]:loc[7]])
			p.EpisodeEnd = &ee
		}
		// "... - S01E01 - 001 - ..." : the first " - NNN - " after SxxExx is the absolute number
		if a := reAbsDash.FindStringSubmatch(stem[loc[1]:]); a != nil {
			n, _ := strconv.Atoi(a[1])
			p.Absolute = &n
		}
	} else if a := reSeasonDash.FindStringSubmatch(stem); a != nil {
		s, _ := strconv.Atoi(firstNonEmpty(a[1], a[2], a[3]))
		e, _ := strconv.Atoi(a[4])
		p.Season, p.Episode = &s, &e
		p.SeasonDash = true
	} else if a := reAbsDash.FindStringSubmatch(stem); a != nil {
		n, _ := strconv.Atoi(a[1])
		p.Absolute = &n
	} else if a := reAbsBracket.FindStringSubmatch(stem); a != nil {
		n, _ := strconv.Atoi(a[1])
		p.Absolute = &n
	} else if a := reAbsUnderscore.FindStringSubmatchIndex(stem); a != nil && !isYear(stem[a[2]:a[3]]) {
		n, _ := strconv.Atoi(stem[a[2]:a[3]])
		p.Absolute = &n
		p.CleanTitle = strings.TrimSpace(strings.ReplaceAll(stripBrackets(stem[:a[0]]), "_", " "))
	} else if a := reEpisodeWord.FindStringSubmatch(stem); a != nil && !isYear(a[1]) {
		n, _ := strconv.Atoi(a[1])
		p.Episode = &n
	}
	if m := reRlsGroup.FindStringSubmatch(stripBrackets(stem)); m != nil {
		p.ReleaseGroup = m[1]
	}
	if p.CleanTitle == "" {
		p.CleanTitle = cleanTitle(stem)
	}
	return p
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// isYear reports a four-digit 19xx/20xx number, which is never an episode number.
func isYear(s string) bool {
	return len(s) == 4 && (strings.HasPrefix(s, "19") || strings.HasPrefix(s, "20"))
}

// ParseSeriesDir extracts title, year and id tags from a series folder name.
func ParseSeriesDir(name string) (title string, year *int, ids map[string]string) {
	ids = idTags(name)
	if m := reYear.FindString(name); m != "" {
		y, _ := strconv.Atoi(strings.Trim(m, "()"))
		year = &y
	}
	t := reIDTag.ReplaceAllString(name, "")
	t = reYear.ReplaceAllString(t, "")
	return strings.TrimSpace(t), year, ids
}

// ParseSeasonDir returns the season number for "Season 01", "S2", "Specials" (0).
func ParseSeasonDir(name string) (int, bool) {
	if reSpecials.MatchString(strings.TrimSpace(name)) {
		return 0, true
	}
	if m := reSeasonDir.FindStringSubmatch(strings.TrimSpace(name)); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n, true
	}
	return 0, false
}

func idTags(s string) map[string]string {
	out := map[string]string{}
	for _, m := range reIDTag.FindAllStringSubmatch(s, -1) {
		key := strings.ToLower(m[1])
		key = strings.TrimSuffix(key, "id")
		switch key {
		case "tvdb":
			out["tvdb"] = m[2]
		case "tmdb":
			out["tmdb"] = m[2]
		case "imdb":
			out["imdb"] = m[2]
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

var reBrackets = regexp.MustCompile(`\[[^\]]*\]|\([^)]*\)|\{[^}]*\}`)

func stripBrackets(s string) string { return strings.TrimSpace(reBrackets.ReplaceAllString(s, "")) }

func cleanTitle(stem string) string {
	s := stripBrackets(stem)
	if i := reSxxExx.FindStringIndex(s); i != nil {
		s = s[:i[0]]
	}
	if i := reSeasonDash.FindStringIndex(s); i != nil {
		s = s[:i[0]]
	}
	s = strings.TrimRight(s, " -")
	return strings.TrimSpace(s)
}
