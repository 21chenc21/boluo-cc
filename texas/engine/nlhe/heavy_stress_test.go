package nlhe

import (
	"math/rand"
	"testing"
)

// TestHeavyRandomStress — order-of-magnitude heavier than TestRandomPlayNoPanic.
//
// 5 RNG seeds × 100,000 random games each = 500,000 game plays.
// At every terminal:
//   - Conservation: Stacks[P0] + Stacks[P1] + Pot == 2 × StartStack
//   - Zero-sum: Payoff(P0) + Payoff(P1) == 0
//   - Showdown reachable: NumBoard == 5 if no fold
//   - All-in detection: if AllIn[X], Stacks[X] == 0
//
// Aggregates statistics so we can spot pathological distributions (e.g. if
// FoldedBy = P0 in 99% of games something is wrong).
//
// Slow (~30 sec). Skipped under -short. Run with -v for stats.
func TestHeavyRandomStress(t *testing.T) {
	if testing.Short() {
		t.Skip("heavy stress skipped in -short mode")
	}

	const trialsPerSeed = 100000
	seeds := []int64{1, 7, 42, 100, 999}

	totalGames := 0
	foldP0 := 0
	foldP1 := 0
	showdown := 0
	bothAllIn := 0
	preflopFold := 0
	flopReached := 0
	turnReached := 0
	riverReached := 0
	violationFail := 0

	for _, seed := range seeds {
		rng := rand.New(rand.NewSource(seed))
		for trial := 0; trial < trialsPerSeed; trial++ {
			s := NewState(DefaultConfig())

			// Deal 4 distinct cards.
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

			// Random play. Loop until both Terminal AND NeedsBoard=false:
			// all-in showdown sets Terminal=true but caller still must deal board.
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
					t.Fatalf("seed %d trial %d: no legal actions, not terminal", seed, trial)
				}
				a := la[rng.Intn(len(la))]
				s.Apply(a)
				steps++
				if steps > 200 {
					t.Fatalf("seed %d trial %d: too many steps", seed, trial)
				}
			}
			totalGames++

			// Classify terminal.
			switch {
			case s.FoldedBy == P0:
				foldP0++
				if s.Street == StreetPreflop {
					preflopFold++
				}
			case s.FoldedBy == P1:
				foldP1++
				if s.Street == StreetPreflop {
					preflopFold++
				}
			default:
				showdown++
				if s.AllIn[P0] && s.AllIn[P1] {
					bothAllIn++
				}
				if s.NumBoard != 5 {
					t.Fatalf("seed %d trial %d: showdown but NumBoard=%d", seed, trial, s.NumBoard)
				}
			}
			if s.Street >= StreetFlop {
				flopReached++
			}
			if s.Street >= StreetTurn {
				turnReached++
			}
			if s.Street >= StreetRiver {
				riverReached++
			}

			// Conservation invariant.
			total := s.Stacks[P0] + s.Stacks[P1] + s.Pot()
			want := 2 * s.Cfg.StartStack
			if total != want {
				violationFail++
				t.Errorf("seed %d trial %d: conservation %d != %d (stacks=%v pot=%d)",
					seed, trial, total, want, s.Stacks, s.Pot())
				if violationFail > 5 {
					t.Fatal("too many conservation failures, abort")
				}
			}

			// Zero-sum payoff (skip showdown without full board, but we verified NumBoard==5 above).
			p0 := s.Payoff(P0)
			p1 := s.Payoff(P1)
			if p0+p1 != 0 {
				violationFail++
				t.Errorf("seed %d trial %d: zero-sum %d+%d != 0", seed, trial, p0, p1)
				if violationFail > 5 {
					t.Fatal("too many zero-sum failures, abort")
				}
			}

			// All-in detection consistency.
			for _, p := range [2]Player{P0, P1} {
				if s.AllIn[p] && s.Stacks[p] != 0 {
					t.Errorf("seed %d trial %d: AllIn[%d] but stack=%d", seed, trial, p, s.Stacks[p])
				}
			}
		}
	}

	t.Logf("===== HEAVY STRESS RESULTS =====")
	t.Logf("total games: %d (%d seeds × %d trials)", totalGames, len(seeds), trialsPerSeed)
	t.Logf("violations:  %d", violationFail)
	t.Logf("")
	t.Logf("terminal breakdown:")
	t.Logf("  P0 folded:    %d (%.1f%%)", foldP0, float64(foldP0)/float64(totalGames)*100)
	t.Logf("  P1 folded:    %d (%.1f%%)", foldP1, float64(foldP1)/float64(totalGames)*100)
	t.Logf("  showdown:     %d (%.1f%%)", showdown, float64(showdown)/float64(totalGames)*100)
	t.Logf("  both all-in:  %d (%.1f%%)", bothAllIn, float64(bothAllIn)/float64(totalGames)*100)
	t.Logf("  preflop fold: %d (%.1f%%)", preflopFold, float64(preflopFold)/float64(totalGames)*100)
	t.Logf("")
	t.Logf("street reached:")
	t.Logf("  flop:  %d (%.1f%%)", flopReached, float64(flopReached)/float64(totalGames)*100)
	t.Logf("  turn:  %d (%.1f%%)", turnReached, float64(turnReached)/float64(totalGames)*100)
	t.Logf("  river: %d (%.1f%%)", riverReached, float64(riverReached)/float64(totalGames)*100)
}
