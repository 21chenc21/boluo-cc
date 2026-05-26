package abstraction

import (
	"testing"

	"github.com/boluo/texas/engine/nlhe"
)

// TestMCCFRWithMultiStreetBuckets — end-to-end Phase 2c smoke.
//
// Runs MCCFR using MultiStreetBuckets.ID as the infoset key, on a small-stack
// full-game config. Verifies:
//  1. The integration compiles and runs without panic.
//  2. Abstract infoset count is much smaller than what lossless InfosetID
//     would produce on the same number of iterations (proving abstraction
//     is actually compressing).
//  3. Visits hit all four streets.
func TestMCCFRWithMultiStreetBuckets(t *testing.T) {
	b := tinyMultiStreetBuckets(t)

	cfg := &nlhe.GameConfig{
		SmallBlind:   1,
		BigBlind:     2,
		StartStack:   40, // 20 BB
		BetSizes:     []float64{1.0},
		PushFoldOnly: false,
	}

	// Lossless baseline.
	losslessM := nlhe.NewMCCFR(cfg, 42)
	for i := 0; i < 200; i++ {
		losslessM.Iter()
	}
	losslessN := losslessM.NumInfosets()

	// Abstract.
	abstractM := nlhe.NewMCCFR(cfg, 42).WithIDFn(func(s *nlhe.State) uint64 {
		return b.ID(s)
	})
	var visits [4]int
	abstractM.WithIDFn(func(s *nlhe.State) uint64 {
		visits[s.Street]++
		return b.ID(s)
	})
	for i := 0; i < 200; i++ {
		abstractM.Iter()
	}
	abstractN := abstractM.NumInfosets()

	t.Logf("lossless infosets: %d, abstract infosets: %d, compression: %.1fx",
		losslessN, abstractN, float64(losslessN)/float64(abstractN))
	t.Logf("abstract street visits: preflop=%d flop=%d turn=%d river=%d",
		visits[0], visits[1], visits[2], visits[3])

	if abstractN == 0 {
		t.Fatalf("abstract MCCFR touched 0 infosets")
	}
	if abstractN >= losslessN {
		t.Errorf("no compression: abstract=%d >= lossless=%d", abstractN, losslessN)
	}
	for st, v := range visits {
		if v == 0 {
			t.Errorf("street %d never visited", st)
		}
	}

	// Average strategy sanity: per-infoset probs sum to ~1.
	avg := abstractM.AverageStrategy()
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
