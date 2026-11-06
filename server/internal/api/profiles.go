package api

import (
	"unicode/utf8"
	"unicode"
	"errors"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/roxy-media/roxy/server/internal/safefs"
	"hash/fnv"
	"image/jpeg"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/roxy-media/roxy/server/internal/auth"
	"github.com/roxy-media/roxy/server/internal/imaging"
	"github.com/roxy-media/roxy/server/internal/metadata"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/netguard"
)

// Profiles (docs/api/native.md "Profiles"): several viewers on one login. Clients send
// X-Roxy-Profile: <id> on every request; watch state, favourites and /v1/home follow it.

// avatarKey is a short hash of the avatar's source URL.
func avatarKey(url string) string {
	if url == "" {
		return ""
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(url))
	return fmt.Sprintf("%08x", h.Sum32())
}

func profileJSON(p *model.Profile) map[string]any {
	return map[string]any{
		"id":         p.ID,
		"name":       p.Name,
		"isDefault":  p.IsDefault,
		"avatarName": p.AvatarName,
		"hasAvatar":  p.AvatarURL != "",
		"avatarUrl":  "/v1/me/profiles/" + p.ID + "/avatar",
		"avatarKey":  avatarKey(p.AvatarURL), // changes with the picture: clients put it on the URL so caches miss
		"createdAt":  p.CreatedAt,
	}
}

// GET /v1/me/profiles
func (s *Server) listProfiles(w http.ResponseWriter, r *http.Request) {
	p := userFrom(w, r)
	if p == nil {
		return
	}
	list, err := s.store.ListProfiles(r.Context(), p.UserID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(list))
	for i := range list {
		out = append(out, profileJSON(&list[i]))
	}
	writeJSON(w, 200, map[string]any{"profiles": out, "current": p.ScopeID})
}

type profileBody struct {
	Name       string `json:"name"`
	AvatarURL  string `json:"avatarUrl"`  // AniList image URL from /v1/avatars/*; "-" clears (PATCH)
	AvatarName string `json:"avatarName"` // the character's name
}

// POST /v1/me/profiles
func (s *Server) createProfile(w http.ResponseWriter, r *http.Request) {
	p := userFrom(w, r)
	if p == nil {
		return
	}
	var body profileBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		writeError(w, 400, "a profile name is required")
		return
	}
	if err := checkProfileName(body.Name); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if !allowedAvatar(body.AvatarURL) {
		writeError(w, 400, "avatarUrl must be an AniList image")
		return
	}
	prof, err := s.store.CreateProfile(r.Context(), p.UserID, strings.TrimSpace(body.Name), body.AvatarURL, body.AvatarName)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	s.fetchAvatar(r.Context(), prof)
	writeJSON(w, 201, profileJSON(prof))
}

// PATCH /v1/me/profiles/{id}
func (s *Server) updateProfile(w http.ResponseWriter, r *http.Request) {
	p, prof := s.ownProfile(w, r)
	if prof == nil {
		return
	}
	_ = p
	var body profileBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	if body.Name != "" {
		if err := checkProfileName(body.Name); err != nil {
			writeError(w, 400, err.Error())
			return
		}
	}
	if body.AvatarURL != "" && body.AvatarURL != "-" && !allowedAvatar(body.AvatarURL) {
		writeError(w, 400, "avatarUrl must be an AniList image")
		return
	}
	if err := s.store.UpdateProfile(r.Context(), prof.ID, strings.TrimSpace(body.Name), body.AvatarURL, body.AvatarName); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	profileBeforeReload()
	prof, err := s.store.GetProfile(r.Context(), prof.ID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	// Deleted by another request since the update (it was checked above, but nothing
	// holds it): answer as for any missing profile instead of dereferencing nil.
	if prof == nil {
		writeError(w, 404, "profile not found")
		return
	}
	if prof.AvatarURL != "" && prof.AvatarPath == "" {
		s.fetchAvatar(r.Context(), prof)
	}
	writeJSON(w, 200, profileJSON(prof))
}

// DELETE /v1/me/profiles/{id}
func (s *Server) deleteProfile(w http.ResponseWriter, r *http.Request) {
	_, prof := s.ownProfile(w, r)
	if prof == nil {
		return
	}
	if err := s.store.DeleteProfile(r.Context(), prof.ID); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if prof.AvatarPath != "" {
		safefs.Remove(prof.AvatarPath)
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /v1/me/profiles/{id}/avatar?w=
func (s *Server) profileAvatar(w http.ResponseWriter, r *http.Request) {
	_, prof := s.ownProfile(w, r)
	if prof == nil {
		return
	}
	if prof.AvatarPath == "" && prof.AvatarURL != "" {
		s.fetchAvatar(r.Context(), prof)
	}
	if prof.AvatarPath == "" {
		writeError(w, 404, "no avatar")
		return
	}
	path := prof.AvatarPath
	// Snapped to the fixed widths item images use: any width 16..1023 used to be cached
	// as its own file, so ?w= could write a thousand files per profile.
	if width, _ := strconv.Atoi(r.URL.Query().Get("w")); width >= 16 && snapWidth(width) < 1024 {
		width = snapWidth(width)
		variant := strings.TrimSuffix(path, ".jpg") + fmt.Sprintf(".w%d.jpg", width)
		if _, err := os.Stat(variant); err != nil {
			if err := resizeToFile(path, variant, width); err == nil {
				path = variant
			}
		} else {
			path = variant
		}
	}
	f, err := os.Open(path)
	if err != nil {
		writeError(w, 404, "avatar file missing")
		return
	}
	defer f.Close()
	fi, _ := f.Stat()
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeContent(w, r, "", fi.ModTime(), f)
}

// profileBeforeReload runs between a profile update and re-reading the profile; tests
// use it to delete the profile in that gap.
var profileBeforeReload = func() {}

// ownProfile loads the profile in the URL and checks it belongs to the caller.
func (s *Server) ownProfile(w http.ResponseWriter, r *http.Request) (*model.Principal, *model.Profile) {
	p := userFrom(w, r)
	if p == nil {
		return nil, nil
	}
	prof, err := s.store.GetProfile(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 500, err.Error())
		return p, nil
	}
	if prof == nil || prof.UserID != p.UserID {
		writeError(w, 404, "profile not found")
		return p, nil
	}
	return p, prof
}

// Avatars come only from AniList's CDN, so the server never fetches arbitrary URLs.
func allowedAvatar(u string) bool {
	return u == "" || strings.HasPrefix(u, "https://s4.anilist.co/") || strings.HasPrefix(u, "https://img.anili.st/")
}

var avatarClient = &http.Client{Timeout: 15 * time.Second, CheckRedirect: netguard.CheckRedirect}

// fetchAvatar caches the profile's picture as a JPEG under <images>/profiles. Best effort.
func (s *Server) fetchAvatar(ctx context.Context, prof *model.Profile) {
	if prof.AvatarURL == "" {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, prof.AvatarURL, nil)
	if err != nil {
		return
	}
	req.Header.Set("User-Agent", "Roxy/1.0 (+https://github.com/drewfoos/roxy-tv)")
	resp, err := avatarClient.Do(req)
	if err != nil {
		slog.Warn("profile avatar download failed", "url", prof.AvatarURL, "err", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		slog.Warn("profile avatar download failed", "url", prof.AvatarURL, "status", resp.StatusCode)
		return
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return
	}
	img, _, err := imaging.Decode(data) // refuses decompression bombs
	if err != nil {
		slog.Warn("profile avatar undecodable", "url", prof.AvatarURL, "err", err)
		return
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 88}); err != nil {
		return
	}
	s.saveAvatar(ctx, prof, buf.Bytes())
}

// avatarMu serialises saving profile pictures: writing the file, recording it and
// cleaning up the profile's other files happen as one step, so one save's cleanup can
// never meet another save's half-done work. Downloads run outside it, in parallel.
var avatarMu sync.Mutex

// avatarBeforeCleanup is called (under avatarMu) after a picture is recorded and before
// the profile's older files are removed. Tests pause there.
var avatarBeforeCleanup = func() {}

// saveAvatar writes a downloaded picture for prof and records it (see fetchAvatar).
func (s *Server) saveAvatar(ctx context.Context, prof *model.Profile, jpegData []byte) {
	dir := filepath.Join(s.cfg.ImagesDir(), "profiles")
	if err := safefs.MkdirAll(dir, 0o755); err != nil {
		return
	}
	// Writing, recording and cleaning up are one step under avatarMu. With the cleanup
	// after the recording but outside any lock, a save that had recorded its picture and
	// was about to clean up could delete the file of a newer save that had been written
	// meanwhile, leaving the profile pointing at a missing file.
	avatarMu.Lock()
	defer avatarMu.Unlock()
	// One file per picture (<profile>-<hash of its URL>.jpg), written whole (temp file, then
	// rename), and recorded only if that URL is still the profile's choice. It used to be
	// <profile>.jpg for every picture and recorded unconditionally, so of two overlapping
	// changes the slower download overwrote the newer picture, and the profile showed one
	// picture under the other's URL.
	base := prof.ID + "-" + avatarKey(prof.AvatarURL)
	path := filepath.Join(dir, base+".jpg")
	tmp := fmt.Sprintf("%s.%d.tmp", path, time.Now().UnixNano())
	if err := safefs.WriteFile(tmp, jpegData, 0o644); err != nil {
		return
	}
	if err := safefs.Rename(tmp, path); err != nil {
		safefs.Remove(tmp)
		return
	}
	current, err := s.store.SetProfileAvatarPath(ctx, prof.ID, prof.AvatarURL, path)
	if err != nil || !current {
		// Another picture was chosen meanwhile (or the profile is gone): this one is not
		// wanted. Its file goes; the newer choice's file has another name and is untouched.
		removeAvatarFiles(dir, base, "")
		return
	}
	prof.AvatarPath = path
	avatarBeforeCleanup()
	// The profile's earlier pictures and their resized variants, and the old <profile>.jpg.
	removeAvatarFiles(dir, prof.ID, base)
}

// removeAvatarFiles deletes the cached pictures in dir whose names start with prefix
// (followed by ".", "-" or nothing else), except those of keep (a picture's base name).
func removeAvatarFiles(dir, prefix, keep string) {
	matches, _ := filepath.Glob(filepath.Join(dir, prefix+"*.jpg"))
	for _, m := range matches {
		name := filepath.Base(m)
		rest := strings.TrimPrefix(name, prefix)
		if rest != ".jpg" && !strings.HasPrefix(rest, ".") && !strings.HasPrefix(rest, "-") {
			continue // another profile whose id merely starts the same way
		}
		if keep != "" && (name == keep+".jpg" || strings.HasPrefix(name, keep+".")) {
			continue
		}
		safefs.Remove(m)
	}
}

// GET /v1/avatars/search?q=: AniList characters by name.
func (s *Server) avatarSearch(w http.ResponseWriter, r *http.Request) {
	if s.meta == nil || s.meta.AniList() == nil {
		writeError(w, 503, "metadata service not running")
		return
	}
	chars, err := s.meta.AniList().SearchCharacters(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	if chars == nil {
		chars = []metadata.Character{}
	}
	writeJSON(w, 200, map[string]any{"characters": chars})
}

// GET /v1/avatars/suggested: the main cast of the series in the library, a few per show.
// GET /v1/avatars/suggested -> {characters}: "From your library", read from what the
// character worker saved (avatarchars.go). Instant, and never a call to AniList.
func (s *Server) avatarSuggested(w http.ResponseWriter, r *http.Request) {
	chars, err := s.store.SuggestedCharacters(r.Context(), 6, 60)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"characters": chars})
}

var _ = auth.PrincipalFrom

// checkProfileName: 1-40 characters, no control characters (a name is shown on every
// screen and on the TV; it was stored verbatim at any size).
func checkProfileName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 40 {
		return errors.New("a profile name is 1 to 40 characters")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return errors.New("a profile name cannot contain control characters")
		}
	}
	return nil
}
