package music

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// A release group's address in a lyrics file is not the song's lyrics. "Smooth Criminal"
// showed one line, "www.t.me/...", where its words should be: 45 songs of one library
// had such a file beside them.
func TestAnAddressIsNotLyrics(t *testing.T) {
	for _, junk := range []string{
		"www.t.me/somebody_music",
		"[00:00.00]www.t.me/somebody_music\n",
		"https://example.com/more",
		"\n  Downloaded from www.example.org  \n",
		"[ar:Somebody]\n[00:01.00] t.me/somebody\n[00:05.00]example.net\n",
		"",
		"[00:01.00]\n[00:09.00]\n", // time marks, nothing sung at them
		"...\n---\n",
	} {
		if l := ParseLyrics(junk, "lrc"); len(l.Lines) != 0 {
			t.Errorf("%q was taken for lyrics: %v", junk, l.Lines)
		}
		if LyricsKind(junk, "lrc") != "" {
			t.Errorf("%q counts as a song having lyrics", junk)
		}
	}
	// Real lyrics with such a line among them keep their words and lose the line.
	l := ParseLyrics("[00:01.00]First line\n[00:03.00]www.t.me/somebody\n[00:05.00]Second line\n", "lrc")
	if !l.Synced || len(l.Lines) != 2 || l.Lines[0].Text != "First line" || l.Lines[1].Text != "Second line" {
		t.Errorf("timed lyrics with an address in them: %+v", l)
	}
	p := ParseLyrics("One\nvisit www.example.com\nTwo\n", "txt")
	if len(p.Lines) != 2 || p.Lines[1].Text != "Two" {
		t.Errorf("plain lyrics with an address in them: %+v", p.Lines)
	}
	// Words that only look a little like an address are words.
	for _, sung := range []string{"I met her at the dot com party", "Mr. Brightside", "99 problems", "wait... what?", "R.I.P. to my youth", "me and you, to the end"} {
		if l := ParseLyrics(sung, "txt"); len(l.Lines) != 1 {
			t.Errorf("%q was left out", sung)
		}
	}
	if LyricsKind("[00:01.00]Hallo", "lrc") != "lrc" || lyricsOrNothing("www.t.me/x") != "" || lyricsOrNothing("La la") != "La la" {
		t.Error("LyricsKind / lyricsOrNothing")
	}
}

// The scan counts a lyrics file only when it holds lyrics: a song with nothing but an
// address beside it has no lyrics, the next kind of file is looked at, and a file that
// changes is read again.
func TestScanCountsOnlyLyricsThatAreLyrics(t *testing.T) {
	l := newTestLib(t)
	write := func(rel, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(l.root, filepath.FromSlash(rel)), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	l.put("A/junk.mp3", song{tags: tags("title", "Junk", "artist", "A")})
	l.put("A/real.mp3", song{tags: tags("title", "Real", "artist", "A")})
	l.put("A/second.mp3", song{tags: tags("title", "Second", "artist", "A")})
	l.put("A/tagged.mp3", song{tags: tags("title", "Tagged", "artist", "A", "lyrics", "www.t.me/somebody_music")})
	l.put("A/sung.mp3", song{tags: tags("title", "Sung", "artist", "A", "lyrics", "La la la\nLa la")})
	write("A/junk.lrc", "www.t.me/somebody_music")
	write("A/real.lrc", "[00:01.00]Hallo\n[00:02.00]wereld\n")
	write("A/second.lrc", "www.t.me/somebody_music") // nothing in the first kind of file...
	write("A/second.txt", "Words\nof the song\n")    // ...the second kind has the words
	l.scan(false)
	for title, want := range map[string]string{"Junk": "", "Real": "lrc", "Second": "txt", "Tagged": "", "Sung": "embedded"} {
		if got := l.track(title).Lyrics; got != want {
			t.Errorf("%s: lyrics %q, want %q", title, got, want)
		}
	}
	full, err := l.st.GetMusicTrack(l.ctx, l.scope, l.track("Tagged").ID)
	if err != nil || full == nil || full.LyricsText != "" {
		t.Errorf("an address in a lyrics tag was kept as the song's lyrics: %+v %v", full, err)
	}
	// The file is replaced by real lyrics (the audio untouched): the song has lyrics now.
	write("A/junk.lrc", "[00:01.00]Now there are words\n[00:04.00]and more of them here\n")
	l.scan(false)
	if got := l.track("Junk").Lyrics; got != "lrc" {
		t.Errorf("a lyrics file that got its lyrics: %q", got)
	}
	// And the other way round.
	write("A/real.lrc", "www.t.me/somebody_music, with more to it")
	l.scan(false)
	if got := l.track("Real").Lyrics; got != "" {
		t.Errorf("a lyrics file that lost its lyrics: %q", got)
	}
}

// lyricsService stands in for the lyrics collection.
type lyricsService struct {
	*httptest.Server
	mu    sync.Mutex
	asked []string // "get <title>|<artist>|<album>|<duration>" / "search <title>|<artist>"
	get   func(title string) (int, any)
	find  func(title string) (int, any)
	agent string
}

func newLyricsService(t *testing.T) *lyricsService {
	s := &lyricsService{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		s.mu.Lock()
		s.agent = r.Header.Get("User-Agent")
		var code int
		var body any
		switch r.URL.Path {
		case "/api/get":
			s.asked = append(s.asked, "get "+strings.Join([]string{q.Get("track_name"), q.Get("artist_name"), q.Get("album_name"), q.Get("duration")}, "|"))
			code, body = 404, map[string]any{"code": 404}
			if s.get != nil {
				code, body = s.get(q.Get("track_name"))
			}
		case "/api/search":
			s.asked = append(s.asked, "search "+q.Get("track_name")+"|"+q.Get("artist_name"))
			code, body = 200, []any{}
			if s.find != nil {
				code, body = s.find(q.Get("track_name"))
			}
		default:
			code, body = 500, "no such path"
		}
		s.mu.Unlock()
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *lyricsService) finder() *LyricsFinder {
	f := NewLyricsFinder()
	f.BaseURL, f.Gap = s.URL, 0
	return f
}

func (s *lyricsService) questions() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.asked...)
}

func TestFindLyricsOnline(t *testing.T) {
	ctx := context.Background()
	svc := newLyricsService(t)
	f := svc.finder()
	q := LyricsQuery{Title: "Smooth Criminal (2012 Remaster)", Artist: "Michael Jackson", Album: "Bad 25", DurationSec: 258}

	// Known as the library names it: the timed lyrics are taken.
	svc.get = func(string) (int, any) {
		return 200, map[string]any{"trackName": "Smooth Criminal", "duration": 258, "plainLyrics": "As he came into the window", "syncedLyrics": "[00:10.00]As he came into the window"}
	}
	text, ok, err := f.Find(ctx, q)
	if err != nil || !ok || text != "[00:10.00]As he came into the window" {
		t.Fatalf("an exact answer: %q %v %v", text, ok, err)
	}
	if got := svc.questions(); len(got) != 1 || got[0] != "get Smooth Criminal (2012 Remaster)|Michael Jackson|Bad 25|258" {
		t.Fatalf("asked: %v", got)
	}
	if !strings.Contains(svc.agent, "Roxy") {
		t.Errorf("the service was not told who asks: %q", svc.agent)
	}

	// Not known under that name: looked for by the song's plain name, and of what comes
	// back the one of the same name and length, timed before plain.
	svc.asked, svc.get = nil, nil
	svc.find = func(string) (int, any) {
		return 200, []map[string]any{
			{"trackName": "Smooth Criminal", "duration": 250, "syncedLyrics": "[00:01.00]the radio edit"}, // another cut: 8 s shorter
			{"trackName": "Smooth Criminal (Live)", "duration": 258, "plainLyrics": "also this name once the note is off"},
			{"trackName": "Smooth Criminal", "duration": 259.4, "plainLyrics": "plain, same length"},
			{"trackName": "Smooth Criminal", "duration": 257, "syncedLyrics": "[00:10.00]timed, same length"},
			{"trackName": "Another Part of Me", "duration": 258, "syncedLyrics": "[00:10.00]another song"},
			{"trackName": "Smooth Criminal", "duration": 258, "instrumental": true},
			{"trackName": "Smooth Criminal", "duration": 258, "plainLyrics": "www.t.me/somebody"},
		}
	}
	text, ok, err = f.Find(ctx, q)
	if err != nil || !ok || text != "[00:10.00]timed, same length" {
		t.Fatalf("picked %q %v %v", text, ok, err)
	}
	if got := svc.questions(); len(got) != 2 || got[1] != "search Smooth Criminal|Michael Jackson" {
		t.Fatalf("asked: %v", got)
	}

	// Nothing of that name and length: not found, and that is not an error.
	svc.find = func(string) (int, any) {
		return 200, []map[string]any{{"trackName": "Smooth Criminal", "duration": 300, "plainLyrics": "a longer version"}}
	}
	if text, ok, err := f.Find(ctx, q); err != nil || ok || text != "" {
		t.Fatalf("no match: %q %v %v", text, ok, err)
	}
	// Known as an instrumental: nothing to show.
	svc.get = func(string) (int, any) { return 200, map[string]any{"trackName": "x", "instrumental": true} }
	if _, ok, err := f.Find(ctx, q); err != nil || ok {
		t.Fatalf("an instrumental: %v %v", ok, err)
	}
	// The service refuses, or is away: an error, so that the song is asked about again.
	svc.get = func(string) (int, any) { return 429, "slow down" }
	if _, _, err := f.Find(ctx, q); !errors.Is(err, ErrLyricsBusy) {
		t.Fatalf("a refusal: %v", err)
	}
	svc.Close()
	if _, _, err := f.Find(ctx, q); !errors.Is(err, ErrLyricsBusy) {
		t.Fatalf("no service: %v", err)
	}
	// A song without a name or an artist is not asked about.
	if _, ok, err := f.Find(ctx, LyricsQuery{Title: "Untitled"}); ok || err != nil {
		t.Fatal("asked about a song with no artist")
	}
}

// A song's entry under its full release name is sometimes marked as having no words by
// whoever filed it, while the same recording under its plain name has them. The mark
// alone does not end the look; a name that itself says the song has no words does.
func TestAWordlessMarkIsNotTheLastWord(t *testing.T) {
	ctx := context.Background()
	svc := newLyricsService(t)
	f := svc.finder()
	sung := func(string) (int, any) {
		return 200, []map[string]any{
			// Filed with words by mistake, under the name of the version without: not taken for the song.
			{"trackName": "Airplanes (Instrumental)", "duration": 180, "syncedLyrics": "[00:01.00]not this one"},
			{"trackName": "Airplanes", "duration": 180.48, "syncedLyrics": "[00:09.00]Can we pretend that airplanes"},
			{"trackName": "Airplanes", "duration": 180, "instrumental": true},
		}
	}

	q := LyricsQuery{Title: "Airplanes (feat. Somebody of Some Band)", Artist: "B.o.B", Album: "An Album", DurationSec: 180}
	svc.get = func(string) (int, any) {
		return 200, map[string]any{"trackName": q.Title, "duration": 181, "instrumental": true}
	}
	svc.find = sung
	text, ok, err := f.Find(ctx, q)
	if err != nil || !ok || text != "[00:09.00]Can we pretend that airplanes" {
		t.Fatalf("marked wordless under its release name, sung under its plain one: %q %v %v", text, ok, err)
	}
	if got := svc.questions(); len(got) != 2 || got[1] != "search Airplanes|B.o.B" {
		t.Fatalf("asked: %v", got)
	}

	// An entry that carries no words at all (and no mark) is no answer either.
	svc.asked = nil
	svc.get = func(string) (int, any) { return 200, map[string]any{"trackName": q.Title, "duration": 181} }
	if text, ok, err := f.Find(ctx, q); err != nil || !ok || text == "" {
		t.Fatalf("an empty entry under the release name: %q %v %v", text, ok, err)
	}

	// A song named as the one without words is never given the sung version's lyrics,
	// which is what its plain name would find: only its own entry is asked for.
	for _, title := range []string{"Airplanes (Instrumental)", "Airplanes - Instrumental", "Airplanes (Off Vocal)", "Airplanes [Karaoke Version]", "Airplanes (Inst.)"} {
		svc.asked = nil
		svc.get = func(string) (int, any) { return 404, nil }
		svc.find = sung
		if text, ok, err := f.Find(ctx, LyricsQuery{Title: title, Artist: "B.o.B", DurationSec: 180}); err != nil || ok || text != "" {
			t.Errorf("%q was given lyrics: %q %v %v", title, text, ok, err)
		}
		if got := svc.questions(); len(got) != 1 || !strings.HasPrefix(got[0], "get ") {
			t.Errorf("%q: asked %v", title, got)
		}
	}
	// A word that only looks like it is not that: "Instant Crush" is sung.
	svc.asked = nil
	svc.get = func(string) (int, any) { return 404, nil }
	svc.find = func(string) (int, any) {
		return 200, []map[string]any{{"trackName": "Instant Crush", "duration": 337, "plainLyrics": "I didn't want to be the one to forget"}}
	}
	if _, ok, err := f.Find(ctx, LyricsQuery{Title: "Instant Crush (feat. Somebody)", Artist: "Daft Punk", DurationSec: 337}); err != nil || !ok {
		t.Errorf("a sung song with \"Inst\" in its name: %v %v", ok, err)
	}
}

func TestPlainTitle(t *testing.T) {
	for in, want := range map[string]string{
		"Smooth Criminal (2012 Remaster)":          "Smooth Criminal",
		"Come Together - Remastered 2009":          "Come Together",
		"Song (feat. Somebody) [Radio Edit]":       "Song",
		"Across The Universe (2021 Mix)":           "Across The Universe",
		"Plain":                                    "Plain",
		"(Everything I Do) I Do It for You":        "(Everything I Do) I Do It for You",
		"Song - Live at Somewhere":                 "Song",
		"Up - Down":                                "Up - Down",
		"(Live)":                                   "(Live)",
		"Hello (Is It Me You're Looking For?)":     "Hello (Is It Me You're Looking For?)",
		"Song (Taylor's Version) (From The Vault)": "Song (From The Vault)",
	} {
		if got := plainTitle(in); got != want {
			t.Errorf("plainTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

// The pass asks about the songs that have no lyrics of their own, once, and keeps what it
// finds; songs with their own are left out of it; a service that is away costs no song
// its turn; and none of it happens unless it was turned on.
func TestLyricsPass(t *testing.T) {
	l := newTestLib(t)
	l.put("A/own.mp3", song{tags: tags("title", "Own", "artist", "A")})
	l.put("A/found.mp3", song{tags: tags("title", "Found (2012 Remaster)", "artist", "A", "album", "Al"), seconds: 200})
	l.put("A/none.mp3", song{tags: tags("title", "None", "artist", "A")})
	l.put("A/junk.mp3", song{tags: tags("title", "Junk", "artist", "A")})
	os.WriteFile(filepath.Join(l.root, "A", "own.lrc"), []byte("[00:01.00]My own words"), 0o644)
	os.WriteFile(filepath.Join(l.root, "A", "junk.lrc"), []byte("www.t.me/somebody_music"), 0o644)
	svc := newLyricsService(t)
	svc.find = func(title string) (int, any) {
		switch title {
		case "Found":
			return 200, []map[string]any{{"trackName": "Found", "duration": 200, "syncedLyrics": "[00:02.00]Found online"}}
		case "Junk":
			return 200, []map[string]any{{"trackName": "Junk", "plainLyrics": "The real words\nof the song"}}
		}
		return 200, []any{}
	}
	// Not turned on: nobody is asked, at a scan or by hand.
	l.scan(false)
	if n, err := l.svc.FindLyrics(l.ctx); n != 0 || err != nil || len(svc.questions()) != 0 {
		t.Fatalf("asked without being turned on: %d %v %v", n, err, svc.questions())
	}
	l.svc.Lyrics = svc.finder()
	n, err := l.svc.FindLyrics(l.ctx)
	if err != nil || n != 2 {
		t.Fatalf("found %d, %v", n, err)
	}
	asked := strings.Join(svc.questions(), "\n")
	if strings.Contains(asked, "Own") {
		t.Errorf("a song with lyrics of its own was asked about:\n%s", asked)
	}
	for title, want := range map[string]string{"Own": "lrc", "Found (2012 Remaster)": "online", "None": "", "Junk": "online"} {
		if got := l.track(title).Lyrics; got != want {
			t.Errorf("%s: lyrics %q, want %q", title, got, want)
		}
	}
	full, _ := l.st.GetMusicTrack(l.ctx, l.scope, l.track("Found (2012 Remaster)").ID)
	if full == nil || full.LyricsOnline != "[00:02.00]Found online" {
		t.Fatalf("what was found was not kept: %+v", full)
	}
	// Asked once: a second pass has nobody to ask about, found or not.
	before := len(svc.questions())
	if n, _ := l.svc.FindLyrics(l.ctx); n != 0 || len(svc.questions()) != before {
		t.Errorf("asked again: %v", svc.questions()[before:])
	}
	// ...until a song nothing was found for has been left alone long enough.
	l.svc.Lyrics.Again = -time.Hour
	if _, err := l.svc.FindLyrics(l.ctx); err != nil {
		t.Fatal(err)
	}
	if again := strings.Join(svc.questions()[before:], "\n"); !strings.Contains(again, "None") || strings.Contains(again, "Found") {
		t.Errorf("after the wait, asked about:\n%s", again)
	}
	// A song that gets lyrics of its own shows those, whatever was found online.
	os.WriteFile(filepath.Join(l.root, "A", "found.lrc"), []byte("[00:01.00]From the file beside it"), 0o644)
	l.scan(false)
	if got := l.track("Found (2012 Remaster)").Lyrics; got != "lrc" {
		t.Errorf("a song with both: %q", got)
	}
}

// The service away: the songs are not marked as asked, and the pass gives up after a few.
func TestLyricsPassLeavesSongsForLaterWhenTheServiceIsAway(t *testing.T) {
	l := newTestLib(t)
	for i := 0; i < 12; i++ {
		l.put("A/s"+string(rune('a'+i))+".mp3", song{tags: tags("title", "Song "+string(rune('A'+i)), "artist", "A")})
	}
	l.scan(false)
	svc := newLyricsService(t)
	svc.get = func(string) (int, any) { return 503, "away" }
	l.svc.Lyrics = svc.finder()
	if n, err := l.svc.FindLyrics(l.ctx); n != 0 || err != nil {
		t.Fatalf("with the service away: %d %v", n, err)
	}
	if got := len(svc.questions()); got != lyricsFailLimit {
		t.Errorf("asked %d times before giving up, want %d", got, lyricsFailLimit)
	}
	// Back: every song is asked about (none was marked as asked while it was away).
	svc.asked, svc.get = nil, nil
	svc.find = func(title string) (int, any) {
		return 200, []map[string]any{{"trackName": title, "plainLyrics": "Words of " + title}}
	}
	if n, err := l.svc.FindLyrics(l.ctx); n != 12 || err != nil {
		t.Fatalf("with the service back: found %d of 12, %v", n, err)
	}
}
