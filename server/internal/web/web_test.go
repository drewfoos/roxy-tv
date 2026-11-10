package web

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
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

func TestHandlerSetsCSP(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	csp := rec.Header().Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'self'", "script-src 'self' 'wasm-unsafe-eval'", "frame-ancestors 'none'", "object-src 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP %q lacks %q", csp, want)
		}
	}
}

func TestOnlyTheSubtitleWorkerMayEval(t *testing.T) {
	if strings.Contains(CSP, "'unsafe-eval'") {
		t.Fatalf("the page CSP must not allow eval: %q", CSP)
	}
	if !isJassubWorker("/assets/jassub-worker-CpwfHrci.js") || !isJassubWorker("/assets/jassub-worker.wasm-D-S1Mcrm.js") || isJassubWorker("/assets/index-abc.js") || isJassubWorker("/jassub-worker.js") {
		t.Fatalf("isJassubWorker matches the wrong files")
	}
}

func testBuild() fstest.MapFS {
	return fstest.MapFS{
		"index.html":             {Data: []byte("<!doctype html><title>Roxy</title>")},
		"favicon.svg":            {Data: []byte("<svg/>")},
		"assets/index-abc123.js": {Data: []byte("console.log(1)")},
	}
}

// A tab left open across an update asks for the old build's files. They were answered
// with the app's page (200, text/html), so the browser got a page where it wanted a
// script.
func TestAMissingBuiltFileIsNotFound(t *testing.T) {
	h := handlerFor(testBuild())
	for _, p := range []string{"/assets/index-old999.js", "/assets/chunk-old.css", "/assets/", "/assets/../assets/gone.js"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", p, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "<title>Roxy</title>") {
			t.Errorf("GET %s was answered with the app's page", p)
		}
		if cc := rec.Header().Get("Cache-Control"); strings.Contains(cc, "immutable") || strings.Contains(cc, "max-age") {
			t.Errorf("GET %s: a missing file must not be cacheable (%q)", p, cc)
		}
	}
}

// What must not change: built files are served as they are, and the app's own routes
// (which are not files) get the page.
func TestBuiltFilesAndAppRoutesAreServed(t *testing.T) {
	h := handlerFor(testBuild())
	get := func(p string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		return rec
	}
	if rec := get("/assets/index-abc123.js"); rec.Code != http.StatusOK || rec.Body.String() != "console.log(1)" || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("built file: %d %q cache %q", rec.Code, rec.Body.String(), rec.Header().Get("Cache-Control"))
	}
	if rec := get("/favicon.svg"); rec.Code != http.StatusOK || rec.Body.String() != "<svg/>" {
		t.Errorf("favicon: %d %q", rec.Code, rec.Body.String())
	}
	for _, p := range []string{"/", "/series/abc", "/watch/xyz", "/assetsfoo", "/library/assets/x"} {
		rec := get(p)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<title>Roxy</title>") || rec.Header().Get("Cache-Control") != "no-cache" {
			t.Errorf("GET %s = %d %q (cache %q), want the app's page", p, rec.Code, rec.Body.String(), rec.Header().Get("Cache-Control"))
		}
	}
}
