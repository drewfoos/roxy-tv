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
