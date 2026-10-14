package library

import "testing"

func TestParseMovieName(t *testing.T) {
	cases := []struct {
		in    string
		title string
		year  int
	}{
		{"Toy Story 5 (2026) (1080p DS4K BluRay x265 10-bit SDR AAC 5.1) [WeSLeY]", "Toy Story 5", 2026},
		{"Toy Story 5 (2026)", "Toy Story 5", 2026},
		{"Kono.Subarashii.Sekai.ni.Shukufuku.o!.Kurenai.Densetsu.2019.1080p.Dual.Audio.BDRip.10.bits.DD.x265-EMBER [06115509]", "Kono Subarashii Sekai ni Shukufuku o! Kurenai Densetsu", 2019},
		{"Kono Subarashii Sekai ni Shukufuku o! Kurenai Densetsu 2019 1080p Dual Audio BDRip 10 bits DD x265-EMBER", "Kono Subarashii Sekai ni Shukufuku o! Kurenai Densetsu", 2019},
		{"Your Name (2016) [tmdbid-372058]", "Your Name", 2016},
		{"Akira_1988_BluRay_1080p", "Akira", 1988},
	}
	for _, c := range cases {
		title, year, _ := ParseMovieName(c.in)
		if title != c.title {
			t.Errorf("%q: title = %q, want %q", c.in, title, c.title)
		}
		if year == nil || *year != c.year {
			t.Errorf("%q: year = %v, want %d", c.in, year, c.year)
		}
	}
	if _, _, ids := ParseMovieName("Your Name (2016) [tmdbid-372058]"); ids["tmdb"] != "372058" {
		t.Errorf("tmdb id not parsed: %v", ids)
	}
}

func TestLooksLikeMovie(t *testing.T) {
	cases := []struct {
		file, dir string
		depth     int
		want      bool
	}{
		{"Toy Story 5 (2026) (1080p) [WeSLeY].mkv", "Toy Story 5 (2026)", 2, true},
		{"Kono.Subarashii.Sekai.2019.1080p.x265-EMBER.mkv", "", 1, true},
		{"Kono.Subarashii.Sekai.2019.1080p.x265-EMBER.mkv", "Kono Subarashii Sekai ni Shukufuku o! Kurenai Densetsu 2019 1080p", 2, true},
		{"Roxy Test Show (2024) - S01E01 - 001 - Pilot Episode [HDTV-1080p][x265]-RoxyGrp.mkv", "Roxy Test Show (2024) [tvdbid-424242]", 3, false},
		{"[SubsPlease] Frieren - 05 (1080p) [ABCD1234].mkv", "Frieren (2023)", 2, false},
		{"test-hi10p-ass.mkv", "", 1, false},
		{"Movie Night.mkv", "Roxy Test Show (2024)", 3, false}, // inside a season folder: stays an episode
	}
	for _, c := range cases {
		p := ParseEpisodeFile(c.file)
		stem := c.file[:len(c.file)-4]
		if got := LooksLikeMovie(p, stem, c.dir, c.depth); got != c.want {
			t.Errorf("%q in %q: LooksLikeMovie = %v, want %v (parsed %+v)", c.file, c.dir, got, c.want, p)
		}
	}
}
