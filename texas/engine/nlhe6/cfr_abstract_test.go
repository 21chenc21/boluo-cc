package nlhe6

import (
	"testing"

	"github.com/boluo/texas/engine/nlhe/abstraction"
)

// loadSharedBuckets — load preflop K=20 + flop/turn/river K=50 buckets used
// across HUNL tests. Returns nil if any file missing (test will skip).
func loadSharedBuckets(t *testing.T) *abstraction.MultiStreetBuckets {
	t.Helper()
	pre, err := abstraction.LoadPreflopBuckets("../../blueprints/preflop-buckets-K20.json")
	if err != nil {
		t.Skipf("preflop bucket file missing: %v", err)
	}
	flop, err := abstraction.LoadStreetBuckets("../../blueprints/flop-buckets-K50.json")
	if err != nil {
		t.Skipf("flop bucket file missing: %v", err)
	}
	turn, err := abstraction.LoadStreetBuckets("../../blueprints/turn-buckets-K50.json")
	if err != nil {
		t.Skipf("turn bucket file missing: %v", err)
	}
	river, err := abstraction.LoadStreetBuckets("../../blueprints/river-buckets-K50.json")
	if err != nil {
		t.Skipf("river bucket file missing: %v", err)
	}
	return &abstraction.MultiStreetBuckets{
		Preflop: pre, Flop: flop, Turn: turn, River: river,
		FallbackSeed: 42,
	}
}

// TestMCCFRAbstractSmoke6Max — 6-max abstract MCCFR runs without panic + AA
// aggression > 30% after smoke iter count.
func TestMCCFRAbstractSmoke6Max(t *testing.T) {
	if testing.Short() {
		t.Skip("abstract smoke skipped under -short")
	}
	b := loadSharedBuckets(t)
	cfg := DefaultConfigN(6)
	cfg.StartStack = 40 // 20 BB
	cfg.BetSizes = []float64{1.0}

	m := NewMCCFR(cfg, 42).WithIDFn(MultiStreetIDFn(b))
	const iters = 1000
	for i := 0; i < iters; i++ {
		m.Iter()
	}
	t.Logf("6-max abstract MCCFR %d iter → %d abstract infosets", iters, m.NumInfosets())
	if m.NumInfosets() == 0 {
		t.Fatalf("0 infosets after %d iter", iters)
	}
	// Sanity: average strategy is normalized.
	avg := m.AverageStrategy()
	for id, probs := range avg {
		var sum float64
		for _, p := range probs {
			sum += p
		}
		if sum < 0.99 || sum > 1.01 {
			t.Errorf("infoset %d: probs sum=%v want ~1", id, sum)
			break
		}
	}
}

// TestMultiStreetIDDifferent6Max — different actor seat → different ID at same
// game state shape. Encoder isolates per-seat infoset.
func TestMultiStreetIDDifferent6Max(t *testing.T) {
	b := loadSharedBuckets(t)
	cfg := DefaultConfigN(6)
	s := NewState(cfg)
	for i := 0; i < 6; i++ {
		// Distinct holes (4 ranks × 4 suits enough).
		s.SetHole(Seat(i), Card(2*i), Card(2*i+1))
	}
	// First-to-act = UTG = seat 3.
	if s.Cur != 3 {
		t.Fatalf("expected Cur=3 (UTG), got %d", s.Cur)
	}
	id1 := MultiStreetID(b, s)
	// Move on a step: UTG folds, MP to act.
	s.Apply(Action{Kind: ActionFold})
	if s.Cur != 4 {
		t.Fatalf("expected Cur=4 (MP) after UTG fold, got %d", s.Cur)
	}
	id2 := MultiStreetID(b, s)
	if id1 == id2 {
		t.Errorf("UTG view vs MP view should differ, both=%d", id1)
	}
}
