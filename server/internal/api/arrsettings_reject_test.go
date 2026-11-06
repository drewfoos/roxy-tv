package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/roxy-media/roxy/server/internal/arr"
	"github.com/roxy-media/roxy/server/internal/auth"
	"github.com/roxy-media/roxy/server/internal/config"
	"github.com/roxy-media/roxy/server/internal/discover"
	"github.com/roxy-media/roxy/server/internal/metadata"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

// A PUT that is refused must leave the live settings exactly as they were. The body used
// to be decoded over a shallow copy, so *bool fields and tag slices were written through
// to the live config before validation failed.
func TestArrSettingsRejectedPutChangesNothing(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	yes := func() *bool { v := true; return &v }
	saved := discover.ArrSettings{
		Sonarr: arr.Config{URL: "http://127.0.0.1:1", APIKey: "k", RootFolder: "/data/TV", QualityProfileID: 4, SearchOnAdd: yes(), SeasonFolders: yes(), Tags: []int{1, 2}, AnimeTags: []int{3}},
		Radarr: arr.Config{URL: "http://127.0.0.1:2", APIKey: "k2", RootFolder: "/data/Movies", QualityProfileID: 4, SearchOnAdd: yes(), Tags: []int{5}},
	}
	if err := st.SetSetting(ctx, "arr", saved); err != nil {
		t.Fatal(err)
	}
	d := discover.New(st, &config.Config{}, metadata.NewTMDB("test-key"))
	before := d.ArrSettings()
	s := &Server{store: st, discover: d}

	body := `{"sonarr":{"url":"ftp://nope","apiKey":"********","searchOnAdd":false,"seasonFolders":false,"tags":[9,9],"animeTags":[8]},` +
		`"radarr":{"searchOnAdd":false,"tags":[7]}}`
	req := httptest.NewRequest(http.MethodPut, "/v1/settings/arr", strings.NewReader(body))
	req = req.WithContext(auth.WithPrincipal(req.Context(), &model.Principal{User: &model.User{ID: "u1", Name: "admin", IsAdmin: true}, IsAdmin: true}))
	rec := httptest.NewRecorder()
	s.arrSettingsPut(rec, req)
	if rec.Code != 400 {
		t.Fatalf("status %d (%s), want 400", rec.Code, rec.Body.String())
	}
	after := d.ArrSettings()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("a refused update changed the live settings:\nbefore %+v\nafter  %+v", before, after)
	}
	if !*after.Sonarr.SearchOnAdd || !*after.Sonarr.SeasonFolders || !reflect.DeepEqual(after.Sonarr.Tags, []int{1, 2}) || !*after.Radarr.SearchOnAdd {
		t.Fatalf("live settings changed: %+v / %+v", after.Sonarr, after.Radarr)
	}
}
