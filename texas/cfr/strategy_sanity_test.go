package cfr

import (
	"testing"

	"github.com/boluo/texas/engine/leduc"
)

// TestStrategySanityCheck — after CFR converges, learned strategy should match
// Leduc poker structure:
//   - Free-fold prob ≈ 0 at opening (strictly dominated)
//   - J (weakest) bets less than K (strongest) at opening
//   - K bets ≥ 50%
//   - J bets < 50% (weak hand mostly checks; bluff frequency is bounded)
//
// We deliberately DON'T enforce strict monotone J < Q < K — Leduc Nash mixes,
// and Q vs K can be near-equal due to slow-play incentive on K.
func TestStrategySanityCheck(t *testing.T) {
	c := New()
	for i := 0; i < 1000; i++ {
		c.Iter()
	}
	avg := c.AverageStrategy()

	// Opening strategy by private rank. Synthesize the InfosetID for each
	// opening (priv=X, no pub, empty history).
	openings := []struct {
		label string
		priv  leduc.Card
	}{
		{"J", leduc.MakeCard(0, 0)},
		{"Q", leduc.MakeCard(1, 0)},
		{"K", leduc.MakeCard(2, 0)},
	}
	bets := make([]float64, 3)
	for i, o := range openings {
		// Construct opening state to fetch the canonical ID.
		s := leduc.NewState(o.priv, leduc.MakeCard((o.priv.Rank()+1)%leduc.NumRanks, 0))
		id := s.InfosetID()
		probs, ok := avg[id]
		if !ok {
			t.Fatalf("missing opening infoset for %s (id=%d)", o.label, id)
		}
		if len(probs) != 3 {
			t.Fatalf("opening %s has %d probs, want 3", o.label, len(probs))
		}
		t.Logf("opening %s: Fold=%.3f Check=%.3f Bet=%.3f", o.label, probs[0], probs[1], probs[2])
		if probs[0] > 0.05 {
			t.Errorf("opening %s: fold prob %.3f > 0.05 (free-fold dominated)", o.label, probs[0])
		}
		bets[i] = probs[2]
	}
	if bets[0] >= bets[2] {
		t.Errorf("J should bet less than K: J=%.3f K=%.3f", bets[0], bets[2])
	}
	if bets[2] < 0.50 {
		t.Errorf("K opening BetRaise=%.3f, want ≥ 0.50 (K is best hand)", bets[2])
	}
	if bets[0] > 0.50 {
		t.Errorf("J opening BetRaise=%.3f, want < 0.50 (J is weakest, bluff bounded)", bets[0])
	}
}

