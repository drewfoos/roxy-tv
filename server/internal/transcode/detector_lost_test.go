package transcode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// lostWorld is a detector over a fake ffmpeg whose VAAPI encoder works until the file
// "gone" exists (a GPU that left while the server ran). Every test encode is counted.
type lostWorld struct {
	d     *Detector
	gone  string
	tests string
}

func newLostWorld(t *testing.T) *lostWorld {
	t.Helper()
	base := t.TempDir()
	w := &lostWorld{gone: filepath.Join(base, "gone"), tests: filepath.Join(base, "tests")}
	fake := filepath.Join(base, "ffmpeg")
	script := "#!/bin/sh\ncase \"$*\" in *-filters*) exit 0;; esac\n" +
		"case \"$*\" in *h264_vaapi*) echo x >> '" + w.tests + "'; if [ -f '" + w.gone + "' ]; then echo 'No VA display found for device' >&2; exit 1; fi;; esac\nexit 0\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	w.d = NewDetector(fake, "vaapi", "")
	w.d.measures = false
	return w
}

func (w *lostWorld) tested() int {
	b, _ := os.ReadFile(w.tests)
	return strings.Count(string(b), "x")
}

func (w *lostWorld) leave(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(w.gone, nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

func shorten(t *testing.T, v *time.Duration, to time.Duration) {
	t.Helper()
	old := *v
	*v = to
	t.Cleanup(func() { *v = old })
}

// A GPU can stop working while the server runs (seen with an RX 550 on Linux: the kernel
// failed to power the card down in the middle of an encode, and the device was gone until
// a reset). The
// test result from the start was kept for good, so every conversion failed at once.
func TestAHardwareEncoderThatStopsWorkingIsLeft(t *testing.T) {
	w := newLostWorld(t)
	if e := w.d.Encoder(); e != VAAPI {
		t.Fatalf("at start: %s, want vaapi", e)
	}
	w.leave(t)
	if e := w.d.Encoder(); e != VAAPI {
		t.Fatalf("nothing has failed yet: %s", e)
	}
	if e := w.d.Failed(VAAPI); e != Software {
		t.Fatalf("after a failed conversion on a GPU that is gone: %s, want software", e)
	}
	if e := w.d.Encoder(); e != Software {
		t.Errorf("Encoder() = %s, want software", e)
	}
	if e := w.d.EncoderFor(Request{SrcWidth: 1920, SrcHeight: 1080, MaxHeight: 720}); e != Software {
		t.Errorf("EncoderFor() = %s, want software", e)
	}
	if w.d.Available()[VAAPI] {
		t.Error("the settings page would still offer the encoder that is gone")
	}
	if args := strings.Join(w.d.Args(Request{Path: "/m/a.mkv", MaxHeight: 720, BitrateK: 2000}), " "); strings.Contains(args, "vaapi") {
		t.Errorf("a new conversion still asks for the GPU: %s", args)
	}
}

// One file the encoder cannot take is not the encoder: its test passes, it stays, and
// the test is not run again for every such failure.
func TestAFailedConversionDoesNotCondemnAWorkingEncoder(t *testing.T) {
	w := newLostWorld(t)
	w.d.Encoder()
	before := w.tested()
	for i := 0; i < 5; i++ {
		if e := w.d.Failed(VAAPI); e != VAAPI {
			t.Fatalf("failure %d: %s, want vaapi kept", i, e)
		}
	}
	if ran := w.tested() - before; ran != 1 {
		t.Errorf("the encoder was tested %d times for five failures in a row, want 1", ran)
	}
	if e := w.d.Failed(Software); e != VAAPI {
		t.Errorf("a failed software conversion changed the choice to %s", e)
	}
}

// The encoder that left is tried again from time to time, and used again when it is back.
func TestALostEncoderIsTriedAgainAndReturns(t *testing.T) {
	shorten(t, &lostRetry, 200*time.Millisecond)
	w := newLostWorld(t)
	w.d.Encoder()
	w.leave(t)
	if e := w.d.Failed(VAAPI); e != Software {
		t.Fatalf("%s, want software", e)
	}
	// Still gone: asked again and again, it is tested once per interval, not once per
	// ask (nor once per ask that finds no test running: a test found it still gone, and
	// the wait starts over). Twenty asks over about half an interval: one test, or two on
	// a machine so slow that the asks ran into the next interval.
	before := w.tested()
	time.Sleep(250 * time.Millisecond)
	for i := 0; i < 20; i++ {
		if e := w.d.Encoder(); e != Software {
			t.Fatalf("still gone, yet %s", e)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if ran := w.tested() - before; ran < 1 || ran > 2 {
		t.Errorf("%d tests for twenty asks within one interval, want 1", ran)
	}
	// Back.
	if err := os.Remove(w.gone); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for w.d.Encoder() != VAAPI {
		if time.Now().After(deadline) {
			t.Fatal("the encoder came back and was never used again")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// And is not doubted any more: no further tests.
	settled := w.tested()
	time.Sleep(300 * time.Millisecond)
	w.d.Encoder()
	time.Sleep(50 * time.Millisecond)
	if w.tested() != settled {
		t.Error("an encoder that is back is still being tested")
	}
}
