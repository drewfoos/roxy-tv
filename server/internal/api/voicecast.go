package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/roxy-media/roxy/server/internal/metadata"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

// An anime's cast per language. TMDB credits the original voices only, so someone who
// watches the dub saw a cast they never hear. AniList lists the dubs: the details
// answers carry a cast per language ("casts") and which one to show first
// ("castLanguage"). The TMDB cast ("tmdb.cast") stays as it was, for older apps and
// for everything AniList does not cover.

// castGroup is one language's cast.
type castGroup struct {
	Language string       `json:"language"` // ISO 639-2/T: "jpn", "eng"
	Name     string       `json:"name"`     // "English"
	Original bool         `json:"original,omitempty"`
	Members  []castMember `json:"members"`
}

const (
	// voiceCastReading: raise when what is read from AniList, or how people are
	// matched, changes; saved casts of an older reading are read again.
	voiceCastReading = 1
	voiceCastTTL     = 7 * 24 * time.Hour
	voiceCastRetry   = 5 * time.Minute  // after AniList or TMDB could not be asked
	voiceCastWait    = 3 * time.Second  // how long a details answer waits for a first read
	voiceCastLimit   = 90 * time.Second // a read goes on after its page was answered
	voiceCastMinimum = 3                // a "cast" of fewer voices is not offered
)

// savedVoiceCast is what is kept per title (setting "voicecast:<kind>:<tmdb id>").
type savedVoiceCast struct {
	At   time.Time           `json:"at"`
	Read int                 `json:"read"`
	Cast *metadata.VoiceCast `json:"cast,omitempty"` // nil: AniList does not know the title
	// People: AniList staff id -> TMDB person id, 0 when TMDB has nobody by that name.
	// Someone not asked about yet is absent.
	People map[int]int `json:"people,omitempty"`
}

func (c *savedVoiceCast) fresh() bool {
	return time.Since(c.At) < voiceCastTTL && c.Read == voiceCastReading
}

// voiceCasts reads and keeps them. read and person are fields so tests can stand in
// for AniList and TMDB.
type voiceCasts struct {
	read   func(ctx context.Context, anilistID int, title string, year int, movie bool) (*metadata.VoiceCast, error)
	person func(ctx context.Context, name string) (int, error) // TMDB person id by exact name, 0 = nobody
	wait   time.Duration

	mu     sync.Mutex
	jobs   map[string]*voiceJob
	failed map[string]time.Time
}

// castAsk is one details page asking for its casts.
type castAsk struct {
	movie     bool
	tmdbID    int
	anilistID int // 0: found by title and year
	title     string
	year      int
	// audio: the audio languages of the copy in the library; empty when the title is
	// not in the library or its files do not say.
	audio []string
	// want: the viewer's audio language ("" = English).
	want string
	// credits: the title's own TMDB credits, by folded actor name and by folded
	// character name. The original cast is matched here first: no search needed, and
	// a character's voice is found even where the two sites spell the actor apart.
	byName, byRole map[string]int
}

func (a castAsk) key() string {
	kind := "tv"
	if a.movie {
		kind = "movie"
	}
	return fmt.Sprintf("voicecast:%s:%d", kind, a.tmdbID)
}

// casts answers a details page: every cast AniList has for the title, and the one to
// show first. Nothing when the title has fewer than two casts worth showing (the TMDB
// cast is then all there is). A first read is waited for briefly and otherwise finishes
// behind the answer, for the next time the page is opened. The page waits only for the
// casts it is likely to show (castPriority); the other languages' people are looked up
// behind the answer, and until then they are shown without a page to open.
func (s *Server) casts(ctx context.Context, ask castAsk) ([]castGroup, string) {
	v := s.voice
	if v == nil || v.read == nil || ask.tmdbID <= 0 {
		return nil, ""
	}
	var saved savedVoiceCast
	have, _ := s.store.GetSetting(ctx, ask.key(), &saved)
	if needsRead(have, &saved, ask) {
		wait := v.wait
		if wait == 0 {
			wait = voiceCastWait
		}
		select {
		case <-s.readVoiceCast(ask).ready:
		case <-time.After(wait):
		case <-ctx.Done():
		}
		saved = savedVoiceCast{}
		have, _ = s.store.GetSetting(ctx, ask.key(), &saved)
	} else if langs := castLanguages(saved.Cast); len(langs) >= 2 && !linked(&saved, langs) {
		s.readVoiceCast(ask) // the other languages' people, behind this answer
	}
	if !have || saved.Cast == nil {
		return nil, ""
	}
	langs := castLanguages(saved.Cast)
	if len(langs) < 2 {
		return nil, ""
	}
	groups := make([]castGroup, 0, len(langs))
	for i, l := range langs {
		g := castGroup{Language: l.Code, Name: l.Name, Original: i == 0}
		for _, m := range l.Members {
			g.Members = append(g.Members, castMember{TMDBID: saved.People[m.StaffID], Name: m.Name, Character: m.Character, Photo: m.Photo})
		}
		groups = append(groups, g)
	}
	return groups, defaultCastLanguage(groups, ask.audio, ask.want)
}

// castLanguages is the casts a page offers: the original first, then every dub AniList
// lists a real cast for, by name (the page shows them as one list to pick from).
func castLanguages(vc *metadata.VoiceCast) []metadata.VoiceLanguage {
	if vc == nil || len(vc.Languages) == 0 {
		return nil
	}
	out := []metadata.VoiceLanguage{vc.Languages[0]}
	for _, l := range vc.Languages[1:] {
		// A dub with a voice or two listed is not a cast (the original is kept as it is).
		if len(l.Members) < voiceCastMinimum || l.Code == vc.Languages[0].Code {
			continue
		}
		out = append(out, l)
	}
	dubs := out[1:]
	sort.SliceStable(dubs, func(i, j int) bool { return dubs[i].Name < dubs[j].Name })
	return out
}

// castPriority splits the casts into those this page is likely to show (the original,
// English, the viewer's language, the languages of the library's copy) and the rest.
// A page waits for the first only: a title can have eight dubs of fifteen voices each.
func castPriority(langs []metadata.VoiceLanguage, ask castAsk) (first, rest []metadata.VoiceLanguage) {
	if len(langs) == 0 {
		return nil, nil
	}
	keep := map[string]bool{"eng": true, langs[0].Code: true}
	if w := langKey(ask.want); w != "" {
		keep[w] = true
	}
	for _, a := range ask.audio {
		keep[langKey(a)] = true
	}
	for _, l := range langs {
		if keep[l.Code] {
			first = append(first, l)
		} else {
			rest = append(rest, l)
		}
	}
	return first, rest
}

// defaultCastLanguage is the cast a page shows first: the viewer's language when there
// is a cast for it and the copy in the library can be heard in it (or there is no copy
// to say otherwise); else the original. So a dub watcher sees the dub's cast for what
// they have dubbed, and the original's for what they only have subtitled.
func defaultCastLanguage(groups []castGroup, audio []string, want string) string {
	if len(groups) == 0 {
		return ""
	}
	w := langKey(want)
	if w == "" {
		w = "eng"
	}
	heard := len(audio) == 0
	for _, a := range audio {
		if langKey(a) == w {
			heard = true
		}
	}
	if heard {
		for _, g := range groups {
			if g.Language == w {
				return w
			}
		}
	}
	return groups[0].Language
}

// needsRead: nothing fresh is saved for this title, or someone in the casts this page
// is likely to show has not been looked for on TMDB yet. A title with one cast shows
// none, so it has nobody to look for (and is not read again at every open of its page).
func needsRead(have bool, saved *savedVoiceCast, ask castAsk) bool {
	if !have || !saved.fresh() {
		return true
	}
	langs := castLanguages(saved.Cast)
	if len(langs) < 2 {
		return false
	}
	first, _ := castPriority(langs, ask)
	return !linked(saved, first)
}

// itemCastAsk is a library title's question: anime only (AniList knows no others).
func itemCastAsk(it *model.Item, tmdbID int, facts *store.MediaFacts, x *tmdbExtras, want string) (castAsk, bool) {
	_, onAniList := it.ProviderIDs["anilist"]
	if !onAniList && !it.IsAnime {
		return castAsk{}, false
	}
	ask := castAsk{movie: it.Type == model.TypeMovie, tmdbID: tmdbID, title: it.Title, want: want}
	ask.anilistID, _ = strconv.Atoi(it.ProviderIDs["anilist"])
	if it.Year != nil {
		ask.year = *it.Year
	}
	if facts != nil {
		ask.audio = facts.Audio
	}
	ask.byName, ask.byRole = creditMaps(x)
	return ask, true
}

// linked: everyone in these casts has been looked for on TMDB.
func linked(saved *savedVoiceCast, langs []metadata.VoiceLanguage) bool {
	for _, l := range langs {
		for _, m := range l.Members {
			if _, ok := saved.People[m.StaffID]; !ok {
				return false
			}
		}
	}
	return true
}

// voiceJob is one read of a title under way. ready closes when the casts the asking
// page is likely to show are saved, done when everything is.
type voiceJob struct{ ready, done chan struct{} }

// readVoiceCast starts (or joins) the one read of a title. The read is on a context of
// its own: the page that asked may be gone long before AniList's pacing lets it through.
func (s *Server) readVoiceCast(ask castAsk) *voiceJob {
	v := s.voice
	key := ask.key()
	v.mu.Lock()
	defer v.mu.Unlock()
	if job, ok := v.jobs[key]; ok {
		return job
	}
	job := &voiceJob{ready: make(chan struct{}), done: make(chan struct{})}
	if at, ok := v.failed[key]; ok && time.Since(at) < voiceCastRetry {
		close(job.ready)
		close(job.done)
		return job
	}
	if v.jobs == nil {
		v.jobs = map[string]*voiceJob{}
	}
	v.jobs[key] = job
	go func() {
		var once sync.Once
		ready := func() { once.Do(func() { close(job.ready) }) }
		ctx, cancel := context.WithTimeout(context.Background(), voiceCastLimit)
		ok := s.fillVoiceCast(ctx, ask, ready)
		cancel()
		v.mu.Lock()
		delete(v.jobs, key)
		if v.failed == nil {
			v.failed = map[string]time.Time{}
		}
		if ok {
			delete(v.failed, key)
		} else {
			v.failed[key] = time.Now()
		}
		v.mu.Unlock()
		ready()
		close(job.done)
	}()
	return job
}

// fillVoiceCast reads the cast when the saved one is old, looks its people up on TMDB
// and saves: first the casts the page is likely to show (then ready is called), after
// them the other languages'. False when AniList or TMDB could not be asked: what was
// learned is still saved, and the rest is tried again after voiceCastRetry.
func (s *Server) fillVoiceCast(ctx context.Context, ask castAsk, ready func()) bool {
	v := s.voice
	var saved savedVoiceCast
	have, _ := s.store.GetSetting(ctx, ask.key(), &saved)
	if !have || !saved.fresh() {
		vc, err := v.read(ctx, ask.anilistID, ask.title, ask.year, ask.movie)
		if err != nil {
			slog.Warn("details: voice cast not read", "title", ask.title, "err", err)
			return false
		}
		// People matched before stay: the same names are not searched for every week.
		saved = savedVoiceCast{At: time.Now(), Read: voiceCastReading, Cast: vc, People: saved.People}
	}
	if saved.People == nil {
		saved.People = map[int]int{}
	}
	langs := castLanguages(saved.Cast)
	if len(langs) < 2 {
		langs = nil // nothing will be shown: nobody to look up
	}
	save := func() bool {
		if err := s.store.SetSetting(ctx, ask.key(), saved); err != nil {
			slog.Warn("details: saving a voice cast", "title", ask.title, "err", err)
			return false
		}
		return true
	}
	first, rest := castPriority(langs, ask)
	asked := s.linkPeople(ctx, ask, &saved, first)
	stored := save()
	ready()
	if !asked || !stored {
		return false // TMDB is away: the other languages wait for the retry too
	}
	if linked(&saved, rest) {
		return true
	}
	asked = s.linkPeople(ctx, ask, &saved, rest)
	return save() && asked
}

// linkPeople looks for the TMDB person of everyone in these casts not looked for yet.
// The original cast is matched with the title's own credits first; a dub's voice only
// by a search for the exact name (the character's credit is the original's). False
// when TMDB could not be asked about someone.
func (s *Server) linkPeople(ctx context.Context, ask castAsk, saved *savedVoiceCast, langs []metadata.VoiceLanguage) bool {
	v := s.voice
	type found struct{ staff, tmdb int }
	var todo []metadata.VoiceActor
	queued := map[int]bool{}
	for _, l := range langs {
		original := l.Code == saved.Cast.Languages[0].Code
		for _, m := range l.Members {
			if _, ok := saved.People[m.StaffID]; ok || queued[m.StaffID] {
				continue
			}
			if id := ask.byName[foldName(m.Name)]; id > 0 {
				saved.People[m.StaffID] = id
				continue
			}
			if id := ask.byRole[foldName(m.Character)]; id > 0 && original {
				saved.People[m.StaffID] = id // TMDB's credit for this character is its original voice
				continue
			}
			queued[m.StaffID] = true
			todo = append(todo, m)
		}
	}
	if len(todo) == 0 || v.person == nil {
		return true
	}
	allAsked := true
	results := make(chan found, len(todo))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	var failed sync.Once
	for _, m := range todo {
		wg.Add(1)
		go func(m metadata.VoiceActor) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			id, err := v.person(ctx, m.Name)
			if err != nil {
				failed.Do(func() { allAsked = false })
				return
			}
			results <- found{m.StaffID, id}
		}(m)
	}
	wg.Wait()
	close(results)
	for r := range results {
		saved.People[r.staff] = r.tmdb
	}
	return allAsked
}

// tmdbPerson finds a person on TMDB by their exact name (accents and case aside): an
// actor when several share it. 0 when nobody does. A dub actor is the same person on
// both sites, but only the name joins them, so a near match is no match.
func (s *Server) tmdbPerson(ctx context.Context, name string) (int, error) {
	if s.meta == nil || s.meta.TMDB() == nil {
		return 0, nil
	}
	var out struct {
		Results []struct {
			ID         int    `json:"id"`
			Name       string `json:"name"`
			Department string `json:"known_for_department"`
		} `json:"results"`
	}
	if err := s.meta.TMDB().Get(ctx, "/search/person", url.Values{"query": {name}}, &out); err != nil {
		return 0, err
	}
	return pickPerson(name, out.Results, func(i int) (int, string, string) {
		r := out.Results[i]
		return r.ID, r.Name, r.Department
	}), nil
}

// pickPerson: the first result with exactly this name, an actor before anyone else.
func pickPerson[T any](name string, results []T, at func(i int) (id int, name, department string)) int {
	want := foldName(name)
	other := 0
	for i := range results {
		id, n, dep := at(i)
		if foldName(n) != want {
			continue
		}
		if dep == "Acting" {
			return id
		}
		if other == 0 {
			other = id
		}
	}
	return other
}

// creditMaps indexes a title's TMDB credits for castAsk.
func creditMaps(x *tmdbExtras) (byName, byRole map[string]int) {
	if x == nil {
		return nil, nil
	}
	return x.byName, x.byRole
}

// foldName makes names comparable across sites: lower case, accents and macrons off,
// "(voice)" and punctuation dropped.
func foldName(s string) string {
	s = strings.ToLower(s)
	if i := strings.Index(s, "("); i >= 0 {
		s = s[:i]
	}
	var b strings.Builder
	space := false
	for _, r := range s {
		if p, ok := plainLetters[r]; ok {
			r = p
		}
		switch {
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
	return b.String()
}

var plainLetters = func() map[rune]rune {
	m := map[rune]rune{}
	for plain, marked := range map[rune]string{
		'a': "àáâãäåāă", 'c': "çćč", 'e': "èéêëēėę", 'i': "ìíîïī", 'n': "ñń", 'o': "òóôõöøō",
		'u': "ùúûüū", 'y': "ýÿ", 's': "śš", 'z': "źżž", 'l': "ł",
	} {
		for _, r := range marked {
			m[r] = plain
		}
	}
	return m
}()

// langKey makes language codes comparable: files say "ger" or "de", apps "deu".
func langKey(code string) string {
	c := strings.ToLower(strings.TrimSpace(code))
	if i := strings.IndexAny(c, "-_"); i > 0 {
		c = c[:i]
	}
	if k, ok := langKeys[c]; ok {
		return k
	}
	return c
}

var langKeys = map[string]string{
	"en": "eng", "ja": "jpn", "jp": "jpn", "de": "deu", "ger": "deu", "fr": "fra", "fre": "fra",
	"es": "spa", "it": "ita", "pt": "por", "ko": "kor", "zh": "zho", "chi": "zho", "ru": "rus",
	"nl": "nld", "dut": "nld", "pl": "pol", "ar": "ara", "hi": "hin", "th": "tha", "tr": "tur",
	"sv": "swe", "da": "dan", "no": "nor", "fi": "fin", "he": "heb", "hu": "hun", "ca": "cat",
	"id": "ind", "vi": "vie", "ms": "msa", "may": "msa", "tl": "tgl",
}
