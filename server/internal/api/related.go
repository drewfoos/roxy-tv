package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/roxy-media/roxy/server/internal/model"
)

// relatedEntry is one related title. When the library has it, `item` is set; otherwise
// only the provider's title/poster are known and the client shows it as "not in library".
type relatedEntry struct {
	Relation   string      `json:"relation"` // movie | ova | special | side_story | spin_off | alternative | summary | sequel | prequel | parent | other
	Provider   string      `json:"provider"`
	ProviderID string      `json:"providerId"`
	Title      string      `json:"title"`
	Format     string      `json:"format,omitempty"`
	Year       int         `json:"year,omitempty"`
	PosterURL  string      `json:"posterUrl,omitempty"` // provider-hosted; only when not in library
	Episodes   int         `json:"episodes,omitempty"`
	InLibrary  bool        `json:"inLibrary"`
	Item       *model.Item `json:"item,omitempty"`
}

// GET /v1/items/{id}/related
//
// Returns {related: [...], partOf: [Item]}: `related` are the provider's linked titles
// (movies, OVAs, side stories) resolved against the library; `partOf` lists library
// series that link to this item (a movie's parent franchise).
func (s *Server) related(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	it, err := s.store.GetItem(ctx, id)
	if err != nil || it == nil {
		writeError(w, 404, "item not found")
		return
	}
	rels, err := s.store.RelationsFor(ctx, it.ID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	byProvider := map[string][]string{}
	for _, rl := range rels {
		byProvider[rl.Provider] = append(byProvider[rl.Provider], rl.ProviderID)
	}
	resolved := map[string]model.Item{}
	for p, ids := range byProvider {
		m, err := s.store.ItemsByProviderIDs(ctx, p, ids)
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
		for k, v := range m {
			resolved[k] = v
		}
	}
	out := make([]relatedEntry, 0, len(rels))
	var libItems []model.Item
	for _, rl := range rels {
		e := relatedEntry{Relation: rl.Relation, Provider: rl.Provider, ProviderID: rl.ProviderID, Title: rl.Title, Format: rl.Format, Year: rl.Year, Episodes: rl.Episodes}
		if li, ok := resolved[rl.Provider+":"+rl.ProviderID]; ok {
			e.InLibrary = true
			cp := li
			e.Item = &cp
			libItems = append(libItems, cp)
		} else {
			e.PosterURL = rl.PosterURL
		}
		out = append(out, e)
	}
	fillAvailability(libItems)
	s.attachUserData(ctx, libItems)
	n := 0
	for i := range out {
		if out[i].InLibrary {
			out[i].Item = &libItems[n]
			n++
		}
	}

	var partOf []model.Item
	for p, v := range it.ProviderIDs {
		parents, err := s.store.ParentsByRelation(ctx, p, v)
		if err == nil {
			partOf = append(partOf, parents...)
		}
	}
	if partOf == nil {
		partOf = []model.Item{}
	}
	s.attachUserData(ctx, partOf)
	writeJSON(w, 200, map[string]any{"related": out, "partOf": partOf})
}
