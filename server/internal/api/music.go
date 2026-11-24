package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/roxy-media/roxy/server/internal/library"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/music"
	"github.com/roxy-media/roxy/server/internal/store"
)

// Music (docs/api/music.md). Songs are read from music folders by their tags; an album
// is listed once two of its songs are there, and plays and favourites are per profile.

// musicScanEvery is how often the music folders are looked at again for new, changed and
// removed files. A pass over an unchanged library only lists its folders.
const musicScanEvery = 10 * time.Minute

// musicConvertedMax is how much room converted songs may take (the longest unused go first).
const musicConvertedMax = 2 << 30

// musicPageMax is the most tracks one request lists.
const musicPageMax = 1000

func queryInt(r *http.Request, name string, def int) int {
	n, err := strconv.Atoi(r.URL.Query().Get(name))
	if err != nil || n < 0 {
		return def
	}
	return n
}

func queryFlag(r *http.Request, name string) bool {
	v := r.URL.Query().Get(name)
	return v == "1" || v == "true"
}

// musicEntry is one tile of "recently added": an album, or a song that stands alone.
type musicEntry struct {
	Album *model.MusicAlbum `json:"album,omitempty"`
	Track *model.MusicTrack `json:"track,omitempty"`
}

// recentlyAddedMax is how many tiles "recently added" holds.
const recentlyAddedMax = 30

// recentlyAdded lists what came in last, newest first. An album that arrived is one
// entry, not one per song (twenty songs copied in together would fill the row with one
// cover); a song that is not part of a listed album stands by itself.
func (s *Server) recentlyAdded(ctx context.Context, scope string) ([]musicEntry, error) {
	tracks, _, err := s.store.ListMusicTracks(ctx, scope, store.MusicTrackQuery{Sort: "added", Limit: 400})
	if err != nil {
		return nil, err
	}
	var ids []string
	seen := map[string]bool{}
	for _, t := range tracks {
		if t.AlbumID != "" && !seen[t.AlbumID] {
			seen[t.AlbumID] = true
			ids = append(ids, t.AlbumID)
		}
	}
	albums := map[string]*model.MusicAlbum{}
	if len(ids) > 0 {
		// Only albums that are listed as albums (two songs or more) come back.
		list, _, err := s.store.ListMusicAlbums(ctx, scope, store.MusicAlbumQuery{IDs: ids})
		if err != nil {
			return nil, err
		}
		for i := range list {
			albums[list[i].ID] = &list[i]
		}
	}
	out := []musicEntry{}
	shown := map[string]bool{}
	for i := range tracks {
		if len(out) >= recentlyAddedMax {
			break
		}
		t := &tracks[i]
		if al := albums[t.AlbumID]; al != nil {
			if !shown[al.ID] {
				shown[al.ID] = true
				out = append(out, musicEntry{Album: al})
			}
			continue
		}
		out = append(out, musicEntry{Track: t})
	}
	return out, nil
}

// GET /v1/music -> {counts, scanning, recentlyAdded, recentlyPlayed, mostPlayed}
// The Music page's first screen. counts.tracks is 0 on a server with no music: clients
// then show no Music section at all. recentlyAdded is a list of {album} or {track}.
func (s *Server) musicHome(w http.ResponseWriter, r *http.Request) {
	p := userFrom(w, r)
	if p == nil {
		return
	}
	ctx := r.Context()
	counts, err := s.store.MusicCounts(ctx)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	out := map[string]any{"counts": counts, "scanning": s.music.Status().Scanning}
	if counts.Tracks > 0 {
		added, err := s.recentlyAdded(ctx, p.ScopeID)
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
		out["recentlyAdded"] = added
		for name, q := range map[string]store.MusicTrackQuery{
			"recentlyPlayed": {Sort: "recent", Limit: 20},
			"mostPlayed":     {Sort: "plays", Played: true, Limit: 20},
		} {
			tracks, _, err := s.store.ListMusicTracks(ctx, p.ScopeID, q)
			if err != nil {
				writeError(w, 500, err.Error())
				return
			}
			out[name] = orEmptyTracks(tracks)
		}
	}
	writeJSON(w, 200, out)
}

func orEmptyTracks(t []model.MusicTrack) []model.MusicTrack {
	if t == nil {
		return []model.MusicTrack{}
	}
	return t
}

func orEmptyAlbums(a []model.MusicAlbum) []model.MusicAlbum {
	if a == nil {
		return []model.MusicAlbum{}
	}
	return a
}

func orEmptyArtists(a []model.MusicArtist) []model.MusicArtist {
	if a == nil {
		return []model.MusicArtist{}
	}
	return a
}

// GET /v1/music/tracks?sort=&q=&artist=&album=&favorite=1&played=1&ids=a,b&limit=&offset=
// -> {total, tracks}
func (s *Server) musicTracks(w http.ResponseWriter, r *http.Request) {
	p := userFrom(w, r)
	if p == nil {
		return
	}
	q := r.URL.Query()
	query := store.MusicTrackQuery{
		ArtistID: q.Get("artist"), AlbumID: q.Get("album"), Favorite: queryFlag(r, "favorite"), Played: queryFlag(r, "played"),
		Search: music.Fold(q.Get("q")), Sort: q.Get("sort"),
		Limit: queryInt(r, "limit", 200), Offset: queryInt(r, "offset", 0),
	}
	if query.Limit == 0 || query.Limit > musicPageMax {
		query.Limit = musicPageMax
	}
	if ids := q.Get("ids"); ids != "" {
		for _, id := range strings.Split(ids, ",") {
			if id = strings.TrimSpace(id); id != "" && len(query.IDs) < musicPageMax {
				query.IDs = append(query.IDs, id)
			}
		}
	}
	tracks, total, err := s.store.ListMusicTracks(r.Context(), p.ScopeID, query)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"total": total, "tracks": orEmptyTracks(tracks)})
}

// musicTrackFromURL finds the track a request names, for the caller's profile.
func (s *Server) musicTrackFromURL(w http.ResponseWriter, r *http.Request) (*model.MusicTrack, *model.Principal) {
	p := userFrom(w, r)
	if p == nil {
		return nil, nil
	}
	t, err := s.store.GetMusicTrack(r.Context(), p.ScopeID, chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 500, err.Error())
		return nil, nil
	}
	if t == nil {
		writeError(w, 404, "song not found")
		return nil, nil
	}
	return t, p
}

// GET /v1/music/tracks/{id} -> track
func (s *Server) musicTrack(w http.ResponseWriter, r *http.Request) {
	if t, _ := s.musicTrackFromURL(w, r); t != nil {
		writeJSON(w, 200, t)
	}
}

// GET /v1/music/tracks/{id}/lyrics -> {source, synced, lines: [{ms, text}]}; 404 when the
// song has none. A lyrics file beside the song ("Song.lrc", then "Song.txt") comes before
// the words the audio file carries.
func (s *Server) musicLyrics(w http.ResponseWriter, r *http.Request) {
	t, _ := s.musicTrackFromURL(w, r)
	if t == nil {
		return
	}
	for _, kind := range []string{"lrc", "txt"} {
		path, ok := music.SidecarPath(t.Path, kind)
		if !ok {
			continue
		}
		real, ok := mediaFile(w, path)
		if !ok {
			return
		}
		if text, ok := music.ReadLyricsFile(real); ok {
			if l := music.ParseLyrics(text, kind); len(l.Lines) > 0 {
				writeJSON(w, 200, l)
				return
			}
		}
	}
	if l := music.ParseLyrics(t.LyricsText, "embedded"); len(l.Lines) > 0 {
		writeJSON(w, 200, l)
		return
	}
	writeError(w, 404, "this song has no lyrics")
}

// GET|HEAD /v1/music/tracks/{id}/stream[?format=opus|mp3|aac|flac&bitrate=<kbit/s>]: the
// audio file as it is, or (with format) converted, both with HTTP ranges. A converted
// song is made whole before the first byte is sent and kept, so it can be sought in like
// any file; the first request for it waits for the conversion (a second or two a song).
func (s *Server) musicStream(w http.ResponseWriter, r *http.Request) {
	t, _ := s.musicTrackFromURL(w, r)
	if t == nil {
		return
	}
	path, ok := mediaFile(w, t.Path)
	if !ok {
		return
	}
	ctype, name := audioMime(t.Container, t.Codec), filepath.Base(t.Path)
	if format := r.URL.Query().Get("format"); format != "" && format != "original" {
		f, err := music.ParseAudioFormat(format, r.URL.Query().Get("bitrate"))
		if err != nil {
			writeError(w, 400, err.Error())
			return
		}
		converted, err := s.musicTC.File(r.Context(), music.Source{TrackID: t.ID, Path: path, Size: t.Size, Mtime: t.Mtime, SampleRate: t.SampleRate, Channels: t.Channels}, f)
		if err != nil {
			if r.Context().Err() != nil {
				return // the player left; the conversion goes on for the next request
			}
			slog.Warn("music: conversion failed", "track", t.ID, "format", f.Codec, "err", err)
			writeError(w, 500, "this song could not be converted")
			return
		}
		path, ctype = converted, f.Mime()
		name = strings.TrimSuffix(name, filepath.Ext(name)) + "." + f.Ext()
	}
	f, err := os.Open(path)
	if err != nil {
		writeError(w, 404, "file missing on disk")
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("ETag", fmt.Sprintf(`"%x-%x"`, fi.ModTime().UnixNano(), fi.Size()))
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("Content-Disposition", contentDisposition("inline", name))
	http.ServeContent(w, r, name, fi.ModTime(), f)
}

// audioMime names an audio file's type for a browser's player.
func audioMime(container, codec string) string {
	switch container {
	case "mp3":
		return "audio/mpeg"
	case "flac":
		return "audio/flac"
	case "m4a", "m4b", "mp4":
		return "audio/mp4"
	case "aac":
		return "audio/aac"
	case "ogg":
		return "audio/ogg"
	case "opus":
		return "audio/ogg"
	case "wav":
		return "audio/wav"
	case "aiff":
		return "audio/aiff"
	case "wma":
		return "audio/x-ms-wma"
	case "mka":
		return "audio/x-matroska"
	}
	return "application/octet-stream"
}

// POST /v1/music/tracks/{id}/played {at} -> {playCount, counted}. A player sends it once
// per listen, when half the song (or four minutes) has been played. `at` is when the
// player counted it (Unix milliseconds; now when left out): it names the listen, so the
// same report sent again after a lost answer is not a second listen, and a report kept
// while the server was away keeps its own time.
func (s *Server) musicPlayed(w http.ResponseWriter, r *http.Request) {
	t, p := s.musicTrackFromURL(w, r)
	if t == nil {
		return
	}
	var b struct {
		At int64 `json:"at"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&b)
	now := time.Now()
	at := now
	if b.At > 0 {
		at = time.UnixMilli(b.At)
		// A clock that is ahead cannot put a listen in the future; one that is far behind
		// (or a report kept for months) is taken as now.
		if at.After(now.Add(2*time.Minute)) || at.Before(now.Add(-musicPlayMaxAge)) {
			at = now
		}
	}
	// The time as the player gave it tells this report from another, whatever it was kept as.
	counted, err := s.store.RecordMusicPlay(r.Context(), p.ScopeID, t.ID, at, max(b.At, 0))
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	count := t.PlayCount
	if counted {
		count++
	}
	writeJSON(w, 200, map[string]any{"playCount": count, "counted": counted})
}

// musicPlayMaxAge is how old a kept listen may be and still be dated as the player says.
const musicPlayMaxAge = 30 * 24 * time.Hour

// GET /v1/music/artists?q=&guests=1&favorite=1&limit=&offset= -> {total, artists}
// The artists who lead at least one song; guests=1 adds those who only appear on others'.
func (s *Server) musicArtists(w http.ResponseWriter, r *http.Request) {
	p := userFrom(w, r)
	if p == nil {
		return
	}
	artists, total, err := s.store.ListMusicArtists(r.Context(), p.ScopeID, store.MusicArtistQuery{
		Search: music.Fold(r.URL.Query().Get("q")), Guests: queryFlag(r, "guests"), Favorite: queryFlag(r, "favorite"),
		Limit: queryInt(r, "limit", 0), Offset: queryInt(r, "offset", 0),
	})
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"total": total, "artists": orEmptyArtists(artists)})
}

// GET /v1/music/artists/{id} -> {artist, tracks, albums}: every song the artist is
// credited on (most played first, then by title) and their albums, newest first.
func (s *Server) musicArtist(w http.ResponseWriter, r *http.Request) {
	p := userFrom(w, r)
	if p == nil {
		return
	}
	ctx := r.Context()
	a, err := s.store.GetMusicArtist(ctx, p.ScopeID, chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if a == nil || a.TrackCount == 0 {
		writeError(w, 404, "artist not found")
		return
	}
	tracks, _, err := s.store.ListMusicTracks(ctx, p.ScopeID, store.MusicTrackQuery{ArtistID: a.ID, Sort: "plays"})
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	albums, _, err := s.store.ListMusicAlbums(ctx, p.ScopeID, store.MusicAlbumQuery{ArtistID: a.ID, Sort: "year"})
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"artist": a, "tracks": orEmptyTracks(tracks), "albums": orEmptyAlbums(albums)})
}

// GET /v1/music/albums?q=&artist=&sort=&favorite=1&singles=1&limit=&offset= -> {total, albums}
func (s *Server) musicAlbums(w http.ResponseWriter, r *http.Request) {
	p := userFrom(w, r)
	if p == nil {
		return
	}
	q := r.URL.Query()
	albums, total, err := s.store.ListMusicAlbums(r.Context(), p.ScopeID, store.MusicAlbumQuery{
		ArtistID: q.Get("artist"), Search: music.Fold(q.Get("q")), Favorite: queryFlag(r, "favorite"), Singles: queryFlag(r, "singles"),
		Sort: q.Get("sort"), Limit: queryInt(r, "limit", 0), Offset: queryInt(r, "offset", 0),
	})
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"total": total, "albums": orEmptyAlbums(albums)})
}

// GET /v1/music/albums/{id} -> {album, tracks} in disc and track order.
func (s *Server) musicAlbum(w http.ResponseWriter, r *http.Request) {
	p := userFrom(w, r)
	if p == nil {
		return
	}
	ctx := r.Context()
	a, err := s.store.GetMusicAlbum(ctx, p.ScopeID, chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if a == nil {
		writeError(w, 404, "album not found")
		return
	}
	tracks, _, err := s.store.ListMusicTracks(ctx, p.ScopeID, store.MusicTrackQuery{AlbumID: a.ID, Sort: "album"})
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"album": a, "tracks": orEmptyTracks(tracks)})
}

// GET /v1/music/search?q= -> {artists, albums, tracks}: a few of each. Every word of q
// must be found; accents and case do not matter.
func (s *Server) musicSearch(w http.ResponseWriter, r *http.Request) {
	p := userFrom(w, r)
	if p == nil {
		return
	}
	ctx := r.Context()
	q := music.Fold(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, 200, map[string]any{"artists": []model.MusicArtist{}, "albums": []model.MusicAlbum{}, "tracks": []model.MusicTrack{}})
		return
	}
	artists, _, err := s.store.ListMusicArtists(ctx, p.ScopeID, store.MusicArtistQuery{Search: q, Guests: true, Limit: 12})
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	albums, _, err := s.store.ListMusicAlbums(ctx, p.ScopeID, store.MusicAlbumQuery{Search: q, Limit: 12})
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	tracks, _, err := s.store.ListMusicTracks(ctx, p.ScopeID, store.MusicTrackQuery{Search: q, Sort: "plays", Limit: 50})
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"artists": orEmptyArtists(artists), "albums": orEmptyAlbums(albums), "tracks": orEmptyTracks(tracks)})
}

// GET /v1/music/art/{hash}[?w=<px>]: a cover picture. The address names the picture's
// own bytes, so what it shows never changes and a client may keep it for good. ?w=
// resizes (never enlarges) to one of a few fixed widths.
func (s *Server) musicArt(w http.ResponseWriter, r *http.Request) {
	hash := chi.URLParam(r, "hash")
	if !music.ValidArtHash(hash) {
		writeError(w, 404, "no such picture")
		return
	}
	path := s.music.Art().Path(hash)
	if _, err := os.Stat(path); err != nil {
		writeError(w, 404, "no such picture")
		return
	}
	if width, _ := strconv.Atoi(r.URL.Query().Get("w")); width > 0 {
		width = snapWidth(width)
		variant := fmt.Sprintf("%s.w%d.jpg", path, width)
		fresh := variantFresh(variant, path)
		if !fresh {
			// A picture that cannot be made smaller (a file whose header reads as an image
			// and whose body does not decode: a cover saved with a bad checksum) is sent as
			// it is: a browser is more forgiving than the decoder here, and one that is not
			// shows its placeholder.
			if err := resizeToFile(path, variant, width); err != nil {
				slog.Debug("music: cover not resized, sent as it is", "hash", hash, "err", err)
			} else {
				fresh = true
			}
		}
		if fresh {
			path = variant
		}
	}
	f, err := os.Open(path)
	if err != nil {
		writeError(w, 404, "no such picture")
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	ctype := "image/jpeg"
	head := make([]byte, 512)
	if n, _ := f.Read(head); n > 0 {
		if t := http.DetectContentType(head[:n]); strings.HasPrefix(t, "image/") {
			ctype = t
		}
		_, _ = f.Seek(0, 0)
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable") // signed-in only: no shared cache
	http.ServeContent(w, r, "", fi.ModTime(), f)
}

// POST|DELETE /v1/music/favorites/{kind}/{id} -> 204. kind is track, album or artist.
func (s *Server) musicFavorite(w http.ResponseWriter, r *http.Request) {
	p := userFrom(w, r)
	if p == nil {
		return
	}
	kind, id := chi.URLParam(r, "kind"), chi.URLParam(r, "id")
	ok, err := s.store.MusicExists(r.Context(), kind, id)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if !ok {
		writeError(w, 404, "not found")
		return
	}
	if err := s.store.SetMusicFavorite(r.Context(), p.ScopeID, kind, id, r.Method == http.MethodPost); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- music folders (admin) ----

// GET /v1/music/folders -> {folders: [{id, name, root, reachable, tracks}], scan}
func (s *Server) musicFolders(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	folders, err := s.store.MusicFolders(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	counts, err := s.store.MusicFolderTrackCounts(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	type out struct {
		model.MusicFolder
		Reachable bool `json:"reachable"`
		Tracks    int  `json:"tracks"`
	}
	list := make([]out, 0, len(folders))
	for _, f := range folders {
		list = append(list, out{MusicFolder: f, Reachable: isDir(f.Root), Tracks: counts[f.ID]})
	}
	writeJSON(w, 200, map[string]any{"folders": list, "scan": s.music.Status(), "loudness": s.musicLoudness(r)})
}

// POST /v1/music/folders {path, name} -> folder (and a scan starts)
func (s *Server) musicAddFolder(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var b struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&b); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	root, err := s.checkNewMusicRoot(r.Context(), b.Path)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	f := &model.MusicFolder{Name: strings.TrimSpace(b.Name), Root: root}
	if f.Name == "" {
		f.Name = filepath.Base(root)
	}
	if err := s.store.CreateMusicFolder(r.Context(), f); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	_ = s.store.SetRootRemoved(r.Context(), root, false)
	library.GuardMediaRoots(r.Context(), s.store, s.cfg.MediaRoots)
	slog.Info("music folder added", "name", f.Name, "root", root)
	go s.musicScanNow(false)
	writeJSON(w, 201, f)
}

// DELETE /v1/music/folders/{id} -> 204. Roxy forgets the folder and its songs; the files
// stay on disk.
func (s *Server) musicRemoveFolder(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, 404, "folder not found")
		return
	}
	f, err := s.store.GetMusicFolder(r.Context(), id)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if f == nil {
		writeError(w, 404, "folder not found")
		return
	}
	if err := s.store.DeleteMusicFolder(r.Context(), id); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	// Remembered, so that ROXY_MUSIC_ROOTS does not bring it back at the next start.
	_ = s.store.SetRootRemoved(r.Context(), f.Root, true)
	library.GuardMediaRoots(r.Context(), s.store, s.cfg.MediaRoots)
	slog.Info("music folder removed (files untouched)", "name", f.Name, "root", f.Root)
	w.WriteHeader(http.StatusNoContent)
}

// POST /v1/music/scan {force} -> 202: look at the music folders now (force reads every
// file again). GET /v1/music/scan -> what the scanner is doing and what it last found.
func (s *Server) musicScan(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if r.Method == http.MethodGet {
		writeJSON(w, 200, struct {
			music.Status
			Loudness map[string]any `json:"loudness"`
		}{s.music.Status(), s.musicLoudness(r)})
		return
	}
	var b struct {
		Force bool `json:"force"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&b)
	go s.musicScanNow(b.Force)
	w.WriteHeader(http.StatusAccepted)
}

// musicScanNow reads the music folders and then measures what is new for loudness.
func (s *Server) musicScanNow(force bool) {
	ctx := context.Background()
	if err := s.music.Scan(ctx, force); err != nil {
		slog.Warn("music: scan failed", "err", err)
	}
	s.music.AfterScan(ctx, false, false)
}

// musicLoudness says how far the loudness measuring is: {measured, total, measuring}.
func (s *Server) musicLoudness(r *http.Request) map[string]any {
	measured, total, _ := s.store.MusicLoudnessCounts(r.Context())
	return map[string]any{"measured": measured, "total": total, "measuring": s.music.Measuring()}
}

// checkNewMusicRoot: an existing, readable folder that is not already a music folder,
// inside one, or around one.
func (s *Server) checkNewMusicRoot(ctx context.Context, path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" || !filepath.IsAbs(path) {
		return "", errors.New("pick a folder (a full path like /data/media/music)")
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
	folders, err := s.store.MusicFolders(ctx)
	if err != nil {
		return "", err
	}
	for _, f := range folders {
		switch {
		case f.Root == root:
			return "", errors.New("that folder is already the music folder \"" + f.Name + "\"")
		case within(root, f.Root):
			return "", errors.New("that folder is inside the music folder \"" + f.Name + "\", which already reads it")
		case within(f.Root, root):
			return "", errors.New("the music folder \"" + f.Name + "\" is inside that folder; remove it first or pick a different folder")
		}
	}
	return root, nil
}
