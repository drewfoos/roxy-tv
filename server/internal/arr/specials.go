package arr

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// EpisodeRef is a special as the catalogue requests are made from (TMDB) lists it.
type EpisodeRef struct {
	Number  int
	Title   string
	AirDate string // YYYY-MM-DD
}

// RenumberSpecials returns eps with the specials (season 0) numbered the way refs
// number them. Sonarr counts specials as TVDB does and requests name them as TMDB does,
// and the two disagree for most shows that have more than a few: "S0E5" asked for from
// the TMDB list is rarely Sonarr's S0E5, so the wrong special was monitored, searched
// for and tracked.
//
// A special is paired with a ref by its title, then by its air date (each only when
// one special and one ref share it), then by its number. A special left without a ref,
// whose number a ref uses, moves past the end of both lists, so no code names it by
// accident. Regular seasons are untouched; with no refs, so is everything.
func RenumberSpecials(eps []Episode, refs []EpisodeRef) []Episode {
	if len(refs) == 0 {
		return eps
	}
	var specials []int // indexes into eps
	top := 0
	for i, e := range eps {
		if e.SeasonNumber == 0 {
			specials = append(specials, i)
			if e.EpisodeNumber > top {
				top = e.EpisodeNumber
			}
		}
	}
	if len(specials) == 0 {
		return eps
	}
	refNumbers := map[int]bool{}
	for _, r := range refs {
		refNumbers[r.Number] = true
		if r.Number > top {
			top = r.Number
		}
	}
	refOf := map[int]int{}  // index into eps -> the ref's number
	taken := map[int]bool{} // ref numbers paired
	pair := func(key func(title, date string, number int) string) {
		ours, theirs := map[string][]int{}, map[string][]int{}
		for _, i := range specials {
			if _, done := refOf[i]; done {
				continue
			}
			date := eps[i].AirDate
			if len(date) > 10 {
				date = date[:10]
			}
			if k := key(eps[i].Title, date, eps[i].EpisodeNumber); k != "" {
				ours[k] = append(ours[k], i)
			}
		}
		for _, r := range refs {
			if taken[r.Number] {
				continue
			}
			if k := key(r.Title, r.AirDate, r.Number); k != "" {
				theirs[k] = append(theirs[k], r.Number)
			}
		}
		for k, rs := range theirs {
			if es := ours[k]; len(rs) == 1 && len(es) == 1 {
				refOf[es[0]], taken[rs[0]] = rs[0], true
			}
		}
	}
	pair(func(title, _ string, _ int) string { return titleKey(title) })
	pair(func(_, date string, _ int) string { return date })
	pair(func(_, _ string, number int) string { return strconv.Itoa(number) })

	out := append([]Episode(nil), eps...)
	var loose []int
	for _, i := range specials {
		if n, ok := refOf[i]; ok {
			out[i].EpisodeNumber = n
		} else if refNumbers[eps[i].EpisodeNumber] {
			loose = append(loose, i)
		}
	}
	sort.Slice(loose, func(a, b int) bool { return eps[loose[a]].EpisodeNumber < eps[loose[b]].EpisodeNumber })
	for k, i := range loose {
		out[i].EpisodeNumber = top + 1 + k
	}
	return out
}

// genericTitle: names that say nothing about which special it is ("Episode 3", "OVA").
var genericTitle = regexp.MustCompile(`^(episode|special|ova|oav|oad|ona|tba|tbd)?\d*$`)

// titleKey folds a title to its letters and digits; empty for a title that is missing
// or generic, which is then paired by date or number.
func titleKey(title string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(title) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	k := b.String()
	if genericTitle.MatchString(k) {
		return ""
	}
	return k
}

// SameSpecial: both titles name something and name the same thing (letters and digits
// compared, as RenumberSpecials pairs them).
func SameSpecial(a, b string) bool {
	k := titleKey(a)
	return k != "" && k == titleKey(b)
}
