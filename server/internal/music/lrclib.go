package music

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Lyrics for songs that have none of their own, from lrclib.net: a public collection of
// lyrics, many of them timed line by line, that answers by a song's name, artist, album
// and length and wants no key.
//
// It is asked only when the owner turned this on (ROXY_LYRICS_ONLINE): the question
// names the song, and what is in somebody's library is theirs to tell. A song is asked
// about once; what comes back is kept with the song, and a song nothing was found for
// is left alone for a month. A song with a lyrics file or a lyrics tag of its own is
// never asked about, and its own lyrics are always the ones shown.

// LyricsFinder looks a song's lyrics up.
type LyricsFinder struct {
	BaseURL string // "https://lrclib.net"
	HTTP    *http.Client
	// Agent names the caller, as the service asks of its callers.
	Agent string
	// Gap is the wait between two songs: the service is free, and is not to be hurried.
	Gap time.Duration
	// Again is how long a song nothing was found for is left alone.
	Again time.Duration
}

// NewLyricsFinder is the finder for lrclib.net.
func NewLyricsFinder() *LyricsFinder {
	return &LyricsFinder{
		BaseURL: "https://lrclib.net",
		// No redirects: the answer comes from the address asked, or it is not taken.
		HTTP:  &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		Agent: "Roxy Server (a self-hosted media server; lyrics for songs that have none)",
		Gap:   700 * time.Millisecond,
		Again: 30 * 24 * time.Hour,
	}
}

// LyricsQuery is the song asked about.
type LyricsQuery struct {
	Title, Artist, Album string
	DurationSec          int
}

// maxLyricsAnswer: an answer is a few kilobytes of text; a search, a few dozen of them.
const maxLyricsAnswer = 2 << 20

type lrclibSong struct {
	TrackName    string  `json:"trackName"`
	ArtistName   string  `json:"artistName"`
	Duration     float64 `json:"duration"`
	Instrumental bool    `json:"instrumental"`
	Plain        string  `json:"plainLyrics"`
	Synced       string  `json:"syncedLyrics"`
}

// text is the lyrics an answer carries: the timed ones when it has them.
func (s lrclibSong) text() string {
	if strings.TrimSpace(s.Synced) != "" {
		return s.Synced
	}
	return s.Plain
}

var (
	// What a release adds to a song's name and a lyrics collection does not file it under:
	// "(2012 Remaster)", "[Live]", "(feat. Somebody)", " - Remastered 2011".
	reTitleNote = regexp.MustCompile(`(?i)\s*[\(\[][^\)\]]*\b(remaster(ed)?|version|mix|edit|mono|stereo|deluxe|bonus|feat\.?|ft\.?|featuring|with|live|single|explicit|clean|anniversary|edition|demo|acoustic|instrumental)\b[^\)\]]*[\)\]]`)
	reTitleTail = regexp.MustCompile(`(?i)\s+-\s+[^-]*\b(remaster(ed)?|version|mix|edit|mono|stereo|live|single|demo)\b[^-]*$`)
	// A name that says the song is the one without words.
	reWordless = regexp.MustCompile(`(?i)\b(instrumental|karaoke|off[ -]?vocal|backing track)\b|\binst\.`)
)

// saysWordless is whether a song's name says it has no words. Such a song is asked about
// under its own name only: its plain name is the sung version's, and that one's lyrics
// would be put on it.
func saysWordless(title string) bool {
	return reWordless.MatchString(title)
}

// plainTitle is a song's name without what a release added to it.
func plainTitle(title string) string {
	t := reTitleTail.ReplaceAllString(reTitleNote.ReplaceAllString(title, ""), "")
	if t = strings.TrimSpace(t); t == "" {
		return strings.TrimSpace(title)
	}
	return t
}

// ErrLyricsBusy: the service asked to be left alone for now (or cannot be reached).
var ErrLyricsBusy = errors.New("the lyrics service is not answering")

func (f *LyricsFinder) get(ctx context.Context, path string, q url.Values, out any) (found bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(f.BaseURL, "/")+path+"?"+q.Encode(), nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", f.Agent)
	req.Header.Set("Accept", "application/json")
	resp, err := f.HTTP.Do(req)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrLyricsBusy, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return false, nil
	case resp.StatusCode != http.StatusOK:
		return false, fmt.Errorf("%w: HTTP %d", ErrLyricsBusy, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxLyricsAnswer))
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrLyricsBusy, err)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return false, fmt.Errorf("%w: an answer that is not JSON", ErrLyricsBusy)
	}
	return true, nil
}

// Find looks a song up: first as the library names it (name, artist, album and length
// together), then by its plain name and artist, taking of what comes back the song of
// the same name and the same length, a timed one before a plain one (never one marked
// as having no words). A song whose own name says it has no words is asked about under
// that name only. The text is LRC when
// the lyrics are timed. found is false when the service has nothing for the song (or
// knows it as an instrumental); err means it could not be asked.
func (f *LyricsFinder) Find(ctx context.Context, q LyricsQuery) (text string, found bool, err error) {
	if strings.TrimSpace(q.Title) == "" || strings.TrimSpace(q.Artist) == "" {
		return "", false, nil
	}
	exact := url.Values{"track_name": {q.Title}, "artist_name": {q.Artist}}
	if q.Album != "" {
		exact.Set("album_name", q.Album)
	}
	if q.DurationSec > 0 {
		exact.Set("duration", strconv.Itoa(q.DurationSec))
	}
	var one lrclibSong
	if ok, err := f.get(ctx, "/api/get", exact, &one); err != nil {
		return "", false, err
	} else if ok {
		if t := one.text(); !one.Instrumental && LyricsKind(t, "online") != "" {
			return t, true, nil
		}
		// An entry with no words, or marked as having none: not the last word. Whoever
		// filed a song under its full release name ("Song (feat. Somebody)") sometimes
		// left it so, while the same recording under its plain name has its lyrics.
	}
	if saysWordless(q.Title) {
		return "", false, nil
	}
	title := plainTitle(q.Title)
	var many []lrclibSong
	if _, err := f.get(ctx, "/api/search", url.Values{"track_name": {title}, "artist_name": {q.Artist}}, &many); err != nil {
		return "", false, err
	}
	want := Fold(title)
	best, bestScore := -1, 0.0
	for i, s := range many {
		if s.Instrumental || saysWordless(s.TrackName) || Fold(plainTitle(s.TrackName)) != want || LyricsKind(s.text(), "online") == "" {
			continue
		}
		// The same recording is the same length, give or take the silence at its ends;
		// another version of the song would be sung at other times.
		off := 0.0
		if q.DurationSec > 0 && s.Duration > 0 {
			if off = s.Duration - float64(q.DurationSec); off < 0 {
				off = -off
			}
			if off > 3 {
				continue
			}
		}
		score := 10 - off
		if strings.TrimSpace(s.Synced) != "" {
			score += 100
		}
		if best < 0 || score > bestScore {
			best, bestScore = i, score
		}
	}
	if best < 0 {
		return "", false, nil
	}
	return many[best].text(), true, nil
}

// lyricsFailLimit: this many songs in a row that could not be asked about end a pass
// (the service is away, or has had enough of us for now): the rest waits for the next.
const lyricsFailLimit = 4

// FindLyrics asks about every song that has no lyrics of its own and was not asked about
// lately, one after the other, and keeps what it finds. It returns how many songs got
// lyrics. Nothing happens when looking online is not turned on. A call while another
// runs returns at once.
func (s *Service) FindLyrics(ctx context.Context) (int, error) {
	f := s.Lyrics
	if f == nil {
		return 0, nil
	}
	s.lyrMu.Lock()
	if s.finding {
		s.lyrMu.Unlock()
		return 0, nil
	}
	s.finding = true
	s.lyrMu.Unlock()
	defer func() {
		s.lyrMu.Lock()
		s.finding = false
		s.lyrMu.Unlock()
	}()

	list, err := s.store.MusicTracksWantingLyrics(ctx, time.Now().Add(-f.Again).Unix())
	if err != nil || len(list) == 0 {
		return 0, err
	}
	found, asked, failed := 0, 0, 0
	for i, t := range list {
		if ctx.Err() != nil {
			break
		}
		if i > 0 && f.Gap > 0 {
			select {
			case <-ctx.Done():
			case <-time.After(f.Gap):
			}
			if ctx.Err() != nil {
				break
			}
		}
		text, ok, err := f.Find(ctx, LyricsQuery{Title: t.Title, Artist: t.Artist, Album: t.Album, DurationSec: int((t.DurationMs + 500) / 1000)})
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			// Not asked: the song is not marked, and is asked about next time.
			if failed++; failed >= lyricsFailLimit {
				slog.Warn("music: lyrics left for later: the lyrics service is not answering", "err", err, "found", found, "asked", asked)
				return found, nil
			}
			continue
		}
		failed = 0
		asked++
		if !ok {
			text = ""
		} else {
			found++
		}
		if err := s.store.SetMusicOnlineLyrics(ctx, t.ID, text, time.Now().Unix()); err != nil {
			return found, err
		}
	}
	if asked > 0 {
		slog.Info("music: lyrics looked up", "songs", asked, "found", found)
	}
	return found, ctx.Err()
}
