package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

// App updates: the server hosts the newest build of each client so phones and TVs can
// offer to install it. `<data>/apps/tv/latest.json` maps a package id to its build:
//
//	{"tv.roxy.tv.debug": {"versionCode": 3, "versionName": "0.3.0", "file": "roxy-tv-0.3.0-debug.apk",
//	                      "notes": "Downloads and autoplay", "publishedAt": "2026-10-19T08:00:00Z"}}
//
// apps/android/tools/publish-apk.sh writes it after a build. A build may also carry
// "profiles": the ahead-of-time compilation profiles (.dm files) the Android build makes,
// one per range of Android versions. The app installs the one for its Android version
// together with the APK, so the update runs compiled from its first launch instead of
// stuttering until the device compiles it in the background (often a day later). Nothing here is public: the
// manifest needs a Bearer token and the APK a token or api_key, like media.

type appBuild struct {
	VersionCode int    `json:"versionCode"`
	VersionName string `json:"versionName"`
	File        string `json:"file"`
	Notes       string `json:"notes,omitempty"`
	PublishedAt string `json:"publishedAt,omitempty"`
	// SHA-256 of the APK (hex): the app checks the download against it before installing.
	SHA256   string       `json:"sha256,omitempty"`
	Profiles []appProfile `json:"profiles,omitempty"`
}

// appProfile is one compilation profile (.dm) of a build, for Android API levels
// MinAPI..MaxAPI.
type appProfile struct {
	MinAPI int    `json:"minApi"`
	MaxAPI int    `json:"maxApi"`
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
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
// 200 {versionCode, versionName, notes, publishedAt, size, sha256, url, profiles?} or 404
// when nothing is published. profiles: [{minApi, maxApi, size, sha256, url}], only those
// whose file is present and whose checksum is recorded (the app refuses one without).
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
	var profiles []map[string]any
	for i, p := range b.Profiles {
		file := safeName(p.File)
		if file == "" || p.SHA256 == "" {
			continue
		}
		fi, err := os.Stat(filepath.Join(s.appsDir(app), file))
		if err != nil {
			continue
		}
		profiles = append(profiles, map[string]any{
			"minApi": p.MinAPI,
			"maxApi": p.MaxAPI,
			"size":   fi.Size(),
			"sha256": p.SHA256,
			"url":    "/v1/apps/" + app + "/profile?package=" + url.QueryEscape(pkg) + "&i=" + strconv.Itoa(i),
		})
	}
	writeJSON(w, 200, map[string]any{
		"profiles":    profiles,
		"versionCode": b.VersionCode,
		"versionName": b.VersionName,
		"notes":       b.Notes,
		"publishedAt": b.PublishedAt,
		"size":        size,
		"sha256":      b.SHA256,
		"url":         "/v1/apps/" + app + "/download?package=" + url.QueryEscape(pkg),
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
	w.Header().Set("Content-Disposition", contentDisposition("attachment", filepath.Base(path)))
	http.ServeContent(w, r, filepath.Base(path), fi.ModTime(), f)
}

// GET /v1/apps/{app}/profile?package=<applicationId>&i=<n>: the build's n-th compilation
// profile (see appProfile).
func (s *Server) appProfileFile(w http.ResponseWriter, r *http.Request) {
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
	i, err := strconv.Atoi(r.URL.Query().Get("i"))
	if err != nil || i < 0 || i >= len(b.Profiles) {
		writeError(w, 404, "no such profile")
		return
	}
	file := safeName(b.Profiles[i].File)
	if app == "" || file == "" {
		writeError(w, 404, "profile file missing")
		return
	}
	path := filepath.Join(s.appsDir(app), file)
	f, err := os.Open(path)
	if err != nil {
		writeError(w, 404, "profile file missing")
		return
	}
	defer f.Close()
	fi, _ := f.Stat()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", contentDisposition("attachment", filepath.Base(path)))
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
