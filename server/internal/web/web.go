// Package web serves Roxy Web, the browser client, from the server binary. apps/web
// builds into dist/ here (git-ignored apart from .keep) and go:embed packs it in, so one
// binary carries the API and the UI. Without a build the root answers with a short note
// instead of a blank page.
package web

import (
	"bytes"
	"compress/gzip"
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
)

//go:embed all:dist
var dist embed.FS

// CSP is Roxy Web's Content-Security-Policy. Scripts only from Roxy itself (plus
// WebAssembly for jassub's libass); images also from https hosts (TMDB, AniList and
// fanart.tv art in Discover and the art picker); connect-src allows any http(s) origin
// because "Change server" probes another Roxy's /v1/system/info, which may be on any
// LAN address. No framing, no <base> or form posts elsewhere.
const CSP = "default-src 'self'; " +
	"script-src 'self' 'wasm-unsafe-eval'; " +
	"worker-src 'self' blob:; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob: https:; " +
	"media-src 'self' blob:; " +
	"font-src 'self' data: blob:; " +
	"connect-src 'self' http: https:; " +
	"object-src 'none'; " +
	"frame-ancestors 'none'; " +
	"base-uri 'self'; " +
	"form-action 'self'"

// workerCSP is for jassub's worker script only. A dedicated worker runs under the policy
// of its own script response, and libass's embind glue builds functions with new
// Function (and falls back to eval'ing asm.js without WebAssembly), so the worker needs
// 'unsafe-eval'. The page itself never gets it.
const workerCSP = "default-src 'self'; script-src 'self' 'unsafe-eval' 'wasm-unsafe-eval'; connect-src 'self'; img-src 'self' data: blob:; font-src 'self' data: blob:"

func isJassubWorker(p string) bool {
	return strings.HasPrefix(p, "/assets/jassub-worker") && strings.HasSuffix(p, ".js")
}

// Handler serves the built app: real files as they are (hashed assets with a long
// cache), a missing file under /assets/ as 404, anything else as index.html so the
// client-side router owns the URL. API paths never reach it (the /v1 router answers
// them, found or not).
func Handler() http.Handler {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return handlerFor(sub)
}

// handlerFor is Handler over any build folder (the tests bring their own).
func handlerFor(sub fs.FS) http.Handler {
	files := http.FS(sub)
	fileServer := http.FileServer(files)
	_, statErr := fs.Stat(sub, "index.html")
	haveIndex := statErr == nil
	var zipped sync.Map // path -> []byte, the file gzipped (a build's files never change)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", CSP)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !haveIndex {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			http.Error(w, "Roxy Server is running. Roxy Web is not built into this binary: run `npm run build` in apps/web, then rebuild the server.", http.StatusNotFound)
			return
		}
		p := path.Clean("/" + r.URL.Path)
		if p != "/" {
			if f, err := sub.Open(strings.TrimPrefix(p, "/")); err == nil {
				if st, err := f.Stat(); err == nil && !st.IsDir() {
					f.Close()
					if isJassubWorker(p) {
						w.Header().Set("Content-Security-Policy", workerCSP)
					}
					if strings.HasPrefix(p, "/assets/") {
						w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
					}
					if serveGzipped(w, r, sub, p, &zipped) {
						return
					}
					fileServer.ServeHTTP(w, r)
					return
				}
				f.Close()
			}
		}
		if p == "/assets" || strings.HasPrefix(p, "/assets/") {
			// A built file that is not in this build: a tab left open across an update
			// asking for the old build's script or style. Answering with the shell (200,
			// text/html) gave the browser a page where it wanted a script, which fails
			// with a baffling error and, behind a cache, can be kept; a plain 404 lets
			// the app see the chunk is gone.
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		// Not a route of the app: a dotfile (/.git/HEAD, /.env) or a file by name
		// (/roxy.db, /backup.zip, /index.html.map). No route has a dot in its last part
		// or a part that starts with one. The shell answered these with 200, which is
		// not a leak (it is the same page every time) but tells every scanner, and
		// anyone checking by status, that the file is there.
		if looksLikeFile(p) {
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		// A route of the app (/series/abc, /watch/xyz): the shell, never cached.
		w.Header().Set("Cache-Control", "no-cache")
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}

// gzipTypes are the built files worth compressing, with the type each is sent as. The
// app's script went out as 509 kB where 158 kB would do, on every deploy and over
// whatever link the browser is on; fonts and images are compressed already.
var gzipTypes = map[string]string{
	".js":   "text/javascript; charset=utf-8",
	".css":  "text/css; charset=utf-8",
	".html": "text/html; charset=utf-8",
	".svg":  "image/svg+xml",
	".json": "application/json",
	".wasm": "application/wasm",
}

// serveGzipped answers with the file gzipped when the browser takes gzip and asked for
// the whole file. Each file is compressed once and kept. False: nothing was written,
// and the file goes out as it is.
func serveGzipped(w http.ResponseWriter, r *http.Request, sub fs.FS, p string, cache *sync.Map) bool {
	ctype, ok := gzipTypes[path.Ext(p)]
	if !ok {
		return false
	}
	w.Header().Add("Vary", "Accept-Encoding")
	if r.Header.Get("Range") != "" || !acceptsGzip(r.Header.Get("Accept-Encoding")) {
		return false
	}
	body, ok := cache.Load(p)
	if !ok {
		raw, err := fs.ReadFile(sub, strings.TrimPrefix(p, "/"))
		if err != nil || len(raw) < 1024 {
			return false
		}
		var buf bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
		if _, err := zw.Write(raw); err != nil || zw.Close() != nil || buf.Len() >= len(raw) {
			return false
		}
		body, _ = cache.LoadOrStore(p, buf.Bytes())
	}
	b := body.([]byte)
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Encoding", "gzip")
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(b)
	}
	return true
}

// acceptsGzip: the Accept-Encoding header names gzip and does not refuse it (q=0).
func acceptsGzip(header string) bool {
	for _, part := range strings.Split(header, ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if strings.EqualFold(strings.TrimSpace(name), "gzip") {
			q := strings.TrimSpace(params)
			return q != "q=0" && q != "q=0.0" && q != "q=0.00" && q != "q=0.000"
		}
	}
	return false
}

// looksLikeFile: the cleaned path names a file and not a page of the app: a part that
// starts with a dot, or a last part with a dot in it.
func looksLikeFile(p string) bool {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	for _, part := range parts {
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	return strings.Contains(parts[len(parts)-1], ".")
}
