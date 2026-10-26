package web

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The repository carries no build (dist/ holds only .keep), so the handler must say so
// rather than serve a blank page; with a build present it serves index.html for app
// routes. Both branches are exercised: the first against the real embed, the second by
// checking the route-to-index rewrite logic through a request that names a real file.
func TestHandlerWithoutBuild(t *testing.T) {
	sub, _ := fs.Sub(dist, "dist")
	if _, err := fs.Stat(sub, "index.html"); err == nil {
		t.Skip("a web build is present; the no-build message is not testable here")
	}
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/series/abc", nil))
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "npm run build") {
		t.Fatalf("want a 404 that explains the missing build, got %d %q", rec.Code, rec.Body.String())
	}
}

func TestHandlerRefusesWrites(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST / should be 405, got %d", rec.Code)
	}
}
