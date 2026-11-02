package segments

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func res(typ string, st, en, length float64) aniskipResult {
	var r aniskipResult
	r.SkipType, r.Interval.StartTime, r.Interval.EndTime, r.EpisodeLength = typ, st, en, length
	return r
}

func TestAniSkipSegmentsFitsTheFile(t *testing.T) {
	// KonoSuba S1E1 as AniSkip answers it for a 1420 s file.
	got := aniskipSegments([]aniskipResult{
		res("op", 60.694, 150.694, 1420),
		res("ed", 1323.173, 1413.173, 1420.08),
	}, 1420)
	if len(got) != 3 {
		t.Fatalf("want intro, outro, preview; got %+v", got)
	}
	if got[0].Type != "intro" || got[0].StartMs != 60694 || got[0].EndMs != 150694 || got[0].Origin != AniSkipOrigin {
		t.Errorf("intro %+v", got[0])
	}
	if got[1].Type != "outro" || got[1].StartMs != 1323173 || got[1].EndMs != 1413173 {
		t.Errorf("outro %+v", got[1])
	}
	if got[2].Type != "preview" || got[2].StartMs != 1413173 || got[2].EndMs != 1420000 {
		t.Errorf("preview %+v", got[2])
	}
}

func TestAniSkipSegmentsRejectsOtherCuts(t *testing.T) {
	// Submitted on a file 93 s longer (another release): its times would be off.
	if got := aniskipSegments([]aniskipResult{res("op", 60, 150, 1513)}, 1420); len(got) != 0 {
		t.Fatalf("want nothing, got %+v", got)
	}
	// No length on the submission: cannot be checked, so not used.
	if got := aniskipSegments([]aniskipResult{res("op", 60, 150, 0)}, 1420); len(got) != 0 {
		t.Fatalf("want nothing, got %+v", got)
	}
}

func TestAniSkipSegmentsSnapsAndFilters(t *testing.T) {
	got := aniskipSegments([]aniskipResult{
		res("mixed-op", 3, 93, 1440),     // starts in the first seconds: from 0
		res("op", 200, 290, 1440),        // a second intro: the first one wins
		res("ed", 1350, 1437, 1440),      // ends 3 s before the end: to the end, no preview
		res("recap", 0, 4, 1440),         // kept as-is
		res("op-unknown", 10, 100, 1440), // unknown type: ignored
	}, 1440)
	want := map[string][2]int64{"intro": {0, 93000}, "outro": {1350000, 1440000}, "recap": {0, 4000}}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for _, g := range got {
		w, ok := want[g.Type]
		if !ok || g.StartMs != w[0] || g.EndMs != w[1] {
			t.Errorf("%s: got %d-%d, want %v", g.Type, g.StartMs, g.EndMs, w)
		}
	}
	// Implausible lengths are dropped (a 5 s "opening", a 10 minute "ending").
	if got := aniskipSegments([]aniskipResult{res("op", 10, 15, 1440), res("ed", 600, 1200, 1440)}, 1440); len(got) != 0 {
		t.Fatalf("want nothing, got %+v", got)
	}
}

func TestAniSkipLookup(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		if r.URL.Path == "/skip-times/1/99" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"found":false,"results":[],"statusCode":404}`))
			return
		}
		_, _ = w.Write([]byte(`{"found":true,"results":[{"interval":{"startTime":0.5,"endTime":90.5},"skipType":"op","episodeLength":1430}],"statusCode":200}`))
	}))
	defer srv.Close()
	a := NewAniSkip()
	a.base = srv.URL

	segs, err := a.Lookup(context.Background(), "30831", 3, 1430.4)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/skip-times/30831/3" {
		t.Errorf("path %s", gotPath)
	}
	if want := "episodeLength=1430&types%5B%5D=op&types%5B%5D=ed&types%5B%5D=mixed-op&types%5B%5D=mixed-ed&types%5B%5D=recap"; gotQuery != want {
		t.Errorf("query %s", gotQuery)
	}
	if len(segs) != 1 || segs[0].Type != "intro" || segs[0].StartMs != 0 || segs[0].EndMs != 90500 {
		t.Errorf("segments %+v", segs)
	}

	segs, err = a.Lookup(context.Background(), "1", 99, 1430)
	if err != nil || segs != nil {
		t.Errorf("unknown episode: %+v, %v", segs, err)
	}
}
