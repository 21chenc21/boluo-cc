package nlhe6

import (
	"testing"

	"github.com/boluo/texas/engine/nlhe"
)

// TestMCCFRRunsNoPanic — basic run sanity.
func TestMCCFRRunsNoPanic(t *testing.T) {
	cfg := DefaultConfigN(3)
	cfg.StartStack = 20 // 10 BB shallow for fast smoke
	m := NewMCCFR(cfg, 42)
	for i := 0; i < 200; i++ {
		m.Iter()
	}
	if m.NumInfosets() == 0 {
		t.Fatalf("MCCFR touched 0 infosets in 200 iter")
	}
	if m.Iters() != 200 {
		t.Errorf("Iters=%d want 200", m.Iters())
	}
	avg := m.AverageStrategy()
	if len(avg) == 0 {
		t.Fatalf("AverageStrategy empty")
	}
	for id, probs := range avg {
		var sum float64
		for _, p := range probs {
			sum += p
		}
		if sum < 0.99 || sum > 1.01 {
			t.Errorf("infoset %d: probs sum=%v want ~1", id, sum)
		}
	}
}

// TestMCCFRMultiStreetVisitsAllStreets — 6-max games reach flop/turn/river
// via chance-node dealing.
func TestMCCFRMultiStreetVisitsAllStreets(t *testing.T) {
	cfg := DefaultConfigN(3)
	cfg.StartStack = 40 // 20 BB
	cfg.BetSizes = []float64{1.0}
	m := NewMCCFR(cfg, 42)
	var visits [4]int
	m.WithIDFn(func(s *State) uint64 {
		visits[s.Street]++
		return s.InfosetID()
	})
	for i := 0; i < 500; i++ {
		m.Iter()
	}
	t.Logf("street visits: preflop=%d flop=%d turn=%d river=%d",
		visits[0], visits[1], visits[2], visits[3])
	if visits[0] == 0 || visits[1] == 0 || visits[2] == 0 || visits[3] == 0 {
		t.Errorf("not all streets visited")
	}
}

// TestMCCFRStrongHandPrefersAggression — 6-max push/fold-ish smoke. AA in
// UTG seat 应大概率 raise/allin (not fold).
func TestMCCFRStrongHandPrefersAggression(t *testing.T) {
	if testing.Short() {
		t.Skip("convergence test skipped under -short")
	}
	cfg := DefaultConfigN(3)
	cfg.StartStack = 20 // 10 BB
	cfg.BetSizes = []float64{1.0}
	m := NewMCCFR(cfg, 42)
	const iters = 10000
	for i := 0; i < iters; i++ {
		m.Iter()
	}
	avg := m.AverageStrategy()
	// AA UTG (seat 0=BTN with N=3 → first to act preflop = (0+3)%3 = 0 = BTN/UTG).
	s := NewState(cfg)
	s.SetHole(Seat(0), nlhe.ParseCard("Ac"), nlhe.ParseCard("Ad"))
	s.SetHole(Seat(1), nlhe.ParseCard("2c"), nlhe.ParseCard("3d"))
	s.SetHole(Seat(2), nlhe.ParseCard("7h"), nlhe.ParseCard("8d"))
	id := s.InfosetID()
	probs, ok := avg[id]
	if !ok {
		t.Fatalf("AA infoset not visited")
	}
	legal := s.LegalActions()
	// Aggression total = bet + allin probability.
	var aggression float64
	for i, a := range legal {
		if a.Kind == ActionBet || a.Kind == ActionAllIn {
			aggression += probs[i]
		}
	}
	t.Logf("3-handed AA UTG aggression after %d iter: %.3f", iters, aggression)
	if aggression < 0.3 {
		t.Errorf("AA aggression=%v after %d iter, want > 0.3", aggression, iters)
	}
}
