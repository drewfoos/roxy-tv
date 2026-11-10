package arr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func special(id, n int, title, date string) Episode {
	return Episode{ID: id, SeasonNumber: 0, EpisodeNumber: n, Title: title, AirDate: date}
}

// Sonarr numbers specials as TVDB does, requests as TMDB does. The shape below is a real
// one: TVDB lists seven shorts first, so TMDB's first OVA is Sonarr's eighth special.
func TestRenumberSpecials(t *testing.T) {
	eps := []Episode{
		{ID: 100, SeasonNumber: 1, EpisodeNumber: 1, Title: "Pilot"},
		special(1, 1, "Short 1", "2012-03-21"),
		special(2, 2, "Short 2", "2012-04-25"),
		special(8, 8, "I'm Harvesting Breasts!", "2012-09-06"),
		special(9, 9, "Special 9", "2013-05-31"),
		special(10, 10, "Unrelated Recap", "2014-01-01"),
		special(11, 3, "Never On The Other List", "2015-01-01"),
	}
	refs := []EpisodeRef{
		{Number: 1, Title: "I'm Harvesting Breasts!", AirDate: "2012-09-06"}, // by title -> Sonarr 8
		{Number: 2, Title: "OVA 2", AirDate: "2013-05-31"},                   // generic title, by date -> Sonarr 9
		{Number: 3, Title: "Something TVDB Lacks", AirDate: "2020-02-02"},    // no title, no date; Sonarr's 3 is free -> by number
		{Number: 10, Title: "Unrelated Recap"},                               // same number both sides
	}
	got := map[int]int{} // Sonarr id -> number now
	for _, e := range RenumberSpecials(eps, refs) {
		if e.SeasonNumber == 0 {
			got[e.ID] = e.EpisodeNumber
		} else if e.EpisodeNumber != 1 {
			t.Fatalf("a regular episode was renumbered: %+v", e)
		}
	}
	want := map[int]int{
		8:  1,  // title
		9:  2,  // date
		11: 3,  // number, nothing better known
		10: 10, // title (and number)
		1:  11, // Sonarr's short 1: no ref, and ref 1 is another special -> past the end
		2:  12, // likewise
	}
	for id, n := range want {
		if got[id] != n {
			t.Errorf("Sonarr special id %d is number %d, want %d (all: %v)", id, got[id], n, got)
		}
	}
	// Nothing known about the other numbering: Sonarr's own stands.
	if same := RenumberSpecials(eps, nil); same[3].EpisodeNumber != 8 {
		t.Fatalf("without refs: %+v", same[3])
	}
	// Two specials on one day, or two with one title, are not paired by that.
	twins := []Episode{special(1, 1, "Recap", "2020-01-01"), special(2, 2, "Recap", "2020-01-01")}
	out := RenumberSpecials(twins, []EpisodeRef{{Number: 2, Title: "Recap", AirDate: "2020-01-01"}})
	if out[0].EpisodeNumber != 1 || out[1].EpisodeNumber != 2 {
		t.Fatalf("ambiguous title and date must fall back to the number: %+v", out)
	}
	// The input is not changed in place (it may be shared).
	if eps[3].EpisodeNumber != 8 {
		t.Fatal("the caller's list was changed")
	}
}

// A special asked for by its TMDB number is monitored and searched as the Sonarr episode
// it is, not as the Sonarr episode that happens to have that number.
func TestMonitorEpisodesBySpecialIdentity(t *testing.T) {
	var monitored, searched []any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/episode":
			w.Write([]byte(`[
				{"id":501,"seasonNumber":0,"episodeNumber":1,"title":"Fantasy Short 1","airDate":"2012-03-21"},
				{"id":508,"seasonNumber":0,"episodeNumber":8,"title":"The First OVA","airDate":"2012-09-06"},
				{"id":509,"seasonNumber":0,"episodeNumber":9,"title":"The Second OVA","airDate":"2013-05-31","hasFile":true},
				{"id":601,"seasonNumber":1,"episodeNumber":1,"title":"One"}]`))
		case r.Method == http.MethodPut && r.URL.Path == "/api/v3/episode/monitor":
			monitored, _ = body["episodeIds"].([]any)
			w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v3/command":
			searched, _ = body["episodeIds"].([]any)
			w.Write([]byte(`{"id":1}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	c := New("sonarr", Config{URL: srv.URL, APIKey: "k"})
	refs := []EpisodeRef{
		{Number: 1, Title: "The First OVA", AirDate: "2012-09-06"},
		{Number: 2, Title: "The Second OVA", AirDate: "2013-05-31"},
	}
	// S0E1 is the first OVA (Sonarr id 508, its eighth special); S0E2 is in the library.
	if err := c.MonitorEpisodes(context.Background(), 7, []string{"S0E1", "s0e2", "S1E1"}, refs); err != nil {
		t.Fatal(err)
	}
	if len(monitored) != 2 || monitored[0] != float64(508) || monitored[1] != float64(601) {
		t.Fatalf("monitored %v, want [508 601]", monitored)
	}
	if len(searched) != 2 || searched[0] != float64(508) {
		t.Fatalf("searched %v, want [508 601]", searched)
	}
}
