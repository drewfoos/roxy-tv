package discover

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/roxy-media/roxy/server/internal/arr"
	"github.com/roxy-media/roxy/server/internal/store"
)

// A pack has to beat Sonarr's own best: its best pack and its weakest episode's best
// single. Dubbed singles everywhere beat a sub-only pack; a mix with a sub-only episode
// does not.
func TestOwnBest(t *testing.T) {
	rels := []arr.SeasonRelease{
		{Title: "big pack", FullSeason: true, Score: 300, Approved: false},
		{Title: "ok pack", FullSeason: true, Score: 100, Approved: true},
		{Episodes: []int{1}, Score: 260, Approved: true},
		{Episodes: []int{2}, Score: 0, Approved: true},
		{Episodes: []int{2}, Score: 260, Approved: false},
	}
	pack, from, single := ownBest(rels, []int{1, 2})
	if pack != 100 || from != "ok pack" || single != 0 {
		t.Fatalf("got pack %d %q single %d", pack, from, single)
	}
	if _, _, single := ownBest(rels, []int{1, 2, 3}); single != -1<<31 {
		t.Fatalf("an episode with no release: single %d", single)
	}
}

func TestSeasonFloor(t *testing.T) {
	now := time.Now()
	past := now.Add(-48 * time.Hour)
	eps := []arr.Episode{{ID: 1, SeasonNumber: 1, HasFile: true, AirDateUTC: &past}, {ID: 2, SeasonNumber: 1, HasFile: true, AirDateUTC: &past}}
	if f, ok := seasonFloor(eps, map[int]int{1: 540, 2: 100}, now); !ok || f != 100 {
		t.Fatalf("floor %d %v", f, ok)
	}
	if _, ok := seasonFloor(eps, map[int]int{1: 540}, now); ok {
		t.Fatal("an episode without a grab must leave the season alone")
	}
	eps[1].HasFile = false
	if _, ok := seasonFloor(eps, map[int]int{1: 540, 2: 100}, now); ok {
		t.Fatal("a missing file is the tracker's job")
	}
}

// End to end against fake Sonarr and Prowlarr: the "(Season 1)" dub pack beats Sonarr's
// mixed singles and is pushed as "S01"; with dubbed singles everywhere, nothing is pushed.
func TestFindPackPushesUnreadablePack(t *testing.T) {
	for _, tc := range []struct {
		name       string
		singleE2   int
		wantPushed bool
	}{
		{"mixed singles", 0, true},
		{"dub singles everywhere", 600, false},
	} {
		var pushed []string
		sonarr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v3/episode":
				w.Write([]byte(`[{"id":1,"seasonNumber":1,"episodeNumber":1,"airDateUtc":"2024-07-03T00:00:00Z","monitored":true},{"id":2,"seasonNumber":1,"episodeNumber":2,"airDateUtc":"2024-07-10T00:00:00Z","monitored":true}]`))
			case "/api/v3/series/35":
				w.Write([]byte(`{"title":"Alya Sometimes Hides Her Feelings in Russian","alternateTitles":[{"title":"Tokidoki Bosotto Russia-go de Dereru Tonari no Alya-san"}]}`))
			case "/api/v3/parse":
				title := r.URL.Query().Get("title")
				score := 100
				if strings.Contains(title, "Dual Audio") {
					score = 540
				}
				json.NewEncoder(w).Encode(map[string]any{"series": map[string]any{"id": 35}, "parsedEpisodeInfo": map[string]any{"seasonNumber": 1, "fullSeason": true}, "customFormatScore": score})
			case "/api/v3/release":
				json.NewEncoder(w).Encode([]map[string]any{
					{"title": "huge dub pack", "fullSeason": true, "approved": false, "customFormatScore": 300},
					{"title": "e1", "episodeNumbers": []int{1}, "approved": true, "customFormatScore": 600},
					{"title": "e2", "episodeNumbers": []int{2}, "approved": true, "customFormatScore": tc.singleE2},
				})
			case "/api/v3/release/push":
				var b map[string]any
				json.NewDecoder(r.Body).Decode(&b)
				pushed = append(pushed, b["title"].(string))
				w.Write([]byte(`[{"approved":true}]`))
			default:
				t.Errorf("unexpected %s", r.URL.Path)
			}
		}))
		prowlarr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode([]map[string]any{
				{"title": "[EMBER] Alya Sometimes Hides Her Feelings in Russian (2024) (Season 1) [BDRip] [1080p Dual Audio HEVC 10 bits DDP] (Batch)", "seeders": 120, "magnetUrl": "magnet:?xt=urn:btih:abc", "size": 4800000000},
				{"title": "[Other] Alya Sometimes Hides Her Feelings in Russian (Season 1) [1080p]", "seeders": 50, "magnetUrl": "magnet:?xt=urn:btih:def"},
				{"title": "Alya Sometimes Hides Her Feelings in Russian S01 1080p", "seeders": 10},
			})
		}))
		c := arr.New("sonarr", arr.Config{URL: sonarr.URL, APIKey: "k"})
		s := newTestService()
		s.prowlarr = arr.NewProwlarr(prowlarr.URL, "k")
		got, err := s.findPack(context.Background(), c, 35, 1, nil)
		sonarr.Close()
		prowlarr.Close()
		if err != nil || got != tc.wantPushed {
			t.Fatalf("%s: pushed %v err %v (%v)", tc.name, got, err, pushed)
		}
		if tc.wantPushed && (len(pushed) != 1 || !strings.Contains(pushed[0], "(2024) S01 [BDRip]")) {
			t.Fatalf("%s: pushed %v, want the EMBER pack renamed to S01", tc.name, pushed)
		}
	}
}

// packFakes is a fake Sonarr holding one finished, sub-only season (every grab scored
// 100, the profile's cutoff is 220) and a fake Prowlarr with a "(Season 1)" dub pack.
// releaseFails makes Sonarr's own search of the season fail.
type packFakes struct {
	sonarr, prowlarr *httptest.Server
	releaseFails     atomic.Bool
	searches, pushes atomic.Int32
}

func newPackFakes(t *testing.T) *packFakes {
	f := &packFakes{}
	f.sonarr = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/episode":
			w.Write([]byte(`[{"id":1,"seasonNumber":1,"episodeNumber":1,"airDateUtc":"2024-07-03T00:00:00Z","monitored":true,"hasFile":true},{"id":2,"seasonNumber":1,"episodeNumber":2,"airDateUtc":"2024-07-10T00:00:00Z","monitored":true,"hasFile":true}]`))
		case "/api/v3/history/series":
			w.Write([]byte(`[{"id":1,"episodeId":1,"eventType":"grabbed","date":"2024-07-04T00:00:00Z","customFormatScore":100},{"id":2,"episodeId":2,"eventType":"grabbed","date":"2024-07-11T00:00:00Z","customFormatScore":100}]`))
		case "/api/v3/series/35":
			w.Write([]byte(`{"title":"Alya Sometimes Hides Her Feelings in Russian","qualityProfileId":4}`))
		case "/api/v3/qualityprofile/4":
			w.Write([]byte(`{"cutoffFormatScore":220}`))
		case "/api/v3/parse":
			json.NewEncoder(w).Encode(map[string]any{"series": map[string]any{"id": 35}, "parsedEpisodeInfo": map[string]any{"seasonNumber": 1, "fullSeason": true}, "customFormatScore": 540})
		case "/api/v3/release":
			f.searches.Add(1)
			if f.releaseFails.Load() {
				http.Error(w, "indexers unavailable", http.StatusServiceUnavailable)
				return
			}
			w.Write([]byte(`[]`))
		case "/api/v3/release/push":
			f.pushes.Add(1)
			w.Write([]byte(`[{"approved":true}]`))
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	}))
	f.prowlarr = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]any{
			{"title": "[EMBER] Alya Sometimes Hides Her Feelings in Russian (2024) (Season 1) [1080p Dual Audio HEVC]", "seeders": 120, "magnetUrl": "magnet:?xt=urn:btih:abc"},
		})
	}))
	t.Cleanup(f.sonarr.Close)
	t.Cleanup(f.prowlarr.Close)
	return f
}

func (f *packFakes) service(st *store.Store) (*Service, *arr.Client) {
	s := newTestService()
	s.store = st
	s.prowlarr = arr.NewProwlarr(f.prowlarr.URL, "k")
	return s, arr.New("sonarr", arr.Config{URL: f.sonarr.URL, APIKey: "k"})
}

// Sonarr's own search of the season is the bar a pack has to beat. When that search
// failed there was no bar, so any pack with seeders was pushed (a sub-only one too) and
// the caller then skipped Sonarr's search for the season.
func TestFindPackPushesNothingWithoutSonarrsSearch(t *testing.T) {
	f := newPackFakes(t)
	f.releaseFails.Store(true)
	s, c := f.service(nil)
	pushed, err := s.findPack(context.Background(), c, 35, 1, nil)
	if pushed || err == nil || f.pushes.Load() != 0 {
		t.Fatalf("pushed %v err %v pushes %d; want no push and an error", pushed, err, f.pushes.Load())
	}
	// The same season with the search working: the pack is pushed.
	f.releaseFails.Store(false)
	if pushed, err := s.findPack(context.Background(), c, 35, 1, nil); !pushed || err != nil || f.pushes.Load() != 1 {
		t.Fatalf("with the search working: pushed %v err %v pushes %d", pushed, err, f.pushes.Load())
	}
}

// The once-a-day look per season lived in memory only, and the sweep ran 12 hours after
// start: a server restarted daily never swept. The time of a look is saved now.
func TestSweepLookIsRememberedAcrossRestarts(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	f := newPackFakes(t)
	all := func(arr.Episode) bool { return true }
	now := time.Now()

	// A look that fails is not counted: the next sweep (same day) looks again.
	f.releaseFails.Store(true)
	s, c := f.service(st)
	s.sweepRequest(ctx, c, 35, all, now)
	if f.searches.Load() != 1 || f.pushes.Load() != 0 {
		t.Fatalf("failed look: searches %d pushes %d", f.searches.Load(), f.pushes.Load())
	}
	f.releaseFails.Store(false)
	s.sweepRequest(ctx, c, 35, all, now.Add(time.Hour))
	if f.searches.Load() != 2 || f.pushes.Load() != 1 {
		t.Fatalf("look after a failed one: searches %d pushes %d, want 2 and 1", f.searches.Load(), f.pushes.Load())
	}

	// "Restart": a new service on the same database does not look again within the day...
	s2, c2 := f.service(st)
	s2.sweepRequest(ctx, c2, 35, all, now.Add(2*time.Hour))
	if f.searches.Load() != 2 {
		t.Fatalf("looked again after a restart: searches %d", f.searches.Load())
	}
	// ...and does once the day has passed.
	s3, c3 := f.service(st)
	s3.sweepRequest(ctx, c3, 35, all, now.Add(time.Hour+packRecheck+time.Minute))
	if f.searches.Load() != 3 {
		t.Fatalf("no look a day later: searches %d", f.searches.Load())
	}
}

// The first sweep comes soon after start, not one whole interval later.
func TestFirstSweepIsSoonAfterStart(t *testing.T) {
	if packSweepFirst >= packSweepEvery/4 {
		t.Fatalf("first sweep after %v, every %v", packSweepFirst, packSweepEvery)
	}
}
