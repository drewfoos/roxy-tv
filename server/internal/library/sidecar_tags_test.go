package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Subtitle file tags in any order, languages as codes or names, flags anywhere. The old
// parser took a language only as the first tag and only as a bare 2-3 letter code.
func TestParseSidecarTags(t *testing.T) {
	cases := []struct {
		rest                  string
		lang, title           string
		forced, hi, isDefault bool
	}{
		{".en", "eng", "", false, false, false},
		{".en.hi", "eng", "", false, true, false},     // Bazarr's, all over the live library
		{".English", "eng", "", false, false, false},  // a full name
		{".forced.en", "eng", "", true, false, false}, // flag first
		{".en.forced", "eng", "", true, false, false},
		{".[Group].eng", "eng", "Group", false, false, false},
		{".eng.sdh.default", "eng", "", false, true, true},
		{".foreign.fre", "fra", "", true, false, false}, // bibliographic code, "foreign" = forced
		{".pt-BR", "por", "pt-BR", false, false, false}, // region kept in the title
		{".Español", "spa", "", false, false, false},    // native name
		{".ja.Signs & Songs", "jpn", "Signs & Songs", false, false, false},
		{".Signs & Songs", "", "Signs & Songs", false, false, false},
		{".cc", "", "", false, true, false},
		{"", "", "", false, false, false},
	}
	for _, c := range cases {
		got := parseSidecarTags(strings.Split(strings.Trim(c.rest, "."), "."))
		if got.language != c.lang || got.title != c.title || got.forced != c.forced || got.hi != c.hi || got.isDefault != c.isDefault {
			t.Errorf("%q: got %+v", c.rest, got)
		}
	}
}

// End to end through sidecarSubtitles, and a VobSub's language from its .idx.
func TestSidecarLanguagesOnDisk(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Show")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"Show - 01.mkv": "", "Show - 01.English.ass": "", "Show - 01.forced.de.srt": "",
		"Show - 01.sub": "", "Show - 01.idx": "# VobSub index file\nsize: 720x480\nid: ja, index: 0\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	streams, err := sidecarSubtitles(filepath.Join(dir, "Show - 01.mkv"), root, 1)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, st := range streams {
		got[filepath.Base(st.ExternalPath)] = st.Language
		if strings.HasSuffix(st.ExternalPath, ".forced.de.srt") && !st.IsForced {
			t.Errorf("forced flag lost")
		}
	}
	want := map[string]string{"Show - 01.English.ass": "eng", "Show - 01.forced.de.srt": "deu", "Show - 01.sub": "jpn"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %q, want %q (all: %v)", k, got[k], v, got)
		}
	}
}
