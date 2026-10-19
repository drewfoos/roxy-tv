package segments

import (
	"math"
	"math/rand"
	"testing"
)

// Two "episodes" of random audio sharing one 90 s block at different offsets: the
// matcher must find the block, at the right place in each, and nothing when there is
// no shared block.
func TestLongestCommon(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	random := func(n int) []uint32 {
		p := make([]uint32, n)
		for i := range p {
			p[i] = rng.Uint32()
		}
		return p
	}
	pts := func(sec float64) int { return int(sec / SampleSec) }
	block := random(pts(90)) // 90 s OP
	a := append(append(random(pts(110)), block...), random(pts(400))...)
	b := append(append(random(pts(60)), block...), random(pts(450))...)
	// Sprinkle a few bit flips into b's copy (encoder differences) within the Hamming budget.
	off := pts(60)
	for i := 0; i < len(block); i += 7 {
		b[off+i] ^= 0x5
	}

	m, ok := longestCommon(a, b, MaxDiff, MaxGapSec, MinIntroSec)
	if !ok {
		t.Fatal("no match found")
	}
	within := func(got, want float64) bool { return math.Abs(got-want) <= 2*SampleSec }
	if !within(m.aStart, 110) || !within(m.aEnd, 200) {
		t.Errorf("a range = %.2f-%.2f, want 110-200", m.aStart, m.aEnd)
	}
	if !within(m.bStart, 60) || !within(m.bEnd, 150) {
		t.Errorf("b range = %.2f-%.2f, want 60-150", m.bStart, m.bEnd)
	}

	if _, ok := longestCommon(random(4000), random(4000), MaxDiff, MaxGapSec, MinIntroSec); ok {
		t.Error("unrelated audio matched")
	}
}

func TestSnapForward(t *testing.T) {
	sil := []Silence{{Start: 1321.25, End: 1321.58}}
	if got := snapForward(1316.0, sil, 2.5, 6.0); got != 1321.25 {
		t.Errorf("snapForward = %.2f, want the cut at 1321.25", got)
	}
	if got := snapForward(1300.0, sil, 2.5, 6.0); got != 1300.0 {
		t.Errorf("snapForward moved a start with no cut nearby: %.2f", got)
	}
}
