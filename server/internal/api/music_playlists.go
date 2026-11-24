package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

// Playlists (docs/api/music.md): a profile's own lists of songs. Another profile's list
// is not there (404), whoever asks.

type playlistBody struct {
	Name     *string  `json:"name"`
	TrackIDs []string `json:"trackIds"`
	// EntryIDs: the playlist's entries in the order they should be in (PUT .../order), or
	// the entries to take out (POST .../remove).
	EntryIDs []int64 `json:"entryIds"`
}

func readPlaylistBody(w http.ResponseWriter, r *http.Request) (playlistBody, bool) {
	var b playlistBody
	// A playlist of the largest size, named in full, is under a megabyte.
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&b); err != nil {
		writeError(w, 400, "invalid request body")
		return b, false
	}
	if len(b.TrackIDs) > store.MaxPlaylistEntries || len(b.EntryIDs) > store.MaxPlaylistEntries {
		writeError(w, 400, "too many songs for one request")
		return b, false
	}
	return b, true
}

// GET /v1/music/playlists -> {playlists}: the profile's, the one changed last first.
func (s *Server) musicPlaylists(w http.ResponseWriter, r *http.Request) {
	p := userFrom(w, r)
	if p == nil {
		return
	}
	lists, err := s.store.ListMusicPlaylists(r.Context(), p.ScopeID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"playlists": lists})
}

// POST /v1/music/playlists {name, trackIds?} -> playlist
func (s *Server) musicCreatePlaylist(w http.ResponseWriter, r *http.Request) {
	p := userFrom(w, r)
	if p == nil {
		return
	}
	b, ok := readPlaylistBody(w, r)
	if !ok {
		return
	}
	name := ""
	if b.Name != nil {
		name = store.CleanPlaylistName(*b.Name)
	}
	if name == "" {
		writeError(w, 400, "a playlist needs a name")
		return
	}
	list, err := s.store.CreateMusicPlaylist(r.Context(), p.ScopeID, name, b.TrackIDs)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, list)
}

// GET /v1/music/playlists/{id} -> {playlist, tracks}: the songs in the list's order,
// each with its entryId.
func (s *Server) musicPlaylist(w http.ResponseWriter, r *http.Request) {
	p := userFrom(w, r)
	if p == nil {
		return
	}
	id := chi.URLParam(r, "id")
	list, err := s.store.GetMusicPlaylist(r.Context(), p.ScopeID, id)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if list == nil {
		writeError(w, 404, "playlist not found")
		return
	}
	entries, err := s.store.MusicPlaylistEntries(r.Context(), p.ScopeID, id)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if entries == nil {
		entries = []model.PlaylistEntry{}
	}
	writeJSON(w, 200, map[string]any{"playlist": list, "tracks": entries})
}

// playlistChanged answers a change to a playlist with the playlist as it is now.
func (s *Server) playlistChanged(w http.ResponseWriter, r *http.Request, scope, id string, found bool, err error) {
	switch {
	case errors.Is(err, store.ErrPlaylistFull):
		writeError(w, 400, "that is more songs than a playlist holds ("+strconv.Itoa(store.MaxPlaylistEntries)+")")
	case err != nil:
		writeError(w, 500, err.Error())
	case !found:
		writeError(w, 404, "playlist not found")
	default:
		list, err := s.store.GetMusicPlaylist(r.Context(), scope, id)
		if err != nil || list == nil {
			writeError(w, 500, "the playlist could not be read back")
			return
		}
		writeJSON(w, 200, list)
	}
}

// PATCH /v1/music/playlists/{id} {name} -> playlist
func (s *Server) musicRenamePlaylist(w http.ResponseWriter, r *http.Request) {
	p := userFrom(w, r)
	if p == nil {
		return
	}
	b, ok := readPlaylistBody(w, r)
	if !ok {
		return
	}
	name := ""
	if b.Name != nil {
		name = store.CleanPlaylistName(*b.Name)
	}
	if name == "" {
		writeError(w, 400, "a playlist needs a name")
		return
	}
	id := chi.URLParam(r, "id")
	found, err := s.store.RenameMusicPlaylist(r.Context(), p.ScopeID, id, name)
	s.playlistChanged(w, r, p.ScopeID, id, found, err)
}

// DELETE /v1/music/playlists/{id} -> 204. The songs themselves stay.
func (s *Server) musicDeletePlaylist(w http.ResponseWriter, r *http.Request) {
	p := userFrom(w, r)
	if p == nil {
		return
	}
	found, err := s.store.DeleteMusicPlaylist(r.Context(), p.ScopeID, chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if !found {
		writeError(w, 404, "playlist not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /v1/music/playlists/{id}/tracks {trackIds} -> playlist: the songs are added at
// the end, in the order given. A song already in the list is added again (its entry is
// its own): a client that wants no doubles leaves them out.
func (s *Server) musicPlaylistAdd(w http.ResponseWriter, r *http.Request) {
	p := userFrom(w, r)
	if p == nil {
		return
	}
	b, ok := readPlaylistBody(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	_, found, err := s.store.AddToMusicPlaylist(r.Context(), p.ScopeID, id, b.TrackIDs)
	s.playlistChanged(w, r, p.ScopeID, id, found, err)
}

// POST /v1/music/playlists/{id}/remove {entryIds} -> playlist: those entries are taken out.
func (s *Server) musicPlaylistRemove(w http.ResponseWriter, r *http.Request) {
	p := userFrom(w, r)
	if p == nil {
		return
	}
	b, ok := readPlaylistBody(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	found, err := s.store.RemoveFromMusicPlaylist(r.Context(), p.ScopeID, id, b.EntryIDs)
	s.playlistChanged(w, r, p.ScopeID, id, found, err)
}

// PUT /v1/music/playlists/{id}/order {entryIds} -> playlist: the entries in the order
// given. Entries the order leaves out keep their order after the ones it names.
func (s *Server) musicPlaylistOrder(w http.ResponseWriter, r *http.Request) {
	p := userFrom(w, r)
	if p == nil {
		return
	}
	b, ok := readPlaylistBody(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	found, err := s.store.OrderMusicPlaylist(r.Context(), p.ScopeID, id, b.EntryIDs)
	s.playlistChanged(w, r, p.ScopeID, id, found, err)
}
