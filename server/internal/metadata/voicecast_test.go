package metadata

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// voiceEdge is one character with its voices as (staff id, name, language, photo).
func voiceEdge(character string, voices ...[4]any) map[string]any {
	var vs []map[string]any
	for _, v := range voices {
		vs = append(vs, map[string]any{"id": v[0], "name": map[string]any{"full": v[1]}, "languageV2": v[2], "image": map[string]any{"large": v[3]}})
	}
	return map[string]any{"node": map[string]any{"name": map[string]any{"full": character}}, "voiceActors": vs}
}

// fakeVoiceAniList answers the voice cast query for one entry, and a title search with
// that entry; queries holds what was asked.
func fakeVoiceAniList(t *testing.T, id int, edges []map[string]any) (*AniList, *[]string) {
	t.Helper()
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &req)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(req.Query, "voiceActors") {
			queries = append(queries, "cast")
			var media any
			if int(req.Variables["id"].(float64)) == id {
				media = map[string]any{"id": id, "characters": map[string]any{"edges": edges}}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"Media": media}})
			return
		}
		queries = append(queries, "search")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"Page": map[string]any{"media": []any{
			map[string]any{"id": id, "format": "TV", "title": map[string]any{"english": "Bunny Girl", "romaji": "Bunny Girl"}, "startDate": map[string]any{"year": 2018}},
		}}}})
	}))
	t.Cleanup(srv.Close)
	a := NewAniList()
	a.endpoint = srv.URL
	a.lim = newLimiter(1000, time.Second)
	return a, &queries
}

const photo = "https://img.example/staff/"

// AniList lists every language's voices together: they are split by language, one
// voice per character and language, the original first even when a dub's voice was
// listed first, and languages AniList names that Roxy has no code for are left out.
func TestVoiceCastSplitsByLanguage(t *testing.T) {
	a, _ := fakeVoiceAniList(t, 7, []map[string]any{
		voiceEdge("Sakuta Azusagawa",
			[4]any{20, "Stephen Fu", "English", photo + "20.jpg"},
			[4]any{10, "Kaito Ishikawa", "Japanese", photo + "10.jpg"},
			[4]any{30, "Someone Else", "Klingon", photo + "30.jpg"},
			[4]any{21, "A Second English Voice", "English", photo + "21.jpg"}),
		voiceEdge("Mai Sakurajima",
			[4]any{11, "Asami Seto", "Japanese", photo + "default.jpg"},
			[4]any{22, "Erica Mendez", "English", photo + "22.jpg"},
			[4]any{40, "Ein Stimme", "German", ""}),
		voiceEdge("Nobody Voiced"),
	})
	vc, err := a.VoiceCast(context.Background(), 7, "", 0, false)
	if err != nil || vc == nil {
		t.Fatalf("VoiceCast = %v, %v", vc, err)
	}
	var got []string
	for _, l := range vc.Languages {
		line := l.Code + "/" + l.Name + ":"
		for _, m := range l.Members {
			line += " " + m.Name + " as " + m.Character + ";"
		}
		got = append(got, line)
	}
	want := []string{
		"jpn/Japanese: Kaito Ishikawa as Sakuta Azusagawa; Asami Seto as Mai Sakurajima;",
		"eng/English: Stephen Fu as Sakuta Azusagawa; Erica Mendez as Mai Sakurajima;",
		"deu/German: Ein Stimme as Mai Sakurajima;",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("languages:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	jp := vc.Languages[0].Members
	if jp[0].StaffID != 10 || jp[0].Photo != photo+"10.jpg" {
		t.Errorf("first voice = %+v, want staff 10 with its photo", jp[0])
	}
	if jp[1].Photo != "" {
		t.Errorf("AniList's stand-in picture was kept: %q", jp[1].Photo)
	}
}

// A cast row shows fifteen people: no more are kept per language.
func TestVoiceCastKeepsARowsWorth(t *testing.T) {
	var edges []map[string]any
	for i := 0; i < 25; i++ {
		edges = append(edges, voiceEdge("Character "+string(rune('A'+i)), [4]any{100 + i, "Voice " + string(rune('A'+i)), "Japanese", ""}))
	}
	a, _ := fakeVoiceAniList(t, 7, edges)
	vc, err := a.VoiceCast(context.Background(), 7, "", 0, false)
	if err != nil || vc == nil || len(vc.Languages) != 1 {
		t.Fatalf("VoiceCast = %+v, %v", vc, err)
	}
	if n := len(vc.Languages[0].Members); n != voiceCastMembers {
		t.Fatalf("%d members kept, want %d", n, voiceCastMembers)
	}
}

// A title with no AniList id on it (a Discover page) is found by title first; one
// AniList does not know is "no cast", not an error.
func TestVoiceCastFindsTheTitleWhenThereIsNoID(t *testing.T) {
	a, queries := fakeVoiceAniList(t, 7, []map[string]any{voiceEdge("Mai", [4]any{11, "Asami Seto", "Japanese", ""})})
	vc, err := a.VoiceCast(context.Background(), 0, "Bunny Girl", 2018, false)
	if err != nil || vc == nil || vc.AniListID != 7 {
		t.Fatalf("by title: %+v, %v", vc, err)
	}
	if strings.Join(*queries, ",") != "search,cast" {
		t.Fatalf("asked %v, want a search then the cast", *queries)
	}
	if vc, err := a.VoiceCast(context.Background(), 999, "", 0, false); vc != nil || err != nil {
		t.Fatalf("unknown id: %+v, %v; want nil, nil", vc, err)
	}
	if vc, err := a.VoiceCast(context.Background(), 0, "  ", 0, false); vc != nil || err != nil {
		t.Fatalf("no id and no title: %+v, %v; want nil, nil", vc, err)
	}
}
