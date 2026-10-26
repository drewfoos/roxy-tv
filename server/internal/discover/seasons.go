package discover

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
)

// TMDB keeps many anime as one long season: The Apothecary Diaries is "Season 1, 60
// episodes" there, all cours together, while TVDB (and so Sonarr, which requests go to)
// numbers them Season 1 (24), Season 2 (24), Season 3 (12). Requesting "Season 1" from
// TMDB's layout would not even mean the same thing to Sonarr. TMDB's own answer is an
// episode group: a community "Seasons" grouping in production or air-date order that
// usually mirrors TVDB. When one splits the show into more seasons than TMDB's default,
// Discover shows (and requests) that layout. request app shows TMDB's merged season.

type tmdbGroupList struct {
	Results []struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		Type         int    `json:"type"` // 1 original air date, 2 absolute, 3 DVD, 4 digital, 5 story arc, 6 production, 7 TV
		GroupCount   int    `json:"group_count"`
		EpisodeCount int    `json:"episode_count"`
	} `json:"results"`
}

type tmdbGroup struct {
	Groups []struct {
		Name     string `json:"name"`
		Order    int    `json:"order"`
		Episodes []struct {
			Name      string `json:"name"`
			Overview  string `json:"overview"`
			AirDate   string `json:"air_date"`
			StillPath string `json:"still_path"`
			Order     int    `json:"order"`
		} `json:"episodes"`
	} `json:"groups"`
}

// groupedSeasons returns the seasons (and each season's episodes) of a "Seasons"
// episode group that splits the show further than TMDB's own [regular] seasons holding
// [regularEps] episodes, or ok=false to keep TMDB's layout.
func (s *Service) groupedSeasons(ctx context.Context, tmdbID, regular, regularEps int) (seasons []Season, episodes map[int][]Episode, ok bool) {
	var list tmdbGroupList
	if err := s.tmdb.Get(ctx, fmt.Sprintf("/tv/%d/episode_groups", tmdbID), nil, &list); err != nil {
		return nil, nil, false
	}
	// Prefer "Seasons" in production order, then original air date order.
	pref := func(t int) int {
		switch t {
		case 6:
			return 0
		case 1:
			return 1
		}
		return 9
	}
	cands := list.Results[:0:0]
	for _, g := range list.Results {
		if pref(g.Type) < 9 && strings.Contains(strings.ToLower(g.Name), "season") && g.GroupCount > regular {
			cands = append(cands, g)
		}
	}
	sort.SliceStable(cands, func(i, j int) bool { return pref(cands[i].Type) < pref(cands[j].Type) })
	for _, c := range cands {
		var g tmdbGroup
		if err := s.tmdb.Get(ctx, "/tv/episode_group/"+c.ID, nil, &g); err != nil {
			continue
		}
		var out []Season
		eps := map[int][]Episode{}
		total, regs := 0, 0
		var specials *Season
		for _, grp := range g.Groups {
			sort.SliceStable(grp.Episodes, func(i, j int) bool { return grp.Episodes[i].Order < grp.Episodes[j].Order })
			list := make([]Episode, 0, len(grp.Episodes))
			for i, e := range grp.Episodes {
				list = append(list, Episode{Number: i + 1, Title: e.Name, Overview: e.Overview, AirDate: e.AirDate, Still: s.tmdb.ImageURL("w300", e.StillPath)})
			}
			sn := Season{Number: grp.Order, Name: grp.Name, Episodes: len(list), Special: grp.Order == 0}
			if len(list) > 0 {
				sn.AirDate = list[0].AirDate
			}
			eps[grp.Order] = list
			if sn.Special {
				if sn.Episodes > 0 {
					specials = &sn
				}
				continue
			}
			if sn.Name == "" {
				sn.Name = fmt.Sprintf("Season %d", sn.Number)
			}
			regs++
			total += sn.Episodes
			out = append(out, sn)
		}
		// It has to split the show further and still cover (about) the same episodes;
		// a grouping that drops or invents half the show is not the same series.
		if regs <= regular || total < regularEps*8/10 || total > regularEps*13/10+2 {
			continue
		}
		sort.SliceStable(out, func(i, j int) bool { return out[i].Number < out[j].Number })
		if specials != nil {
			out = append(out, *specials)
		}
		slog.Info("discover: seasons from TMDB episode group", "tmdb", tmdbID, "group", c.Name, "seasons", regs, "episodes", total)
		return out, eps, true
	}
	return nil, nil, false
}
