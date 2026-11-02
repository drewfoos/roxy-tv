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

func TestParseEpisodeFilePatterns(t *testing.T) {
	type want struct{ season, episode, absolute int } // 0 = not set
	cases := []struct {
		in string
		want
		title string // CleanTitle, checked when set
	}{
		// TRaSH names keep winning: SxxExx, then the absolute number after it.
		{"JUJUTSU KAISEN (2020) - S02E01 - 025 - Hidden Inventory [Bluray-1080p][x265]-Group.mkv", want{2, 1, 25}, "JUJUTSU KAISEN"},
		{"Roxy Test Show (2024) - S01E01 - 001 - Pilot Episode [HDTV-1080p][x265]-RoxyGrp.mkv", want{1, 1, 1}, ""},
		{"Show - S01E05 - Episode 7 Is Not The Number.mkv", want{1, 5, 0}, ""},
		{"[SubsPlease] Frieren - 05 (1080p) [ABCD1234].mkv", want{0, 0, 5}, ""},
		{"Show - 012 - Title.mkv", want{0, 0, 12}, ""},
		// "_-_NN_" with underscores for spaces.
		{"[DB]Tokyo Magnitude 8.0_-_01_(Dual Audio_10bit_BD1080p_x265).mkv", want{0, 0, 1}, "Tokyo Magnitude 8.0"},
		{"[DB]Tokyo Magnitude 8.0_-_11_(Dual Audio_10bit_BD1080p_x265).mkv", want{0, 0, 11}, ""},
		{"Some_Show_-_07v2_[720p].mkv", want{0, 0, 7}, ""},
		// "Episode N - Title".
		{"Episode 1 - The Golden Age.mkv", want{0, 1, 0}, ""},
		{"Episode 13 - Vow of Retaliation.mkv", want{0, 13, 0}, ""},
		{"Berserk.Episode.04.1080p.mkv", want{0, 4, 0}, ""},
		// Negatives: a year or a resolution is not an episode number.
		{"Some_Movie_-_2019_(BD1080p).mkv", want{}, ""},
		{"Show_-_1080p_x265.mkv", want{}, ""},
		{"Episode 2019 Special.mkv", want{}, ""},
		{"Akira_1988_BluRay_1080p.mkv", want{}, ""},
		{"Wano 01.mp4", want{}, ""},
		{"Episodes of Life.mkv", want{}, ""},
	}
	val := func(p *int) int {
		if p == nil {
			return 0
		}
		return *p
	}
	for _, c := range cases {
		p := ParseEpisodeFile(c.in)
		got := want{val(p.Season), val(p.Episode), val(p.Absolute)}
		if got != c.want {
			t.Errorf("%q: season/episode/absolute = %v, want %v", c.in, got, c.want)
		}
		if c.title != "" && p.CleanTitle != c.title {
			t.Errorf("%q: clean title = %q, want %q", c.in, p.CleanTitle, c.title)
		}
	}
}
