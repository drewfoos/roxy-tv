package library

import "strings"

// Languages a subtitle file name may carry, as ISO 639-2 codes (the terminology forms
// ffprobe and the apps use: "fra", "deu", "zho"), with the ISO 639-1 code, the
// bibliographic code where it differs, and the English and native names. A sidecar named
// "Show.English.ass", "Show.en.ass", "Show.eng.ass" or "Show.fre.ass" gets the same code.
var languages = []struct {
	code    string
	aliases string // space separated, lower case; multi-word names use "_"
}{
	{"eng", "en english"},
	{"jpn", "ja japanese 日本語 nihongo"},
	{"spa", "es spanish español espanol castellano"},
	{"por", "pt portuguese português portugues"},
	{"fra", "fr fre french français francais"},
	{"deu", "de ger german deutsch"},
	{"ita", "it italian italiano"},
	{"rus", "ru russian русский"},
	{"zho", "zh chi chinese 中文 mandarin"},
	{"yue", "cantonese 粵語"},
	{"kor", "ko korean 한국어"},
	{"ara", "ar arabic العربية"},
	{"hin", "hi hindi हिन्दी"},
	{"ben", "bn bengali"},
	{"tam", "ta tamil"},
	{"tel", "te telugu"},
	{"tha", "th thai ไทย"},
	{"vie", "vi vietnamese tiếng_việt"},
	{"ind", "id indonesian bahasa_indonesia"},
	{"msa", "ms may malay bahasa_melayu"},
	{"fil", "tl tagalog filipino"},
	{"tur", "tr turkish türkçe turkce"},
	{"pol", "pl polish polski"},
	{"nld", "nl dut dutch nederlands"},
	{"swe", "sv swedish svenska"},
	{"nor", "no norwegian norsk nob nb nno nn"},
	{"dan", "da danish dansk"},
	{"fin", "fi finnish suomi"},
	{"isl", "is ice icelandic"},
	{"ces", "cs cze czech čeština cestina"},
	{"slk", "sk slo slovak"},
	{"slv", "sl slovenian slovene"},
	{"hrv", "hr croatian hrvatski"},
	{"srp", "sr serbian srpski"},
	{"bos", "bs bosnian"},
	{"mkd", "mk mac macedonian"},
	{"bul", "bg bulgarian"},
	{"ron", "ro rum romanian română romana"},
	{"hun", "hu hungarian magyar"},
	{"ell", "el gre greek ελληνικά"},
	{"ukr", "uk ukrainian українська"},
	{"bel", "be belarusian"},
	{"lit", "lt lithuanian"},
	{"lav", "lv latvian"},
	{"est", "et estonian"},
	{"heb", "he iw hebrew עברית"},
	{"fas", "fa per persian farsi"},
	{"urd", "ur urdu"},
	{"cat", "ca catalan català"},
	{"eus", "eu baq basque"},
	{"glg", "gl galician"},
	{"gle", "ga irish"},
	{"cym", "cy wel welsh"},
	{"afr", "af afrikaans"},
	{"swa", "sw swahili"},
	{"sqi", "sq alb albanian"},
	{"hye", "hy arm armenian"},
	{"kat", "ka geo georgian"},
	{"aze", "az azerbaijani"},
	{"kaz", "kk kazakh"},
	{"mon", "mn mongolian"},
	{"khm", "km khmer"},
	{"lao", "lo lao"},
	{"mya", "my bur burmese"},
	{"nep", "ne nepali"},
	{"sin", "si sinhala"},
	{"mal", "ml malayalam"},
	{"kan", "kn kannada"},
	{"mar", "mr marathi"},
	{"guj", "gu gujarati"},
	{"pan", "pa punjabi"},
	{"lat", "la latin"},
	{"epo", "eo esperanto"},
}

var languageByAlias = func() map[string]string {
	m := map[string]string{}
	for _, l := range languages {
		m[l.code] = l.code
		for _, a := range strings.Fields(l.aliases) {
			m[strings.ReplaceAll(a, "_", " ")] = l.code
		}
	}
	// Chinese scripts and Brazilian Portuguese are often written as their own tag.
	for alias, code := range map[string]string{"chs": "zho", "cht": "zho", "sc": "zho", "tc": "zho", "zh-hans": "zho", "zh-hant": "zho", "pob": "por", "ptbr": "por"} {
		m[alias] = code
	}
	return m
}()

// subtitleLanguage reads one token of a subtitle file name as a language: an ISO 639
// code ("en", "eng", "fre"), an English or native name ("English", "Español"), or a code
// with a region ("pt-BR", "zh_Hant", "es-419"). It returns the ISO 639-2 code and, for a
// regional tag, the tag as written (kept in the track's title).
func subtitleLanguage(token string) (code, region string, ok bool) {
	t := strings.ToLower(strings.TrimSpace(token))
	if t == "" {
		return "", "", false
	}
	if c, ok := languageByAlias[t]; ok {
		if strings.ContainsAny(t, "-_") || t == "pob" || t == "ptbr" || t == "chs" || t == "cht" {
			return c, token, true
		}
		return c, "", true
	}
	if i := strings.IndexAny(t, "-_"); i > 0 {
		if c, ok := languageByAlias[t[:i]]; ok && len(t[:i]) <= 3 {
			return c, token, true
		}
	}
	return "", "", false
}
