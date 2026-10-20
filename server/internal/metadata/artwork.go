package metadata

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Candidate is one piece of art a title could use: where it comes from, a full-size
// URL the server can fetch and lock in, and a small preview for the picker.
type Candidate struct {
	Source   string  `json:"source"`          // tmdb | fanart
	URL      string  `json:"url"`             // full size
	Preview  string  `json:"preview"`         // small, for the grid
	Lang     string  `json:"lang,omitempty"`  // "en", "ja", "" for textless
	Width    int     `json:"width,omitempty"` // 0 when the source does not say
	Height   int     `json:"height,omitempty"`
	Score    float64 `json:"score"`    // provider votes/likes, for ordering
	Selected bool    `json:"selected"` // the art in use now
}

// tmdbImageList is the /images response: every rendition TMDB holds for a title.
type tmdbImageList struct {
	Backdrops []tmdbSizedImage `json:"backdrops"`
	Posters   []tmdbSizedImage `json:"posters"`
	Logos     []tmdbSizedImage `json:"logos"`
}

type tmdbSizedImage struct {
	FilePath string  `json:"file_path"`
	Lang     string  `json:"iso_639_1"`
	Votes    float64 `json:"vote_average"`
	Count    int     `json:"vote_count"`
	Width    int     `json:"width"`
	Height   int     `json:"height"`
}

// Candidates lists what TMDB has for one image type of a title.
func (t *TMDB) Candidates(ctx context.Context, isMovie bool, tmdbID int, typ string) ([]Candidate, error) {
	if t.key == "" || tmdbID == 0 {
		return nil, nil
	}
	kind := "tv"
	if isMovie {
		kind = "movie"
	}
	// TMDB's CDN resets a fair share of connections from some networks; the picker is
	// an interactive one-shot, so try a few times before giving up on the whole set.
	var list tmdbImageList
	var err error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * 300 * time.Millisecond):
			}
		}
		if err = t.get(ctx, "/"+kind+"/"+strconv.Itoa(tmdbID)+"/images", url.Values{"include_image_language": {"en,ja,null"}}, &list); err == nil {
			break
		}
	}
	if err != nil {
		return nil, err
	}
	var src []tmdbSizedImage
	preview := "w780"
	switch typ {
	case "backdrop":
		src = list.Backdrops
	case "poster":
		src = list.Posters
		preview = "w342"
	case "hero":
		// The Home banner: posters without a title on them.
		for _, im := range list.Posters {
			if im.Lang == "" {
				src = append(src, im)
			}
		}
		preview = "w342"
	case "logo":
		src = list.Logos
		preview = "w500"
	default:
		return nil, nil
	}
	out := make([]Candidate, 0, len(src))
	for _, im := range src {
		if typ == "logo" && strings.HasSuffix(strings.ToLower(im.FilePath), ".svg") {
			continue
		}
		out = append(out, Candidate{
			Source: "tmdb", URL: t.image("original", im.FilePath), Preview: t.image(preview, im.FilePath),
			Lang: im.Lang, Width: im.Width, Height: im.Height, Score: im.Votes,
		})
	}
	return out, nil
}

// textlessOnly keeps fanart.tv's "no language" ("00") images: the ones without a title.
func textlessOnly(list []fanartImage) []fanartImage {
	out := make([]fanartImage, 0, len(list))
	for _, im := range list {
		if im.Lang == "00" {
			out = append(out, im)
		}
	}
	return out
}

// Fanart is the fanart.tv client: a free project key, images keyed by TVDB id for
// series and TMDB id for movies. It is the best public source of logos and textless
// backgrounds; everything it returns is already full size.
type Fanart struct {
	key    string
	client *http.Client
	lim    *limiter
}

func NewFanart(key string) *Fanart {
	return &Fanart{key: strings.TrimSpace(key), client: &http.Client{Timeout: 20 * time.Second}, lim: newLimiter(4, time.Second)}
}

func (f *Fanart) Enabled() bool { return f != nil && f.key != "" }

type fanartImage struct {
	URL   string `json:"url"`
	Lang  string `json:"lang"`
	Likes string `json:"likes"`
}

type fanartTV struct {
	HDTVLogo       []fanartImage `json:"hdtvlogo"`
	ClearLogo      []fanartImage `json:"clearlogo"`
	ShowBackground []fanartImage `json:"showbackground"`
	TVPoster       []fanartImage `json:"tvposter"`
}

type fanartMovie struct {
	HDMovieLogo     []fanartImage `json:"hdmovielogo"`
	MovieLogo       []fanartImage `json:"movielogo"`
	MovieBackground []fanartImage `json:"moviebackground"`
	MoviePoster     []fanartImage `json:"movieposter"`
}

// Candidates lists what fanart.tv has for one image type. Series need the TVDB id,
// movies the TMDB id; a title without the right id gets nothing.
func (f *Fanart) Candidates(ctx context.Context, isMovie bool, id string, typ string) ([]Candidate, error) {
	if !f.Enabled() || id == "" {
		return nil, nil
	}
	var images []fanartImage
	if isMovie {
		var m fanartMovie
		if err := f.get(ctx, "movies/"+url.PathEscape(id), &m); err != nil {
			return nil, err
		}
		switch typ {
		case "backdrop":
			images = m.MovieBackground
		case "poster":
			images = m.MoviePoster
		case "hero":
			images = textlessOnly(m.MoviePoster)
		case "logo":
			images = append(m.HDMovieLogo, m.MovieLogo...)
		}
	} else {
		var tv fanartTV
		if err := f.get(ctx, "tv/"+url.PathEscape(id), &tv); err != nil {
			return nil, err
		}
		switch typ {
		case "backdrop":
			images = tv.ShowBackground
		case "poster":
			images = tv.TVPoster
		case "hero":
			images = textlessOnly(tv.TVPoster)
		case "logo":
			images = append(tv.HDTVLogo, tv.ClearLogo...)
		}
	}
	out := make([]Candidate, 0, len(images))
	for _, im := range images {
		likes, _ := strconv.ParseFloat(im.Likes, 64)
		lang := im.Lang
		if lang == "00" {
			lang = "" // fanart's "no language"
		}
		out = append(out, Candidate{
			Source: "fanart", URL: im.URL, Preview: strings.Replace(im.URL, "/fanart/", "/preview/", 1), Lang: lang, Score: likes,
		})
	}
	return out, nil
}

func (f *Fanart) get(ctx context.Context, path string, out any) error {
	u := "https://webservice.fanart.tv/v3/" + path + "?api_key=" + url.QueryEscape(f.key)
	err := getJSON(ctx, f.client, f.lim, http.MethodGet, u, nil, nil, out)
	if err != nil {
		// A missing title is a 404 from fanart.tv: nothing to offer, not a failure.
		if isNotFound(err) {
			return nil
		}
		// The key must never leave the server (logs or API).
		return errors.New(strings.ReplaceAll(err.Error(), f.key, "***"))
	}
	return nil
}

// SortCandidates puts the art in use first, then by provider score.
func SortCandidates(c []Candidate) {
	sort.SliceStable(c, func(i, j int) bool {
		if c[i].Selected != c[j].Selected {
			return c[i].Selected
		}
		return c[i].Score > c[j].Score
	})
}
