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
