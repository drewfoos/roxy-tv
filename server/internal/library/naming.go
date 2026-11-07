package library

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Fallback filename parsing for libraries not fed by Sonarr/Radarr webhooks (ADR-0005).
// Handles TRaSH-style names and the common anime shapes. Webhook ingest supersedes this.

var (
	// "S02E01", "S02E01v2" (a release-version suffix glued on), and more episodes after it:
	// "S01E01-E02", "S01E01E02E03", "S01E01-02" (group 3; "-1080p" is not one: a word
	// follows the digits).
	reSxxExx   = regexp.MustCompile(`(?i)\bS(\d{1,3})E(\d{1,4})((?:-?E\d{1,4}|-\d{1,4})*)(?:v\d)?\b`)
	reChainNum = regexp.MustCompile(`\d{1,4}`)
	reAbsDash  = regexp.MustCompile(`\s-\s(\d{1,4})(?:v\d)?\s-\s`) // "Title - S01E01 - 001 - Ep"  or "Title - 001 - Ep"
	// "[Group] Title S2 - 01 (1080p)", "Title 2nd Season - 03", "Title Season 2 - 03": a
	// season token before the dash makes the number an episode of that season, not an
	// absolute number.
	reSeasonDash = regexp.MustCompile(`(?i)(?:^|[\s._\])])(?:S(\d{1,2})|(\d{1,2})(?:st|nd|rd|th)[\s._]+Season|Season[\s._]+(\d{1,2}))\s+-\s+(\d{1,4})(?:v\d)?(?:$|[\s._\[(])`)
	reAbsBracket = regexp.MustCompile(`\s-\s(\d{1,4})(?:v\d)?\s*(?:\[|\(|$)`)
	// "[DB]Tokyo Magnitude 8.0_-_01_(Dual Audio_10bit_BD1080p_x265)": underscores for spaces.
	reAbsUnderscore = regexp.MustCompile(`_-_(\d{1,4})(?:v\d)?(?:_|\(|\[|$)`)
	// "Episode 1 - The Golden Age" (Berserk Memorial Edition): an episode of the folder's season.
	reEpisodeWord = regexp.MustCompile(`(?i)(?:^|[\s._\-\]])episode[\s._]*(\d{1,4})(?:v\d)?(?:$|[\s._\-\[(])`)
	// Provider ids in a folder or file name: [tmdbid-123], {tvdb-1}, (imdb=tt1), [anilist-5]
	// (Jellyfin reads the same set, in any of the three brackets, with - or =).
	reIDTag = regexp.MustCompile(`(?i)[\[{(](tvdbid|tmdbid|imdbid|tvmazeid|anidbid|anilistid|anisearchid|tvdb|tmdb|imdb|tvmaze|anidb|anilist|anisearch)[-=](tt\d+|\d+)[\]})]`)
	// "1x01", "01x05", with "-1x02" / "x02" for more episodes. Two-digit episode at least,
	// so a resolution such as 1920x1080 (four digits each side) never matches.
	reNxM     = regexp.MustCompile(`(?i)(?:^|[\s._\-\[(])(\d{1,2})x(\d{2,3})((?:-?(?:\d{1,2})?x\d{2,3})*)(?:$|[\s._\-\])])`)
	reNxMMore = regexp.MustCompile(`(?i)x(\d{2,3})`)
	// "Season 1 Episode 2", "Season 01 - Episode 02".
	reSeasonEpisodeWords = regexp.MustCompile(`(?i)season[\s._]*(\d{1,3})[\s._\-]*episode[\s._]*(\d{1,4})`)
	reYear               = regexp.MustCompile(`\((19|20)\d{2}\)`)
	// Season folders: "Season 2", "S2", "Season 2 - Arc name", "Season 02 (2023)", the word
	// in other languages ("Staffel 2", "Saison 2", "Temporada 2", ...), "2nd Season",
	// "第2季", "シーズン2", "시즌 2".
	reSeasonDir     = regexp.MustCompile(`(?i)^(?:season|series|s|staffel|saison|temporada|temporadas|stagione|seizoen|sezon|sezona|säsong|sæson|sesong|kausi|évad|evad|sezóna|сезон|сезона|σεζόν|mevsim|musim)[\s._-]*(\d{1,3})(?:$|[\s._\-(\[])`)
	reSeasonOrdinal = regexp.MustCompile(`(?i)^(\d{1,3})(?:st|nd|rd|th)[\s._-]+season\b`)
	reSeasonCJK     = regexp.MustCompile(`^(?:第\s*(\d{1,3})\s*季|シーズン\s*(\d{1,3})|시즌\s*(\d{1,3})|(\d{1,3})\s*시즌)`)
	// A release folder holding a season pack: "Show.S02.1080p.WEB" / "Show S02 [1080p]".
	reSeasonRelease = regexp.MustCompile(`(?i)[\s._\-\[]S(\d{1,2})(?:[\s._\-\]]|$)`)
	reSpecials      = regexp.MustCompile(`(?i)^specials?$`)
	reRlsGroup      = regexp.MustCompile(`-([A-Za-z0-9]+)$`)
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
	// The last year with something before it is the film's, as Jellyfin reads it: a year
	// that starts the name, or one before the release year, is part of the title ("1917
	// (2019)", "2001 A Space Odyssey (1968)", "Blade Runner 2049 (2017)"). The first year
	// used to win, so these came out with the wrong title and year (and no match).
	var ym []int
	for from := 0; from < len(s); {
		m := reSceneYear.FindStringSubmatchIndex(s[from:])
		if m == nil {
			break
		}
		for i := range m {
			m[i] += from
		}
		if m[2] > 0 && strings.TrimSpace(strings.Trim(s[:m[2]], "([ ._")) != "" {
			ym = m
		}
		from = m[3] // the separator after a year can start the next one ("2049.2017")
	}
	if ym == nil {
		if m := reSceneYear.FindStringSubmatchIndex(s); m != nil && m[2] > 0 {
			ym = m
		}
	}
	if ym != nil {
		y, _ := strconv.Atoi(s[ym[2]:ym[3]])
		year = &y
		s = s[:ym[2]] // everything before the year is the title
	}
	s = stripBrackets(s)
	if year == nil {
		// No year to end the title: it ends at the first release tag instead ("Movie
		// 1080p BluRay x264" is "Movie"), as Jellyfin's CleanStrings do.
		for _, m := range reReleaseTag.FindAllStringIndex(s, -1) {
			if m[0] > 0 { // a tag word that starts the name is part of the title
				s = s[:m[0]]
				break
			}
		}
	}
	s = strings.TrimRight(strings.TrimSpace(s), " -([")
	return strings.TrimSpace(s), year, ids
}

// reReleaseTag finds the first release tag in a name: resolution, source, codec, audio,
// language and edition words that are never part of a film's title (Jellyfin's
// CleanStrings list), separated by spaces, dots, dashes or underscores.
var reReleaseTag = regexp.MustCompile(`(?i)(?:^|[ _.,\-])(?:3d|hsbs|htab|fsbs|ftab|sbs|mvc|hdr|hdr10|dv|uhd|ultrahd|4k|2160p|1080p|1080i|720p|720i|576p|576i|480p|480i|ac3|dts|aac|truehd|atmos|ddp5\.1|dd5\.1|dc|divx|divx5|dsr|dsrip|dvd|dvdrip|dvdscr|dvdscreener|screener|cam|hdtv|hdrip|hdtvrip|internal|limited|multi|ntsc|pal|pdtv|proper|repack|rerip|retail|cd[1-9]|r5|bd5|bd|svcd|unrated|ws|telesync|telecine|brrip|bdrip|bluray|blu-ray|remux|web-?dl|web-?rip|webrip|x264|x265|h\.?264|h\.?265|hevc|avc|xvid|10bit|8bit|extended|remastered|imax)(?:[ _.,\-]|$)`)

// LooksLikeMovie reports whether a file in a series library is better treated as a
// movie: no season/episode/absolute numbering, and a year in the file name, or in the
// folder name when the file sits directly in that folder (depth 2 = <dir>/<file>).
// Files deeper than that are inside a season folder and stay episodes.
func LooksLikeMovie(p ParsedName, fileStem, dir string, depth int) bool {
	if p.Season != nil || p.Episode != nil || p.Absolute != nil || p.AirDate != "" {
		return false // numbered, or named by a full date (a daily show's episode)
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
	// AirDate (YYYY-MM-DD) of a file named by its date ("Show 2024.03.15"), read only when
	// the name carries no episode number: daily shows. The metadata pass finds its episode.
	AirDate string
}

func ParseEpisodeFile(name string) ParsedName {
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	p := ParsedName{ProviderIDs: idTags(stem)}
	if loc := reSxxExx.FindStringSubmatchIndex(stem); loc != nil {
		s, _ := strconv.Atoi(stem[loc[2]:loc[3]])
		e, _ := strconv.Atoi(stem[loc[4]:loc[5]])
		p.Season, p.Episode = &s, &e
		// The last number of the chain is the last episode the file holds.
		if nums := reChainNum.FindAllString(stem[loc[6]:loc[7]], -1); len(nums) > 0 {
			if ee, _ := strconv.Atoi(nums[len(nums)-1]); ee > e {
				p.EpisodeEnd = &ee
			}
		}
		// "... - S01E01 - 001 - ..." : the first " - NNN - " after SxxExx is the absolute number
		if a := reAbsDash.FindStringSubmatch(stem[loc[1]:]); a != nil {
			n, _ := strconv.Atoi(a[1])
			p.Absolute = &n
		}
	} else if a := reSeasonEpisodeWords.FindStringSubmatch(stem); a != nil {
		s, _ := strconv.Atoi(a[1])
		e, _ := strconv.Atoi(a[2])
		p.Season, p.Episode = &s, &e
	} else if loc := reNxM.FindStringSubmatchIndex(stem); loc != nil && !isYear(stem[loc[4]:loc[5]]) {
		s, _ := strconv.Atoi(stem[loc[2]:loc[3]])
		e, _ := strconv.Atoi(stem[loc[4]:loc[5]])
		p.Season, p.Episode = &s, &e
		for _, m := range reNxMMore.FindAllStringSubmatch(stem[loc[6]:loc[7]], -1) {
			if ee, _ := strconv.Atoi(m[1]); ee > e {
				p.EpisodeEnd = &ee
			}
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
	if p.Season == nil && p.Episode == nil && p.Absolute == nil {
		p.AirDate = parseAirDate(stem)
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

var (
	reDateYMD = regexp.MustCompile(`(?:^|[\s._\-\[(])((?:19|20)\d{2})[.\-_ ](\d{1,2})[.\-_ ](\d{1,2})(?:$|[\s._\-\])])`)
	reDateDMY = regexp.MustCompile(`(?:^|[\s._\-\[(])(\d{1,2})[.\-_ ](\d{1,2})[.\-_ ]((?:19|20)\d{2})(?:$|[\s._\-\])])`)
)

// parseAirDate reads a date in a file name, "2024.03.15" / "2024-03-15" or "15.03.2024",
// as YYYY-MM-DD; "" when there is none (or it is not a real date).
func parseAirDate(stem string) string {
	var y, m, d string
	if a := reDateYMD.FindStringSubmatch(stem); a != nil {
		y, m, d = a[1], a[2], a[3]
	} else if a := reDateDMY.FindStringSubmatch(stem); a != nil {
		y, m, d = a[3], a[2], a[1]
	} else {
		return ""
	}
	yi, _ := strconv.Atoi(y)
	mi, _ := strconv.Atoi(m)
	di, _ := strconv.Atoi(d)
	t := time.Date(yi, time.Month(mi), di, 0, 0, 0, 0, time.UTC)
	if mi < 1 || mi > 12 || di < 1 || t.Day() != di {
		return ""
	}
	return t.Format("2006-01-02")
}

// SeriesFolderIndex is which folder of a file's path (relative to the library root, split
// on "/") is its show: the first, or the second when the show sits inside a grouping folder
// ("Kids/Show/Season 01/x.mkv": the third folder is a season and the first two are not), as
// Jellyfin finds shows at any depth.
func SeriesFolderIndex(parts []string) int {
	if len(parts) >= 4 {
		_, s0 := ParseSeasonDir(parts[0])
		_, s1 := ParseSeasonDir(parts[1])
		_, s2 := ParseSeasonDir(parts[2])
		if s2 && !s0 && !s1 {
			return 1
		}
	}
	return 0
}

// ParseSeasonDir returns the season number for "Season 01", "S2", "Specials" (0).
func ParseSeasonDir(name string) (int, bool) {
	if reSpecials.MatchString(strings.TrimSpace(name)) {
		return 0, true
	}
	name = strings.TrimSpace(name)
	if m := reSeasonDir.FindStringSubmatch(name); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n, true
	}
	if m := reSeasonOrdinal.FindStringSubmatch(name); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n, true
	}
	if m := reSeasonCJK.FindStringSubmatch(name); m != nil {
		n, _ := strconv.Atoi(firstNonEmpty(m[1], m[2], m[3], m[4]))
		return n, true
	}
	// A bare number ("01", "2"): a season folder in a show folder (never a year).
	if n, err := strconv.Atoi(name); err == nil && n >= 0 && len(name) <= 3 {
		return n, true
	}
	if m := reSeasonRelease.FindStringSubmatch(name); m != nil {
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
		case "tvdb", "tmdb", "imdb", "tvmaze", "anidb", "anilist", "anisearch":
			out[key] = m[2]
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
	if i := reSeasonEpisodeWords.FindStringIndex(s); i != nil {
		s = s[:i[0]]
	}
	if i := reNxM.FindStringIndex(s); i != nil {
		s = s[:i[0]]
	}
	s = strings.TrimRight(s, " -._")
	return strings.TrimSpace(s)
}

var reTitleNumber = regexp.MustCompile(`^(.*\S)[ ._]+(\d{1,3})$`)

// FolderTitleNumber reads "<folder name> <number>" file stems, e.g. "Arlong Park 07" in
// "[One Pace][69-95] Arlong Park". The name must equal the folder's (bracket tags
// removed, case and punctuation ignored), so titles that merely end in a number
// ("Mob Psycho 100", "Blade Runner 2049") are never read as an episode.
func FolderTitleNumber(stem, dir string) (int, bool) {
	m := reTitleNumber.FindStringSubmatch(stem)
	if m == nil {
		return 0, false
	}
	key := func(s string) string {
		var b strings.Builder
		for _, r := range strings.ToLower(reBrackets.ReplaceAllString(s, " ")) {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				b.WriteRune(r)
			}
		}
		return b.String()
	}
	name, folder := key(m[1]), key(dir)
	if name == "" || name != folder {
		return 0, false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

var (
	// "[Group] Show [04][1080p]", "[Group][Show][21]", "Show [12v2]": a bracket holding only
	// an episode number (not a year or a resolution).
	reBracketNumber = regexp.MustCompile(`[\[(](\d{1,4})(?:v\d)?[\])]`)
	// "Show ep01", "Show.E01.1080p", "Show_ep_03".
	reEpToken = regexp.MustCompile(`(?i)(?:^|[\s._\-\]])(?:ep[\s._]?(\d{1,4})|e(\d{2,4}))(?:v\d)?(?:$|[\s._\-\[(])`)
	// "01.mkv", "01 - Title", "01.Title", "012v2": a file named by its number.
	reLeadingNumber = regexp.MustCompile(`^(\d{1,4})(?:v\d)?(?:$|[\s._\-])`)
)

var resolutions = map[string]bool{"240": true, "360": true, "480": true, "540": true, "576": true, "720": true, "1080": true, "1440": true, "2160": true, "4320": true}

// ParseWeakEpisode reads episode numbers from names the main parser leaves alone, as
// Jellyfin also accepts: a number in its own brackets after the title (fansub style), an
// "ep01" / ".E01." token, or a name that starts with the number. These are guesses about
// loosely named files, so the scanner tries them only after the arr app's numbers (the
// hint) and only when nothing else gave a number. ok false when none fits.
// reNotAnEpisode: words that mark a loosely numbered file as something else (a creditless
// opening or ending, an OVA, a recap, a preview, a menu): "[Grp] Show NCED [02]" is not
// episode 2. Short tags only in capitals ("Ed, Edd n Eddy [05]" is an episode).
var (
	reNotAnEpisode     = regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}])(?:nc ?op|nc ?ed|ova|oad|recap|preview|menu|teaser|trailer)\d*(?:[^\p{L}\p{N}]|$)`)
	reNotAnEpisodeTags = regexp.MustCompile(`(?:^|[^\p{L}\p{N}])(?:OP|ED|PV|CM|SP)\d*(?:[^\p{L}\p{N}]|$)`)
)

func ParseWeakEpisode(stem string) (n int, ok bool) {
	// Loose numbers are a guess: an NCED, OVA or recap carrying the same number became
	// that episode (several of them one item, as "versions", beside the real one).
	if reNotAnEpisode.MatchString(stem) || reNotAnEpisodeTags.MatchString(stem) {
		return 0, false
	}
	// The first bracket is usually the release group; any later one holding only a
	// number is the episode.
	for i, m := range reBracketNumber.FindAllStringSubmatchIndex(stem, -1) {
		v := stem[m[2]:m[3]]
		if (i == 0 && m[0] == 0) || isYear(v) || resolutions[v] {
			continue
		}
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n, true
		}
	}
	if m := reEpToken.FindStringSubmatch(stem); m != nil {
		v := firstNonEmpty(m[1], m[2])
		if n, err := strconv.Atoi(v); err == nil && n > 0 && !isYear(v) {
			return n, true
		}
	}
	if m := reLeadingNumber.FindStringSubmatch(stem); m != nil && !isYear(m[1]) && !resolutions[m[1]] {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			return n, true
		}
	}
	return 0, false
}
