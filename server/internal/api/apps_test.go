package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/roxy-media/roxy/server/internal/config"
)

// The update manifest lists a build's compilation profiles, each with a URL that serves
// exactly that file. A profile whose file is missing or whose checksum is not recorded is
// left out (the app would refuse it), and an index out of range is a 404.
func TestAppLatestListsProfiles(t *testing.T) {
	data := t.TempDir()
	dir := filepath.Join(data, "apps", "tv")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("app.apk", "apk")
	write("p28.dm", "profile for 28-30")
	write("p31.dm", "profile for 31+")
	write("nosum.dm", "no checksum")
	write("latest.json", `{"tv.roxy.tv.debug": {"versionCode": 71, "versionName": "0.5.50", "file": "app.apk", "sha256": "aa",
		"profiles": [
			{"minApi": 28, "maxApi": 30, "file": "p28.dm", "sha256": "bb"},
			{"minApi": 31, "maxApi": 2147483647, "file": "p31.dm", "sha256": "cc"},
			{"minApi": 31, "maxApi": 31, "file": "gone.dm", "sha256": "dd"},
			{"minApi": 31, "maxApi": 31, "file": "nosum.dm"},
			{"minApi": 31, "maxApi": 31, "file": "../escape.dm", "sha256": "ee"}
		]}}`)
	s := &Server{cfg: &config.Config{DataDir: data}}
	call := func(h http.HandlerFunc, target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		rc := chi.NewRouteContext()
		rc.URLParams.Add("app", "tv")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rc))
		rec := httptest.NewRecorder()
		h(rec, req)
		return rec
	}

	rec := call(s.appLatest, "/v1/apps/tv/latest?package=tv.roxy.tv.debug")
	if rec.Code != 200 {
		t.Fatalf("latest: %d %s", rec.Code, rec.Body)
	}
	var got struct {
		Profiles []struct {
			MinAPI int    `json:"minApi"`
			MaxAPI int    `json:"maxApi"`
			Size   int64  `json:"size"`
			SHA256 string `json:"sha256"`
			URL    string `json:"url"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Profiles) != 2 || got.Profiles[0].SHA256 != "bb" || got.Profiles[1].SHA256 != "cc" || got.Profiles[1].MinAPI != 31 {
		t.Fatalf("profiles: %+v", got.Profiles)
	}
	for _, p := range got.Profiles {
		want := map[string]string{"bb": "profile for 28-30", "cc": "profile for 31+"}[p.SHA256]
		if p.Size != int64(len(want)) {
			t.Fatalf("%s: size %d", p.URL, p.Size)
		}
		r := call(s.appProfileFile, p.URL)
		if r.Code != 200 || r.Body.String() != want {
			t.Fatalf("%s: %d %q, want %q", p.URL, r.Code, r.Body, want)
		}
	}
	for _, bad := range []string{"i=9", "i=-1", "i=x", "i=4", "i=2"} {
		if r := call(s.appProfileFile, "/v1/apps/tv/profile?package=tv.roxy.tv.debug&"+bad); r.Code != 404 {
			t.Fatalf("%s: %d, want 404", bad, r.Code)
		}
	}
}
