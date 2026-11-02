package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/roxy-media/roxy/server/internal/safefs"
	"hash/fnv"
	"image"
	"image/jpeg"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/roxy-media/roxy/server/internal/auth"
	"github.com/roxy-media/roxy/server/internal/metadata"
	"github.com/roxy-media/roxy/server/internal/model"
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
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		writeError(w, 400, "a profile name is required")
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
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	if body.AvatarURL != "" && body.AvatarURL != "-" && !allowedAvatar(body.AvatarURL) {
		writeError(w, 400, "avatarUrl must be an AniList image")
		return
	}
	if err := s.store.UpdateProfile(r.Context(), prof.ID, strings.TrimSpace(body.Name), body.AvatarURL, body.AvatarName); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	prof, _ = s.store.GetProfile(r.Context(), prof.ID)
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
	if width, _ := strconv.Atoi(r.URL.Query().Get("w")); width >= 16 && width < 1024 {
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

var avatarClient = &http.Client{Timeout: 15 * time.Second}

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
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		slog.Warn("profile avatar undecodable", "url", prof.AvatarURL, "err", err)
		return
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 88}); err != nil {
		return
	}
	dir := filepath.Join(s.cfg.ImagesDir(), "profiles")
	if err := safefs.MkdirAll(dir, 0o755); err != nil {
		return
	}
	path := filepath.Join(dir, prof.ID+".jpg")
	if err := safefs.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return
	}
	// Drop stale resized variants from a previous avatar.
	matches, _ := filepath.Glob(filepath.Join(dir, prof.ID+".w*.jpg"))
	for _, m := range matches {
		safefs.Remove(m)
	}
	if err := s.store.SetProfileAvatarPath(ctx, prof.ID, path); err == nil {
		prof.AvatarPath = path
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
