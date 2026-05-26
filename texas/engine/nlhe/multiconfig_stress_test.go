package nlhe

import (
	"math/rand"
	"testing"
)

// TestMultiConfigStress — heavy random play across diverse GameConfigs.
// Catches bugs that EQUAL-stack default config never exercises (under-call all-in,
// auto-allin via CheckCall, deep stack chip math, etc.)
//
// Configs:
//   - DefaultConfig (200 chips each, BB=2)
//   - Shallow stack (5 BB each)
//   - Deep stack (1000 BB each)
//   - Uneven via stack override (after NewState, set Stacks[P1] = small)
//   - Larger blinds (SB=5, BB=10)
//   - Fewer bet sizes
//
// Slow (~3 sec). Skipped under -short.
func TestMultiConfigStress(t *testing.T) {
	if testing.Short() {
		t.Skip("multi-config stress skipped in -short mode")
	}

	type configSpec struct {
		name        string
		cfg         *GameConfig
		unevenP1    int // if > 0, override Stacks[P1] to this value
		trialsLimit int // override trials count
	}
	specs := []configSpec{
		{"default200bb", DefaultConfig(), 0, 50000},
		{"shallow5bb", &GameConfig{SmallBlind: 1, BigBlind: 2, StartStack: 10, BetSizes: []float64{0.5, 1, 2}}, 0, 50000},
		{"shallow10bb", &GameConfig{SmallBlind: 1, BigBlind: 2, StartStack: 20, BetSizes: []float64{0.5, 1, 2}}, 0, 50000},
		{"deep1000bb", &GameConfig{SmallBlind: 1, BigBlind: 2, StartStack: 2000, BetSizes: []float64{0.5, 1, 2}}, 0, 20000},
		{"unevenP1=3", DefaultConfig(), 3, 20000},
		{"unevenP1=10", DefaultConfig(), 10, 20000},
		{"largeBlind", &GameConfig{SmallBlind: 5, BigBlind: 10, StartStack: 200, BetSizes: []float64{1, 2}}, 0, 20000},
		{"manyBetSizes", &GameConfig{SmallBlind: 1, BigBlind: 2, StartStack: 200, BetSizes: []float64{0.33, 0.66, 1.0, 1.5, 2.5}}, 0, 20000},
	}

	totalGames := 0
	for _, spec := range specs {
		rng := rand.New(rand.NewSource(int64(len(spec.name))))
		violations := 0
		showdowns := 0
		folds := 0
		unevenWinDist := [3]int{0, 0, 0} // P0 +, tie, P1 +
		for trial := 0; trial < spec.trialsLimit; trial++ {
			s := NewState(spec.cfg)
			if spec.unevenP1 > 0 {
				s.Stacks[P1] = spec.unevenP1
			}
			// Deal random cards.
			var used [DeckSize]bool
			var picked [4]Card
			for i := 0; i < 4; i++ {
				for {
					c := Card(rng.Intn(DeckSize))
					if !used[c] {
						picked[i] = c
						used[c] = true
						break
					}
				}
			}
			s.SetHole(P0, picked[0], picked[1])
			s.SetHole(P1, picked[2], picked[3])

			steps := 0
			for {
				if n, needs := s.NeedsBoard(); needs {
					dealt := 0
					for c := Card(0); c < DeckSize && dealt < n; c++ {
						if !used[c] {
							s.Board[s.NumBoard] = c
							s.NumBoard++
							used[c] = true
							dealt++
						}
					}
					continue
				}
				if s.Terminal {
					break
				}
				la := s.LegalActions()
				if len(la) == 0 {
					t.Fatalf("%s trial %d: no legal, not terminal: %s", spec.name, trial, s.summary())
				}
				a := la[rng.Intn(len(la))]
				s.Apply(a)
				steps++
				if steps > 200 {
					t.Fatalf("%s trial %d: too many steps", spec.name, trial)
				}
			}
			totalGames++

			// Invariants.
			startTotal := spec.cfg.StartStack * 2
			if spec.unevenP1 > 0 {
				startTotal = spec.cfg.StartStack + spec.unevenP1 + spec.cfg.SmallBlind
				// Hmm — when we override Stacks[P1] post-NewState, the BB blind was
				// already deducted from the original StartStack. So true starting
				// chips = override + 2 (the deducted blind).
				startTotal = spec.cfg.StartStack + spec.unevenP1 + spec.cfg.BigBlind
				_ = startTotal
			}
			// Conservation: stacks + pot = chips entering hand (P0 start + P1 effective start).
			p1Entry := spec.cfg.StartStack
			if spec.unevenP1 > 0 {
				p1Entry = spec.unevenP1 + spec.cfg.BigBlind // re-added the blind we pre-deducted in NewState
			}
			expectedTotal := spec.cfg.StartStack + p1Entry
			total := s.Stacks[P0] + s.Stacks[P1] + s.Pot()
			if total != expectedTotal {
				violations++
				if violations <= 3 {
					t.Errorf("%s trial %d: conservation %d != %d (stacks=%v pot=%d)",
						spec.name, trial, total, expectedTotal, s.Stacks, s.Pot())
				}
				if violations > 10 {
					t.Fatalf("%s: too many conservation failures, abort", spec.name)
				}
			}
			// Zero-sum payoff.
			p0p := s.Payoff(P0)
			p1p := s.Payoff(P1)
			if p0p+p1p != 0 {
				violations++
				if violations <= 3 {
					t.Errorf("%s trial %d: zero-sum %d+%d != 0", spec.name, trial, p0p, p1p)
				}
			}
			switch {
			case p0p > 0:
				unevenWinDist[0]++
			case p0p == 0:
				unevenWinDist[1]++
			default:
				unevenWinDist[2]++
			}
			// Terminal classification.
			if s.FoldedBy != NoPlayer {
				folds++
			} else {
				showdowns++
			}
			// All-in/stack consistency.
			for _, p := range [2]Player{P0, P1} {
				if s.AllIn[p] && s.Stacks[p] != 0 {
					t.Errorf("%s trial %d: AllIn[%d]=true but Stacks[%d]=%d",
						spec.name, trial, p, p, s.Stacks[p])
				}
				if s.Stacks[p] < 0 {
					t.Errorf("%s trial %d: negative stack[%d]=%d", spec.name, trial, p, s.Stacks[p])
				}
			}
		}
		t.Logf("%s: %d games, %d violations, %d folds, %d showdowns | wins: P0=%d ties=%d P1=%d",
			spec.name, spec.trialsLimit, violations, folds, showdowns,
			unevenWinDist[0], unevenWinDist[1], unevenWinDist[2])
	}
	t.Logf("TOTAL: %d games across %d configs", totalGames, len(specs))
}
