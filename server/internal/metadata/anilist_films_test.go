package metadata

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"
)

// fakeAniList answers by-id queries from a graph of entries: id -> (format, title, year,
// edges as relation type -> ids).
type aniNode struct {
	format, title string
	year          int
	edges         map[string][]int
}

func fakeAniList(t *testing.T, nodes map[int]aniNode) (*AniList, *int) {
	t.Helper()
	calls := 0
	media := func(id int) map[string]any {
		n := nodes[id]
		var edges []map[string]any
		for rel, ids := range n.edges {
			for _, to := range ids {
				m := nodes[to]
				edges = append(edges, map[string]any{"relationType": rel, "node": map[string]any{
					"id": to, "type": "ANIME", "format": m.format, "title": map[string]any{"english": m.title}, "startDate": map[string]any{"year": m.year}, "coverImage": map[string]any{},
				}})
			}
		}
		eps := 13
		return map[string]any{"id": id, "format": n.format, "title": map[string]any{"english": n.title}, "startDate": map[string]any{"year": n.year},
			"status": "FINISHED", "episodes": eps, "relations": map[string]any{"edges": edges}}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req struct {
			Variables map[string]any `json:"variables"`
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &req)
		id := int(req.Variables["id"].(float64))
		if _, ok := nodes[id]; !ok {
			t.Errorf("asked for unknown id %d", id)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"Media": media(id)}})
	}))
	t.Cleanup(srv.Close)
	a := NewAniList()
	a.endpoint = srv.URL
	a.lim = newLimiter(1000, time.Second) // a fake server: no need to pace
	return a, &calls
}

// Bunny Girl Senpai: AniList links Season 1 to Season 2 only through three films
// (Dreaming Girl, Sister Venturing Out, Knapsack Kid). The chain walk went through TV
// entries only, so it stopped at Season 1 and knew one film. It now passes through the
// films: both seasons, all three films marked as story; a side-story film and a recap
// are related but not story.
func TestChainWalksThroughStoryFilms(t *testing.T) {
	const s1, dream, sister, knap, s2, side, recap = 1, 2, 3, 4, 5, 6, 7
	nodes := map[int]aniNode{
		s1:     {"TV", "Bunny Girl Senpai", 2018, map[string][]int{"SEQUEL": {dream}, "SIDE_STORY": {side}, "SUMMARY": {recap}}},
		dream:  {"MOVIE", "Dreaming Girl", 2019, map[string][]int{"PREQUEL": {s1}, "SEQUEL": {sister}}},
		sister: {"MOVIE", "Sister Venturing Out", 2023, map[string][]int{"PREQUEL": {dream}, "SEQUEL": {knap}}},
		knap:   {"MOVIE", "Knapsack Kid", 2023, map[string][]int{"PREQUEL": {sister}, "SEQUEL": {s2}}},
		s2:     {"TV", "Santa Claus", 2025, map[string][]int{"PREQUEL": {knap}}},
		side:   {"MOVIE", "Beach Side Story", 2020, map[string][]int{"PARENT": {s1}}},
		recap:  {"SPECIAL", "Recap", 2018, nil},
	}
	for _, from := range []int{s1, s2} { // from either season the same result
		a, _ := fakeAniList(t, nodes)
		start, err := a.query(context.Background(), anilistByIDQuery, map[string]any{"id": from})
		if err != nil {
			t.Fatal(err)
		}
		r, err := a.followChain(context.Background(), start.Data.Media)
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Seasons) != 2 {
			t.Fatalf("from %d: %d seasons, want 2 (through the films)", from, len(r.Seasons))
		}
		var story, other []string
		for _, rel := range r.Related {
			if rel.Story {
				story = append(story, rel.Title)
			} else {
				other = append(other, rel.Title)
			}
		}
		sort.Strings(story)
		sort.Strings(other)
		if got := fmtList(story); got != "Dreaming Girl, Knapsack Kid, Sister Venturing Out" {
			t.Errorf("from %d: story films %s", from, got)
		}
		if got := fmtList(other); got != "Beach Side Story, Recap" {
			t.Errorf("from %d: other relations %s", from, got)
		}
	}
}

// A sequel film after the last season (no further TV) is story; the walk through films
// stops after a few hops and never loops.
func TestChainStoryFilmAfterTheLastSeason(t *testing.T) {
	const s1, film, film2 = 1, 2, 3
	nodes := map[int]aniNode{
		s1:    {"TV", "Show", 2016, map[string][]int{"SEQUEL": {film}}},
		film:  {"MOVIE", "Show: The Movie", 2019, map[string][]int{"PREQUEL": {s1}, "SEQUEL": {film2}}},
		film2: {"MOVIE", "Show: Movie 2", 2021, map[string][]int{"PREQUEL": {film}, "SEQUEL": {film}}}, // a loop back
	}
	a, calls := fakeAniList(t, nodes)
	start, _ := a.query(context.Background(), anilistByIDQuery, map[string]any{"id": s1})
	r, err := a.followChain(context.Background(), start.Data.Media)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Seasons) != 1 || *calls > 6 {
		t.Fatalf("%d seasons, %d calls", len(r.Seasons), *calls)
	}
	n := 0
	for _, rel := range r.Related {
		if rel.Story {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("%d story films, want 2: %+v", n, r.Related)
	}
}

func fmtList(xs []string) string {
	out := ""
	for i, x := range xs {
		if i > 0 {
			out += ", "
		}
		out += x
	}
	return out
}
