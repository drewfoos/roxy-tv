package metadata

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/roxy-media/roxy/server/internal/model"
)

func TestAniListMediaResult(t *testing.T) {
	var resp anilistResponse
	fixture(t, "anilist_search.json", &resp)
	if resp.Data.Page == nil || len(resp.Data.Page.Media) != 2 {
		t.Fatal("fixture did not decode")
	}
	a := NewAniList()
	r := a.mediaResult(&resp.Data.Page.Media[0])

	if r.Provider != "anilist" || r.ID != "21202" || r.ProviderIDs["anilist"] != "21202" || r.ProviderIDs["mal"] != "30831" {
		t.Errorf("ids = %s %v", r.ID, r.ProviderIDs)
	}
	if r.Title != "KONOSUBA -God's blessing on this wonderful world!" || r.OriginalTitle != "Kono Subarashii Sekai ni Shukufuku wo!" {
		t.Errorf("titles = %q / %q", r.Title, r.OriginalTitle)
	}
	wantOverview := "Kazuma Satou died a laughable death.\n\nNow he must fight. & more.\n(Source: Crunchyroll)"
	if r.Overview != wantOverview {
		t.Errorf("overview = %q, want %q", r.Overview, wantOverview)
	}
	if r.CommunityRating != 8.1 || r.Year != 2016 || r.Premiered != "2016-01-14" || r.Status != "Ended" || r.Studio != "Studio Deen" {
		t.Errorf("rating/year/premiered/status/studio = %v/%d/%q/%q/%q", r.CommunityRating, r.Year, r.Premiered, r.Status, r.Studio)
	}
	if len(r.Genres) != 3 || r.Genres[0] != "Adventure" {
		t.Errorf("genres = %v", r.Genres)
	}
	if r.Images["poster"] != "https://img.example/cover-xl.png" || r.Images["backdrop"] != "https://img.example/banner.jpg" {
		t.Errorf("images = %v", r.Images)
	}
	if r.EpisodeCount != 10 || len(r.Seasons) != 1 || r.Seasons[0].EpisodeCount != 10 {
		t.Errorf("episodes = %d, seasons = %+v", r.EpisodeCount, r.Seasons)
	}

	// Nulls everywhere must not blow up and must leave fields empty.
	r2 := a.mediaResult(&resp.Data.Page.Media[1])
	if r2.Title != "WWW.Working!!" || r2.CommunityRating != 0 || r2.Studio != "" || r2.Premiered != "2015" || r2.Status != "Continuing" || r2.EpisodeCount != 0 {
		t.Errorf("sparse media = %+v", r2)
	}
	if _, ok := r2.ProviderIDs["mal"]; ok {
		t.Error("null idMal must not produce a mal id")
	}
	if _, ok := r2.Images["backdrop"]; ok {
		t.Error("null bannerImage must not produce a backdrop")
	}
}

func TestAniListSeasonResult(t *testing.T) {
	var resp anilistResponse
	fixture(t, "anilist_search.json", &resp)
	se := NewAniList().seasonResult(&resp.Data.Page.Media[0], 1)
	if len(se.Episodes) != 2 {
		t.Fatalf("episodes = %+v (duplicates and unnumbered entries must be dropped)", se.Episodes)
	}
	if e := se.Episodes[0]; e.Episode != 1 || e.Absolute != 1 || e.Title != "This Self-Proclaimed Goddess and Reincarnation in Another World!" || e.Thumb != "https://img.example/ep1.jpg" {
		t.Errorf("episode 1 = %+v", e)
	}
	if se.Episodes[1].Episode != 2 || se.Episodes[1].Title != "An Explosive Mage in This Wonderful World!" {
		t.Errorf("episode 2 = %+v", se.Episodes[1])
	}
}

func TestAniListMatchViaGraphQL(t *testing.T) {
	body, _ := os.ReadFile(filepath.Join("testdata", "anilist_search.json"))
	var gotVars map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected request: %s %s", r.Method, r.Header.Get("Content-Type"))
		}
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &req)
		gotVars = req.Variables
		w.Write(body)
	}))
	defer srv.Close()
	a := NewAniList()
	a.endpoint = srv.URL

	year := 2016
	r, err := a.Match(context.Background(), &model.Item{Type: model.TypeSeries, Title: "Kono Subarashii Sekai ni Shukufuku o! (2016)", Year: &year}, Hints{Anime: true})
	if err != nil || r == nil || r.ID != "21202" {
		t.Fatalf("match = %+v, %v", r, err)
	}
	if gotVars["search"] != "Kono Subarashii Sekai ni Shukufuku o!" {
		t.Errorf("search variable = %v (year should be stripped)", gotVars["search"])
	}

	// A title nothing in the page resembles must not match, even though the API
	// always returns something.
	r, err = a.Match(context.Background(), &model.Item{Type: model.TypeSeries, Title: "Roxy Test Show"}, Hints{Anime: true})
	if err != nil || r != nil {
		t.Fatalf("Roxy Test Show matched %+v, %v", r, err)
	}
}

func TestTitleSimilarity(t *testing.T) {
	cases := []struct {
		a, b string
		min  float64
		max  float64
	}{
		{"Kono Subarashii Sekai ni Shukufuku o! Kurenai Densetsu", "Kono Subarashii Sekai ni Shukufuku wo! Kurenai Densetsu", 0.8, 0.99},
		{"KonoSuba", "Konosuba", 1, 1},
		{"Roxy Test Show", "Working!!!", 0, 0.1},
		{"Toy Story 5", "Toy Story", 0.6, 0.9},
		{"Frieren", "Sousou no Frieren", 0.8, 0.9},
	}
	for _, c := range cases {
		if s := TitleSimilarity(c.a, c.b); s < c.min || s > c.max {
			t.Errorf("TitleSimilarity(%q, %q) = %.2f, want [%.2f, %.2f]", c.a, c.b, s, c.min, c.max)
		}
	}
	if StripHTML("a<br/>b<BR>c &amp; d <b>e</b>") != "a\nb\nc & d e" {
		t.Errorf("StripHTML = %q", StripHTML("a<br/>b<BR>c &amp; d <b>e</b>"))
	}
}
