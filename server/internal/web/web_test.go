package web

import (
	"bytes"
	"compress/gzip"
	"io"
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

// A path that names a file and is not in the build was answered with the app's page:
// 200 for /.git/HEAD, /.env, /roxy.db. The same page every time, so nothing leaked, but
// by status the file "was there". The app's routes have no such names.
func TestAFileThatIsNotThereIsNotFound(t *testing.T) {
	h := handlerFor(testBuild())
	for _, p := range []string{"/.git/HEAD", "/.git/config", "/.env", "/roxy.db", "/config/roxy.db", "/index.html.map", "/backup.zip", "/.well-known/security.txt", "/a/.hidden/page", "/..%2f..%2fetc/passwd.txt"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", p, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "<title>Roxy</title>") {
			t.Errorf("GET %s was answered with the app's page", p)
		}
	}
	// The app's own pages still get the shell, a sign-in link among them.
	for _, p := range []string{"/link/ABC234", "/discover/tv/1234", "/account/addresses"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<title>Roxy</title>") {
			t.Errorf("GET %s = %d, want the app's page", p, rec.Code)
		}
	}
}

// The app's script went out uncompressed: 509 kB where 158 kB would do. Built text
// files are gzipped for a browser that takes gzip; everything else is as it was.
func TestBuiltTextFilesAreGzipped(t *testing.T) {
	script := []byte(strings.Repeat("function roxy(){return 'the same words again and again'}\n", 200))
	font := []byte(strings.Repeat("wOF2 font bytes ", 200))
	build := testBuild()
	build["assets/app-abc.js"] = &fstest.MapFile{Data: script}
	build["assets/font-abc.woff2"] = &fstest.MapFile{Data: font}
	h := handlerFor(build)
	get := func(p string, hdr map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	for i := 0; i < 2; i++ { // the second answer comes from the kept copy
		rec := get("/assets/app-abc.js", map[string]string{"Accept-Encoding": "gzip, deflate, br"})
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Encoding") != "gzip" {
			t.Fatalf("script: %d encoding %q, want gzip", rec.Code, rec.Header().Get("Content-Encoding"))
		}
		if rec.Body.Len() >= len(script)/4 {
			t.Errorf("script went out as %d bytes of %d", rec.Body.Len(), len(script))
		}
		zr, err := gzip.NewReader(rec.Body)
		if err != nil {
			t.Fatal(err)
		}
		plain, _ := io.ReadAll(zr)
		if !bytes.Equal(plain, script) {
			t.Errorf("the gzipped script does not unpack to the file")
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/javascript") {
			t.Errorf("script type %q", ct)
		}
		if !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") || !strings.Contains(rec.Header().Get("Vary"), "Accept-Encoding") {
			t.Errorf("script headers: cache %q vary %q", rec.Header().Get("Cache-Control"), rec.Header().Get("Vary"))
		}
	}
	plainCases := []struct {
		name, path string
		hdr        map[string]string
		want       []byte
	}{
		{"no gzip asked", "/assets/app-abc.js", nil, script},
		{"gzip refused", "/assets/app-abc.js", map[string]string{"Accept-Encoding": "br, gzip;q=0"}, script},
		{"a font", "/assets/font-abc.woff2", map[string]string{"Accept-Encoding": "gzip"}, font},
		{"a small file", "/assets/index-abc123.js", map[string]string{"Accept-Encoding": "gzip"}, []byte("console.log(1)")},
	}
	for _, c := range plainCases {
		rec := get(c.path, c.hdr)
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Encoding") != "" || !bytes.Equal(rec.Body.Bytes(), c.want) {
			t.Errorf("%s: %d encoding %q, %d bytes; want the file as it is", c.name, rec.Code, rec.Header().Get("Content-Encoding"), rec.Body.Len())
		}
	}
	// A part of the file is a part of the file itself, never of its gzipped form.
	rec := get("/assets/app-abc.js", map[string]string{"Accept-Encoding": "gzip", "Range": "bytes=0-9"})
	if rec.Code != http.StatusPartialContent || rec.Header().Get("Content-Encoding") != "" || !bytes.Equal(rec.Body.Bytes(), script[:10]) {
		t.Errorf("range: %d encoding %q body %q", rec.Code, rec.Header().Get("Content-Encoding"), rec.Body.String())
	}
}
