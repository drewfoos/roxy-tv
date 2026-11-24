package music

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode"
)

// Fold makes a name comparable: lower case, accents off, punctuation and runs of spaces
// as one space ("Fryslân" and "FRYSLAN" are one word, "AC/DC" and "ac dc" one name). A
// name that is nothing but punctuation ("!!!") is kept as it is written, lower case.
func Fold(s string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.ToLower(s) {
		if unicode.Is(unicode.Mn, r) {
			continue // a combining accent: the letter before it is already written
		}
		plain, ok := plainLetter[r]
		switch {
		case ok:
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			b.WriteString(plain)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			b.WriteRune(r)
		default:
			space = true
		}
	}
	if b.Len() == 0 {
		return strings.Join(strings.Fields(strings.ToLower(s)), " ")
	}
	return b.String()
}

// SortName is the name a list is ordered by: folded, without a leading article.
func SortName(s string) string {
	f := Fold(s)
	for _, article := range []string{"the ", "a ", "an "} {
		if rest, ok := strings.CutPrefix(f, article); ok && rest != "" {
			return rest
		}
	}
	return f
}

// keyID makes an id from what a thing is (an artist's folded name, an album's key): the
// same artist or album always has the same id, across scans and across databases.
func keyID(kind, key string) string {
	sum := sha256.Sum256([]byte(kind + keySep + key))
	return hex.EncodeToString(sum[:16])
}

// ArtistID is the id of the artist with this name.
func ArtistID(name string) string { return keyID("artist", Fold(name)) }

// UnknownArtist is who a file without an artist tag is filed under.
const UnknownArtist = "Unknown Artist"

// VariousArtists is the album artist of a compilation that names none.
const VariousArtists = "Various Artists"

// Names that contain what otherwise separates two artists. A library's own artist folders
// are the better guide (see Credits); these cover the well-known ones met as guests.
var wholeNames = map[string]bool{
	"tyler the creator":               true,
	"earth wind fire":                 true,
	"crosby stills nash young":        true,
	"crosby stills nash":              true,
	"emerson lake palmer":             true,
	"blood sweat tears":               true,
	"peter paul and mary":             true,
	"now now":                         true,
	"10 000 maniacs":                  true,
	"simon garfunkel":                 true,
	"hall oates":                      true,
	"daryl hall john oates":           true,
	"mumford sons":                    true,
	"florence the machine":            true,
	"iron wine":                       true,
	"marina and the diamonds":         true,
	"above beyond":                    true,
	"chloe x halle":                   true,
	"brooks dunn":                     true,
	"big brother the holding company": true,
	"captain tennille":                true,
	"kool the gang":                   true,
	"derek the dominos":               true,
	"dan shay":                        true,
	"she him":                         true,
	"matt kim":                        true,
	"angus julia stone":               true,
	"me first and the gimme gimmes":   true,
	"sofi tukker":                     true,
	"zeds dead":                       true,
}

// Separators that always stand between two artists.
var strongSeps = []string{";", " / ", " feat. ", " feat ", " ft. ", " ft ", " featuring ", " with ", " • ", " · ", " vs. ", " vs "}

// Credits reads an artist tag as the artists it names, the main one first.
//
// Tags join artists in many ways: "A feat. B", "A, B & C", "A; B", "A • B". Splitting at
// every comma or ampersand would cut real names in two ("Tyler, The Creator", "Simon &
// Garfunkel"), so a name is kept whole when the library knows it (known: the folded names
// of its artist folders and of artists tagged alone) or it is one of the famous ones
// above, and an ampersand or " x " only separates the last name of a comma list or two
// names of which one is known.
//
// hint is the name of the artist folder the file sits in ("" for none). When one of the
// credited artists is that folder's, that one is the main artist: the folder is where
// the song was filed.
func Credits(tag, hint string, known func(folded string) bool) []string {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return []string{UnknownArtist}
	}
	isWhole := func(name string) bool {
		f := Fold(name)
		return f != "" && (wholeNames[f] || (hint != "" && f == Fold(hint)) || (known != nil && known(f)))
	}
	var out []string
	if isWhole(tag) {
		out = []string{tag}
	} else {
		for _, part := range splitAny(tag, strongSeps) {
			out = append(out, splitList(part, isWhole)...)
		}
	}
	return orderCredits(out, tag, hint)
}

// orderCredits tidies a list of credited names: one of each, in order, the artist whose
// folder the file is in (hint) first. An empty list is the tag itself.
func orderCredits(names []string, tag, hint string) []string {
	seen := map[string]bool{}
	var credits []string
	for _, name := range names {
		name = unwrap(strings.TrimSpace(name))
		f := Fold(name)
		if name == "" || seen[f] {
			continue
		}
		seen[f] = true
		credits = append(credits, name)
	}
	if len(credits) == 0 {
		if tag = strings.TrimSpace(tag); tag == "" {
			tag = UnknownArtist
		}
		return []string{tag}
	}
	if hint != "" {
		h := Fold(hint)
		for i, name := range credits {
			if i > 0 && Fold(name) == h {
				credits = append([]string{name}, append(append([]string{}, credits[:i]...), credits[i+1:]...)...)
				break
			}
		}
	}
	return credits
}

// unwrap takes a name out of the brackets around the whole of it ("(Guest)"); brackets
// inside a name are part of it ("Nodoka Toyohama (CV: Maaya Uchida)").
func unwrap(name string) string {
	for len(name) >= 2 && (name[0] == '(' && name[len(name)-1] == ')' && !strings.ContainsAny(name[1:len(name)-1], "()") ||
		name[0] == '[' && name[len(name)-1] == ']' && !strings.ContainsAny(name[1:len(name)-1], "[]")) {
		name = strings.TrimSpace(name[1 : len(name)-1])
	}
	return name
}

// splitList reads one stretch between strong separators: "A", "A, B", "A, B & C", "A & B".
func splitList(s string, isWhole func(string) bool) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if isWhole(s) {
		return []string{s}
	}
	parts := strings.Split(s, ", ")
	if len(parts) == 1 {
		return splitPair(s, isWhole, false)
	}
	var out []string
	for i := 0; i < len(parts); {
		// A known name may itself hold commas ("Tyler, The Creator, Frank Ocean"): the
		// longest run of parts that is one is taken whole.
		taken := false
		for j := min(len(parts), i+4); j > i+1; j-- {
			if whole := strings.Join(parts[i:j], ", "); isWhole(whole) {
				out = append(out, whole)
				i, taken = j, true
				break
			}
		}
		if taken {
			continue
		}
		// "A, B & C": the ampersand ends the list. Anywhere else it is part of a name
		// unless one side is an artist the library knows.
		out = append(out, splitPair(parts[i], isWhole, i == len(parts)-1)...)
		i++
	}
	return out
}

// splitPair separates "A & B" (or "A x B", "A and B" is left alone) when listEnd says the
// ampersand closes a comma list, or when one of the two is a known artist.
func splitPair(s string, isWhole func(string) bool, listEnd bool) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if isWhole(s) {
		return []string{s}
	}
	for _, sep := range []string{" & ", " x ", " X "} {
		i := strings.Index(s, sep)
		if i <= 0 {
			continue
		}
		left, right := strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+len(sep):])
		if left == "" || right == "" {
			continue
		}
		if (listEnd && sep == " & ") || isWhole(left) || isWhole(right) {
			return append([]string{left}, splitPair(right, isWhole, listEnd)...)
		}
	}
	return []string{s}
}

// splitAny cuts s at every occurrence of any of seps (matched without regard to case).
func splitAny(s string, seps []string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); {
		matched := 0
		for _, sep := range seps {
			if len(s)-i >= len(sep) && strings.EqualFold(s[i:i+len(sep)], sep) {
				matched = len(sep)
				break
			}
		}
		if matched == 0 {
			i++
			continue
		}
		out = append(out, s[start:i])
		i += matched
		start = i
	}
	return append(out, s[start:])
}
