package langdetect

import "testing"

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
