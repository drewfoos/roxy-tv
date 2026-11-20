package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// The server states which apps it works with where an app looks before anything else.
func TestSystemInfoStatesCompatibility(t *testing.T) {
	w := newAuthWorld(t)
	rec := httptest.NewRecorder()
	w.h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/system/info", nil))
	var got struct {
		Product string `json:"product"`
		API     *struct {
			Level     int `json:"level"`
			MinClient int `json:"minClient"`
		} `json:"api"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.API == nil {
		t.Fatalf("no api in %s", rec.Body.String())
	}
	if got.API.Level != APILevel || got.API.MinClient != MinClientLevel {
		t.Fatalf("api = %+v, want level %d minClient %d", *got.API, APILevel, MinClientLevel)
	}
	if MinClientLevel > APILevel || MinClientLevel < 1 {
		t.Fatalf("minClient %d must be between 1 and the server's own level %d", MinClientLevel, APILevel)
	}
}

// The real router refuses nobody today: an app at the oldest level it serves, and callers
// that state no level (the web app, older apps), get their answers.
func TestRouterServesSupportedAndSilentClients(t *testing.T) {
	w := newAuthWorld(t)
	for _, level := range []string{"", strconv.Itoa(MinClientLevel), strconv.Itoa(APILevel + 5), "abc"} {
		req := httptest.NewRequest(http.MethodGet, "/v1/setup", nil)
		if level != "" {
			req.Header.Set(ClientLevelHeader, level)
		}
		rec := httptest.NewRecorder()
		w.h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("level %q: /v1/setup = %d, want 200", level, rec.Code)
		}
	}
	// And one below it is refused by that same router (the rule is wired in).
	req := httptest.NewRequest(http.MethodGet, "/v1/setup", nil)
	req.Header.Set(ClientLevelHeader, strconv.Itoa(MinClientLevel-1))
	rec := httptest.NewRecorder()
	w.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUpgradeRequired {
		t.Fatalf("level %d: /v1/setup = %d, want 426", MinClientLevel-1, rec.Code)
	}
}

// An app below the minimum is refused with a coded 426 everywhere except where it learns
// it is too old and where it gets its update.
func TestTooOldClientIsRefusedExceptOnItsWayToAnUpdate(t *testing.T) {
	reached := 0
	h := clientCompat(3)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached++; w.WriteHeader(http.StatusOK) }))
	ask := func(path, level string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if level != "" {
			req.Header.Set(ClientLevelHeader, level)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	rec := ask("/v1/home", "2")
	if rec.Code != http.StatusUpgradeRequired || reached != 0 {
		t.Fatalf("old app on /v1/home = %d (handler reached %d times), want 426 and not reached", rec.Code, reached)
	}
	var body struct {
		Error string `json:"error"`
		Code  string `json:"code"`
		API   struct {
			MinClient int `json:"minClient"`
		} `json:"api"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != CodeClientTooOld || body.Error == "" || body.API.MinClient != MinClientLevel {
		t.Fatalf("refusal body = %s", rec.Body.String())
	}

	for _, path := range []string{
		"/healthz", "/v1/system/info", "/v1/auth/login", "/v1/auth/me", "/v1/auth/logout",
		"/v1/apps/tv/latest", "/v1/apps/tv/download", "/v1/apps/tv/profile",
	} {
		if rec := ask(path, "2"); rec.Code != http.StatusOK {
			t.Errorf("old app on %s = %d, want it served", path, rec.Code)
		}
	}
	for _, path := range []string{"/v1/items/x", "/v1/apps", "/v1/appsx/tv/latest", "/v1/system/information", "/v1/me/progress"} {
		if rec := ask(path, "2"); rec.Code != http.StatusUpgradeRequired {
			t.Errorf("old app on %s = %d, want 426", path, rec.Code)
		}
	}
	for _, level := range []string{"", "3", "4", "new", " 3 "} {
		if rec := ask("/v1/home", level); rec.Code != http.StatusOK {
			t.Errorf("level %q on /v1/home = %d, want 200", level, rec.Code)
		}
	}
}
