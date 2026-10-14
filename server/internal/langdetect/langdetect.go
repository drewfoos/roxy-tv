// Package langdetect guesses the language of subtitle text. Release groups mislabel
// tracks often enough (two "Dialogue" tracks both tagged eng, one of them Spanish) that
// the tag alone cannot drive track selection. This is a small stop-word and script
// classifier, not a general detector: it only needs to separate the languages that show
// up in anime and film releases.
package langdetect

import (
	"regexp"
	"strings"
	"unicode"
)

// Result is an ISO 639-2/B code ("eng", "spa", ...) and a confidence in [0,1].
type Result struct {
	Lang       string
	Confidence float64
}

var stopWords = map[string][]string{
	"eng": {"the", "and", "you", "that", "this", "with", "what", "have", "your", "not", "for", "are", "but", "was", "just", "don't", "it's", "i'm", "can't", "we're"},
	"spa": {"que", "los", "las", "una", "por", "para", "con", "pero", "esto", "está", "como", "más", "tiene", "muy", "nada", "bien", "qué", "del", "ser", "hay"},
	"por": {"que", "não", "uma", "com", "para", "você", "isso", "mas", "está", "ele", "ela", "mais", "muito", "bem", "nós", "também", "então", "aqui", "por", "vamos"},
	"fra": {"que", "les", "une", "pas", "pour", "vous", "est", "des", "dans", "nous", "mais", "c'est", "avec", "elle", "sur", "tout", "bien", "très", "moi", "fait"},
	"deu": {"und", "das", "ich", "nicht", "ist", "die", "der", "sie", "wir", "mit", "ein", "eine", "was", "auch", "aber", "noch", "wenn", "nur", "sich", "dich"},
	"ita": {"che", "non", "una", "per", "con", "sono", "questo", "come", "anche", "della", "del", "più", "cosa", "bene", "hai", "sei", "noi", "quello", "ora", "molto"},
	"rus": {"что", "это", "как", "так", "они", "мне", "тебя", "все", "его", "нет", "вот", "был", "она", "еще", "уже", "если", "только", "меня", "тоже", "ничего"},
	"ind": {"yang", "dan", "tidak", "ini", "itu", "aku", "kamu", "untuk", "dengan", "ada", "akan", "saya", "bisa", "dari", "sudah", "apa", "kau", "mereka", "kita", "juga"},
}

var (
	reASSTag   = regexp.MustCompile(`\{[^}]*\}`)
	reSRTTag   = regexp.MustCompile(`<[^>]*>`)
	reSRTIndex = regexp.MustCompile(`^\d+$`)
	reSRTTime  = regexp.MustCompile(`^\d\d:\d\d:\d\d[,.]\d+\s+-->`)
	reWord     = regexp.MustCompile(`[\p{L}']+`)
)

// Detect classifies the dialogue text of an ASS/SSA or SRT subtitle file.
func Detect(subtitle string) Result {
	text := dialogueText(subtitle)
	if len(text) < 40 {
		return Result{}
	}

	// Script-based languages first: a majority of CJK/Hangul/Cyrillic/Arabic letters decides.
	var total, han, kana, hangul, cyr, arab, thai int
	for _, r := range text {
		if !unicode.IsLetter(r) {
			continue
		}
		total++
		switch {
		case unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r):
			kana++
		case unicode.Is(unicode.Han, r):
			han++
		case unicode.Is(unicode.Hangul, r):
			hangul++
		case unicode.Is(unicode.Cyrillic, r):
			cyr++
		case unicode.Is(unicode.Arabic, r):
			arab++
		case unicode.Is(unicode.Thai, r):
			thai++
		}
	}
	if total == 0 {
		return Result{}
	}
	frac := func(n int) float64 { return float64(n) / float64(total) }
	switch {
	case frac(kana) > 0.15:
		return Result{"jpn", min1(frac(kana+han) * 1.5)}
	case frac(han) > 0.5:
		return Result{"zho", min1(frac(han))}
	case frac(hangul) > 0.5:
		return Result{"kor", min1(frac(hangul))}
	case frac(arab) > 0.5:
		return Result{"ara", min1(frac(arab))}
	case frac(thai) > 0.5:
		return Result{"tha", min1(frac(thai))}
	case frac(cyr) > 0.5:
		// Russian vs other Cyrillic languages is left to the stop-word pass below.
	}

	// Stop-word scoring for Latin/Cyrillic-script languages.
	words := reWord.FindAllString(strings.ToLower(text), -1)
	if len(words) < 20 {
		return Result{}
	}
	counts := map[string]int{}
	for _, w := range words {
		counts[w]++
	}
	best, second := "", 0.0
	bestScore := 0.0
	for lang, sw := range stopWords {
		hits := 0
		for _, w := range sw {
			hits += counts[w]
		}
		score := float64(hits) / float64(len(words))
		if score > bestScore {
			best, second, bestScore = lang, bestScore, score
		} else if score > second {
			second = score
		}
	}
	if best == "" || bestScore < 0.03 {
		return Result{}
	}
	// Confidence: how far ahead the winner is, scaled by how much stop-word mass it has.
	margin := 1.0
	if bestScore > 0 {
		margin = (bestScore - second) / bestScore
	}
	return Result{best, min1(0.5*margin + 0.5*min1(bestScore/0.12))}
}

// dialogueText strips ASS/SRT structure and tags, keeping only spoken text.
func dialogueText(s string) string {
	var b strings.Builder
	isASS := strings.Contains(s, "[Events]") || strings.Contains(s, "[Script Info]")
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimRight(line, "\r")
		if isASS {
			if !strings.HasPrefix(line, "Dialogue:") {
				continue
			}
			parts := strings.SplitN(line, ",", 10)
			if len(parts) < 10 {
				continue
			}
			t := reASSTag.ReplaceAllString(parts[9], " ")
			t = strings.ReplaceAll(t, `\N`, " ")
			t = strings.ReplaceAll(t, `\n`, " ")
			t = strings.ReplaceAll(t, `\h`, " ")
			b.WriteString(t)
			b.WriteByte('\n')
			continue
		}
		if line == "" || reSRTIndex.MatchString(line) || reSRTTime.MatchString(line) || strings.HasPrefix(line, "WEBVTT") {
			continue
		}
		b.WriteString(reSRTTag.ReplaceAllString(line, " "))
		b.WriteByte('\n')
	}
	return b.String()
}

// Normalize maps common 2-letter and 639-2/T codes onto the 639-2/B codes Detect returns,
// so a tag like "en" or "ger" compares equal to a detection of "eng" / "deu".
func Normalize(code string) string {
	c := strings.ToLower(strings.TrimSpace(code))
	if i := strings.IndexAny(c, "-_"); i > 0 {
		c = c[:i]
	}
	switch c {
	case "en":
		return "eng"
	case "es":
		return "spa"
	case "pt":
		return "por"
	case "fr", "fre":
		return "fra"
	case "de", "ger":
		return "deu"
	case "it":
		return "ita"
	case "ru":
		return "rus"
	case "id":
		return "ind"
	case "ja":
		return "jpn"
	case "zh", "chi":
		return "zho"
	case "ko":
		return "kor"
	case "ar":
		return "ara"
	case "th":
		return "tha"
	}
	return c
}

func min1(f float64) float64 {
	if f > 1 {
		return 1
	}
	return f
}
