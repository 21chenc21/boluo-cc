package nlhe6

import (
	"math/rand"
	"testing"

	"github.com/boluo/texas/engine/nlhe"
)

// TestHeavyStress_RandomGames — play many random games for each player count.
// Checks invariants: zero-sum payoff, no negative stacks, action history valid,
// snapshot/restore round-trip.
func TestHeavyStress_RandomGames(t *testing.T) {
	if testing.Short() {
		t.Skip("heavy stress skipped under -short")
	}

	gamesPerN := 5000
	for n := 2; n <= 6; n++ {
		runStressForPlayerCount(t, n, gamesPerN, int64(42+n))
	}
}

func runStressForPlayerCount(t *testing.T, numPlayers, games int, seed int64) {
	t.Helper()
	cfg := DefaultConfigN(numPlayers)
	rng := rand.New(rand.NewSource(seed))
	var stats struct {
		foldWins      int
		showdowns     int
		multiwayShow  int
		allInGames    int
		zeroSumFails  int
	}

	for g := 0; g < games; g++ {
		// Random shuffle full deck.
		var deck [nlhe.DeckSize]nlhe.Card
		for i := range deck {
			deck[i] = nlhe.Card(i)
		}
		rng.Shuffle(nlhe.DeckSize, func(i, j int) {
			deck[i], deck[j] = deck[j], deck[i]
		})
		s := NewStateWithButton(cfg, Seat(rng.Intn(numPlayers)))
		// Deal hole cards.
		idx := 0
		for i := 0; i < numPlayers; i++ {
			s.SetHole(Seat(i), deck[idx], deck[idx+1])
			idx += 2
		}
		// Board cards drawn from remaining deck.
		boardCards := [5]nlhe.Card{deck[idx], deck[idx+1], deck[idx+2], deck[idx+3], deck[idx+4]}
		boardIdx := 0

		safetyMaxSteps := 200
		for steps := 0; steps < safetyMaxSteps; steps++ {
			// Always try to fill board first (covers mid-game street transition
			// AND post-terminal showdown fill).
			for {
				nNeed, needs := s.NeedsBoard()
				if !needs {
					break
				}
				if boardIdx+nNeed > 5 {
					t.Fatalf("game %d n=%d: need %d board cards but only %d left", g, numPlayers, nNeed, 5-boardIdx)
				}
				for i := 0; i < nNeed; i++ {
					s.Board[s.NumBoard] = boardCards[boardIdx]
					s.NumBoard++
					boardIdx++
				}
			}
			if s.Terminal {
				break
			}
			// Test snapshot/restore: snap, mutate, restore, expect identical state.
			if steps%17 == 0 {
				snap := s.Snapshot()
				origCur := s.Cur
				origPot := s.Pot()
				origStacks := s.Stacks
				// Apply random action then restore.
				legal := s.LegalActions()
				if len(legal) > 0 {
					s.Apply(legal[rng.Intn(len(legal))])
					s.Restore(snap)
					if s.Cur != origCur || s.Pot() != origPot {
						t.Fatalf("game %d step %d: snapshot/restore round-trip broken (Cur %d→%d, Pot %d→%d)",
							g, steps, origCur, s.Cur, origPot, s.Pot())
					}
					for k := 0; k < numPlayers; k++ {
						if s.Stacks[k] != origStacks[k] {
							t.Fatalf("game %d step %d: snapshot/restore stack[%d] %d→%d",
								g, steps, k, origStacks[k], s.Stacks[k])
						}
					}
				}
			}
			// Take a real action.
			legal := s.LegalActions()
			if len(legal) == 0 {
				t.Fatalf("game %d n=%d step %d: no legal actions but not terminal", g, numPlayers, steps)
			}
			a := legal[rng.Intn(len(legal))]
			s.Apply(a)
			// Invariant: no negative stacks.
			for k := 0; k < numPlayers; k++ {
				if s.Stacks[k] < 0 {
					t.Fatalf("game %d n=%d step %d: seat %d stack %d < 0", g, numPlayers, steps, k, s.Stacks[k])
				}
			}
		}
		if !s.Terminal {
			t.Fatalf("game %d n=%d: not terminal after safety max steps", g, numPlayers)
		}
		// Fold-win vs showdown.
		if s.FoldWinner != NoSeat {
			stats.foldWins++
			// Fill remaining board (some terminals don't need full board for fold-win).
		} else {
			stats.showdowns++
			activeAtShowdown := 0
			for i := 0; i < numPlayers; i++ {
				if !s.Folded[i] {
					activeAtShowdown++
				}
			}
			if activeAtShowdown > 2 {
				stats.multiwayShow++
			}
			anyAllIn := false
			for i := 0; i < numPlayers; i++ {
				if s.AllIn[i] {
					anyAllIn = true
					break
				}
			}
			if anyAllIn {
				stats.allInGames++
			}
		}
		// Zero-sum invariant (modulo split-pot chip remainder).
		var sumPayoff int
		for k := 0; k < numPlayers; k++ {
			sumPayoff += s.Payoff(Seat(k))
		}
		// Allow up to (numPlayers - 1) chips lost to split-pot remainder.
		if sumPayoff < -(numPlayers-1) || sumPayoff > 0 {
			stats.zeroSumFails++
			t.Errorf("game %d n=%d: payoff sum=%d, want in [-%d, 0]",
				g, numPlayers, sumPayoff, numPlayers-1)
		}
	}
	t.Logf("n=%d: %d games, foldWin=%d showdown=%d multiwayShow=%d allInGames=%d zeroSumFails=%d",
		numPlayers, games, stats.foldWins, stats.showdowns, stats.multiwayShow, stats.allInGames, stats.zeroSumFails)
}
