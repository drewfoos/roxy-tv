package api

import (
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

// The sign-in page's poster wall. Public by necessity (nobody is signed in yet), so it
// gives out as little as possible: how many posters there are, and each poster by its
// position in a shuffled list (no item ids, no titles), at card size. The list is a
// random pick of series and movies with a poster, redrawn every hour.
//
//	GET /v1/showcase        -> {count}
//	GET /v1/showcase/{n}    -> image/jpeg, 300 px wide

const (
	showcaseSize    = 36
	showcaseRefresh = time.Hour
	showcaseWidth   = 300
)

type showcase struct {
	mu  sync.Mutex
	ids []string
	at  time.Time
}

func (s *Server) showcaseIDs(r *http.Request) []string {
	s.show.mu.Lock()
	defer s.show.mu.Unlock()
	if s.show.ids == nil || time.Since(s.show.at) > showcaseRefresh {
		// Ask for extra and keep only posters whose file is on disk: a broken image on
		// the sign-in page looks worse than a shorter wall.
		ids, err := s.store.ShowcasePosters(r.Context(), showcaseSize*2)
		if err == nil {
			kept := make([]string, 0, showcaseSize)
			for _, id := range ids {
				if len(kept) == showcaseSize {
					break
				}
				if img, err := s.store.GetItemImage(r.Context(), id, "poster"); err == nil && img != nil {
					if _, err := os.Stat(img.Path); err == nil {
						kept = append(kept, id)
					}
				}
			}
			s.show.ids, s.show.at = kept, time.Now()
		}
	}
	return s.show.ids
}

func (s *Server) showcaseCount(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"count": len(s.showcaseIDs(r))})
}

func (s *Server) showcasePoster(w http.ResponseWriter, r *http.Request) {
	ids := s.showcaseIDs(r)
	n, err := strconv.Atoi(chi.URLParam(r, "n"))
	if err != nil || n < 0 || n >= len(ids) {
		writeError(w, http.StatusNotFound, "no such poster")
		return
	}
	img, err := s.store.GetItemImage(r.Context(), ids[n], "poster")
	if err != nil || img == nil {
		writeError(w, http.StatusNotFound, "no poster")
		return
	}
	// Card size only, whatever the request asks for.
	q := r.URL.Query()
	q.Set("w", strconv.Itoa(showcaseWidth))
	r.URL.RawQuery = q.Encode()
	s.writeImage(w, r, img, "poster")
}
