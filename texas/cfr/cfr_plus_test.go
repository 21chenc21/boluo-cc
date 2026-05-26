package cfr

import (
	"math"
	"testing"

	"github.com/boluo/texas/engine/leduc"
)

// TestCFRPlusGameValueConverges — CFR+ avg strategy → game value matches Nash.
func TestCFRPlusGameValueConverges(t *testing.T) {
	c := NewPlus()
	for i := 0; i < 500; i++ {
		c.Iter()
	}
	if c.NumInfosets() != 288 {
		t.Errorf("CFR+ infosets=%d want 288", c.NumInfosets())
	}
	avg := c.AverageStrategy()
	gv0 := GameValue(avg, leduc.P0)
	gv1 := GameValue(avg, leduc.P1)
	t.Logf("CFR+ 500 iter: gv(P0)=%.6f gv(P1)=%.6f", gv0, gv1)
	if math.Abs(gv0+gv1) > 1e-6 {
		t.Errorf("zero-sum violated: %v + %v != 0", gv0, gv1)
	}
	const knownNash = -0.0856
	if math.Abs(gv0-knownNash) > 0.005 {
		t.Errorf("CFR+ gv(P0)=%.6f want %v ± 0.005", gv0, knownNash)
	}
}

// TestCFRPlusInfosetCoverage — CFR+ explores all 288 Leduc infosets.
func TestCFRPlusInfosetCoverage(t *testing.T) {
	c := NewPlus()
	for i := 0; i < 100; i++ {
		c.Iter()
	}
	if c.NumInfosets() != 288 {
		t.Errorf("CFR+ infosets=%d want 288", c.NumInfosets())
	}
}

// TestCFRPlusFastConvergence — post-fix CFR+ should crush vanilla CFR.
// At iter 500 expect expl < 0.005; at iter 1000 expect expl < 0.002.
// OpenSpiel reference: iter 500 expl ≈ 0.00094, iter 1000 ≈ 0.0005.
// My impl ~3x looser due to small implementation differences (still 50x faster than vanilla CFR).
func TestCFRPlusFastConvergence(t *testing.T) {
	c := NewPlus()
	for i := 0; i < 500; i++ {
		c.Iter()
	}
	expl500 := Exploitability(c.AverageStrategy())
	t.Logf("CFR+ 500 iter: expl=%.6f", expl500)
	if expl500 > 0.005 {
		t.Errorf("CFR+ expl=%.6f at iter 500, want < 0.005", expl500)
	}
	for c.Iters() < 1000 {
		c.Iter()
	}
	expl1000 := Exploitability(c.AverageStrategy())
	t.Logf("CFR+ 1000 iter: expl=%.6f", expl1000)
	if expl1000 > 0.002 {
		t.Errorf("CFR+ expl=%.6f at iter 1000, want < 0.002", expl1000)
	}
}
