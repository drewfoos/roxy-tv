package library

import (
	"fmt"
	"testing"
)

func fmtP(p *int) string {
	if p == nil {
		return "-"
	}
	return fmt.Sprint(*p)
}

// Episode shapes Jellyfin reads that Roxy did not.
func TestParseEpisodeMoreShapes(t *testing.T) {
	cases := []struct{ name, season, ep, end string }{
		{"Show - S01E01-E02-E03 - Title.mkv", "1", "1", "3"}, // end used to stop at E02
		{"Show.S01E01E02E03.1080p.mkv", "1", "1", "3"},
		{"Show - S01E01-1080p.mkv", "1", "1", "-"}, // a resolution is not an end
		{"Show 1x01.mkv", "1", "1", "-"},
		{"Show - 2x05 - Title.mkv", "2", "5", "-"},
		{"Show 1x01-1x02.mkv", "1", "1", "2"},
		{"Show 1x01x02.mkv", "1", "1", "2"},
		{"Show Season 1 Episode 2.mkv", "1", "2", "-"},
		{"Show - Season 03 - Episode 10.mkv", "3", "10", "-"},
		{"Show 1920x1080 sample.mkv", "-", "-", "-"}, // a resolution is not 1x
	}
	for _, c := range cases {
		p := ParseEpisodeFile(c.name)
		if fmtP(p.Season) != c.season || fmtP(p.Episode) != c.ep || fmtP(p.EpisodeEnd) != c.end {
			t.Errorf("%s: S%s E%s end %s, want S%s E%s end %s", c.name, fmtP(p.Season), fmtP(p.Episode), fmtP(p.EpisodeEnd), c.season, c.ep, c.end)
		}
	}
}

func TestParseWeakEpisode(t *testing.T) {
	cases := []struct {
		stem string
		ep   int
		ok   bool
	}{
		{"[Group] Show [04][1080p]", 4, true},
		{"[Group][Show][21]", 21, true},
		{"Show [12v2]", 12, true},
		{"[Group] Show [1080p]", 0, false}, // resolution
		{"[Group] Show [2019]", 0, false},  // year
		{"[12] Show", 0, false},            // a leading bracket is the group
		{"Show ep01", 1, true},
		{"Show.E01.1080p", 1, true},
		{"Show_ep_03", 3, true},
		{"01", 1, true},
		{"01 - Pilot", 1, true},
		{"03.The Title", 3, true},
		{"2019 Documentary", 0, false}, // a year
		{"1080 Remastered", 0, false},  // a resolution
		{"Pilot", 0, false},
	}
	for _, c := range cases {
		n, ok := ParseWeakEpisode(c.stem)
		if n != c.ep || ok != c.ok {
			t.Errorf("%q: %d %v, want %d %v", c.stem, n, ok, c.ep, c.ok)
		}
	}
}

func TestParseSeasonDirMore(t *testing.T) {
	cases := []struct {
		dir string
		n   int
		ok  bool
	}{
		{"Season 1", 1, true}, {"S2", 2, true}, {"Specials", 0, true}, // as before
		{"Season 2 - The Arc", 2, true}, {"Season 02 (2023)", 2, true}, {"Season 3 [anidbid-5678]", 3, true},
		{"2nd Season", 2, true}, {"3rd season", 3, true},
		{"Staffel 4", 4, true}, {"Saison 2", 2, true}, {"Temporada 3", 3, true}, {"Сезон 1", 1, true},
		{"第2季", 2, true}, {"シーズン3", 3, true}, {"시즌 2", 2, true},
		{"01", 1, true}, {"2", 2, true},
		{"Show.S02.1080p.WEB-DL", 2, true},
		{"Extras", 0, false}, {"1999", 0, false}, {"S1mple", 0, false}, {"Arc 2", 0, false},
		{"[One Pace][442-489] Thriller Bark", 0, false},
	}
	for _, c := range cases {
		n, ok := ParseSeasonDir(c.dir)
		if n != c.n || ok != c.ok {
			t.Errorf("%q: %d %v, want %d %v", c.dir, n, ok, c.n, c.ok)
		}
	}
}

func TestIDTagsMore(t *testing.T) {
	ids := idTags("Show (2020) (tmdbid-123) [anidb-456] {anilist=789} [tvmazeid-1]")
	want := map[string]string{"tmdb": "123", "anidb": "456", "anilist": "789", "tvmaze": "1"}
	for k, v := range want {
		if ids[k] != v {
			t.Errorf("%s: %q, want %q (all %v)", k, ids[k], v, ids)
		}
	}
}
