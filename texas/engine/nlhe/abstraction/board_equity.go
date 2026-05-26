package abstraction

import (
	"math/rand"

	"github.com/boluo/texas/engine/nlhe"
)

// MCEquityBoard — generic Monte Carlo equity computation for (hole, board)
// where board has 0..5 cards. Returns P(win) + 0.5*P(tie) over remaining
// chance (opp hand + remaining board cards).
//
// Replaces MCEquityFlop/Turn/River with one signature. Existing specialized
// versions are kept as wrappers.
//
//	board 长度 → 含义                     → 采样 (opp 2 + 剩余 board)
//	0 (preflop) → opp + 5 board           → 7 cards
//	3 (flop)    → opp + 2 board           → 4 cards
//	4 (turn)    → opp + 1 board           → 3 cards
//	5 (river)   → opp                     → 2 cards
func MCEquityBoard(hole [2]nlhe.Card, board []nlhe.Card, samples int, seed int64) float64 {
	rng := rand.New(rand.NewSource(seed))

	// Build deck minus hole + board.
	usedMask := func(c nlhe.Card) bool {
		if c == hole[0] || c == hole[1] {
			return true
		}
		for _, b := range board {
			if c == b {
				return true
			}
		}
		return false
	}
	deck := make([]nlhe.Card, 0, nlhe.DeckSize-2-len(board))
	for c := nlhe.Card(0); c < nlhe.DeckSize; c++ {
		if !usedMask(c) {
			deck = append(deck, c)
		}
	}

	missingBoard := 5 - len(board)
	cardsToSample := 2 + missingBoard

	// Reusable 7-card holders.
	var myCards, opCards [7]nlhe.Card
	myCards[0] = hole[0]
	myCards[1] = hole[1]
	// Copy fixed board cards.
	for i, c := range board {
		myCards[2+i] = c
		opCards[2+i] = c
	}

	var wins, ties float64
	for trial := 0; trial < samples; trial++ {
		// Partial Fisher-Yates on first cardsToSample positions.
		for i := 0; i < cardsToSample; i++ {
			j := i + rng.Intn(len(deck)-i)
			deck[i], deck[j] = deck[j], deck[i]
		}
		// First 2 are opp's hole.
		opCards[0] = deck[0]
		opCards[1] = deck[1]
		// Next missingBoard are remaining board cards.
		for i := 0; i < missingBoard; i++ {
			myCards[2+len(board)+i] = deck[2+i]
			opCards[2+len(board)+i] = deck[2+i]
		}
		myR := nlhe.Evaluate7(myCards)
		opR := nlhe.Evaluate7(opCards)
		switch {
		case myR > opR:
			wins++
		case myR == opR:
			ties++
		}
	}
	return (wins + 0.5*ties) / float64(samples)
}
