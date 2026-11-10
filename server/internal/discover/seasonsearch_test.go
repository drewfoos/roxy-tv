package discover

import (
	"reflect"
	"testing"
)

func TestSplitSeasonSearches(t *testing.T) {
	seasons := map[int]*seasonTally{
		1: {aired: 12, ids: []int{11, 12, 13}},                             // partly missing: singles
		2: {aired: 10, ids: []int{21, 22, 23, 24, 25, 26, 27, 28, 29, 30}}, // all missing: whole
		3: {aired: 1, ids: []int{31}},                                      // one episode: single
		0: {aired: 2, ids: []int{1, 2}},                                    // specials: never a SeasonSearch
	}
	whole, rest := splitSeasonSearches(seasons)
	if !reflect.DeepEqual(whole, []int{2}) {
		t.Errorf("whole = %v, want [2]", whole)
	}
	if !reflect.DeepEqual(rest, []int{1, 2, 11, 12, 13, 31}) {
		t.Errorf("rest = %v", rest)
	}
	// A whole season where one episode is busy (in the client) is not searched whole.
	busy := map[int]*seasonTally{2: {aired: 3, ids: []int{21, 22}}}
	if w, r := splitSeasonSearches(busy); len(w) != 0 || !reflect.DeepEqual(r, []int{21, 22}) {
		t.Errorf("busy season: whole=%v rest=%v", w, r)
	}
}

// A new series with specials: asking for "everything" is a whole-series request (later
// seasons get monitored). The Specials entry used to count as a season the request did
// not cover, so the request was narrowed to the seasons known today.
func TestSeasonsToRequest(t *testing.T) {
	specials := Season{Number: 0, Episodes: 3, Special: true}
	s1 := Season{Number: 1, Episodes: 12}
	s2 := Season{Number: 2, Episodes: 12}
	have1 := Season{Number: 1, Episodes: 12, Have: 12, Known: 12}
	for _, tc := range []struct {
		name    string
		all     []Season
		asked   []int
		want    []int
		wantErr bool
	}{
		{"new series with specials", []Season{s1, s2, specials}, nil, nil, false},
		{"new series without specials", []Season{s1, s2}, nil, nil, false},
		{"one season in the library", []Season{have1, s2, specials}, nil, []int{2}, false},
		{"all in the library", []Season{have1, specials}, nil, nil, true},
		{"asked season, missing", []Season{have1, s2, specials}, []int{2}, []int{2}, false},
		{"asked season, in the library", []Season{have1, s2, specials}, []int{1}, nil, true},
		{"specials as a season", []Season{have1, specials}, []int{0}, []int{0}, false},
		{"specials asked, none exist", []Season{have1, s2}, []int{0}, nil, true},
		{"nothing known about seasons", nil, []int{3}, []int{3}, false},
	} {
		got, err := seasonsToRequest(tc.all, tc.asked)
		if (err != nil) != tc.wantErr || len(got) != len(tc.want) {
			t.Fatalf("%s: got %v err %v, want %v (error %v)", tc.name, got, err, tc.want, tc.wantErr)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("%s: got %v, want %v", tc.name, got, tc.want)
			}
		}
	}
}
