package cfr

import (
	"math"
	"testing"

	"github.com/boluo/texas/engine/leduc"
)

// TestMCCFRGameValueConverges — MCCFR avg strategy → game value ≈ Nash within ~50k iter.
func TestMCCFRGameValueConverges(t *testing.T) {
	m := NewMCCFR(42)
	for i := 0; i < 50000; i++ {
		m.Iter()
	}
	if m.NumInfosets() != 288 {
		t.Errorf("MCCFR infosets=%d want 288", m.NumInfosets())
	}
	avg := m.AverageStrategy()
	gv0 := GameValue(avg, leduc.P0)
	gv1 := GameValue(avg, leduc.P1)
	expl := Exploitability(avg)
	t.Logf("MCCFR 50k iter: gv(P0)=%.6f gv(P1)=%.6f expl=%.6f", gv0, gv1, expl)

	if math.Abs(gv0+gv1) > 1e-6 {
		t.Errorf("zero-sum violated: %v + %v != 0", gv0, gv1)
	}
	// MCCFR has higher variance — relax Nash gv check vs vanilla.
	const knownNash = -0.0856
	if math.Abs(gv0-knownNash) > 0.03 {
		t.Errorf("MCCFR gv(P0)=%.6f want %v ± 0.03 (Nash, looser due to sampling)", gv0, knownNash)
	}
}

// TestMCCFRExploitabilityDecreases — expl monotonically (mostly) decreases.
func TestMCCFRExploitabilityDecreases(t *testing.T) {
	m := NewMCCFR(42)
	checkpoints := []int{5000, 20000, 80000}
	prev := math.Inf(1)
	for _, n := range checkpoints {
		for m.Iters() < n {
			m.Iter()
		}
		avg := m.AverageStrategy()
		expl := Exploitability(avg)
		t.Logf("iter %6d: expl=%.4f", n, expl)
		if expl > prev*1.1 { // allow 10% noise
			t.Errorf("expl jumped UP: iter %d expl=%.4f prev=%.4f", n, expl, prev)
		}
		prev = expl
	}
}

// TestMCCFRDeterministic — same seed → identical result.
func TestMCCFRDeterministic(t *testing.T) {
	run := func(seed int64) float64 {
		m := NewMCCFR(seed)
		for i := 0; i < 1000; i++ {
			m.Iter()
		}
		return GameValue(m.AverageStrategy(), leduc.P0)
	}
	a := run(7)
	b := run(7)
	if a != b {
		t.Errorf("non-deterministic with same seed: %v != %v", a, b)
	}
	c := run(8)
	if a == c {
		t.Errorf("different seed gave same result (suspicious): %v == %v", a, c)
	}
}

// TestMCCFRVanillaConverges — vanilla MCCFR (no RM+/no linear) eventually converges too,
// just slower. Sanity check that ablation variant isn't broken.
func TestMCCFRVanillaConverges(t *testing.T) {
	m := NewMCCFRVanilla(42)
	for i := 0; i < 100000; i++ {
		m.Iter()
	}
	avg := m.AverageStrategy()
	gv0 := GameValue(avg, leduc.P0)
	t.Logf("vanilla MCCFR 100k: gv(P0)=%.6f", gv0)
	const knownNash = -0.0856
	if math.Abs(gv0-knownNash) > 0.05 {
		t.Errorf("vanilla MCCFR gv(P0)=%.6f want %v ± 0.05", gv0, knownNash)
	}
}

// BenchmarkMCCFRIter — per-iter cost (~10 µs expected).
func BenchmarkMCCFRIter(b *testing.B) {
	m := NewMCCFR(42)
	m.Iter() // warm
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		m.Iter()
	}
}
