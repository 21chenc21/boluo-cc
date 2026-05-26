package abstraction

import (
	"math"
	"testing"

	"github.com/boluo/texas/engine/nlhe"
)

// TestMCEquityKnownHands — equity of AA vs random ≈ 0.85, of 72o ≈ 0.34
// (well-known published numbers within MC noise).
func TestMCEquityKnownHands(t *testing.T) {
	cases := []struct {
		c1, c2 string
		want   float64
		tol    float64
	}{
		// Published vs-random equity numbers (approximate):
		{"As", "Ah", 0.853, 0.01}, // AA ≈ 85.3%
		{"Ks", "Kh", 0.823, 0.01}, // KK ≈ 82.4%
		{"Qs", "Qh", 0.799, 0.01}, // QQ ≈ 79.9%
		{"As", "Ks", 0.671, 0.01}, // AKs ≈ 67.0%
		{"As", "Kh", 0.651, 0.01}, // AKo ≈ 65.3%
		{"7c", "2d", 0.349, 0.01}, // 72o ≈ 34.6% (the canonical worst)
		{"3c", "2c", 0.354, 0.01}, // 32s ≈ 35.4%
	}
	const samples = 100000
	for _, c := range cases {
		c1 := nlhe.ParseCard(c.c1)
		c2 := nlhe.ParseCard(c.c2)
		got := MCEquity(c1, c2, samples, 42)
		if math.Abs(got-c.want) > c.tol {
			t.Errorf("MCEquity(%s,%s) = %.4f, want %.4f ± %.4f",
				c.c1, c.c2, got, c.want, c.tol)
		} else {
			t.Logf("MCEquity(%s,%s) = %.4f (ref %.4f)", c.c1, c.c2, got, c.want)
		}
	}
}

// TestCanonicalRepresentativeRoundTrip — for each hand type idx, its representative
// re-encodes to the same idx.
func TestCanonicalRepresentativeRoundTrip(t *testing.T) {
	for idx := 0; idx < NumPreflopHandTypes; idx++ {
		c1, c2 := CanonicalRepresentative(idx)
		idx2 := HandTypeIdx(c1, c2)
		if idx2 != idx {
			t.Errorf("CanonicalRepresentative(%d=%s)=(%v,%v) but HandTypeIdx says %d (%s)",
				idx, HandTypeLabel(idx), c1, c2, idx2, HandTypeLabel(idx2))
		}
	}
}

// TestMCEquityRanking — premium hands should have higher equity than trash.
func TestMCEquityRanking(t *testing.T) {
	const samples = 20000
	pairs := []struct {
		good, bad           string
		gc1, gc2, bc1, bc2 string
	}{
		{"AA", "72o", "As", "Ah", "7c", "2d"},
		{"KK", "32o", "Ks", "Kh", "3c", "2d"},
		{"AKs", "T9o", "As", "Ks", "Tc", "9d"},
	}
	for _, p := range pairs {
		good := MCEquity(nlhe.ParseCard(p.gc1), nlhe.ParseCard(p.gc2), samples, 7)
		bad := MCEquity(nlhe.ParseCard(p.bc1), nlhe.ParseCard(p.bc2), samples, 7)
		if good <= bad {
			t.Errorf("%s eq=%.4f should be > %s eq=%.4f", p.good, good, p.bad, bad)
		}
	}
}
