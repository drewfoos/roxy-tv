package langdetect

import (
	"strings"
	"testing"
)

const assHeader = "[Script Info]\nScriptType: v4.00+\n\n[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n"

func dlg(lines ...string) string {
	s := assHeader
	for _, l := range lines {
		s += "Dialogue: 0,0:00:01.00,0:00:02.00,Default,,0,0,0,," + l + "\n"
	}
	return s
}

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{"en": "eng", "en-US": "eng", "eng": "eng", "ger": "deu", "ja": "jpn", "jpn": "jpn", "es": "spa", "xx": "xx"} {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDetect(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"english ass", dlg("I don't wanna work part-time anymore!", "Shut it!", "But the boy did not know that this was the day everything would change.", "What are you doing with that? It's not yours and you can't just take it.", "We're going to have to talk about this later, aren't we?"), "eng"},
		{"spanish ass with tags", dlg(`{\an8}Y la paladín que todo lo apesta.`, "¡Cálmense!", `¿Quieres enviar a un bebé\Ncontra el Rey Demonio?`, "No tiene nada que ver con lo que está pasando en la ciudad.", "Es muy importante que lleguemos a tiempo para la reunión."), "spa"},
		{"japanese", dlg("もう働きたくない！", "うるさい！", "しかしその少年は知らなかった。この日がすべてを変える日だということを。", "何をしているの？それはあなたのものじゃない。"), "jpn"},
		{"srt english", "1\n00:00:01,000 --> 00:00:02,000\nIf we leave her in there for a month or so, she's bound to quiet down.\n\n2\n00:00:03,000 --> 00:00:04,000\nThat's not what I meant and you know it. We have to do something about this.\n", "eng"},
		{"german", dlg("Ich habe das nicht gewusst, und ich bin mir nicht sicher, ob sie es auch wissen.", "Wir müssen noch einmal mit ihm reden, aber nur wenn du dich traust."), "deu"},
		{"too short", dlg("Hi."), ""},
	}
	for _, c := range cases {
		got := Detect(c.in)
		if got.Lang != c.want {
			t.Errorf("%s: got %q (%.2f), want %q", c.name, got.Lang, got.Confidence, c.want)
		}
	}
}

var malay = dlg("Saya tidak tahu apa yang awak cakap ini, dan itu bukan untuk kita.", "Awak akan pergi dengan mereka dari sini, saya juga ada di sana.", "Apa yang awak buat dengan itu? Ini bukan untuk awak.", "Mereka sudah ada di sini dan kita tidak boleh pergi.")

// A track tagged with a language the detector has no words for kept its tag overruled by
// the nearest language it does know: correctly tagged Malay became Indonesian, at full
// confidence.
func TestATagForALanguageDetectCannotTellStands(t *testing.T) {
	if r := Detect(malay); r.Lang != "ind" {
		t.Fatalf("the sample no longer reads as Indonesian (%+v): the test needs another", r)
	}
	for _, tag := range []string{"may", "msa", "ms", "ms-MY"} {
		if r := DetectTagged(tag, malay); r.Lang != "" {
			t.Errorf("tagged %q: detected %q (%.2f), want the tag left alone", tag, r.Lang, r.Confidence)
		}
	}
	// No tag, or a tag that says nothing: the text is all there is to go on.
	for _, tag := range []string{"", "und", "UND"} {
		if r := DetectTagged(tag, malay); r.Lang != "ind" {
			t.Errorf("tagged %q: detected %q, want ind", tag, r.Lang)
		}
	}
	// What the detector is for: a track tagged with one language it knows, holding another.
	spanish := dlg(`{\an8}Y la paladín que todo lo apesta.`, "¡Cálmense!", `¿Quieres enviar a un bebé\Ncontra el Rey Demonio?`, "No tiene nada que ver con lo que está pasando en la ciudad.", "Es muy importante que lleguemos a tiempo para la reunión.")
	for _, tag := range []string{"eng", "en", "jpn", "por"} {
		if r := DetectTagged(tag, spanish); r.Lang != "spa" {
			t.Errorf("Spanish tagged %q: detected %q, want spa", tag, r.Lang)
		}
	}
}

// Words Spanish and Portuguese share scored the same for both, and the answer was
// whichever the map gave first, at a confidence (0.5) above the 0.4 the library acts on.
func TestTwoLanguagesThatScoreTheSameAreNotAFinding(t *testing.T) {
	tie := dlg(strings.Repeat("que por para está ", 10), strings.Repeat("que por para está ", 10))
	for i := 0; i < 20; i++ {
		if r := Detect(tie); r.Lang != "" {
			t.Fatalf("run %d: detected %q (%.2f) from words two languages share", i, r.Lang, r.Confidence)
		}
	}
	// Real Portuguese still wins over Spanish, and the other way round.
	portuguese := dlg("Eu não sei o que você está fazendo aqui, mas isso não é para nós.", "Ela também disse que vamos ficar muito bem, então não se preocupe com isso.", "Você não pode fazer isso com ele, não é uma coisa boa.")
	if r := Detect(portuguese); r.Lang != "por" || r.Confidence < 0.4 {
		t.Errorf("Portuguese: got %q (%.2f)", r.Lang, r.Confidence)
	}
}
