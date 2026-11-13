package metadata

import (
	"context"
	"encoding/json"
	"testing"
)

// A series as AniList links it, kept as stated: two seasons joined only through a run of
// three films (the middle one has no season edge of its own), a film after the last
// season, and a side story. The graph decides nothing; it must lose nothing either.
func TestStoryGraphKeepsTheLinks(t *testing.T) {
	a, _ := fakeAniList(t, map[int]aniNode{
		1: {"TV", "Season 1", 2018, map[string][]int{"SEQUEL": {2}}},
		2: {"MOVIE", "First Film", 2019, map[string][]int{"PREQUEL": {1}, "SEQUEL": {3}}},
		3: {"MOVIE", "Middle Film", 2023, map[string][]int{"PREQUEL": {2}, "SEQUEL": {4}}},
		4: {"MOVIE", "Third Film", 2023, map[string][]int{"PREQUEL": {3}, "SEQUEL": {5}}},
		5: {"TV", "Season 2", 2025, map[string][]int{"PREQUEL": {4}, "SEQUEL": {6}, "SIDE_STORY": {7}, "CHARACTER": {8}}},
		6: {"MOVIE", "Later Film", 2026, map[string][]int{"PREQUEL": {5}}},
		7: {"MOVIE", "Side Film", 2024, map[string][]int{"PARENT": {5}}},
		8: {"MOVIE", "Crossover", 2024, nil},
	})
	g, err := a.StoryGraph(context.Background(), 1, "", 0)
	if err != nil || g == nil {
		t.Fatalf("%v, %v", g, err)
	}
	if len(g.Seasons) != 2 || g.Seasons[0].ID != 1 || g.Seasons[1].ID != 5 {
		t.Fatalf("seasons %+v, want 1 then 5", g.Seasons)
	}
	got := map[int]string{}
	for _, f := range g.Films {
		b, _ := json.Marshal(struct {
			L []StoryLink
			P []StoryPath
		}{f.Links, f.Paths})
		got[f.ID] = string(b)
	}
	want := map[int]string{
		2: `{"L":[{"season":1,"relation":"SEQUEL"}],"P":[{"after":1,"before":5}]}`,
		3: `{"L":null,"P":[{"after":1,"before":5}]}`,
		4: `{"L":[{"season":5,"relation":"PREQUEL"}],"P":[{"after":1,"before":5}]}`,
		6: `{"L":[{"season":5,"relation":"SEQUEL"}],"P":null}`,
		7: `{"L":[{"season":5,"relation":"SIDE_STORY"}],"P":null}`,
	}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("film %d: %s, want %s", id, got[id], w)
		}
	}
	if _, ok := got[8]; ok || len(got) != len(want) {
		t.Errorf("films %v: a crossover says nothing about the story and is left out", got)
	}
}

// The Quintessential Quintuplets: the film that ends the story hangs off a special, not
// off the second season (season 2 -> "Specials" -> the film), and the film's own sequel
// is another special. The chain walk takes TV entries and films only, so the graph had
// no film at all. The film is season 2's sequel, with the way there kept; a film a side
// story OVA leads to is not, and a link the season states itself is not doubled.
func TestStoryGraphFollowsSpecialsToTheFilm(t *testing.T) {
	const s1, s2, specials, movie, specials2, ova, ovaFilm, own = 1, 2, 3, 4, 5, 6, 7, 8
	a, calls := fakeAniList(t, map[int]aniNode{
		s1:        {"TV", "Quintuplets", 2019, map[string][]int{"SEQUEL": {s2}}},
		s2:        {"TV", "Quintuplets 2", 2021, map[string][]int{"PREQUEL": {s1}, "SEQUEL": {specials, own}, "SIDE_STORY": {ova}}},
		specials:  {"SPECIAL", "Specials", 2023, map[string][]int{"PREQUEL": {s2}, "SEQUEL": {movie, own}}},
		movie:     {"MOVIE", "The Movie", 2022, map[string][]int{"PREQUEL": {specials}, "SEQUEL": {specials2}}},
		specials2: {"SPECIAL", "Specials 2", 2024, map[string][]int{"PREQUEL": {movie}}},
		ova:       {"OVA", "Honeymoon", 2025, map[string][]int{"PARENT": {s2}, "SEQUEL": {ovaFilm}}},
		ovaFilm:   {"MOVIE", "Honeymoon Film", 2026, nil},
		own:       {"MOVIE", "Named By The Season", 2022, nil},
	})
	g, err := a.StoryGraph(context.Background(), s1, "", 0)
	if err != nil || g == nil {
		t.Fatalf("%v, %v", g, err)
	}
	if len(g.Seasons) != 2 {
		t.Fatalf("seasons %+v, want the two TV entries", g.Seasons)
	}
	got := map[int]string{}
	for _, f := range g.Films {
		b, _ := json.Marshal(f.Links)
		got[f.ID] = f.Title + " " + string(b)
	}
	want := map[int]string{
		movie: `The Movie [{"season":2,"relation":"SEQUEL","via":[3]}]`,
		own:   `Named By The Season [{"season":2,"relation":"SEQUEL"}]`,
	}
	if len(got) != len(want) || got[movie] != want[movie] || got[own] != want[own] {
		t.Errorf("films %v, want %v", got, want)
	}
	// Read: the two seasons, the film season 2 names (the chain walk looks past it for
	// a third season) and one special. The side story OVA is not followed.
	if *calls != 4 {
		t.Errorf("%d queries, want 4", *calls)
	}
}
