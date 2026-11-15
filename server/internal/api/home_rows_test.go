package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/roxy-media/roxy/server/internal/auth"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

// Home fills its cards once for the whole screen (it did so row by row, working out a
// title's languages again for every row it is in). Every card of every row must still
// come out complete, and each in its own row: a film is in two rows, a show's newest
// episode names its show.
func TestHomeRowsAreFilledTogether(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "roxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	lib := &model.Library{Name: "All", Kind: model.KindMovies, Root: t.TempDir()}
	if err := st.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	user := &model.User{Name: "viewer", PasswordHash: "x"}
	if err := st.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	scope, err := st.DefaultProfileID(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	p := func(v string) *string { return &v }
	one := 1
	for _, it := range []*model.Item{
		{ID: "show", Type: model.TypeSeries, Title: "The Show"},
		{ID: "season", Type: model.TypeSeason, ParentID: p("show"), IndexAired: &one},
		{ID: "ep", Type: model.TypeEpisode, ParentID: p("season"), IndexAired: &one, Title: "Pilot"},
		{ID: "film", Type: model.TypeMovie, Title: "The Film"},
	} {
		it.GUID, it.LibraryID = "g:"+it.ID, lib.ID
		if err := st.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	for id, lang := range map[string]string{"ep": "jpn", "film": "eng"} {
		src := &model.MediaSource{ID: "src-" + id, ItemID: id, IsPrimary: true,
			Parts:   []model.MediaPart{{ID: "part-" + id, SourceID: "src-" + id, Path: "/m/" + id + ".mkv"}},
			Streams: []model.MediaStream{{SourceID: "src-" + id, Index: 1, Type: model.StreamAudio, Codec: "aac", Language: lang}}}
		if err := st.ReplaceSource(ctx, src, ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.MarkPlayed(ctx, scope, "g:film", true); err != nil {
		t.Fatal(err)
	}

	s := &Server{store: st}
	r := httptest.NewRequest("GET", "/v1/home", nil)
	r = r.WithContext(auth.WithPrincipal(r.Context(), &model.Principal{User: user, ScopeID: scope}))
	w := httptest.NewRecorder()
	s.home(w, r)
	if w.Code != 200 {
		t.Fatalf("home: %d %s", w.Code, w.Body.String())
	}
	var got struct {
		Rows []struct {
			Key   string
			Items []struct {
				ID, SeriesID, SeriesTitle string
				AudioLanguages            []string
				UserData                  *struct{ Played bool }
			}
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	ids := map[string][]string{}
	for _, row := range got.Rows {
		ids[row.Key] = []string{}
		for _, it := range row.Items {
			ids[row.Key] = append(ids[row.Key], it.ID)
			switch it.ID {
			case "film":
				if !reflect.DeepEqual(it.AudioLanguages, []string{"eng"}) || it.UserData == nil || !it.UserData.Played {
					t.Errorf("row %s: the film came out as %+v (userData %+v)", row.Key, it, it.UserData)
				}
			case "show":
				if !reflect.DeepEqual(it.AudioLanguages, []string{"jpn"}) {
					t.Errorf("row %s: the show's languages are %v", row.Key, it.AudioLanguages)
				}
			case "ep":
				if it.SeriesID != "show" || it.SeriesTitle != "The Show" {
					t.Errorf("row %s: the episode names its show as %q %q", row.Key, it.SeriesID, it.SeriesTitle)
				}
			}
		}
	}
	want := map[string][]string{"continue": {}, "recentEpisodes": {"ep"}, "recentMovies": {"film"}, "series": {"show"}, "movies": {"film"}}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("rows hold %v, want %v", ids, want)
	}
}
