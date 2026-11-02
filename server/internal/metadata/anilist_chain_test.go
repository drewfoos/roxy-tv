package metadata

import (
	"fmt"
	"testing"
)

func chainEntry(id, eps int, titles ...string) *anilistMedia {
	m := &anilistMedia{ID: id}
	if eps > 0 {
		m.Episodes = &eps
	}
	for i, t := range titles {
		m.StreamingEpisodes = append(m.StreamingEpisodes, struct {
			Title     string `json:"title"`
			Thumbnail string `json:"thumbnail"`
		}{Title: fmt.Sprintf("Episode %d - %s", i+1, t), Thumbnail: "https://cr/" + t})
	}
	return m
}

func names(prefix string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("%s %d", prefix, i+1)
	}
	return out
}

// Crunchyroll lists AniList attaches to the wrong entry are refused: a later season's
// list on season 1 (KonoSuba: S3's 11 on S1's 10; Mushoku Tensei: S2's 25 on an 11-episode
// cour), and season 1's repeated on a sequel. A list one longer than the entry (the OVA
// Crunchyroll counts in) still counts.
func TestChainSeasonsRejectForeignStreamingLists(t *testing.T) {
	a := &AniList{}
	s3 := names("S3", 11)
	got := a.chainSeasons([]*anilistMedia{
		chainEntry(21202, 10, s3...),
		chainEntry(21699, 10, names("S2", 10)...),
		chainEntry(136804, 11, s3...),
	})
	if !got[0].NoEpisodeData || got[0].EpisodeCount != 10 {
		t.Errorf("KonoSuba S1 = %+v; want S3's list refused, 10 episodes", got[0])
	}
	if got[1].NoEpisodeData || got[2].NoEpisodeData || got[2].EpisodeCount != 11 {
		t.Errorf("S2 = %+v, S3 = %+v; want both trusted", got[1], got[2])
	}

	got = a.chainSeasons([]*anilistMedia{chainEntry(108465, 11, names("S2", 25)...), chainEntry(127720, 12)})
	if !got[0].NoEpisodeData || got[0].EpisodeCount != 11 {
		t.Errorf("Mushoku S1 = %+v; want the 25-title list refused", got[0])
	}

	s1 := names("S1", 12)
	got = a.chainSeasons([]*anilistMedia{chainEntry(1, 12, s1...), chainEntry(2, 13, s1...)})
	if got[0].NoEpisodeData || !got[1].NoEpisodeData || got[1].EpisodeCount != 13 {
		t.Errorf("repeated on the sequel: %+v", got)
	}

	got = a.chainSeasons([]*anilistMedia{chainEntry(1, 10, names("S1", 11)...)})
	if got[0].NoEpisodeData || got[0].EpisodeCount != 11 {
		t.Errorf("OVA as the 11th: %+v; want trusted, 11 episodes", got[0])
	}
	// Still airing (no count yet): taken at its word.
	got = a.chainSeasons([]*anilistMedia{chainEntry(1, 0, names("S1", 5)...)})
	if got[0].NoEpisodeData || got[0].EpisodeCount != 5 {
		t.Errorf("airing: %+v", got[0])
	}
}
