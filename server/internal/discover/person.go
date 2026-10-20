package discover

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// Person is one cast member as the app's person page shows them: who they are, and
// what they have been in, with the library's titles marked.
type Person struct {
	TMDBID       int      `json:"tmdbId"`
	Name         string   `json:"name"`
	Biography    string   `json:"biography,omitempty"`
	Birthday     string   `json:"birthday,omitempty"`
	Deathday     string   `json:"deathday,omitempty"`
	PlaceOfBirth string   `json:"placeOfBirth,omitempty"`
	KnownFor     string   `json:"knownFor,omitempty"` // TMDB's department: Acting, Directing ...
	Photo        string   `json:"photo,omitempty"`    // absolute TMDB URL (h632)
	AlsoKnownAs  []string `json:"alsoKnownAs,omitempty"`
	Homepage     string   `json:"homepage,omitempty"`
	IMDBID       string   `json:"imdbId,omitempty"`
	// Credits are the titles they appear in, best known first, each with the role.
	Credits []Item `json:"credits"`
}

type tmdbPerson struct {
	ID           int      `json:"id"`
	Name         string   `json:"name"`
	Biography    string   `json:"biography"`
	Birthday     string   `json:"birthday"`
	Deathday     string   `json:"deathday"`
	PlaceOfBirth string   `json:"place_of_birth"`
	KnownFor     string   `json:"known_for_department"`
	ProfilePath  string   `json:"profile_path"`
	AlsoKnownAs  []string `json:"also_known_as"`
	Homepage     string   `json:"homepage"`
	IMDBID       string   `json:"imdb_id"`
	Credits      struct {
		Cast []tmdbCredit `json:"cast"`
	} `json:"combined_credits"`
}

type tmdbCredit struct {
	tmdbListEntry
	Character string `json:"character"`
	VoteCount int    `json:"vote_count"`
	Episodes  int    `json:"episode_count"`
}

// Person is TMDB's person with their combined credits, cached for an hour.
func (s *Service) Person(ctx context.Context, tmdbID int) (*Person, error) {
	key := strconv.Itoa(tmdbID)
	if p, ok := s.people.get(key); ok {
		out := *p
		out.Credits = append([]Item(nil), p.Credits...)
		s.annotate(ctx, out.Credits) // library and request state are local and may have changed
		return &out, nil
	}
	var d tmdbPerson
	if err := s.tmdb.Get(ctx, fmt.Sprintf("/person/%d", tmdbID), url.Values{"append_to_response": {"combined_credits"}}, &d); err != nil {
		return nil, err
	}
	p := &Person{
		TMDBID: d.ID, Name: d.Name, Biography: strings.TrimSpace(d.Biography), Birthday: d.Birthday, Deathday: d.Deathday,
		PlaceOfBirth: d.PlaceOfBirth, KnownFor: d.KnownFor, Photo: s.tmdb.ImageURL("h632", d.ProfilePath),
		AlsoKnownAs: d.AlsoKnownAs, Homepage: d.Homepage, IMDBID: d.IMDBID,
	}
	// One entry per title (a voice actor is credited per character in a long series),
	// the best-known first: votes carry the weight, a guest spot in one episode counts less.
	type scored struct {
		item  Item
		score float64
	}
	seen := map[string]int{}
	var list []scored
	for _, c := range d.Credits.Cast {
		if (c.MediaType != "tv" && c.MediaType != "movie") || c.PosterPath == "" {
			continue
		}
		k := c.MediaType + ":" + strconv.Itoa(c.ID)
		score := float64(c.VoteCount)
		if c.MediaType == "tv" && c.Episodes > 0 && c.Episodes < 3 {
			score *= 0.2
		}
		if i, ok := seen[k]; ok {
			if c.Character != "" && !strings.Contains(list[i].item.Character, c.Character) {
				list[i].item.Character += " / " + c.Character
			}
			continue
		}
		it := s.toItem(ctx, c.tmdbListEntry, c.MediaType)
		it.Character = c.Character
		seen[k] = len(list)
		list = append(list, scored{it, score})
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].score > list[j].score })
	if len(list) > 60 {
		list = list[:60]
	}
	p.Credits = make([]Item, 0, len(list))
	for _, sc := range list {
		p.Credits = append(p.Credits, sc.item)
	}
	s.people.put(key, p)
	out := *p
	out.Credits = append([]Item(nil), p.Credits...)
	s.annotate(ctx, out.Credits)
	return &out, nil
}

// Similar is "More like this" for a title: TMDB's recommendations (what people who
// watched it also watched), falling back to its "similar" list when there are none.
// Cached an hour; the library and request marks are refreshed every call.
func (s *Service) Similar(ctx context.Context, kind string, tmdbID int) ([]Item, error) {
	if kind != "tv" && kind != "movie" {
		return nil, fmt.Errorf("type must be tv or movie")
	}
	key := fmt.Sprintf("%s:%d", kind, tmdbID)
	items, ok := s.similar.get(key)
	if !ok {
		var err error
		items, err = s.list(ctx, fmt.Sprintf("/%s/%d/recommendations", kind, tmdbID), url.Values{}, kind, 30)
		if err != nil {
			return nil, err
		}
		if len(items) == 0 {
			if items, err = s.list(ctx, fmt.Sprintf("/%s/%d/similar", kind, tmdbID), url.Values{}, kind, 30); err != nil {
				return nil, err
			}
		}
		if items == nil {
			items = []Item{}
		}
		s.similar.put(key, items)
	}
	out := append([]Item(nil), items...)
	s.annotate(ctx, out)
	return out, nil
}
