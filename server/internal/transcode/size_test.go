package transcode

import (
	"strings"
	"testing"
)

// A quality is a class named by its height. A film wider than 16:9 has fewer lines than
// that: "1440p" of a 3840 x 1604 film used to come out 3448 x 1440, which is still 4K by
// its width (and more than a small GPU encodes as fast as it plays).
func TestOutputSize(t *testing.T) {
	cases := []struct {
		name            string
		srcW, srcH, max int
		wantW, wantH    int
	}{
		{"wide 4K film at 1440p", 3840, 1604, 1440, 2562, 1070},
		{"wide 4K film at 1080p", 3840, 1604, 1080, 1920, 802},
		{"wide 1080p film at 720p", 1920, 802, 720, 1278, 534},
		{"16:9 at 720p", 1920, 1080, 720, 1280, 720},
		{"16:9 4K at 1440p", 3840, 2160, 1440, 2560, 1440},
		{"4:3 at 720p", 1440, 1080, 720, 960, 720},
		{"kept as it is", 1920, 1080, 0, 1920, 1080},
		{"never larger than the file", 1920, 1080, 2160, 1920, 1080},
		{"wide film asked for its own class", 1920, 802, 1080, 1920, 802},
		{"odd sizes come out even", 1915, 1077, 720, 1280, 720},
		{"width not known: the height alone", 0, 1080, 720, 0, 720},
		{"nothing known about the file", 0, 0, 720, 0, 720},
		{"nothing known, nothing asked", 0, 0, 0, 0, 0},
	}
	for _, c := range cases {
		w, h := OutputSize(c.srcW, c.srcH, c.max)
		if w != c.wantW || h != c.wantH {
			t.Errorf("%s: OutputSize(%d, %d, %d) = %d x %d, want %d x %d", c.name, c.srcW, c.srcH, c.max, w, h, c.wantW, c.wantH)
		}
		if w%2 != 0 || h%2 != 0 {
			t.Errorf("%s: %d x %d is not even", c.name, w, h)
		}
	}
}

func TestArgsScaleToTheQualityClass(t *testing.T) {
	d := NewDetector("ffmpeg", string(Software), "")
	wide := strings.Join(d.Args(Request{Path: "/m/a.mkv", MaxHeight: 1440, BitrateK: 16000, SrcWidth: 3840, SrcHeight: 1604, Web: true}), " ")
	if !strings.Contains(wide, "scale=-2:1070,") {
		t.Errorf("a 3840 x 1604 film at 1440p should be scaled to 1070 lines (2562 wide): %s", wide)
	}
	plain := strings.Join(d.Args(Request{Path: "/m/a.mkv", MaxHeight: 720, BitrateK: 4000, SrcWidth: 1920, SrcHeight: 1080}), " ")
	if !strings.Contains(plain, "scale=-2:720,") {
		t.Errorf("a 16:9 file at 720p should be 720 lines: %s", plain)
	}
	// Width not known (an old record): as before, by height.
	old := strings.Join(d.Args(Request{Path: "/m/a.mkv", MaxHeight: 720, BitrateK: 4000, SrcHeight: 1080}), " ")
	if !strings.Contains(old, "scale=-2:720,") {
		t.Errorf("without a width the height asked for is used: %s", old)
	}
}

// hardware returns a detector that has chosen [e], with the measured speeds given
// (pixels a second; 0 = not measured).
func hardware(e Encoder, hw, sw float64) *Detector {
	d := &Detector{ffmpeg: "ffmpeg", device: "/dev/dri/renderD128", want: string(e), decided: true, chosen: e, tested: map[Encoder]bool{e: true}}
	d.SetRates(hw, sw)
	return d
}

// A small GPU encodes 1080p with room to spare and a 1440p-class picture barely as fast as
// it plays; the CPU of the same server does that one three times faster. Measured on an
// RX 550 beside sixteen cores: 90 and 560 million pixels a second.
func TestEncoderForLeavesLargePicturesToTheFasterEncoder(t *testing.T) {
	const rx550, cpu = 90e6, 560e6
	film4K := Request{SrcWidth: 3840, SrcHeight: 1604, SrcFPS: 23.976}
	at := func(r Request, h int) Request { r.MaxHeight = h; return r }
	cases := []struct {
		name string
		d    *Detector
		r    Request
		want Encoder
	}{
		{"1440p of a 4K film: too much for the small GPU", hardware(VAAPI, rx550, cpu), at(film4K, 1440), Software},
		{"1080p of it: the GPU's", hardware(VAAPI, rx550, cpu), at(film4K, 1080), VAAPI},
		{"a 1080p file kept at its size", hardware(VAAPI, rx550, cpu), Request{SrcWidth: 1920, SrcHeight: 1080, SrcFPS: 23.976}, VAAPI},
		{"1080p at 60 frames a second", hardware(VAAPI, rx550, cpu), Request{SrcWidth: 1920, SrcHeight: 1080, SrcFPS: 60}, Software},
		{"a fast GPU keeps the large picture", hardware(NVENC, 2000e6, cpu), at(film4K, 1440), NVENC},
		{"a slow CPU does not get it", hardware(VAAPI, rx550, 150e6), at(film4K, 1440), VAAPI},
		{"speeds not measured: as chosen", hardware(VAAPI, 0, 0), at(film4K, 1440), VAAPI},
		{"hardware speed not measured: as chosen", hardware(VAAPI, 0, cpu), at(film4K, 1440), VAAPI},
		{"picture size not known: as chosen", hardware(VAAPI, rx550, cpu), Request{MaxHeight: 1440}, VAAPI},
		{"frame rate not known: counted as film", hardware(VAAPI, rx550, cpu), Request{SrcWidth: 3840, SrcHeight: 1604, MaxHeight: 1440}, Software},
		{"software chosen stays software", hardware(Software, 0, 0), at(film4K, 1080), Software},
	}
	for _, c := range cases {
		if got := c.d.EncoderFor(c.r); got != c.want {
			t.Errorf("%s: EncoderFor = %s, want %s", c.name, got, c.want)
		}
	}
}

// The command line follows the encoder picked for the picture: no GPU set-up or upload
// for one handed to the CPU.
func TestArgsUseTheEncoderForThePicture(t *testing.T) {
	d := hardware(VAAPI, 90e6, 560e6)
	film := Request{Path: "/m/a.mkv", BitrateK: 16000, SrcWidth: 3840, SrcHeight: 1604, SrcFPS: 23.976, Web: true}
	film.MaxHeight = 1440
	large := strings.Join(d.Args(film), " ")
	for _, want := range []string{"-c:v libx264", "scale=-2:1070,format=yuv420p"} {
		if !strings.Contains(large, want) {
			t.Errorf("1440p of a 4K film on a small GPU: want %q in: %s", want, large)
		}
	}
	for _, not := range []string{"vaapi", "hwupload"} {
		if strings.Contains(large, not) {
			t.Errorf("1440p of a 4K film on a small GPU: %q should not be in: %s", not, large)
		}
	}
	film.MaxHeight, film.BitrateK = 1080, 8000
	small := strings.Join(d.Args(film), " ")
	for _, want := range []string{"-init_hw_device vaapi=va:/dev/dri/renderD128", "-c:v h264_vaapi", "scale=-2:802,format=nv12,hwupload"} {
		if !strings.Contains(small, want) {
			t.Errorf("1080p of a 4K film: want %q in: %s", want, small)
		}
	}
}

// A new choice of encoder forgets the speeds measured for the old one.
func TestNewChoiceForgetsTheSpeeds(t *testing.T) {
	d := hardware(VAAPI, 90e6, 560e6)
	if _, err := d.SetChoice(string(Software)); err != nil {
		t.Fatal(err)
	}
	d.mu.Lock()
	hw, sw, gen := d.hwRate, d.swRate, d.gen
	d.mu.Unlock()
	if hw != 0 || sw != 0 || gen == 0 {
		t.Errorf("after a new choice: hw %v, sw %v, gen %d; want 0, 0 and a new generation", hw, sw, gen)
	}
	// A measurement of the encoder since replaced is dropped; the current one's is kept.
	d.measured(gen-1, VAAPI, 90e6, 560e6)
	d.mu.Lock()
	hw = d.hwRate
	d.mu.Unlock()
	if hw != 0 {
		t.Errorf("a measurement of a replaced choice was kept: %v", hw)
	}
	d.measured(gen, Software, 90e6, 560e6)
	d.mu.Lock()
	hw, sw = d.hwRate, d.swRate
	d.mu.Unlock()
	if hw != 90e6 || sw != 560e6 {
		t.Errorf("the current choice's measurement was not kept: %v, %v", hw, sw)
	}
	// A second reading: the higher of each counts (the lower one was a busy machine).
	d.measured(gen, Software, 40e6, 700e6)
	d.mu.Lock()
	hw, sw = d.hwRate, d.swRate
	d.mu.Unlock()
	if hw != 90e6 || sw != 700e6 {
		t.Errorf("after a second reading: %v, %v; want the higher of each (90e6, 700e6)", hw, sw)
	}
}
