package api

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/roxy-media/roxy/server/internal/metadata"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

func voices(code, name string, firstStaff int, names ...string) metadata.VoiceLanguage {
	l := metadata.VoiceLanguage{Code: code, Name: name}
	for i, n := range names {
		l.Members = append(l.Members, metadata.VoiceActor{StaffID: firstStaff + i, Name: n, Character: "Character " + string(rune('A'+i)), Photo: "https://img.example/" + n})
	}
	return l
}

// bunnyCast: a Japanese original with English, German and French dubs.
func bunnyCast() *metadata.VoiceCast {
	return &metadata.VoiceCast{AniListID: 7, Languages: []metadata.VoiceLanguage{
		voices("jpn", "Japanese", 10, "Kaito Ishikawa", "Asami Seto", "Nao Touyama"),
		voices("deu", "German", 40, "Eins", "Zwei", "Drei"),
		voices("eng", "English", 20, "Stephen Fu", "Erica Mendez", "Kayli Mills"),
		voices("fra", "French", 30, "Un", "Deux", "Trois"),
	}}
}

// voiceServer is a server with a store and stand-ins for AniList and TMDB's person
// search; reads and searched say what was asked of them.
type voiceServer struct {
	*Server
	mu        sync.Mutex
	reads     int
	searched  []string
	cast      *metadata.VoiceCast
	readErr   error
	people    map[string]int
	personErr error
}

func newVoiceServer(t *testing.T) *voiceServer {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	v := &voiceServer{cast: bunnyCast(), people: map[string]int{"Stephen Fu": 520, "Erica Mendez": 521}}
	v.Server = &Server{store: st, voice: &voiceCasts{
		wait: 5 * time.Second,
		read: func(ctx context.Context, anilistID int, title string, year int, movie bool) (*metadata.VoiceCast, error) {
			v.mu.Lock()
			defer v.mu.Unlock()
			v.reads++
			return v.cast, v.readErr
		},
		person: func(ctx context.Context, name string) (int, error) {
			v.mu.Lock()
			defer v.mu.Unlock()
			v.searched = append(v.searched, name)
			return v.people[name], v.personErr
		},
	}}
	return v
}

// allCasts: bunnyCast as a page gets it.
const allCasts = "jpn,eng,fra,deu"

// settle waits for the read behind an answer (the other languages' people).
func (v *voiceServer) settle(ask castAsk) {
	v.voice.mu.Lock()
	job := v.voice.jobs[ask.key()]
	v.voice.mu.Unlock()
	if job != nil {
		<-job.done
	}
}

func (v *voiceServer) searches() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return len(v.searched)
}

func groupNames(gs []castGroup) string {
	var out []string
	for _, g := range gs {
		out = append(out, g.Language)
	}
	return strings.Join(out, ",")
}

// The point of it all: someone who watches dubs sees the dub's cast for a title they
// have dubbed, the original's for one they only have subtitled, and can switch.
func TestCastsDefaultFollowsTheLibrarysAudio(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name  string
		audio []string
		want  string
		first string
	}{
		{"dubbed copy", []string{"jpn", "eng"}, "", "eng"},
		{"dubbed copy, two-letter codes", []string{"ja", "en"}, "eng", "eng"},
		{"subtitled copy", []string{"jpn"}, "eng", "jpn"},
		{"not in the library", nil, "eng", "eng"},
		{"viewer listens in Japanese", []string{"jpn", "eng"}, "jpn", "jpn"},
		{"viewer listens in German, copy has it", []string{"jpn", "ger"}, "deu", "deu"},
		{"viewer listens in German, copy has not", []string{"jpn", "eng"}, "deu", "jpn"},
		{"a language nobody dubbed it in", []string{"jpn"}, "kor", "jpn"},
	}
	for i, c := range cases {
		v := newVoiceServer(t)
		groups, first := v.casts(ctx, castAsk{tmdbID: 100 + i, title: "Bunny Girl", audio: c.audio, want: c.want})
		// Every cast is offered whoever asks: the original first, the dubs by name.
		if groupNames(groups) != allCasts || first != c.first {
			t.Errorf("%s: casts %s first %q; want %s first %q", c.name, groupNames(groups), first, allCasts, c.first)
		}
		for j, g := range groups {
			if g.Original != (j == 0) {
				t.Errorf("%s: only the first cast is the original's: %+v", c.name, groups)
			}
		}
	}
}

// With one cast there is nothing to choose: the answer carries none, and the page
// keeps the TMDB cast it always had. A dub with a voice or two listed is not a cast.
func TestCastsNeedTwoLanguagesWorthShowing(t *testing.T) {
	ctx := context.Background()
	v := newVoiceServer(t)
	v.cast = &metadata.VoiceCast{Languages: []metadata.VoiceLanguage{voices("jpn", "Japanese", 10, "A", "B", "C")}}
	if g, first := v.casts(ctx, castAsk{tmdbID: 1, title: "x"}); g != nil || first != "" {
		t.Errorf("original only: %+v %q, want none", g, first)
	}
	v = newVoiceServer(t)
	v.cast = &metadata.VoiceCast{Languages: []metadata.VoiceLanguage{voices("jpn", "Japanese", 10, "A", "B", "C"), voices("eng", "English", 20, "D", "E")}}
	if g, _ := v.casts(ctx, castAsk{tmdbID: 1, title: "x"}); g != nil {
		t.Errorf("a two-voice dub was offered as a cast: %+v", g)
	}
	v.settle(castAsk{tmdbID: 1})
	if v.searches() != 0 {
		t.Errorf("nothing is shown, yet TMDB was searched for %v", v.searched)
	}
	v = newVoiceServer(t)
	v.cast = nil // AniList does not know the title
	if g, _ := v.casts(ctx, castAsk{tmdbID: 1, title: "x"}); g != nil {
		t.Errorf("unknown title: %+v, want none", g)
	}
	if g, _ := (&Server{}).casts(ctx, castAsk{tmdbID: 1}); g != nil {
		t.Errorf("no AniList: %+v, want none", g)
	}
}

// People open their page only when TMDB has them. The original cast is matched with
// the title's own credits (by name, or by the character they voice where the sites
// spell the actor apart); a dub's by a search for the exact name; nobody found = shown,
// not opened.
func TestCastsLinkPeopleToTMDB(t *testing.T) {
	ctx := context.Background()
	v := newVoiceServer(t)
	ask := castAsk{tmdbID: 1, title: "Bunny Girl", audio: []string{"jpn", "eng"},
		byName: map[string]int{foldName("Kaito Ishikawa"): 910, foldName("Nao Toyama"): 912},
		byRole: map[string]int{foldName("Character B (voice)"): 911, foldName("Character C"): 912},
	}
	groups, _ := v.casts(ctx, ask)
	if groupNames(groups) != allCasts {
		t.Fatalf("casts = %+v", groups)
	}
	ids := func(g castGroup) []int {
		var out []int
		for _, m := range g.Members {
			out = append(out, m.TMDBID)
		}
		return out
	}
	if got := ids(groups[0]); got[0] != 910 || got[1] != 911 || got[2] != 912 {
		t.Errorf("original cast linked to %v, want 910 (by name), 911 and 912 (by character)", got)
	}
	if got := ids(groups[1]); got[0] != 520 || got[1] != 521 || got[2] != 0 {
		t.Errorf("dub cast linked to %v, want 520, 521 and 0 (TMDB has no Kayli Mills here)", got)
	}
	if m := groups[1].Members[0]; m.Name != "Stephen Fu" || m.Character != "Character A" || m.Photo == "" {
		t.Errorf("dub member = %+v", m)
	}
	// A dub's voice is never linked through the character: that credit is the original's.
	v.settle(ask)
	if v.searches() != 9 {
		t.Errorf("searched TMDB for %v, want the nine dub voices only", v.searched)
	}
}

// What was read is kept: a second look asks neither AniList nor TMDB again, also for
// the people TMDB did not have, whatever language the next viewer listens in.
func TestCastsAreReadOnce(t *testing.T) {
	ctx := context.Background()
	v := newVoiceServer(t)
	ask := castAsk{tmdbID: 1, title: "Bunny Girl"}
	first, _ := v.casts(ctx, ask)
	v.settle(ask)
	again, _ := v.casts(ctx, ask)
	v.settle(ask)
	if v.reads != 1 || v.searches() != 12 {
		t.Fatalf("after two looks: %d reads, %d searches (%v); want 1 and 12", v.reads, v.searches(), v.searched)
	}
	if groupNames(first) != allCasts || groupNames(again) != allCasts || again[1].Members[0].TMDBID != 520 {
		t.Fatalf("second look = %+v", again)
	}
	german, first2 := v.casts(ctx, castAsk{tmdbID: 1, title: "Bunny Girl", want: "deu"})
	v.settle(ask)
	if groupNames(german) != allCasts || first2 != "deu" || v.reads != 1 || v.searches() != 12 {
		t.Fatalf("a German listener later: %s first %q, %d reads, %d searches; want all four, deu, 1, 12", groupNames(german), first2, v.reads, v.searches())
	}
}

// The page waits for the casts it is likely to show, not for every dub's people: those
// are looked up behind the answer (shown meanwhile, without a page to open), and the
// next open has them linked.
func TestOtherLanguagesAreLinkedBehindTheAnswer(t *testing.T) {
	ctx := context.Background()
	v := newVoiceServer(t)
	v.people["Eins"] = 540
	release := make(chan struct{})
	inner := v.voice.person
	v.voice.person = func(ctx context.Context, name string) (int, error) {
		if name == "Eins" || name == "Un" {
			<-release // German and French are slow to look up
		}
		return inner(ctx, name)
	}
	ask := castAsk{tmdbID: 1, title: "Bunny Girl", audio: []string{"jpn", "eng"}}
	answered := make(chan []castGroup, 1)
	go func() { g, _ := v.casts(ctx, ask); answered <- g }()
	var groups []castGroup
	select {
	case groups = <-answered:
	case <-time.After(3 * time.Second):
		close(release)
		t.Fatal("the page waited for the people of languages it does not show first")
	}
	if groupNames(groups) != allCasts || groups[1].Members[0].TMDBID != 520 || groups[3].Members[0].TMDBID != 0 {
		t.Fatalf("first answer = %+v; want all four casts, English linked, German not yet", groups)
	}
	close(release)
	v.settle(ask)
	groups, _ = v.casts(ctx, ask)
	if groups[3].Language != "deu" || groups[3].Members[0].TMDBID != 540 {
		t.Fatalf("next open: German cast = %+v, want Eins linked to 540", groups[3])
	}
	// A viewer who listens in German has the page wait for the German cast.
	v = newVoiceServer(t)
	v.people["Eins"] = 540
	groups, first := v.casts(ctx, castAsk{tmdbID: 2, title: "Bunny Girl", want: "deu"})
	if first != "deu" || groups[3].Members[0].TMDBID != 540 {
		t.Fatalf("German listener's first answer: first %q, German cast %+v", first, groups[3])
	}
}

// TMDB could not be asked about a language the page does not wait for: a later open
// is answered at once with what there is, and looks those people up behind it.
func TestOtherLanguagesAreTriedAgain(t *testing.T) {
	ctx := context.Background()
	v := newVoiceServer(t)
	v.people["Eins"] = 540
	away := true
	inner := v.voice.person
	v.voice.person = func(ctx context.Context, name string) (int, error) {
		v.mu.Lock()
		gone := away && name == "Eins"
		v.mu.Unlock()
		if gone {
			return 0, errors.New("tmdb is away")
		}
		return inner(ctx, name)
	}
	ask := castAsk{tmdbID: 1, title: "Bunny Girl"}
	v.casts(ctx, ask)
	v.settle(ask)
	v.mu.Lock()
	away = false
	v.mu.Unlock()
	v.voice.mu.Lock()
	v.voice.failed[ask.key()] = time.Now().Add(-voiceCastRetry - time.Second)
	v.voice.mu.Unlock()
	if g, _ := v.casts(ctx, ask); g[1].Members[0].TMDBID != 520 {
		t.Fatalf("English cast = %+v", g[1])
	}
	v.settle(ask)
	if g, _ := v.casts(ctx, ask); g[3].Members[0].TMDBID != 540 || v.reads != 1 {
		t.Fatalf("German cast = %+v after %d reads; want Eins linked, 1 read", g[3], v.reads)
	}
}

// AniList away: no casts (the TMDB cast shows), and it is not asked again at every
// open of the page, only after a while.
func TestCastsWhenAniListCannotBeAsked(t *testing.T) {
	ctx := context.Background()
	v := newVoiceServer(t)
	v.readErr = errors.New("anilist is away")
	ask := castAsk{tmdbID: 1, title: "Bunny Girl"}
	for i := 0; i < 3; i++ {
		if g, _ := v.casts(ctx, ask); g != nil {
			t.Fatalf("casts = %+v, want none", g)
		}
	}
	if v.reads != 1 {
		t.Fatalf("AniList asked %d times in a row, want once", v.reads)
	}
	v.readErr = nil
	v.voice.mu.Lock()
	v.voice.failed[ask.key()] = time.Now().Add(-voiceCastRetry - time.Second)
	v.voice.mu.Unlock()
	if g, _ := v.casts(ctx, ask); groupNames(g) != allCasts {
		t.Fatalf("after the wait: %s, want %s", groupNames(g), allCasts)
	}
}

// TMDB away while people are looked up: the cast is shown without links, and those
// people are asked about again later instead of staying unlinked for a week.
func TestCastsWhenTMDBCannotBeAsked(t *testing.T) {
	ctx := context.Background()
	v := newVoiceServer(t)
	v.personErr = errors.New("tmdb is away")
	ask := castAsk{tmdbID: 1, title: "Bunny Girl"}
	g, _ := v.casts(ctx, ask)
	if groupNames(g) != allCasts || g[1].Members[0].TMDBID != 0 {
		t.Fatalf("casts = %+v, want the casts, unlinked", g)
	}
	v.settle(ask)
	v.personErr = nil
	v.voice.mu.Lock()
	v.voice.failed[ask.key()] = time.Now().Add(-voiceCastRetry - time.Second)
	v.voice.mu.Unlock()
	g, _ = v.casts(ctx, ask)
	v.settle(ask)
	if g[1].Members[0].TMDBID != 520 || v.reads != 1 {
		t.Fatalf("later: Stephen Fu linked to %d after %d reads; want 520 after 1", g[1].Members[0].TMDBID, v.reads)
	}
}

// A slow first read does not hold the page: the answer goes without casts, the read
// finishes behind it, and the next open has them.
func TestCastsDoNotHoldThePage(t *testing.T) {
	ctx := context.Background()
	v := newVoiceServer(t)
	release := make(chan struct{})
	inner := v.voice.read
	v.voice.read = func(ctx context.Context, id int, title string, year int, movie bool) (*metadata.VoiceCast, error) {
		<-release
		return inner(ctx, id, title, year, movie)
	}
	v.voice.wait = 20 * time.Millisecond
	ask := castAsk{tmdbID: 1, title: "Bunny Girl"}
	start := time.Now()
	if g, _ := v.casts(ctx, ask); g != nil {
		t.Fatalf("casts = %+v before AniList answered", g)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("the page waited %v", time.Since(start))
	}
	job := v.readVoiceCast(ask) // joins the read that is running
	close(release)
	<-job.done
	v.voice.wait = 5 * time.Second
	if g, _ := v.casts(ctx, ask); groupNames(g) != allCasts || v.reads != 1 {
		t.Fatalf("next open: %s after %d reads; want all four after 1", groupNames(g), v.reads)
	}
}

func TestPickPersonWantsTheExactName(t *testing.T) {
	type res struct {
		id        int
		name, dep string
	}
	pick := func(name string, rs ...res) int {
		return pickPerson(name, rs, func(i int) (int, string, string) { return rs[i].id, rs[i].name, rs[i].dep })
	}
	if got := pick("Erica Mendez", res{1, "Erica Mendes", "Acting"}, res{2, "Érica  MENDEZ", "Acting"}); got != 2 {
		t.Errorf("exact name aside from accents and case: got %d, want 2", got)
	}
	if got := pick("John Smith", res{1, "John Smith", "Directing"}, res{2, "John Smith", "Acting"}); got != 2 {
		t.Errorf("an actor before a namesake: got %d, want 2", got)
	}
	if got := pick("John Smith", res{1, "John Smith", "Directing"}); got != 1 {
		t.Errorf("the only one by that name: got %d, want 1", got)
	}
	if got := pick("John Smith", res{1, "John Smithson", "Acting"}, res{2, "Johnny Smith", "Acting"}); got != 0 {
		t.Errorf("a near match is no match: got %d, want 0", got)
	}
}

func TestFoldNameAndLangKey(t *testing.T) {
	for in, want := range map[string]string{
		"Kaito  Ishikawa": "kaito ishikawa", "Sakuta Azusagawa (voice)": "sakuta azusagawa", "Yūki Ono": "yuki ono",
		"Jean-Luc O'Neil": "jean luc o neil", " (voice)": "", "Érica MENDEZ": "erica mendez",
	} {
		if got := foldName(in); got != want {
			t.Errorf("foldName(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"en": "eng", "ENG": "eng", "ger": "deu", "de": "deu", "fre": "fra", "ja": "jpn", "pt-BR": "por", "jpn": "jpn", "": "", "xx": "xx"} {
		if got := langKey(in); got != want {
			t.Errorf("langKey(%q) = %q, want %q", in, got, want)
		}
	}
}

// A library title asks with what its own records say: the AniList entry it was matched
// to, the audio of its files, the viewer's language. Titles that are not anime do not
// ask at all.
func TestItemCastAsk(t *testing.T) {
	year := 2018
	it := &model.Item{Type: model.TypeSeries, Title: "Bunny Girl", Year: &year, ProviderIDs: map[string]string{"anilist": "101291", "tmdb": "5"}}
	x := &tmdbExtras{byName: map[string]int{"a": 1}, byRole: map[string]int{"b": 2}}
	ask, ok := itemCastAsk(it, 5, &store.MediaFacts{Audio: []string{"jpn", "eng"}}, x, "eng")
	if !ok || ask.anilistID != 101291 || ask.tmdbID != 5 || ask.year != 2018 || ask.movie || ask.want != "eng" ||
		strings.Join(ask.audio, ",") != "jpn,eng" || ask.byName["a"] != 1 || ask.byRole["b"] != 2 || ask.key() != "voicecast:tv:5" {
		t.Errorf("series ask = %+v, %v", ask, ok)
	}
	film := &model.Item{Type: model.TypeMovie, Title: "A Film", IsAnime: true}
	if ask, ok := itemCastAsk(film, 9, nil, nil, ""); !ok || !ask.movie || ask.anilistID != 0 || ask.audio != nil || ask.key() != "voicecast:movie:9" {
		t.Errorf("anime film without an AniList match = %+v, %v", ask, ok)
	}
	if _, ok := itemCastAsk(&model.Item{Type: model.TypeSeries, Title: "Not Anime"}, 3, nil, nil, "eng"); ok {
		t.Error("a title that is not anime asked for voice casts")
	}
}

// A title with one cast shows none, so its page must not start a read at every open
// looking for people nobody will see.
func TestNeedsRead(t *testing.T) {
	ask := castAsk{tmdbID: 1}
	one := &savedVoiceCast{At: time.Now(), Read: voiceCastReading, Cast: &metadata.VoiceCast{Languages: []metadata.VoiceLanguage{voices("jpn", "Japanese", 10, "A", "B", "C")}}}
	two := &savedVoiceCast{At: time.Now(), Read: voiceCastReading, Cast: bunnyCast()}
	old := &savedVoiceCast{At: time.Now().Add(-voiceCastTTL - time.Hour), Read: voiceCastReading, Cast: bunnyCast()}
	older := &savedVoiceCast{At: time.Now(), Read: voiceCastReading - 1, Cast: bunnyCast()}
	unknown := &savedVoiceCast{At: time.Now(), Read: voiceCastReading}
	for name, c := range map[string]struct {
		have  bool
		saved *savedVoiceCast
		want  bool
	}{
		"nothing saved": {false, &savedVoiceCast{}, true}, "one cast": {true, one, false}, "AniList does not know it": {true, unknown, false},
		"two casts, people not looked for": {true, two, true}, "a week old": {true, old, true}, "an older reading": {true, older, true},
	} {
		if got := needsRead(c.have, c.saved, ask); got != c.want {
			t.Errorf("%s: needsRead = %v, want %v", name, got, c.want)
		}
	}
}
