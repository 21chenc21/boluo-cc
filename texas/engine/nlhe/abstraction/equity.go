package abstraction

import (
	"math/rand"

	"github.com/boluo/texas/engine/nlhe"
)

// MCEquity — estimate equity (win + 0.5*tie probability) of (c1, c2) hole cards
// against a random opponent hand on a random board, via Monte Carlo.
//
// Returns equity in [0, 1]. MC error ~ 1/sqrt(samples).
func MCEquity(c1, c2 nlhe.Card, samples int, seed int64) float64 {
	rng := rand.New(rand.NewSource(seed))

	// Deck minus my hole cards (50 cards remaining).
	deck := make([]nlhe.Card, 0, nlhe.DeckSize-2)
	for c := nlhe.Card(0); c < nlhe.DeckSize; c++ {
		if c != c1 && c != c2 {
			deck = append(deck, c)
		}
	}

	var wins, ties float64
	for trial := 0; trial < samples; trial++ {
		// Partial Fisher-Yates: shuffle first 7 positions only (2 opp + 5 board).
		for i := 0; i < 7; i++ {
			j := i + rng.Intn(len(deck)-i)
			deck[i], deck[j] = deck[j], deck[i]
		}
		var my [7]nlhe.Card
		my[0] = c1
		my[1] = c2
		var op [7]nlhe.Card
		op[0] = deck[0]
		op[1] = deck[1]
		for k := 0; k < 5; k++ {
			my[2+k] = deck[2+k]
			op[2+k] = deck[2+k]
		}
		myR := nlhe.Evaluate7(my)
		opR := nlhe.Evaluate7(op)
		switch {
		case myR > opR:
			wins++
		case myR == opR:
			ties++
		}
	}
	return (wins + 0.5*ties) / float64(samples)
}

// CanonicalRepresentative — for a given hand type index, return ONE representative
// (c1, c2) hole pair. Used to compute equity per type (since suit ordering doesn't
// matter for preflop equity, just rank+suitedness).
func CanonicalRepresentative(handTypeIdx int) (nlhe.Card, nlhe.Card) {
	if handTypeIdx < 13 {
		// Pair: pick two suits.
		rank := uint8(12 - handTypeIdx) // AA=0→rank 12, ..., 22=12→rank 0
		return nlhe.MakeCard(rank, 0), nlhe.MakeCard(rank, 1)
	}
	suited := handTypeIdx < 13+78
	off := handTypeIdx - 13
	if !suited {
		off -= 78
	}
	for high := uint8(nlhe.NumRanks - 1); high >= 1; high-- {
		if off < int(high) {
			low := uint8(int(high) - 1 - off)
			if suited {
				return nlhe.MakeCard(high, 0), nlhe.MakeCard(low, 0)
			}
			return nlhe.MakeCard(high, 0), nlhe.MakeCard(low, 1)
		}
		off -= int(high)
	}
	panic("bad handTypeIdx")
}
