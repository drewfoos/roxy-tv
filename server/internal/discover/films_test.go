package discover

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/roxy-media/roxy/server/internal/config"
	"github.com/roxy-media/roxy/server/internal/metadata"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

// The graphs below are AniList's own, as read on 2026-11-12 (ids, dates and relation
// types), cut down to what each case needs; the season lists are TMDB's.

func season(id int, start, title string) metadata.StorySeason {
	return metadata.StorySeason{ID: id, Title: title, Start: start, Format: "TV"}
}

func film(id int, start, title string, links ...metadata.StoryLink) metadata.StoryFilm {
	y, _ := strconv.Atoi(firstN(start, 4))
	return metadata.StoryFilm{ID: id, Title: title, Romaji: title, Start: start, Year: y, Links: links}
}

func link(season int, rel string) metadata.StoryLink {
	return metadata.StoryLink{Season: season, Relation: rel}
}

func joined(f metadata.StoryFilm, after, before int) metadata.StoryFilm {
	f.Paths = append(f.Paths, metadata.StoryPath{After: after, Before: before})
	return f
}

func listed(dates ...string) []Season {
	var out []Season
	for i, d := range dates {
		out = append(out, Season{Number: i + 1, AirDate: d, Episodes: 12})
	}
	return out
}

func bunnyGirl() *metadata.StoryGraph {
	return &metadata.StoryGraph{
		Seasons: []metadata.StorySeason{
			season(101291, "2018-10-04", "Rascal Does Not Dream of Bunny Girl Senpai"),
			season(171046, "2025-07-05", "Rascal Does Not Dream of Santa Claus"),
		},
		Films: []metadata.StoryFilm{
			joined(film(104157, "2019-06-15", "Rascal Does Not Dream of a Dreaming Girl", link(101291, "SEQUEL")), 101291, 171046),
			joined(film(154967, "2023-06-23", "Rascal Does Not Dream of a Sister Venturing Out"), 101291, 171046),
			joined(film(161474, "2023-12-01", "Rascal Does Not Dream of a Knapsack Kid", link(171046, "PREQUEL")), 101291, 171046),
			film(199340, "2026-10-16", "Rascal Does Not Dream of a Dear Friend", link(171046, "SEQUEL")),
		},
	}
}

// placed is one film as a test reads it: "title | after | eligible or the note".
func placedLines(ps []placedFilm) []string {
	var out []string
	for _, p := range ps {
		what := p.Note
		if p.Eligible {
			what = fmt.Sprintf("eligible %d-%d", p.Between[0], p.Between[1])
		}
		out = append(out, fmt.Sprintf("%s | %d | %s", p.Film.Title, p.After, what))
	}
	return out
}

func wantPlaced(t *testing.T, got []placedFilm, want ...string) {
	t.Helper()
	g := placedLines(got)
	if strings.Join(g, "\n") != strings.Join(want, "\n") {
		t.Fatalf("placed:\n  %s\nwant:\n  %s", strings.Join(g, "\n  "), strings.Join(want, "\n  "))
	}
}

// Dragon Ball: AniList's chain runs on into Dragon Ball Z and GT, which TMDB keeps as
// shows of their own. None of Z's films belongs on Dragon Ball's page, and Dragon
// Ball's own four films are retellings: listed, never brought along.
func TestPlaceFilmsDragonBall(t *testing.T) {
	g := &metadata.StoryGraph{
		Seasons: []metadata.StorySeason{season(223, "1986-02-26", "Dragon Ball"), season(813, "1989-04-26", "Dragon Ball Z"), season(225, "1996-02-07", "Dragon Ball GT")},
		Films: []metadata.StoryFilm{
			film(502, "1986-12-20", "Dragon Ball: Curse of the Blood Rubies", link(223, "ALTERNATIVE")),
			film(891, "1987-07-18", "Dragon Ball: Sleeping Princess in Devil's Castle", link(223, "ALTERNATIVE")),
			film(892, "1988-07-09", "Dragon Ball: Mystical Adventure", link(223, "ALTERNATIVE")),
			film(893, "1996-03-02", "Dragon Ball: The Path to Power", link(223, "ALTERNATIVE")),
			film(894, "1989-07-15", "Dragon Ball Z: Dead Zone", link(813, "SIDE_STORY")),
			film(901, "1993-03-06", "Dragon Ball Z: Broly - The Legendary Super Saiyan", link(813, "SIDE_STORY")),
			film(14837, "2013-03-30", "Dragon Ball Z: Battle of Gods", link(813, "SIDE_STORY")),
		},
	}
	got := placeFilms(g, listed("1986-02-26"), "1989-04-19")
	wantPlaced(t, got,
		"Dragon Ball: Curse of the Blood Rubies | -1 | Alternate version",
		"Dragon Ball: Sleeping Princess in Devil's Castle | -1 | Alternate version",
		"Dragon Ball: Mystical Adventure | -1 | Alternate version",
		"Dragon Ball: The Path to Power | -1 | Alternate version")
}

// Digimon (TMDB: Adventure and 02 as two seasons of one show): Our War Game is linked
// as the sequel of Adventure and the prequel of 02 and came out between them, and 02
// names Revenge of Diaboromon as its sequel: the story after the latest season. Nothing
// else is brought along: the film before the series, a side story, a spin-off; the tri. films hang off other films and are not this show's.
func TestPlaceFilmsDigimon(t *testing.T) {
	g := &metadata.StoryGraph{
		Seasons: []metadata.StorySeason{season(552, "1999-03-07", "Digimon: Digital Monsters"), season(1313, "2000-04-02", "Digimon: Digital Monsters 02")},
		Films: []metadata.StoryFilm{
			film(2123, "2005-01-03", "Digmon X-Evolution", link(552, "SPIN_OFF")),
			joined(film(2397, "2000-03-04", "Digimon: Our War Game", link(552, "SEQUEL"), link(1313, "PREQUEL")), 552, 1313),
			film(2398, "2001-03-03", "Digimon Adventure 02: Revenge of Diaboromon", link(1313, "SEQUEL")),
			film(2961, "1999-03-06", "Digimon Adventure (Movie)", link(552, "PREQUEL")),
			film(2962, "2000-07-08", "Digimon: Hurricane Touchdown", link(1313, "SIDE_STORY")),
			film(20802, "2015-11-21", "Digimon Adventure tri. Chapter 1: Reunion"),
		},
	}
	got := placeFilms(g, listed("1999-03-07", "2000-04-02"), "2001-03-25")
	wantPlaced(t, got,
		"Digimon Adventure (Movie) | 0 | Before the first season",
		"Digimon: Our War Game | 1 | eligible 552-1313",
		"Digimon Adventure 02: Revenge of Diaboromon | 2 | eligible 1313-0",
		"Digimon: Hurricane Touchdown | -1 | Side story",
		"Digmon X-Evolution | -1 | Spin-off")
}

// Evangelion: one season. The End of Evangelion follows it (nothing to sit between), a
// recap is a recap, and the Rebuild films hang off other films.
func TestPlaceFilmsEvangelion(t *testing.T) {
	g := &metadata.StoryGraph{
		Seasons: []metadata.StorySeason{season(30, "1995-10-03", "Neon Genesis Evangelion")},
		Films: []metadata.StoryFilm{
			film(31, "1997-03-15", "Neon Genesis Evangelion: Death & Rebirth", link(30, "SUMMARY")),
			film(32, "1997-07-19", "Neon Genesis Evangelion: The End of Evangelion", link(30, "SEQUEL")),
			film(2759, "2007-09-01", "Evangelion: 1.0 You Are (Not) Alone"),
		},
	}
	got := placeFilms(g, listed("1995-10-04"), "1996-03-27")
	wantPlaced(t, got,
		"Neon Genesis Evangelion: The End of Evangelion | 1 | eligible 30-0",
		"Neon Genesis Evangelion: Death & Rebirth | -1 | Recap")
}

// Bunny Girl Senpai: three films join its two seasons (the middle one has no season
// link of its own, only its place on the run); the 2026 film follows the last season.
func TestPlaceFilmsBunnyGirl(t *testing.T) {
	got := placeFilms(bunnyGirl(), listed("2018-10-04", "2025-07-05"), "2025-09-27")
	wantPlaced(t, got,
		"Rascal Does Not Dream of a Dreaming Girl | 1 | eligible 101291-171046",
		"Rascal Does Not Dream of a Sister Venturing Out | 1 | eligible 101291-171046",
		"Rascal Does Not Dream of a Knapsack Kid | 1 | eligible 101291-171046",
		"Rascal Does Not Dream of a Dear Friend | 2 | eligible 171046-0")
}

// TMDB lists both AniList seasons as one (or a page renumbers them): the films are
// still between the same two AniList seasons, shown after the one listed season, and
// the pair they are tied to is the same whatever the page calls its seasons.
func TestPlaceFilmsMergedAndRenumberedSeasons(t *testing.T) {
	merged := placeFilms(bunnyGirl(), listed("2018-10-04"), "2025-09-27")
	wantPlaced(t, merged,
		"Rascal Does Not Dream of a Dreaming Girl | 1 | eligible 101291-171046",
		"Rascal Does Not Dream of a Sister Venturing Out | 1 | eligible 101291-171046",
		"Rascal Does Not Dream of a Knapsack Kid | 1 | eligible 101291-171046",
		"Rascal Does Not Dream of a Dear Friend | 1 | eligible 171046-0")
	// The same show listed as seasons 4 and 9.
	renumbered := []Season{{Number: 9, AirDate: "2025-07-05"}, {Number: 4, AirDate: "2018-10-04"}}
	got := placeFilms(bunnyGirl(), renumbered, "2025-09-27")
	if got[0].After != 4 || !got[0].Eligible || got[0].Between != merged[0].Between {
		t.Fatalf("renumbered: %v", placedLines(got))
	}
}

// One season: nothing can be between two seasons, but the film the season names as its
// sequel continues the story; the one before it is only listed.
func TestPlaceFilmsSingleSeason(t *testing.T) {
	g := &metadata.StoryGraph{
		Seasons: []metadata.StorySeason{season(1, "2020-01-05", "Show")},
		Films:   []metadata.StoryFilm{film(2, "2021-06-01", "Show: The Movie", link(1, "SEQUEL")), film(3, "2019-06-01", "Show: Episode 0", link(1, "PREQUEL"))},
	}
	wantPlaced(t, placeFilms(g, listed("2020-01-05"), "2020-03-29"),
		"Show: Episode 0 | 0 | Before the first season",
		"Show: The Movie | 1 | eligible 1-0")
}

// Demon Slayer: Infinity Castle is the sequel of the latest season (Hashira Training)
// and comes with a request that covers it. Mugen Train is retold by a TV season of its
// own and stays listed; the later Infinity Castle films hang off the first one only.
func TestPlaceFilmsDemonSlayer(t *testing.T) {
	g := &metadata.StoryGraph{
		Seasons: []metadata.StorySeason{
			season(101922, "2019-04-06", "Demon Slayer"), season(129874, "2021-10-10", "Mugen Train Arc"),
			season(142329, "2021-12-05", "Entertainment District Arc"), season(145139, "2023-04-09", "Swordsmith Village Arc"),
			season(166240, "2024-05-12", "Hashira Training Arc"),
		},
		Films: []metadata.StoryFilm{
			film(112151, "2020-10-16", "Mugen Train", link(101922, "SEQUEL"), link(129874, "ALTERNATIVE"), link(142329, "PREQUEL")),
			film(178788, "2025-07-18", "Infinity Castle", link(166240, "SEQUEL")),
			film(195200, "", "Infinity Castle Part 2"),
		},
	}
	got := placeFilms(g, listed("2019-04-06", "2021-10-10", "2021-12-05", "2023-04-09", "2024-05-12"), "2024-06-30")
	wantPlaced(t, got,
		"Infinity Castle | 5 | eligible 166240-0",
		"Mugen Train | -1 | Alternate version")
}

// The Quintessential Quintuplets, as AniList links it: the film that ends the story is
// the second season's sequel through a special (which came out after the film). It
// continues the latest season like any film that season names itself.
func TestPlaceFilmsQuintupletsFilmThroughASpecial(t *testing.T) {
	via := metadata.StoryLink{Season: 109261, Relation: "SEQUEL", Via: []int{163327}}
	g := &metadata.StoryGraph{
		Seasons: []metadata.StorySeason{season(103572, "2019-01-11", "The Quintessential Quintuplets"), season(109261, "2021-01-08", "The Quintessential Quintuplets 2")},
		Films:   []metadata.StoryFilm{film(131520, "2022-05-20", "The Quintessential Quintuplets Movie", via)},
	}
	got := placeFilms(g, listed("2019-01-11", "2021-01-08"), "2021-03-26")
	wantPlaced(t, got, "The Quintessential Quintuplets Movie | 2 | eligible 109261-0")
	if !coversFilm([]int{103572, 109261}, got[0].Between) || coversFilm([]int{103572}, got[0].Between) {
		t.Fatal("the film comes with a request that covers season 2, and with no other")
	}
}

// After the latest season, and still only listed: a film on a run that leaves the show
// without the season naming it, a sequel dated before the season began or by year
// only, a recap, a side story; and the sequel of a season that is not the latest (its
// next season is another AniList entry inside the same listed season).
func TestPlaceFilmsAfterTheLatestSeasonNotEligible(t *testing.T) {
	seasons := []metadata.StorySeason{season(1, "2019-04-06", "S1"), season(2, "2021-10-10", "S2")}
	cases := []struct {
		name string
		film metadata.StoryFilm
		note string
	}{
		{"only on a run out of the show", joined(film(10, "2022-06-01", "Bridge"), 2, 99), "After the latest season"},
		{"dated before the season", film(11, "2021-01-01", "Early", link(2, "SEQUEL")), "After the latest season"},
		{"year only", film(12, "2022", "Sometime", link(2, "SEQUEL")), "After the latest season"},
		{"recap", film(13, "2022-06-01", "Show: Compilation Film", link(2, "SEQUEL")), "Recap"},
		{"side story", film(14, "2022-06-01", "Aside", link(2, "SIDE_STORY")), "Side story"},
		{"also an alternate version", film(15, "2022-06-01", "Retold", link(2, "SEQUEL"), link(1, "ALTERNATIVE")), "Alternate version"},
	}
	for _, c := range cases {
		g := &metadata.StoryGraph{Seasons: seasons, Films: []metadata.StoryFilm{c.film}}
		got := placeFilms(g, listed("2019-04-06", "2021-10-10"), "2021-12-26")
		if len(got) != 1 || got[0].Eligible || got[0].Note != c.note {
			t.Errorf("%s: %v, want listed with %q", c.name, placedLines(got), c.note)
		}
	}
	// The control: the latest season's sequel, dated after it.
	g := &metadata.StoryGraph{Seasons: seasons, Films: []metadata.StoryFilm{film(20, "2022-06-01", "The Movie", link(2, "SEQUEL"))}}
	if got := placeFilms(g, listed("2019-04-06", "2021-10-10"), "2021-12-26"); len(got) != 1 || !got[0].Eligible || got[0].Between != [2]int{2, 0} {
		t.Fatalf("control: %v", placedLines(got))
	}
}

// What does not make a film eligible: a date between two seasons without a story link
// (My Hero Academia's films are side stories), a recap or retelling that is also
// linked as a sequel or prequel (Jujutsu Kaisen: Execution; Demon Slayer: Mugen Train,
// which its TV arc retells), a recap by its title alone, links that name two different
// gaps, a sequel dated after the season it would have to come before, a date AniList
// gives by year only, and a graph that is another series of the same name.
func TestPlaceFilmsNotEligible(t *testing.T) {
	seasons := []metadata.StorySeason{season(1, "2019-04-06", "S1"), season(2, "2021-10-10", "S2"), season(3, "2021-12-05", "S3"), season(4, "2023-04-09", "S4")}
	dates := []string{"2019-04-06", "2021-10-10", "2021-12-05", "2023-04-09"}
	cases := []struct {
		name string
		film metadata.StoryFilm
		note string
	}{
		{"side story between seasons", film(10, "2020-08-03", "Two Heroes", link(1, "SIDE_STORY")), "Side story"},
		{"recap that is also a prequel", film(11, "2021-11-07", "Execution", link(2, "SUMMARY"), link(3, "PREQUEL")), "Recap"},
		{"retold by a TV arc", film(12, "2020-10-16", "Mugen Train", link(1, "SEQUEL"), link(2, "ALTERNATIVE"), link(3, "PREQUEL")), "Alternate version"},
		{"recap by title", film(13, "2020-10-16", "Show: Compilation Film", link(1, "SEQUEL")), "Recap"},
		{"links name two gaps", film(14, "2021-11-01", "Between What", link(1, "SEQUEL"), link(4, "PREQUEL")), "Can't tell where it fits"},
		{"sequel dated after the next season", film(15, "2022-05-01", "Late", link(1, "SEQUEL")), "Can't tell where it fits"},
		{"year only", film(16, "2020", "Sometime", link(1, "SEQUEL")), "Can't tell where it fits"},
	}
	for _, c := range cases {
		g := &metadata.StoryGraph{Seasons: seasons, Films: []metadata.StoryFilm{c.film}}
		got := placeFilms(g, listed(dates...), "2023-06-18")
		if len(got) != 1 || got[0].Eligible || got[0].Note != c.note {
			t.Errorf("%s: %v, want listed with %q", c.name, placedLines(got), c.note)
		}
	}
	// The control: the same film with a plain sequel link and a date in the gap is eligible.
	g := &metadata.StoryGraph{Seasons: seasons, Films: []metadata.StoryFilm{film(20, "2020-10-16", "The Movie", link(1, "SEQUEL"))}}
	if got := placeFilms(g, listed(dates...), "2023-06-18"); len(got) != 1 || !got[0].Eligible {
		t.Fatalf("control: %v", placedLines(got))
	}
	// Another series of the same name: no season starts where this show does.
	if got := placeFilms(g, listed("2001-01-01", "2002-01-01"), "2002-06-01"); got != nil {
		t.Fatalf("a graph of another series placed films: %v", placedLines(got))
	}
}

// ---- the service: listing, requesting, the background run ----

type filmWorld struct {
	t      *testing.T
	st     *store.Store
	s      *Service
	admin  *model.Principal
	mu     sync.Mutex
	tv     string               // TMDB's answer for /tv/100
	graph  *metadata.StoryGraph // AniList's answer
	inArr  map[int]bool         // films Radarr holds
	adds   []int                // films Radarr was asked to add
	failRA bool                 // Radarr answers 500 to adds
	onAdd  func()               // runs inside Radarr's add
	search map[string]string    // film title -> TMDB search results (JSON)
	reads  int                  // times AniList was read
}

const bunnyTV = `{"id":100,"name":"Bunny Girl Senpai","first_air_date":"2018-10-04","last_air_date":"2025-09-27","original_language":"ja","genres":[{"id":16}],
	"seasons":[{"season_number":1,"air_date":"2018-10-04","episode_count":13},{"season_number":2,"air_date":"2025-07-05","episode_count":13}],"external_ids":{"tvdb_id":345596}}`

// TMDB ids of the films in the world.
const (
	dreamingGirl = 200
	sisterOut    = 201
	knapsackKid  = 300
	dearFriend   = 400
)

func newFilmWorld(t *testing.T) *filmWorld {
	t.Helper()
	ctx := context.Background()
	w := &filmWorld{t: t, tv: bunnyTV, graph: bunnyGirl(), inArr: map[int]bool{}}
	one := func(id int, title, date string) string {
		return fmt.Sprintf(`{"results":[{"id":%d,"title":%q,"release_date":%q,"genre_ids":[16]}]}`, id, title, date)
	}
	w.search = map[string]string{
		"Rascal Does Not Dream of a Dreaming Girl":        one(dreamingGirl, "Rascal Does Not Dream of a Dreaming Girl", "2019-06-15"),
		"Rascal Does Not Dream of a Sister Venturing Out": one(sisterOut, "Rascal Does Not Dream of a Sister Venturing Out", "2023-06-23"),
		"Rascal Does Not Dream of a Knapsack Kid":         one(knapsackKid, "Rascal Does Not Dream of a Knapsack Kid", "2023-12-01"),
		"Rascal Does Not Dream of a Dear Friend":          one(dearFriend, "Rascal Does Not Dream of a Dear Friend", "2026-10-16"),
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	w.st = st
	tmdb := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		w.mu.Lock()
		defer w.mu.Unlock()
		switch {
		case r.URL.Path == "/tv/100":
			rw.Write([]byte(w.tv))
		case strings.HasPrefix(r.URL.Path, "/movie/"):
			id := strings.TrimPrefix(r.URL.Path, "/movie/")
			rw.Write([]byte(`{"id":` + id + `,"title":"Film ` + id + `","release_date":"2019-06-15","original_language":"ja","genres":[{"id":16}]}`))
		case r.URL.Path == "/search/movie":
			if res, ok := w.search[r.URL.Query().Get("query")]; ok {
				rw.Write([]byte(res))
			} else {
				rw.Write([]byte(`{"results":[]}`))
			}
		default:
			rw.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(tmdb.Close)
	radarr := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/movie":
			id, _ := strconv.Atoi(r.URL.Query().Get("tmdbId"))
			w.mu.Lock()
			has := w.inArr[id]
			w.mu.Unlock()
			if has {
				fmt.Fprintf(rw, `[{"id":9,"tmdbId":%d,"monitored":false,"hasFile":true}]`, id)
			} else {
				rw.Write([]byte(`[]`))
			}
		case r.URL.Path == "/api/v3/movie/lookup/tmdb":
			rw.Write([]byte(`{"title":"film","tmdbId":` + r.URL.Query().Get("tmdbId") + `}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v3/movie":
			var m map[string]any
			_ = json.NewDecoder(r.Body).Decode(&m)
			if w.onAdd != nil {
				w.onAdd()
			}
			w.mu.Lock()
			defer w.mu.Unlock()
			if w.failRA {
				http.Error(rw, "down", 500)
				return
			}
			id, _ := m["tmdbId"].(float64)
			w.adds = append(w.adds, int(id))
			rw.Write([]byte(`{"id":7}`))
		default:
			rw.Write([]byte(`[]`))
		}
	}))
	t.Cleanup(radarr.Close)
	// Sonarr already holds the series, monitored: a request for it is "sent" at once.
	sonarr := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/series":
			rw.Write([]byte(`[{"id":5,"tvdbId":345596,"monitored":true,"monitorNewItems":"all","seasons":[]}]`))
		case r.Method == http.MethodGet:
			rw.Write([]byte(`[]`))
		default:
			rw.Write([]byte(`{"id":1}`))
		}
	}))
	t.Cleanup(sonarr.Close)
	cfg := &config.Config{
		RadarrURL: radarr.URL, RadarrAPIKey: "k", RadarrRootFolder: "/data/Anime Movies", RadarrQualityProfile: "4",
		SonarrURL: sonarr.URL, SonarrAPIKey: "k", SonarrRootFolder: "/data/Anime", SonarrQualityProfile: "4",
	}
	w.s = New(st, cfg, metadata.NewTMDBAt("k", tmdb.URL))
	w.s.storyGraph = func(ctx context.Context, anilistID int, title string, year int) (*metadata.StoryGraph, error) {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.reads++
		b, _ := json.Marshal(w.graph) // a copy, as a fresh read would be
		var g metadata.StoryGraph
		_ = json.Unmarshal(b, &g)
		return &g, nil
	}
	for _, l := range []*model.Library{{Name: "Anime", Kind: model.KindSeries, Root: "/a"}, {Name: "Anime Movies", Kind: model.KindMovies, Root: "/m"}} {
		if err := st.CreateLibrary(ctx, l); err != nil {
			t.Fatal(err)
		}
	}
	user := &model.User{ID: "u-admin", Name: "drewfoos", Role: model.RoleAdmin}
	if err := st.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	w.admin = &model.Principal{UserID: user.ID, User: user, IsAdmin: true}
	return w
}

// added: the films Radarr was asked to add, sorted.
func (w *filmWorld) added() []int {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := append([]int{}, w.adds...)
	sort.Ints(out)
	return out
}

func (w *filmWorld) wantAdded(what string, want ...int) {
	w.t.Helper()
	if got := w.added(); fmt.Sprint(got) != fmt.Sprint(append([]int{}, want...)) {
		w.t.Fatalf("%s: Radarr was asked for %v, want %v", what, got, want)
	}
}

// aDayLater: the saved AniList links are a day old, so the next look reads them again.
func (w *filmWorld) aDayLater() {
	w.t.Helper()
	ctx := context.Background()
	var sc storyCache
	if ok, err := w.st.GetSetting(ctx, storyKey(100), &sc); err != nil || !ok {
		w.t.Fatalf("no saved links: %v", err)
	}
	sc.At = sc.At.Add(-25 * time.Hour)
	if err := w.st.SetSetting(ctx, storyKey(100), sc); err != nil {
		w.t.Fatal(err)
	}
}

func (w *filmWorld) films() map[int]Film {
	w.t.Helper()
	list, err := w.s.Films(context.Background(), 100)
	if err != nil {
		w.t.Fatal(err)
	}
	out := map[int]Film{}
	for _, f := range list.Films {
		out[f.AniListID] = f
	}
	return out
}

func (w *filmWorld) movieItem(id string, providers map[string]string) {
	w.t.Helper()
	ctx := context.Background()
	libs, _ := w.st.ListLibraries(ctx)
	for _, l := range libs {
		if l.Kind == model.KindMovies {
			if err := w.st.UpsertItem(ctx, &model.Item{ID: id, GUID: "g-" + id, LibraryID: l.ID, Type: model.TypeMovie, Title: id, ProviderIDs: providers}); err != nil {
				w.t.Fatal(err)
			}
		}
	}
}

// The page: all four films, each after its season; the three between the seasons and
// the one that continues the latest season are on the main button; AniList is read once.
func TestFilmsListedOnTheSeriesPage(t *testing.T) {
	w := newFilmWorld(t)
	fs := w.films()
	if len(fs) != 4 {
		t.Fatalf("%d films listed, want 4", len(fs))
	}
	for anilist, want := range map[int]Film{
		104157: {TMDBID: dreamingGirl, AfterSeason: 1, Eligible: true, WithRequest: true},
		154967: {TMDBID: sisterOut, AfterSeason: 1, Eligible: true, WithRequest: true},
		161474: {TMDBID: knapsackKid, AfterSeason: 1, Eligible: true, WithRequest: true},
		199340: {TMDBID: dearFriend, AfterSeason: 2, Eligible: true, WithRequest: true},
	} {
		f := fs[anilist]
		if f.TMDBID != want.TMDBID || f.AfterSeason != want.AfterSeason || f.Eligible != want.Eligible || f.WithRequest != want.WithRequest || f.Note != want.Note {
			t.Errorf("film %d: %+v, want %+v", anilist, f, want)
		}
	}
	w.films()
	if w.reads != 1 {
		t.Fatalf("AniList read %d times for two looks at one page, want 1", w.reads)
	}
}

// The main button named the films: they are requested with the series, and the request
// remembers it and the seasons it covered. A film the server would not put on the
// button is not requested, whatever the client sent.
func TestRequestWithFilms(t *testing.T) {
	ctx := context.Background()
	w := newFilmWorld(t)
	r, err := w.s.RequestWithFilms(ctx, 100, w.admin, nil, []int{dreamingGirl, sisterOut, knapsackKid, dearFriend, 999})
	if err != nil {
		t.Fatal(err)
	}
	w.wantAdded("the named films", dreamingGirl, sisterOut, knapsackKid, dearFriend)
	if r.Status != "sent" || !r.FilmAuto || fmt.Sprint(r.FilmSeasons) != "[101291 171046]" {
		t.Fatalf("series request %+v, want sent, films asked for, both seasons covered", r)
	}
	saved, _ := w.st.GetRequest(ctx, "tv", 100)
	if saved == nil || !saved.FilmAuto || fmt.Sprint(saved.FilmSeasons) != "[101291 171046]" {
		t.Fatalf("saved %+v", saved)
	}
	for _, id := range []int{dreamingGirl, sisterOut, knapsackKid, dearFriend} {
		if fr, _ := w.st.GetRequest(ctx, "movie", id); fr == nil || fr.Status != "sent" || fr.UserName != "drewfoos" {
			t.Fatalf("film %d request %+v", id, fr)
		}
	}
	// Pressing again (an old page: the series has nothing left to ask for, which is an
	// error of its own) and the background run ask for nothing twice.
	_, _ = w.s.RequestWithFilms(ctx, 100, w.admin, nil, []int{dreamingGirl, sisterOut, knapsackKid})
	w.s.AcquireStoryFilms(ctx)
	w.wantAdded("after a second press and a background run", dreamingGirl, sisterOut, knapsackKid, dearFriend)
}

// The film after the latest season comes only with a request that covers that season:
// asked for season 1 alone, it stays; announced after a request that covered both
// seasons and named films, it comes by itself.
func TestFilmAfterTheLatestSeasonNeedsThatSeason(t *testing.T) {
	ctx := context.Background()
	w := newFilmWorld(t)
	if _, err := w.s.RequestWithFilms(ctx, 100, w.admin, []int{1}, []int{dearFriend}); err != nil {
		t.Fatal(err)
	}
	w.wantAdded("season 1 alone")
	w.s.AcquireStoryFilms(ctx)
	w.wantAdded("season 1 alone, the background run")

	w = newFilmWorld(t)
	full := bunnyGirl()
	w.graph = &metadata.StoryGraph{Seasons: full.Seasons, Films: full.Films[:3]} // Dear Friend is not announced yet
	if _, err := w.s.RequestWithFilms(ctx, 100, w.admin, nil, []int{dreamingGirl, sisterOut, knapsackKid}); err != nil {
		t.Fatal(err)
	}
	w.wantAdded("at the request", dreamingGirl, sisterOut, knapsackKid)
	w.mu.Lock()
	w.graph = full
	w.mu.Unlock()
	w.aDayLater()
	w.s.AcquireStoryFilms(ctx)
	w.wantAdded("once the film after the latest season is announced", dreamingGirl, sisterOut, knapsackKid, dearFriend)
}

// Only part of the films was named (one was unticked, or the page was old): only those.
func TestRequestWithSomeFilms(t *testing.T) {
	w := newFilmWorld(t)
	if _, err := w.s.RequestWithFilms(context.Background(), 100, w.admin, nil, []int{sisterOut}); err != nil {
		t.Fatal(err)
	}
	w.wantAdded("one named film", sisterOut)
}

// A request that did not name films brings none, then or later: not when both seasons
// are requested, not when the library holds them. This is also every request made
// before films could be named (their film_scope is empty).
func TestPlainRequestNeverBringsFilms(t *testing.T) {
	ctx := context.Background()
	w := newFilmWorld(t)
	r, err := w.s.Request(ctx, "tv", 100, w.admin, nil, nil)
	if err != nil || r.Status != "sent" {
		t.Fatalf("%+v, %v", r, err)
	}
	_, _ = w.s.RequestWithFilms(ctx, 100, w.admin, nil, nil) // again, still naming none
	// The series arrives in the library, both seasons whole.
	libs, _ := w.st.ListLibraries(ctx)
	if err := w.st.UpsertItem(ctx, &model.Item{ID: "series", GUID: "g-series", LibraryID: libs[0].ID, Type: model.TypeSeries, Title: "Bunny Girl Senpai", ProviderIDs: map[string]string{"tmdb": "100", "anilist": "101291"}}); err != nil {
		t.Fatal(err)
	}
	w.s.AcquireStoryFilms(ctx)
	w.wantAdded("a request that named no films")
	// Nor does knowing the seasons stand in for having been asked: the flag is what counts.
	saved, _ := w.st.GetRequest(ctx, "tv", 100)
	saved.FilmSeasons = []int{101291, 171046}
	if err := w.st.UpsertRequest(ctx, saved); err != nil {
		t.Fatal(err)
	}
	w.s.AcquireStoryFilms(ctx)
	w.wantAdded("seasons known, films never asked for")
	saved.FilmSeasons = nil
	if err := w.st.UpsertRequest(ctx, saved); err != nil {
		t.Fatal(err)
	}
	if saved, _ := w.st.GetRequest(ctx, "tv", 100); saved == nil || saved.FilmAuto || len(saved.FilmSeasons) != 0 {
		t.Fatalf("saved %+v, want no film scope", saved)
	}
}

// Films announced after the request. One that AniList places between the two seasons
// the request covered comes by itself, a day later at most. A new third season and the
// film leading to it do not: that season was not part of what was asked for. Asking
// again (the button names the new film) covers it.
func TestFilmAnnouncedAfterTheRequest(t *testing.T) {
	ctx := context.Background()
	w := newFilmWorld(t)
	full := bunnyGirl()
	w.graph = &metadata.StoryGraph{Seasons: full.Seasons, Films: full.Films[:1]} // only Dreaming Girl is known yet
	if _, err := w.s.RequestWithFilms(ctx, 100, w.admin, []int{1, 2}, []int{dreamingGirl}); err != nil {
		t.Fatal(err)
	}
	w.wantAdded("at the request", dreamingGirl)

	w.mu.Lock()
	w.graph = &metadata.StoryGraph{Seasons: full.Seasons, Films: full.Films[:2]} // Sister Venturing Out is announced
	w.mu.Unlock()
	w.s.AcquireStoryFilms(ctx)
	w.wantAdded("the same day (AniList is read once a day)", dreamingGirl)
	w.aDayLater()
	w.s.AcquireStoryFilms(ctx)
	w.wantAdded("a day later", dreamingGirl, sisterOut)

	// Season 3 is announced, with a film between seasons 2 and 3.
	w.mu.Lock()
	g := &metadata.StoryGraph{Seasons: append(append([]metadata.StorySeason{}, full.Seasons...), season(300000, "2027-10-02", "Season 3")), Films: append([]metadata.StoryFilm{}, full.Films...)}
	g.Films[3] = joined(g.Films[3], 171046, 300000) // Dear Friend now joins seasons 2 and 3
	w.graph = g
	w.tv = strings.Replace(bunnyTV, `"episode_count":13}],`, `"episode_count":13},{"season_number":3,"air_date":"2027-10-02","episode_count":13}],`, 1)
	w.mu.Unlock()
	w.s.details = newTTLCache[*Detail](10*time.Minute, 500)
	w.aDayLater()
	w.s.AcquireStoryFilms(ctx)
	w.wantAdded("after a third season was announced", dreamingGirl, sisterOut, knapsackKid)
	fs := w.films()
	if f := fs[199340]; !f.Eligible || !f.WithRequest || f.AfterSeason != 2 {
		t.Fatalf("the film before the new season: %+v, want eligible and on the button", f)
	}
	if saved, _ := w.st.GetRequest(ctx, "tv", 100); fmt.Sprint(saved.FilmSeasons) != "[101291 171046]" {
		t.Fatalf("the request's seasons grew by themselves: %v", saved.FilmSeasons)
	}
	// Asked again, for the new season and naming the film.
	if _, err := w.s.RequestWithFilms(ctx, 100, w.admin, []int{3}, []int{dearFriend}); err != nil {
		t.Fatal(err)
	}
	w.wantAdded("after asking again", dreamingGirl, sisterOut, knapsackKid, dearFriend)
	if saved, _ := w.st.GetRequest(ctx, "tv", 100); fmt.Sprint(saved.FilmSeasons) != "[101291 171046 300000]" {
		t.Fatalf("seasons covered after asking again: %v", saved.FilmSeasons)
	}
}

// No duplicates: a film in the library (found by its TMDB id, another by its AniList
// id), one already requested and one Radarr already holds are not on the button and
// are not requested, by the button or by the background run.
func TestFilmsAlreadyHadAreNotRequested(t *testing.T) {
	ctx := context.Background()
	w := newFilmWorld(t)
	w.graph.Films = append(w.graph.Films, joined(film(170000, "2024-03-01", "Rascal Does Not Dream of a Fifth Film", link(101291, "SEQUEL")), 101291, 171046))
	w.search["Rascal Does Not Dream of a Fifth Film"] = `{"results":[{"id":500,"title":"Rascal Does Not Dream of a Fifth Film","release_date":"2024-03-01","genre_ids":[16]}]}`
	w.movieItem("dg", map[string]string{"tmdb": strconv.Itoa(dreamingGirl)})
	w.movieItem("fifth", map[string]string{"anilist": "170000"})
	if err := w.st.UpsertRequest(ctx, &store.Request{ID: "req-sister", MediaType: "movie", TMDBID: sisterOut, Title: "Sister", UserID: w.admin.UserID, Status: "sent"}); err != nil {
		t.Fatal(err)
	}
	w.inArr[knapsackKid] = true
	w.inArr[dearFriend] = true
	fs := w.films()
	for id, f := range fs {
		if f.WithRequest {
			t.Errorf("film %d is on the button: %+v", id, f)
		}
	}
	if fs[104157].LibraryItemID != "dg" || fs[170000].LibraryItemID != "fifth" || fs[154967].RequestStatus != "sent" || !fs[161474].InRadarr {
		t.Fatalf("states: %+v", fs)
	}
	if _, err := w.s.RequestWithFilms(ctx, 100, w.admin, nil, []int{dreamingGirl, sisterOut, knapsackKid, 500}); err != nil {
		t.Fatal(err)
	}
	// Even for a request that did ask for films, the background run adds none of them.
	saved, _ := w.st.GetRequest(ctx, "tv", 100)
	saved.FilmAuto, saved.FilmSeasons = true, []int{101291, 171046}
	if err := w.st.UpsertRequest(ctx, saved); err != nil {
		t.Fatal(err)
	}
	w.s.AcquireStoryFilms(ctx)
	w.wantAdded("films already had")
	// Radarr's film is Radarr's: no request row was made for it, and it is not held
	// against a later request once Radarr no longer has it.
	if fr, _ := w.st.GetRequest(ctx, "movie", knapsackKid); fr != nil {
		t.Fatalf("a request was saved for a film Radarr already had: %+v", fr)
	}
	w.mu.Lock()
	w.inArr[knapsackKid] = false
	w.mu.Unlock()
	w.s.radarrHas = newTTLCache[bool](2*time.Minute, 500)
	w.s.AcquireStoryFilms(ctx)
	w.wantAdded("once Radarr no longer has it", knapsackKid)
}

// A film TMDB cannot be matched to for certain (two animated films of that name and
// year) is listed and cannot be requested: nobody can say which one Radarr would fetch.
func TestFilmNotMatchedOnTMDBIsOnlyListed(t *testing.T) {
	ctx := context.Background()
	w := newFilmWorld(t)
	w.search["Rascal Does Not Dream of a Dreaming Girl"] = `{"results":[
		{"id":200,"title":"Rascal Does Not Dream of a Dreaming Girl","release_date":"2019-06-15","genre_ids":[16]},
		{"id":299,"title":"Rascal Does Not Dream of a Dreaming Girl","release_date":"2019-09-01","genre_ids":[16]}]}`
	f := w.films()[104157]
	if f.TMDBID != 0 || f.Eligible || f.WithRequest || f.Note == "" || f.AfterSeason != 1 {
		t.Fatalf("%+v, want listed after season 1, not requestable, with a note", f)
	}
	if _, err := w.s.RequestWithFilms(ctx, 100, w.admin, nil, []int{200, 299, sisterOut}); err != nil {
		t.Fatal(err)
	}
	w.wantAdded("with one film unmatched", sisterOut)
}

// A basic user's films wait for approval like the series does: Radarr hears nothing.
func TestBasicUsersFilmsWaitForApproval(t *testing.T) {
	ctx := context.Background()
	w := newFilmWorld(t)
	u := &model.User{ID: "u-guest", Name: "guest", Role: model.RoleBasic}
	if err := w.st.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	r, err := w.s.RequestWithFilms(ctx, 100, &model.Principal{UserID: u.ID, User: u}, nil, []int{dreamingGirl})
	if err != nil || r.Status != "approval" {
		t.Fatalf("%+v, %v", r, err)
	}
	w.wantAdded("a basic user's request")
	if fr, _ := w.st.GetRequest(ctx, "movie", dreamingGirl); fr == nil || fr.Status != "approval" {
		t.Fatalf("film request %+v, want waiting for approval", fr)
	}
	w.s.AcquireStoryFilms(ctx) // the series is not sent: nothing comes by itself
	w.wantAdded("before approval")
}

// Several runs at once: however they interleave, a film is sent once and saved once.
func TestFilmSentOnceUnderConcurrency(t *testing.T) {
	ctx := context.Background()
	w := newFilmWorld(t)
	series := &store.Request{ID: "req-tv", MediaType: "tv", TMDBID: 100, Title: "Bunny Girl Senpai", UserID: w.admin.UserID, UserName: "drewfoos", Status: "sent"}
	if err := w.st.UpsertRequest(ctx, series); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			w.s.requestFilm(ctx, series, dreamingGirl, 104157, nil)
		}()
	}
	close(start)
	wg.Wait()
	w.wantAdded("10 runs at once", dreamingGirl)
	all, _ := w.st.ListRequests(ctx)
	films := 0
	for _, r := range all {
		if r.MediaType == "movie" {
			films++
		}
	}
	if films != 1 {
		t.Fatalf("%d film requests saved, want 1", films)
	}
}

// The request is saved before Radarr is asked (a send can never happen unrecorded);
// when Radarr fails it stays, failed, is not made twice, and is sent once Radarr is
// back (RetryPending). A film request somebody deleted is not made again.
func TestFilmSavedBeforeSendAndRetried(t *testing.T) {
	ctx := context.Background()
	w := newFilmWorld(t)
	series := &store.Request{ID: "req-tv", MediaType: "tv", TMDBID: 100, Title: "Bunny Girl Senpai", UserID: w.admin.UserID, UserName: "drewfoos", Status: "sent",
		FilmAuto: true, FilmSeasons: []int{101291, 171046}}
	if err := w.st.UpsertRequest(ctx, series); err != nil {
		t.Fatal(err)
	}
	w.graph.Films = w.graph.Films[:1]
	savedAtSend := false
	w.onAdd = func() {
		r, err := w.st.GetRequest(ctx, "movie", dreamingGirl)
		savedAtSend = err == nil && r != nil
	}
	w.failRA = true
	w.s.AcquireStoryFilms(ctx)
	if !savedAtSend {
		t.Fatal("Radarr was asked before the request was saved")
	}
	fr, _ := w.st.GetRequest(ctx, "movie", dreamingGirl)
	if fr == nil || fr.Status != "failed" || fr.UserName != "drewfoos" {
		t.Fatalf("after Radarr failed: %+v, want kept as failed, by the series' requester", fr)
	}
	w.s.AcquireStoryFilms(ctx)
	w.mu.Lock()
	w.failRA = false
	w.mu.Unlock()
	w.s.RetryPending(ctx)
	fr, _ = w.st.GetRequest(ctx, "movie", dreamingGirl)
	if fr == nil || fr.Status != "sent" {
		t.Fatalf("after Radarr came back: %+v", fr)
	}
	w.wantAdded("after Radarr came back", dreamingGirl)
	if err := w.st.DeleteRequest(ctx, fr.ID); err != nil {
		t.Fatal(err)
	}
	w.s.AcquireStoryFilms(ctx)
	w.wantAdded("after the film request was deleted", dreamingGirl)
	if f := w.films()[104157]; f.WithRequest {
		t.Fatalf("a film whose request was deleted is back on the button: %+v", f)
	}
}

// Links saved by an older reading of AniList (before films reached through specials
// were followed) are read again at the next look, not kept for the rest of their day.
func TestStoryLinksOfAnOlderReadingAreReadAgain(t *testing.T) {
	w := newFilmWorld(t)
	w.films()
	w.films()
	if w.reads != 1 {
		t.Fatalf("%d reads, want 1: fresh links are kept", w.reads)
	}
	ctx := context.Background()
	var sc storyCache
	if ok, err := w.st.GetSetting(ctx, storyKey(100), &sc); err != nil || !ok {
		t.Fatalf("no saved links: %v", err)
	}
	sc.Read = storyGraphReading - 1
	if err := w.st.SetSetting(ctx, storyKey(100), sc); err != nil {
		t.Fatal(err)
	}
	w.films()
	if w.reads != 2 {
		t.Fatalf("%d reads, want 2", w.reads)
	}
}
