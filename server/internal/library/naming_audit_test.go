package library

import "testing"

// Found by an audit: the first year in a film's name was taken for its release year, so
// a title that starts with or contains a year came out wrong (and failed to match).
func TestMovieNameYearInTitle(t *testing.T) {
	cases := []struct {
		in, title string
		year      int
	}{
		{"1917 (2019)", "1917", 2019},
		{"2001 A Space Odyssey (1968)", "2001 A Space Odyssey", 1968},
		{"Blade Runner 2049 (2017)", "Blade Runner 2049", 2017},
		{"Wonder Woman 1984 (2020)", "Wonder Woman 1984", 2020},
		{"Blade.Runner.2049.2017.1080p.BluRay.x264-GRP", "Blade Runner 2049", 2017},
		// Still as before:
		{"Film (2020)", "Film", 2020},
		{"Movie.Title.2019.1080p.BluRay.x265-GRP", "Movie Title", 2019},
		{"Some Movie 2008 [1080p]", "Some Movie", 2008},
		{"1917", "1917", 0},
	}
	for _, c := range cases {
		title, year, _ := ParseMovieName(c.in)
		y := 0
		if year != nil {
			y = *year
		}
		if title != c.title || y != c.year {
			t.Errorf("%q: %q %d, want %q %d", c.in, title, y, c.title, c.year)
		}
	}
}

// Found by an audit: creditless openings and endings, OVAs and recaps that carry a loose
// number were read as that episode, so several became one item beside the real episode.
func TestWeakEpisodeSkipsExtrasWords(t *testing.T) {
	for _, stem := range []string{
		"[Grp] Show NCED [02][1080p]", "[Grp] Show NCOP [02]", "[Grp] Show NC ED 02 [1080p]",
		"[Grp] Show OVA [02][1080p]", "[Grp] Show - Recap ep02 [1080p]", "[Grp] Show Preview [03]",
		"[Grp] Show OP2 [02]", "[Grp] Show - ED [02]", "[Grp] Show Menu [01]",
	} {
		if n, ok := ParseWeakEpisode(stem); ok {
			t.Errorf("%q read as episode %d", stem, n)
		}
	}
	for stem, want := range map[string]int{
		"[Grp] Show [05][1080p]":   5,
		"Ed, Edd n Eddy [05]":      5,
		"Show ep07":                7,
		"[Grp] Opening Night [03]": 3, // "Opening" is a word, not the OP tag
		"[Grp] Edward's Show [04]": 4,
		"[Grp] Overall Show [02]":  2, // "ova" inside a word
	} {
		if n, ok := ParseWeakEpisode(stem); !ok || n != want {
			t.Errorf("%q: %d %v, want %d", stem, n, ok, want)
		}
	}
}
