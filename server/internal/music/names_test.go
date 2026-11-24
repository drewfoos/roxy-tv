package music

import (
	"reflect"
	"testing"
)

func TestFold(t *testing.T) {
	for in, want := range map[string]string{
		"Fryslân":            "fryslan",
		"FRYSLAN":            "fryslan",
		"Beyoncé":            "beyonce",
		"Beyoncé":           "beyonce", // the accent as its own character
		"AC/DC":              "ac dc",
		"  The   Beatles  ":  "the beatles",
		"Tyler, The Creator": "tyler the creator",
		"Sigur Rós":          "sigur ros",
		"Motörhead":          "motorhead",
		"Straße":             "strasse",
		"!!!":                "!!!",
		"宇多田ヒカル":             "宇多田ヒカル",
		"":                   "",
	} {
		if got := Fold(in); got != want {
			t.Errorf("Fold(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSortName(t *testing.T) {
	for in, want := range map[string]string{
		"The Beatles":         "beatles",
		"A Perfect Circle":    "perfect circle",
		"The":                 "the", // nothing left without it
		"Theory of a Deadman": "theory of a deadman",
	} {
		if got := SortName(in); got != want {
			t.Errorf("SortName(%q) = %q, want %q", in, got, want)
		}
	}
}

// The same artist has the same id however the name is cased or accented.
func TestArtistIDFollowsTheName(t *testing.T) {
	if ArtistID("Bring Me the Horizon") != ArtistID("BRING ME THE HORIZON") {
		t.Error("case changed the id")
	}
	if ArtistID("Joost") == ArtistID("Joost Klein") {
		t.Error("two names share an id")
	}
}

func TestCredits(t *testing.T) {
	// The library's artist folders, folded.
	folders := map[string]bool{
		"joost": true, "bring me the horizon": true, "the beatles": true, "ajr": true,
		"arcane": true, "dua lipa": true, "b o b": true, "24kgoldn": true,
	}
	known := func(f string) bool { return folders[f] }
	for _, c := range []struct {
		tag, hint string
		want      []string
	}{
		// Forms found in a real library.
		{"24kGoldn, iann dior", "24kGoldn", []string{"24kGoldn", "iann dior"}},
		{"AJR, Steve Aoki", "AJR", []string{"AJR", "Steve Aoki"}},
		{"Joost, Otto Waalkes & Ski Aggu", "Joost", []string{"Joost", "Otto Waalkes", "Ski Aggu"}},
		{"Joost, Aldo2Swag & Jungle Bobby", "Joost", []string{"Joost", "Aldo2Swag", "Jungle Bobby"}},
		{"Bring Me the Horizon feat. Amy Lee", "Bring Me the Horizon", []string{"Bring Me the Horizon", "Amy Lee"}},
		{"Bring Me the Horizon with YUNGBLUD", "Bring Me the Horizon", []string{"Bring Me the Horizon", "YUNGBLUD"}},
		{"The Beatles • Billy Preston", "The Beatles", []string{"The Beatles", "Billy Preston"}},
		{"B.o.B, Hayley Williams of Paramore", "B.o.B", []string{"B.o.B", "Hayley Williams of Paramore"}},
		{"Aaron Smith, Krono, Luvli, Krono", "Aaron Smith", []string{"Aaron Smith", "Krono", "Luvli"}},
		// Filed under one of the guests: the folder's artist leads.
		{"Ashnikko, Arcane, League of Legends", "Arcane", []string{"Arcane", "Ashnikko", "League of Legends"}},
		// One artist.
		{"Dua Lipa", "Dua Lipa", []string{"Dua Lipa"}},
		{"Tones and I", "Tones", []string{"Tones and I"}},
		// Names that hold a separator stay whole: by the folder, by the library, by fame.
		{"Tyler, The Creator", "Tyler, The Creator", []string{"Tyler, The Creator"}},
		{"Earth, Wind & Fire", "", []string{"Earth, Wind & Fire"}},
		{"Simon & Garfunkel", "", []string{"Simon & Garfunkel"}},
		{"Hall & Oates", "", []string{"Hall & Oates"}},
		{"Mumford & Sons, Baaba Maal", "", []string{"Mumford & Sons", "Baaba Maal"}},
		{"Dua Lipa, Tyler, The Creator", "Dua Lipa", []string{"Dua Lipa", "Tyler, The Creator"}},
		{"Someone & Someone Else", "", []string{"Someone & Someone Else"}}, // nobody known: left alone
		// An ampersand or "x" between two artists, one of them known.
		{"Joost & Ski Aggu", "Joost", []string{"Joost", "Ski Aggu"}},
		{"Ski Aggu x Joost", "", []string{"Ski Aggu", "Joost"}},
		// Other joins.
		{"A; B;C", "", []string{"A", "B", "C"}},
		{"Artist One / Artist Two", "", []string{"Artist One", "Artist Two"}},
		{"Artist FEAT. Guest", "", []string{"Artist", "Guest"}},
		{"Artist ft. Guest", "", []string{"Artist", "Guest"}},
		{"AC/DC", "", []string{"AC/DC"}}, // no spaces around the slash: one name
		// Brackets inside a name stay; brackets around a whole name go.
		{"Nodoka Toyohama(CV:Maaya Uchida)", "", []string{"Nodoka Toyohama(CV:Maaya Uchida)"}},
		{"Artist feat. (Guest)", "", []string{"Artist", "Guest"}},
		{"", "", []string{UnknownArtist}},
		{"   ", "Joost", []string{UnknownArtist}},
	} {
		if got := Credits(c.tag, c.hint, known); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Credits(%q, folder %q) = %q, want %q", c.tag, c.hint, got, c.want)
		}
	}
}

// Without anything known about the library, only what always separates artists does.
func TestCreditsWithNoLibrary(t *testing.T) {
	if got := Credits("A & B", "", nil); !reflect.DeepEqual(got, []string{"A & B"}) {
		t.Errorf("an ampersand with nobody known split the name: %q", got)
	}
	if got := Credits("A, B", "", nil); !reflect.DeepEqual(got, []string{"A", "B"}) {
		t.Errorf("a comma list was not read: %q", got)
	}
}
