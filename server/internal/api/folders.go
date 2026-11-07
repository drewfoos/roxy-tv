package api

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/roxy-media/roxy/server/internal/library"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

// Media folders (admin), Jellyfin's "Libraries" page: each folder the server reads is a
// library with a type ("series" = shows and movies, detected per file, which is what
// every library did before; "movies" = movies only) and an anime switch (AniList first).
// Folders come from ROXY_MEDIA_ROOTS on first start and from this screen after.

var videoExt = map[string]bool{".mkv": true, ".mp4": true, ".m4v": true, ".avi": true, ".mov": true, ".webm": true, ".ts": true, ".m2ts": true, ".wmv": true}

// GET /v1/library/folders -> {libraries}
func (s *Server) listFolders(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	libs, err := s.store.LibraryInfos(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	type out struct {
		store.LibraryInfo
		Reachable bool `json:"reachable"`
	}
	list := make([]out, 0, len(libs))
	for _, l := range libs {
		st, err := os.Stat(l.Root)
		list = append(list, out{LibraryInfo: l, Reachable: err == nil && st.IsDir()})
	}
	writeJSON(w, 200, map[string]any{"libraries": list})
}

type folderBody struct {
	Path  string `json:"path"`
	Name  string `json:"name"`
	Kind  string `json:"kind"`  // series | movies
	Anime *bool  `json:"anime"` // nil = leave as is (PATCH)
}

// POST /v1/library/folders {path, name, kind, anime} -> library (and a scan starts)
func (s *Server) addFolder(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var b folderBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&b); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	root, err := s.checkNewRoot(r.Context(), b.Path)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	kind := model.KindSeries
	if b.Kind == string(model.KindMovies) {
		kind = model.KindMovies
	}
	name := strings.TrimSpace(b.Name)
	if name == "" {
		name = filepath.Base(root)
	}
	lib := &model.Library{Name: name, Kind: kind, Root: root, AnimeMode: b.Anime != nil && *b.Anime}
	if err := s.store.CreateLibrary(r.Context(), lib); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	_ = s.store.SetRootRemoved(r.Context(), root, false)
	library.GuardMediaRoots(r.Context(), s.store, s.cfg.MediaRoots)
	slog.Info("library added", "name", lib.Name, "root", root, "kind", kind, "anime", lib.AnimeMode)
	go func() { _ = s.lib.ScanAll(context.Background(), false) }()
	writeJSON(w, 201, lib)
}

// PATCH /v1/library/folders/{id} {name?, kind?, anime?} -> library
func (s *Server) updateFolder(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	lib, ok := s.folderFromURL(w, r)
	if !ok {
		return
	}
	var b folderBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&b); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	changed := false
	if n := strings.TrimSpace(b.Name); n != "" {
		lib.Name = n
	}
	if b.Kind == string(model.KindMovies) || b.Kind == string(model.KindSeries) {
		changed = changed || lib.Kind != model.LibraryKind(b.Kind)
		lib.Kind = model.LibraryKind(b.Kind)
	}
	if b.Anime != nil {
		changed = changed || lib.AnimeMode != *b.Anime
		lib.AnimeMode = *b.Anime
	}
	if err := s.store.UpdateLibrary(r.Context(), lib); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	// A new type or anime setting changes how titles are matched: look them up again.
	if changed && s.meta != nil {
		go func() { _ = s.meta.RefreshAll(context.Background(), false) }()
	}
	writeJSON(w, 200, lib)
}

// POST /v1/library/folders/{id}/accept-empty -> 202. The admin confirms that a library
// the last scan found with no media files (and left alone, as it does for an unmounted
// drive) really is empty: a scan starts and removes the records of its files. Only for a
// library in that state (409 otherwise), so it cannot be armed ahead of a missing drive.
func (s *Server) acceptEmptyFolder(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	lib, ok := s.folderFromURL(w, r)
	if !ok {
		return
	}
	if _, waiting := s.lib.EmptyRefused()[lib.ID]; !waiting {
		writeError(w, 409, "this library is not waiting for that: its last scan did not find it empty")
		return
	}
	s.lib.AcceptEmpty(lib.ID)
	slog.Info("library confirmed empty by an admin", "name", lib.Name, "root", lib.Root)
	go func() { _ = s.lib.ScanAll(context.Background(), false) }()
	w.WriteHeader(http.StatusAccepted)
}

// DELETE /v1/library/folders/{id} -> 204. Roxy forgets the folder; files stay on disk.
func (s *Server) removeFolder(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	lib, ok := s.folderFromURL(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteLibrary(r.Context(), lib.ID); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	_ = s.store.SetRootRemoved(r.Context(), lib.Root, true)
	slog.Info("library removed (files untouched)", "name", lib.Name, "root", lib.Root)
	w.WriteHeader(http.StatusNoContent)
}

// GET /v1/library/browse?path= -> {path, parent, dirs: [{name, path, videos}], roots}
// The server's own disk, folders only (Jellyfin's folder picker). In Docker this is the
// container's view: only mounted folders exist.
func (s *Server) browseFolders(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	p := strings.TrimSpace(r.URL.Query().Get("path"))
	if p == "" {
		writeJSON(w, 200, map[string]any{"path": "", "dirs": s.startingPoints()})
		return
	}
	p = filepath.Clean(p)
	if !filepath.IsAbs(p) {
		writeError(w, 400, "give a full path, like /data/media")
		return
	}
	entries, err := os.ReadDir(p)
	if err != nil {
		writeError(w, 404, "can't open that folder on the server: "+friendlyFSError(err))
		return
	}
	type dir struct {
		Name   string `json:"name"`
		Path   string `json:"path"`
		Videos int    `json:"videos"`         // video files up to two levels in (a hint, not a full count)
		Skip   string `json:"skip,omitempty"` // why a scan leaves it alone (download folder, extras...)
	}
	dirs := []dir{}
	videosHere := 0
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		full := filepath.Join(p, name)
		if p == "/" && systemDirs[name] {
			continue // kernel and system folders, never media (and slow to look into)
		}
		if e.IsDir() || (e.Type()&fs.ModeSymlink != 0 && isDir(full)) {
			if why := library.SkipDirReason(full); why != "" {
				dirs = append(dirs, dir{Name: name, Path: full, Skip: why})
			} else {
				dirs = append(dirs, dir{Name: name, Path: full, Videos: countVideos(full)})
			}
		} else if videoExt[strings.ToLower(filepath.Ext(name))] && library.SkipFileReason(name) == "" {
			videosHere++
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name) })
	parent := filepath.Dir(p)
	if parent == p {
		parent = ""
	}
	writeJSON(w, 200, map[string]any{"path": p, "parent": parent, "dirs": dirs, "videos": videosHere})
}

// startingPoints: the usual places media lives, the folders already in use, and "/".
func (s *Server) startingPoints() []map[string]any {
	seen := map[string]bool{}
	var out []map[string]any
	add := func(p, label string) {
		p = filepath.Clean(p)
		if seen[p] || !isDir(p) {
			return
		}
		seen[p] = true
		n := 0
		if p != "/" {
			n = countVideos(p)
		}
		out = append(out, map[string]any{"name": label, "path": p, "videos": n})
	}
	if libs, err := s.store.ListLibraries(context.Background()); err == nil {
		for _, l := range libs {
			add(filepath.Dir(l.Root), filepath.Dir(l.Root))
		}
	}
	for _, p := range []string{"/data", "/media", "/mnt", "/srv", "/storage"} {
		add(p, p)
	}
	if home, err := os.UserHomeDir(); err == nil {
		add(home, home)
	}
	add("/", "/ (whole disk)")
	return out
}

func (s *Server) folderFromURL(w http.ResponseWriter, r *http.Request) (*model.Library, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, 404, "library not found")
		return nil, false
	}
	lib, err := s.store.GetLibrary(r.Context(), id)
	if err != nil || lib == nil {
		writeError(w, 404, "library not found")
		return nil, false
	}
	return lib, true
}

// checkNewRoot: an existing, readable folder that is not already a library, inside one,
// or around one (a folder must be read by one library only).
func (s *Server) checkNewRoot(ctx context.Context, path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" || !filepath.IsAbs(path) {
		return "", errors.New("pick a folder (a full path like /data/media/tv)")
	}
	root := filepath.Clean(path)
	st, err := os.Stat(root)
	if err != nil {
		return "", errors.New("the server can't see that folder: " + friendlyFSError(err))
	}
	if !st.IsDir() {
		return "", errors.New("that is a file, not a folder")
	}
	if _, err := os.ReadDir(root); err != nil {
		return "", errors.New("the server can't read that folder: " + friendlyFSError(err))
	}
	libs, err := s.store.ListLibraries(ctx)
	if err != nil {
		return "", err
	}
	for _, l := range libs {
		switch {
		case l.Root == root:
			return "", errors.New("that folder is already the library \"" + l.Name + "\"")
		case within(root, l.Root):
			return "", errors.New("that folder is inside the library \"" + l.Name + "\", which already reads it")
		case within(l.Root, root):
			return "", errors.New("the library \"" + l.Name + "\" is inside that folder; remove it first or pick a different folder")
		}
	}
	return root, nil
}

var systemDirs = map[string]bool{"proc": true, "sys": true, "dev": true, "run": true, "boot": true, "etc": true, "bin": true, "sbin": true, "lib": true, "lib64": true, "usr": true, "var": true, "tmp": true, "lost+found": true}

func within(child, parent string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != "." && !strings.HasPrefix(rel, "..")
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// countVideos counts video files at most two levels down (enough to tell an empty
// folder from a media one without walking a whole drive).
func countVideos(p string) int {
	n := 0
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if n > 999 {
				return
			}
			if e.IsDir() {
				if sub := filepath.Join(dir, e.Name()); depth < 2 && library.SkipDirReason(sub) == "" {
					walk(sub, depth+1)
				}
			} else if videoExt[strings.ToLower(filepath.Ext(e.Name()))] && library.SkipFileReason(e.Name()) == "" {
				n++
			}
		}
	}
	walk(p, 0)
	return n
}

func friendlyFSError(err error) string {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "it doesn't exist (in Docker, is it mounted into the container?)"
	case errors.Is(err, fs.ErrPermission):
		return "permission denied (the server's user can't read it)"
	default:
		return err.Error()
	}
}
