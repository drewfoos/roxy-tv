package metadata

import (
	"context"
	"strings"

	"github.com/roxy-media/roxy/server/internal/model"
)

// VoiceCast is an anime's characters with who voices them, per language: AniList lists
// the original cast and the dubs, where TMDB credits only the original.
type VoiceCast struct {
	AniListID int `json:"anilistId"`
	// Languages are in AniList's order of first appearance, the original first when
	// it has one. Codes are ISO 639-2/T ("jpn", "eng", "deu").
	Languages []VoiceLanguage `json:"languages,omitempty"`
}

// VoiceLanguage is one language's cast.
type VoiceLanguage struct {
	Code    string       `json:"code"`
	Name    string       `json:"name"` // "English"
	Members []VoiceActor `json:"members"`
}

// VoiceActor is one character's voice in one language.
type VoiceActor struct {
	StaffID   int    `json:"staffId"` // AniList's id for the person
	Name      string `json:"name"`
	Character string `json:"character,omitempty"`
	Photo     string `json:"photo,omitempty"`
}

// voiceCastMembers is how many of a language's cast are kept: what a cast row shows.
const voiceCastMembers = 15

// One read gives every language: asking per language with aliases does not work
// (AniList answers each alias with the same list), so the language is read off each
// person instead.
const anilistVoiceCastQuery = `query ($id: Int) {
  Media(id: $id, type: ANIME) {
    id
    characters(sort: [ROLE, RELEVANCE, ID], perPage: 25) {
      edges {
        node { name { full } }
        voiceActors(sort: [RELEVANCE, ID]) { id name { full } image { large } languageV2 }
      }
    }
  }
}`

type anilistVoiceCastResp struct {
	Data struct {
		Media *struct {
			ID         int `json:"id"`
			Characters struct {
				Edges []struct {
					Node struct {
						Name struct{ Full string }
					} `json:"node"`
					VoiceActors []struct {
						ID    int `json:"id"`
						Name  struct{ Full string }
						Image struct{ Large string }
						Lang  string `json:"languageV2"`
					} `json:"voiceActors"`
				} `json:"edges"`
			} `json:"characters"`
		} `json:"Media"`
	} `json:"data"`
}

// voiceLanguages: AniList's names for a voice actor's language, as codes.
var voiceLanguages = map[string]string{
	"japanese": "jpn", "english": "eng", "korean": "kor", "italian": "ita", "spanish": "spa",
	"portuguese": "por", "french": "fra", "german": "deu", "hebrew": "heb", "hungarian": "hun",
	"chinese": "zho", "arabic": "ara", "filipino": "fil", "catalan": "cat", "finnish": "fin",
	"turkish": "tur", "dutch": "nld", "swedish": "swe", "thai": "tha", "tagalog": "tgl",
	"malaysian": "msa", "indonesian": "ind", "vietnamese": "vie", "nepali": "nep", "hindi": "hin",
	"urdu": "urd", "polish": "pol", "russian": "rus", "danish": "dan", "norwegian": "nor",
}

// VoiceCast reads an anime's voice cast: by AniList id when known, else found by title
// and year. Nil without error when AniList does not know the title.
func (a *AniList) VoiceCast(ctx context.Context, anilistID int, title string, year int, movie bool) (*VoiceCast, error) {
	if anilistID <= 0 {
		if strings.TrimSpace(title) == "" {
			return nil, nil
		}
		it := &model.Item{Type: model.TypeSeries, Title: title}
		if movie {
			it.Type = model.TypeMovie
		}
		if year > 0 {
			it.Year = &year
		}
		m, err := a.search(ctx, it)
		if err != nil || m == nil {
			return nil, err
		}
		anilistID = m.ID
	}
	var resp anilistVoiceCastResp
	if err := a.rawQuery(ctx, anilistVoiceCastQuery, map[string]any{"id": anilistID}, &resp); err != nil {
		return nil, err
	}
	if resp.Data.Media == nil {
		return nil, nil
	}
	out := &VoiceCast{AniListID: resp.Data.Media.ID}
	at := map[string]int{} // code -> place in out.Languages
	for _, e := range resp.Data.Media.Characters.Edges {
		heard := map[string]bool{} // one voice per character and language: the first listed
		for _, v := range e.VoiceActors {
			name := strings.TrimSpace(v.Lang)
			code := voiceLanguages[strings.ToLower(name)]
			if code == "" || heard[code] || strings.TrimSpace(v.Name.Full) == "" {
				continue
			}
			heard[code] = true
			i, ok := at[code]
			if !ok {
				i = len(out.Languages)
				at[code] = i
				out.Languages = append(out.Languages, VoiceLanguage{Code: code, Name: name})
			}
			if len(out.Languages[i].Members) >= voiceCastMembers {
				continue
			}
			out.Languages[i].Members = append(out.Languages[i].Members, VoiceActor{
				StaffID: v.ID, Name: strings.TrimSpace(v.Name.Full), Character: strings.TrimSpace(e.Node.Name.Full), Photo: staffPhoto(v.Image.Large),
			})
		}
	}
	// The original first, wherever its first voice was listed.
	for i, l := range out.Languages {
		if l.Code == "jpn" && i > 0 {
			copy(out.Languages[1:i+1], out.Languages[:i])
			out.Languages[0] = l
			break
		}
	}
	return out, nil
}

// staffPhoto drops AniList's stand-in picture for people it has none of.
func staffPhoto(u string) string {
	if strings.HasSuffix(u, "/default.jpg") {
		return ""
	}
	return u
}
