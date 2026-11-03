package arr

import "testing"

func TestSeasonWordRewrite(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		season   int
		ok       bool
	}{
		{"[EMBER] Alya Sometimes Hides Her Feelings in Russian (2024) (Season 1) [BDRip] [1080p Dual Audio HEVC 10 bits DDP] (Batch)", "[EMBER] Alya Sometimes Hides Her Feelings in Russian (2024) S01 [BDRip] [1080p Dual Audio HEVC 10 bits DDP] (Batch)", 1, true},
		{"[Judas] Tokidoki Bosotto Russiago de Dereru Tonari no Alya-san (Season 01) [BD 1080p][HEVC x265 10bit][Dual-Audio]", "[Judas] Tokidoki Bosotto Russiago de Dereru Tonari no Alya-san S01 [BD 1080p][HEVC x265 10bit][Dual-Audio]", 1, true},
		{"[neoDESU] Alya Sometimes Hides Her Feelings in Russian [Season 1] [BD 1080p x265 HEVC OPUS AAC] [Dual Audio]", "[neoDESU] Alya Sometimes Hides Her Feelings in Russian S01 [BD 1080p x265 HEVC OPUS AAC] [Dual Audio]", 1, true},
		{"Tenrai Sensei Show Season 2 BD 1080p HEVC 10bit x265 Dual Audio", "Tenrai Sensei Show S02 BD 1080p HEVC 10bit x265 Dual Audio", 2, true},
		{"Show S01 1080p CR WEB-DL AAC2.0 H 264-VARYG", "", 0, false},        // Sonarr reads it already
		{"[ToonsHub] Show S01E02 1080p AMZN WEB-DL (Season 1)", "", 0, false}, // has SxxEyy
		{"Show The Movie 1080p", "", 0, false},                               // no season at all
	} {
		got, n, ok := SeasonWordRewrite(tc.in)
		if ok != tc.ok || got != tc.want || n != tc.season {
			t.Errorf("%q: got %q %d %v", tc.in, got, n, ok)
		}
	}
}
