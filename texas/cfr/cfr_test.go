package cfr

import (
	"math"
	"testing"

	"github.com/boluo/texas/engine/leduc"
)

// ──────────────────────── regret matching ────────────────────────

func TestRegretMatchingUniformOnZero(t *testing.T) {
	r := []float64{0, 0, 0}
	out := make([]float64, 3)
	regretMatching(r, out)
	for i, v := range out {
		if math.Abs(v-1.0/3.0) > 1e-9 {
			t.Errorf("uniform[%d]=%v want 1/3", i, v)
		}
	}
}

func TestRegretMatchingClampsNegative(t *testing.T) {
	r := []float64{-1, 2, 3}
	out := make([]float64, 3)
	regretMatching(r, out)
	if out[0] != 0 {
		t.Errorf("negative not clamped: out[0]=%v", out[0])
	}
	if math.Abs(out[1]-2.0/5.0) > 1e-9 || math.Abs(out[2]-3.0/5.0) > 1e-9 {
		t.Errorf("clamped normalize wrong: out=%v want [0, 0.4, 0.6]", out)
	}
}

// ──────────────────────── CFR convergence ────────────────────────

// TestCFRConvergesToNash — quick Day-2 cross-check: 1000 vanilla CFR iters should
// reach exploitability < 0.05 + game value(P0) ≈ -0.0856 ± 0.02 (canonical Leduc Nash).
//
// Engine + CFR + BR triple cross-validation: only passes if all three are correct.
func TestCFRConvergesToNash(t *testing.T) {
	c := New()
	const iters = 1000
	for i := 0; i < iters; i++ {
		c.Iter()
	}
	t.Logf("CFR done %d iters, infosets touched: %d", iters, c.NumInfosets())
	if c.NumInfosets() != 288 {
		t.Errorf("infosets=%d want 288 (full Leduc coverage)", c.NumInfosets())
	}

	avg := c.AverageStrategy()
	gv0 := GameValue(avg, leduc.P0)
	gv1 := GameValue(avg, leduc.P1)
	expl := Exploitability(avg)
	t.Logf("game value: P0=%.6f P1=%.6f (sum=%.6f, should ≈ 0)", gv0, gv1, gv0+gv1)
	t.Logf("exploitability: %.6f", expl)

	if math.Abs(gv0+gv1) > 1e-6 {
		t.Errorf("avg-strategy game values not zero-sum: P0+P1 = %v", gv0+gv1)
	}
	if expl > 0.05 {
		t.Errorf("exploitability=%.4f after %d iters, want < 0.05", expl, iters)
	}
	const knownNash = -0.0856
	const tol = 0.02
	if math.Abs(gv0-knownNash) > tol {
		t.Errorf("P0 game value %.4f, want %.4f ± %v (Leduc canonical Nash value)",
			gv0, knownNash, tol)
	}
}

// TestCFRWeek1Gate — Week 1 gate: vanilla CFR converges enough on Leduc.
// Targets: 5000 iter → expl < 0.02 + gv(P0) within 0.005 of canonical Nash -0.0856.
// (Tighter exploitability (< 0.001) requires CFR+ or MCCFR with linear averaging,
//  TODO Day 3+. For Day 2 cross-check, expl < 0.02 + gv within 0.005 of Nash is
//  sufficient evidence that engine + CFR + BR are correct.)
//
// Slow (~90s). Skipped in -short mode.
func TestCFRWeek1Gate(t *testing.T) {
	if testing.Short() {
		t.Skip("Week 1 gate skipped in -short mode (slow ~90s)")
	}
	c := New()
	const iters = 5000
	for i := 0; i < iters; i++ {
		c.Iter()
	}
	avg := c.AverageStrategy()
	gv0 := GameValue(avg, leduc.P0)
	expl := Exploitability(avg)
	t.Logf("Week 1 gate: %d iters, expl=%.6f, gv(P0)=%+.6f (Nash≈-0.0856)", iters, expl, gv0)
	if expl > 0.02 {
		t.Errorf("Week 1 gate FAIL: expl=%.6f after %d iters, want < 0.02", expl, iters)
	}
	const knownNash = -0.0856
	if gv0 < knownNash-0.005 || gv0 > knownNash+0.005 {
		t.Errorf("Week 1 gate FAIL: gv(P0)=%.6f want %v ± 0.005 (Nash)", gv0, knownNash)
	}
}

// TestCFRExploitabilityDecreases — exploitability should monotonically (roughly) decrease.
// Sanity check that CFR is actually working, not just oscillating.
func TestCFRExploitabilityDecreases(t *testing.T) {
	c := New()
	checkpoints := []int{100, 500, 2000}
	prev := math.Inf(1)
	for _, n := range checkpoints {
		for c.Iters() < n {
			c.Iter()
		}
		avg := c.AverageStrategy()
		expl := Exploitability(avg)
		t.Logf("iter %5d: expl=%.6f", n, expl)
		if expl > prev {
			t.Errorf("expl increased: iter %d expl=%.4f prev=%.4f", n, expl, prev)
		}
		prev = expl
	}
}

