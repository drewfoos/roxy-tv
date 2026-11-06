package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Only a body that really asks for everything (none, or empty lists) may mean "every
// missing episode"; anything malformed is refused before any search or replacement runs.
func TestSearchBody(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		ok         bool
		episodes   []string
		seasons    []int
	}{
		{"no body: everything missing", "", true, nil, nil},
		{"empty lists: everything missing", `{"episodes": [], "seasons": []}`, true, []string{}, []int{}},
		{"one episode", `{"episodes": ["S1E3"]}`, true, []string{"S1E3"}, nil},
		{"zero-padded and lower case, written as the service compares", `{"episodes": [" s01e03 "]}`, true, []string{"S1E3"}, nil},
		{"a season", `{"seasons": [2]}`, true, nil, []int{2}},
		{"specials", `{"seasons": [0]}`, true, nil, []int{0}},
		{"malformed JSON", `{"episodes": ["S1E3"`, false, nil, nil},
		{"wrong type", `{"episodes": "S1E3"}`, false, nil, nil},
		{"blank code (was dropped, leaving everything)", `{"episodes": [""]}`, false, nil, nil},
		{"not a code", `{"episodes": ["episode 3"]}`, false, nil, nil},
		{"negative season", `{"seasons": [-1]}`, false, nil, nil},
		{"two JSON values", `{"episodes": ["S1E3"]} {"episodes": []}`, false, nil, nil},
		{"too many episodes (was cut off silently)", `{"episodes": [` + strings.Repeat(`"S1E1",`, 200) + `"S1E1"]}`, false, nil, nil},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(tc.body))
		got, ok := searchBody(rec, req)
		if ok != tc.ok {
			t.Errorf("%s: ok = %v (status %d %s), want %v", tc.name, ok, rec.Code, rec.Body.String(), tc.ok)
			continue
		}
		if !ok {
			if rec.Code != 400 {
				t.Errorf("%s: status %d, want 400", tc.name, rec.Code)
			}
			continue
		}
		if strings.Join(got.Episodes, ",") != strings.Join(tc.episodes, ",") || len(got.Seasons) != len(tc.seasons) {
			t.Errorf("%s: got %+v", tc.name, got)
		}
		for i := range tc.seasons {
			if got.Seasons[i] != tc.seasons[i] {
				t.Errorf("%s: seasons %v", tc.name, got.Seasons)
			}
		}
	}
}
