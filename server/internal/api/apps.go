package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
)

// App updates: the server hosts the newest build of each client so phones and TVs can
// offer to install it. `<data>/apps/tv/latest.json` maps a package id to its build:
//
//	{"tv.roxy.tv.debug": {"versionCode": 3, "versionName": "0.3.0", "file": "roxy-tv-0.3.0-debug.apk",
//	                      "notes": "Downloads and autoplay", "publishedAt": "2026-10-19T08:00:00Z"}}
//
// apps/android/tools/publish-apk.ps1 writes it after a build. Nothing here is public: the
// manifest needs a Bearer token and the APK a token or api_key, like media.

type appBuild struct {
	VersionCode int    `json:"versionCode"`
	VersionName string `json:"versionName"`
	File        string `json:"file"`
	Notes       string `json:"notes,omitempty"`
	PublishedAt string `json:"publishedAt,omitempty"`
}

func (s *Server) appsDir(app string) string { return filepath.Join(s.cfg.DataDir, "apps", app) }

func (s *Server) readLatest(app string) (map[string]appBuild, error) {
	b, err := os.ReadFile(filepath.Join(s.appsDir(app), "latest.json"))
	if err != nil {
		return nil, err
	}
	var m map[string]appBuild
	b = bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF}) // PowerShell 5.1 writes a BOM
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// GET /v1/apps/{app}/latest?package=<applicationId>
// 200 {versionCode, versionName, notes, publishedAt, size, url} or 404 when nothing is published.
func (s *Server) appLatest(w http.ResponseWriter, r *http.Request) {
	app := safeName(chi.URLParam(r, "app"))
	pkg := r.URL.Query().Get("package")
	if app == "" {
		writeError(w, 400, "bad app name")
		return
	}
	m, err := s.readLatest(app)
	if err != nil {
		writeError(w, 404, "no builds published")
		return
	}
	b, ok := m[pkg]
	if !ok {
		writeError(w, 404, "no build for this package")
		return
	}
	var size int64
	if fi, err := os.Stat(filepath.Join(s.appsDir(app), safeName(b.File))); err == nil {
		size = fi.Size()
	}
	writeJSON(w, 200, map[string]any{
		"versionCode": b.VersionCode,
		"versionName": b.VersionName,
		"notes":       b.Notes,
		"publishedAt": b.PublishedAt,
		"size":        size,
		"url":         "/v1/apps/" + app + "/download?package=" + pkg,
	})
}

// GET /v1/apps/{app}/download?package=<applicationId>: the APK itself.
func (s *Server) appDownload(w http.ResponseWriter, r *http.Request) {
	app := safeName(chi.URLParam(r, "app"))
	m, err := s.readLatest(app)
	if err != nil {
		writeError(w, 404, "no builds published")
		return
	}
	b, ok := m[r.URL.Query().Get("package")]
	if !ok {
		writeError(w, 404, "no build for this package")
		return
	}
	file := safeName(b.File)
	if app == "" || file == "" {
		writeError(w, 404, "build file missing")
		return
	}
	path := filepath.Join(s.appsDir(app), file)
	f, err := os.Open(path)
	if err != nil {
		writeError(w, 404, "build file missing")
		return
	}
	defer f.Close()
	fi, _ := f.Stat()
	w.Header().Set("Content-Type", "application/vnd.android.package-archive")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filepath.Base(path)+"\"")
	http.ServeContent(w, r, filepath.Base(path), fi.ModTime(), f)
}

// safeName keeps a file name inside its directory: only letters, digits, dot, dash and
// underscore survive, and ".." is rejected outright (a bad name yields "", which opens nothing).
func safeName(name string) string {
	if name == "" || strings.Contains(name, "..") {
		return ""
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_') {
			return ""
		}
	}
	return name
}
