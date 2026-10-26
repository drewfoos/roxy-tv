// Package web serves Roxy Web, the browser client, from the server binary. apps/web
// builds into dist/ here (git-ignored apart from .keep) and go:embed packs it in, so one
// binary carries the API and the UI. Without a build the root answers with a short note
// instead of a blank page.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var dist embed.FS

// Handler serves the built app: real files as they are (hashed assets with a long
// cache), anything else as index.html so the client-side router owns the URL. API
// paths never reach it (the /v1 router answers them, found or not).
func Handler() http.Handler {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	files := http.FS(sub)
	fileServer := http.FileServer(files)
	_, statErr := fs.Stat(sub, "index.html")
	haveIndex := statErr == nil
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
					if strings.HasPrefix(p, "/assets/") {
						w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
					}
					fileServer.ServeHTTP(w, r)
					return
				}
				f.Close()
			}
		}
		// A route of the app (/series/abc, /watch/xyz): the shell, never cached.
		w.Header().Set("Cache-Control", "no-cache")
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
