package metadata

import (
	"context"
	"sort"

	"github.com/roxy-media/roxy/server/internal/model"
)

// StoryGraph is a series as AniList links it: its seasons in order (one AniList entry
// each) and the films those seasons name, with every link kept as AniList states it.
// Nothing is decided here: Discover reads the links to say where a film sits and
// whether it may come along with a request (discover/films.go). Ids are AniList's, the
// one identity that stays the same however a page numbers or orders the seasons.
type StoryGraph struct {
	Seasons []StorySeason `json:"seasons"`
	Films   []StoryFilm   `json:"films,omitempty"`
}

type StorySeason struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Start  string `json:"start,omitempty"` // 2018-10-04, 2018-10 or 2018
	Format string `json:"format,omitempty"`
}

type StoryFilm struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Romaji string `json:"romaji,omitempty"`
	Start  string `json:"start,omitempty"`
	Year   int    `json:"year,omitempty"`
	Poster string `json:"poster,omitempty"`
	// Links: what each season calls the film (SEQUEL: the film follows that season).
	Links []StoryLink `json:"links,omitempty"`
	// Paths: the season pairs AniList joins through this film (and no direct edge).
	Paths []StoryPath `json:"paths,omitempty"`
}

// StoryLink is one season's relation to a film, read from the season's side.
type StoryLink struct {
	Season   int    `json:"season"`
	Relation string `json:"relation"` // SEQUEL | PREQUEL | SIDE_STORY | SPIN_OFF | ALTERNATIVE | SUMMARY | COMPILATION | PARENT
}

// StoryPath: season After leads to season Before only through films.
type StoryPath struct {
	After  int   `json:"after"`
	Before int   `json:"before"`
	Films  []int `json:"films,omitempty"`
}

// storyRelations are the relation types that say something about a film's place in a
// series. Crossovers (CHARACTER, OTHER) and source material say nothing and are left out.
var storyRelations = map[string]bool{
	"SEQUEL": true, "PREQUEL": true, "SIDE_STORY": true, "SPIN_OFF": true,
	"ALTERNATIVE": true, "SUMMARY": true, "COMPILATION": true, "PARENT": true,
}

// inverseRelation is a film's edge to a season, read from the season's side.
func inverseRelation(t string) string {
	switch t {
	case "PREQUEL":
		return "SEQUEL" // the season is the film's prequel: the film follows it
	case "SEQUEL":
		return "PREQUEL"
	case "PARENT":
		return "SIDE_STORY"
	case "SIDE_STORY":
		return "PARENT"
	case "CONTAINS":
		return "COMPILATION"
	}
	return t
}

// StoryGraph finds a series on AniList (by id when known, else by title and year) and
// returns its seasons and their films. Nil when AniList does not know the series.
func (a *AniList) StoryGraph(ctx context.Context, anilistID int, title string, year int) (*StoryGraph, error) {
	var media *anilistMedia
	if anilistID > 0 {
		resp, err := a.query(ctx, anilistByIDQuery, map[string]any{"id": anilistID})
		if err != nil {
			return nil, err
		}
		media = resp.Data.Media
	}
	if media == nil {
		if title == "" {
			return nil, nil
		}
		var err error
		media, err = a.search(ctx, &model.Item{Type: model.TypeSeries, Title: title, Year: &year})
		if err != nil || media == nil {
			return nil, err
		}
	}
	w, err := a.walkChain(ctx, media)
	if err != nil {
		return nil, err
	}
	return storyGraph(w), nil
}

// storyGraph is the pure part: a finished walk as a graph.
func storyGraph(w *chainWalk) *StoryGraph {
	g := &StoryGraph{}
	films := map[int]*StoryFilm{}
	var order []int
	film := func(id int) *StoryFilm {
		f := films[id]
		if f == nil {
			f = &StoryFilm{ID: id}
			films[id] = f
			order = append(order, id)
		}
		return f
	}
	link := func(f *StoryFilm, season int, rel string) {
		for _, l := range f.Links {
			if l.Season == season && l.Relation == rel {
				return
			}
		}
		f.Links = append(f.Links, StoryLink{Season: season, Relation: rel})
	}
	for _, m := range w.chain {
		g.Seasons = append(g.Seasons, StorySeason{ID: m.ID, Title: firstNonEmpty(m.Title.English, m.Title.Romaji), Start: m.StartDate.String(), Format: m.Format})
		for i := range m.Relations.Edges {
			e := &m.Relations.Edges[i]
			if e.Node.Type != "ANIME" || e.Node.Format != "MOVIE" || !storyRelations[e.RelationType] || w.seen[e.Node.ID] {
				continue
			}
			f := film(e.Node.ID)
			if f.Title == "" {
				f.Title, f.Romaji = e.title(), e.Node.Title.Romaji
				f.Start, f.Year, f.Poster = e.Node.StartDate.String(), e.Node.StartDate.Year, e.Node.CoverImage.ExtraLarge
			}
			link(f, m.ID, e.RelationType)
		}
	}
	// Films read in full on the way (a run of films between two seasons): their own
	// edges to the seasons count too, and the middle of a run has no season edge at all.
	for id, m := range w.films {
		if w.seen[id] || m.Format != "MOVIE" {
			continue
		}
		f := film(id)
		f.Title, f.Romaji = firstNonEmpty(m.Title.English, m.Title.Romaji), m.Title.Romaji
		f.Start, f.Year = m.StartDate.String(), m.StartDate.Year
		if p := firstNonEmpty(m.CoverImage.ExtraLarge, m.CoverImage.Large); p != "" {
			f.Poster = p
		}
		for i := range m.Relations.Edges {
			e := &m.Relations.Edges[i]
			if rel := inverseRelation(e.RelationType); w.seen[e.Node.ID] && storyRelations[rel] {
				link(f, e.Node.ID, rel)
			}
		}
	}
	for _, p := range w.paths {
		for _, id := range p.Films {
			if f := films[id]; f != nil {
				f.Paths = append(f.Paths, StoryPath{After: p.After, Before: p.Before})
			}
		}
	}
	sort.Ints(order)
	for _, id := range order {
		f := films[id]
		sort.Slice(f.Links, func(i, j int) bool {
			if f.Links[i].Season != f.Links[j].Season {
				return f.Links[i].Season < f.Links[j].Season
			}
			return f.Links[i].Relation < f.Links[j].Relation
		})
		g.Films = append(g.Films, *f)
	}
	return g
}
