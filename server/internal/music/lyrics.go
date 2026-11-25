package music

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Lyrics are a song's words. Synced lyrics carry the time each line is sung at.
type Lyrics struct {
	Source string      `json:"source"` // "lrc" | "txt" | "embedded" | "online"
	Synced bool        `json:"synced"`
	Lines  []LyricLine `json:"lines"`
}

// LyricLine is one line; Ms is when it starts (0 on every line of unsynced lyrics).
type LyricLine struct {
	Ms   int64  `json:"ms"`
	Text string `json:"text"`
}

// maxLyricsFile: a lyrics file is a few kilobytes; anything much larger is not one.
const maxLyricsFile = 1 << 20

// lyricsExts are the files read as a song's lyrics when they sit beside it under its
// name ("Song.mp3" + "Song.lrc"), in the order they are preferred.
var lyricsExts = []string{".lrc", ".txt"}

// SidecarKind says which lyrics file a song has beside it ("lrc", "txt" or ""), given the
// names in its folder (lower case, as present[name] = true).
func SidecarKind(audioName string, present map[string]bool) string {
	stem := strings.ToLower(strings.TrimSuffix(audioName, filepath.Ext(audioName)))
	for _, ext := range lyricsExts {
		if present[stem+ext] {
			return ext[1:]
		}
	}
	return ""
}

// SidecarPath finds the lyrics file of kind ("lrc" / "txt") beside an audio file,
// whatever the case of its name ("Song.LRC").
func SidecarPath(audioPath, kind string) (string, bool) {
	dir := filepath.Dir(audioPath)
	want := strings.ToLower(strings.TrimSuffix(filepath.Base(audioPath), filepath.Ext(audioPath))) + "." + kind
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.ToLower(e.Name()) == want {
			return filepath.Join(dir, e.Name()), true
		}
	}
	return "", false
}

// ReadLyricsFile reads a lyrics file. One that is too large, or not text, is refused.
func ReadLyricsFile(path string) (string, bool) {
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() || fi.Size() > maxLyricsFile {
		return "", false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return decodeText(b)
}

// decodeText reads a text file as UTF-8, or as UTF-16 when it starts with that mark
// (lyrics tools on Windows write both). Anything else that is not valid UTF-8 is read as
// Latin-1, which is what old .lrc files are.
func decodeText(b []byte) (string, bool) {
	switch {
	case len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF:
		b = b[3:]
	case len(b) >= 2 && ((b[0] == 0xFF && b[1] == 0xFE) || (b[0] == 0xFE && b[1] == 0xFF)):
		little := b[0] == 0xFF
		b = b[2:]
		runes := make([]rune, 0, len(b)/2)
		for i := 0; i+1 < len(b); i += 2 {
			u := uint16(b[i])<<8 | uint16(b[i+1])
			if little {
				u = uint16(b[i+1])<<8 | uint16(b[i])
			}
			runes = append(runes, rune(u))
		}
		return string(runes), true
	}
	if utf8.Valid(b) {
		if strings.ContainsRune(string(b), 0) {
			return "", false // binary
		}
		return string(b), true
	}
	runes := make([]rune, len(b))
	for i, c := range b {
		if c == 0 {
			return "", false
		}
		runes[i] = rune(c)
	}
	return string(runes), true
}

var (
	reLrcTime = regexp.MustCompile(`^\[(\d{1,3}):(\d{1,2})(?:[.:](\d{1,3}))?\]`)
	reLrcMeta = regexp.MustCompile(`^\[([a-zA-Z#]+):([^\]]*)\]\s*$`)
	// Word-by-word times inside a line ("<00:12.34>"), which only karaoke players use.
	reLrcWord = regexp.MustCompile(`<\d{1,3}:\d{1,2}(?:[.:]\d{1,3})?>`)
	// An address where words should be: what a release group leaves in a lyrics file or a
	// lyrics tag in place of the lyrics ("www.t.me/somebody"). Nobody sings one.
	reAdvert = regexp.MustCompile(`(?i)(https?://|www\.|\bt\.me/|\b[a-z0-9-]+\.(com|net|org|io|me|tv|ru|cc|to|xyz)(/|\b))`)
)

// isAdvert reports whether a line is an address left in place of words.
func isAdvert(line string) bool {
	return reAdvert.MatchString(line)
}

// hasWords reports whether lines hold anything that is sung: a letter or a digit somewhere.
func hasWords(lines []LyricLine) bool {
	for _, l := range lines {
		for _, r := range l.Text {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				return true
			}
		}
	}
	return false
}

// lyricsOrNothing is a file's own lyrics tag as it is kept: nothing when it holds no lyrics.
func lyricsOrNothing(text string) string {
	if LyricsKind(text, "embedded") == "" {
		return ""
	}
	return text
}

// LyricsKind says whether text is lyrics at all ("" when it is not: empty, or nothing
// but an address), and of which kind when it is.
func LyricsKind(text, source string) string {
	if len(ParseLyrics(text, source).Lines) == 0 {
		return ""
	}
	return source
}

// ParseLyrics reads lyrics text. Text with LRC time marks ("[01:23.45]words") comes back
// synced and in time order: a line with several marks is sung at each of them, and the
// file's own offset ("[offset:+250]", milliseconds, positive = earlier) is applied. Text
// without marks comes back line by line, unsynced. An address left where words should be
// is left out, and text with nothing sung in it comes back with no lines: not lyrics.
func ParseLyrics(text, source string) Lyrics {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	out := Lyrics{Source: source}
	var synced []LyricLine
	var plain []string
	var offset int64
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if m := reLrcMeta.FindStringSubmatch(line); m != nil {
			if strings.EqualFold(m[1], "offset") {
				offset, _ = strconv.ParseInt(strings.TrimSpace(m[2]), 10, 64)
			}
			continue // [ar:], [ti:], [length:], [by:] ... are about the file, not words
		}
		var times []int64
		for {
			m := reLrcTime.FindStringSubmatch(line)
			if m == nil {
				break
			}
			min, _ := strconv.ParseInt(m[1], 10, 64)
			sec, _ := strconv.ParseInt(m[2], 10, 64)
			var ms int64
			if m[3] != "" {
				frac, _ := strconv.ParseInt(m[3], 10, 64)
				switch len(m[3]) {
				case 1:
					ms = frac * 100
				case 2:
					ms = frac * 10
				default:
					ms = frac
				}
			}
			times = append(times, min*60000+sec*1000+ms)
			line = strings.TrimSpace(line[len(m[0]):])
		}
		line = strings.TrimSpace(reLrcWord.ReplaceAllString(line, ""))
		if isAdvert(line) {
			continue
		}
		if len(times) == 0 {
			plain = append(plain, line)
			continue
		}
		for _, t := range times {
			synced = append(synced, LyricLine{Ms: t, Text: line})
		}
	}
	if len(synced) > 0 {
		for i := range synced {
			if synced[i].Ms -= offset; synced[i].Ms < 0 {
				synced[i].Ms = 0
			}
		}
		sort.SliceStable(synced, func(i, j int) bool { return synced[i].Ms < synced[j].Ms })
		if !hasWords(synced) {
			return out // time marks and nothing sung at them
		}
		out.Synced, out.Lines = true, synced
		return out
	}
	// Unsynced: the words as written, without blank lines at either end.
	for len(plain) > 0 && plain[0] == "" {
		plain = plain[1:]
	}
	for len(plain) > 0 && plain[len(plain)-1] == "" {
		plain = plain[:len(plain)-1]
	}
	lines := make([]LyricLine, 0, len(plain))
	for _, l := range plain {
		lines = append(lines, LyricLine{Text: l})
	}
	if hasWords(lines) {
		out.Lines = lines
	}
	return out
}
